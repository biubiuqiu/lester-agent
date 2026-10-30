package personal

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// MemoryDetails is a complete metadata replacement. A nil ProjectID means a
// personal memory; a project-scoped memory remains private to its author.
type MemoryDetails struct {
	Topic     string     `json:"topic"`
	Kind      string     `json:"kind"`
	ProjectID *uuid.UUID `json:"project_id"`
}

func (d *MemoryDetails) UnmarshalJSON(raw []byte) error {
	// Requiring the scope key avoids accidentally widening a project fact into
	// personal context when a metadata editor submits only a title/category.
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 3 {
		return ErrInvalid
	}
	for _, key := range []string{"topic", "kind", "project_id"} {
		if len(fields[key]) == 0 {
			return ErrInvalid
		}
	}
	if fields["topic"][0] != '"' || fields["kind"][0] != '"' {
		return ErrInvalid
	}
	type plain MemoryDetails
	var value plain
	if json.Unmarshal(raw, &value) != nil {
		return ErrInvalid
	}
	*d = MemoryDetails(value)
	return nil
}

type ManualMemoryInput struct {
	Topic     string     `json:"topic"`
	Content   string     `json:"content"`
	Kind      string     `json:"kind"`
	ProjectID *uuid.UUID `json:"project_id"`
}

type MemoryReceipt struct {
	Memory   Memory    `json:"memory"`
	ChangeID uuid.UUID `json:"change_id"`
}

func memoryWriteError(err error) error {
	var conflict *pgconn.PgError
	if errors.As(err, &conflict) && conflict.Code == "23505" {
		return ErrConflict
	}
	return err
}

func checkMemoryProject(ctx context.Context, tx pgx.Tx, p auth.Principal, project *uuid.UUID) error {
	if project == nil {
		return nil
	}
	if *project == uuid.Nil {
		return ErrInvalid
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM projects WHERE workspace_id=$1 AND id=$2 FOR KEY SHARE`, p.WorkspaceID, project).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func sameMemoryInput(m Memory, input ManualMemoryInput) bool {
	return m.Topic == input.Topic && m.Content == input.Content && m.Kind == input.Kind &&
		(m.ProjectID == nil && input.ProjectID == nil || m.ProjectID != nil && input.ProjectID != nil && *m.ProjectID == *input.ProjectID)
}

// CreateMemory accepts a client-owned stable UUID and never calls a model. Its
// original creation change is also the immutable request receipt: retrying the
// same creation cannot undo a subsequent edit, forgetting or undo operation.
func (s *Service) CreateMemory(ctx context.Context, p auth.Principal, id uuid.UUID, input ManualMemoryInput) (MemoryReceipt, error) {
	input.Topic = strings.TrimSpace(input.Topic)
	input.Content = strings.TrimSpace(input.Content)
	if id == uuid.Nil || !validMemory(Memory{Topic: input.Topic, Content: input.Content, Kind: input.Kind, ProjectID: input.ProjectID}) {
		return MemoryReceipt{}, ErrInvalid
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return MemoryReceipt{}, err
	}
	defer tx.Rollback(ctx)
	if _, _, _, err = ensureThread(ctx, tx, p); err != nil {
		return MemoryReceipt{}, err
	}
	current, err := scanMemory(tx.QueryRow(ctx, `SELECT `+memoryColumns+` FROM personal_memories WHERE workspace_id=$1 AND user_id=$2 AND id=$3 FOR UPDATE`, p.WorkspaceID, p.UserID, id))
	if err == nil {
		var changeID uuid.UUID
		var raw []byte
		if err = tx.QueryRow(ctx, `SELECT id,after_value FROM personal_memory_changes WHERE workspace_id=$1 AND user_id=$2 AND memory_id=$3 AND before_value IS NULL AND turn_id IS NULL ORDER BY created_at,id LIMIT 1`, p.WorkspaceID, p.UserID, id).Scan(&changeID, &raw); errors.Is(err, pgx.ErrNoRows) {
			return MemoryReceipt{}, ErrConflict
		} else if err != nil {
			return MemoryReceipt{}, err
		}
		var original Memory
		if json.Unmarshal(raw, &original) != nil || !sameMemoryInput(original, input) {
			return MemoryReceipt{}, ErrConflict
		}
		return MemoryReceipt{Memory: current, ChangeID: changeID}, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return MemoryReceipt{}, err
	}
	if err = checkMemoryProject(ctx, tx, p, input.ProjectID); err != nil {
		return MemoryReceipt{}, err
	}
	// Tombstones reserve the topic too. A new creation must not silently revive
	// something that was forgotten, even when it uses a fresh client UUID.
	var topicExists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM personal_memories WHERE workspace_id=$1 AND user_id=$2 AND project_id IS NOT DISTINCT FROM $3::uuid AND lower(topic)=lower($4))`, p.WorkspaceID, p.UserID, input.ProjectID, input.Topic).Scan(&topicExists); err != nil {
		return MemoryReceipt{}, err
	}
	if topicExists {
		return MemoryReceipt{}, ErrConflict
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM personal_memories WHERE workspace_id=$1 AND user_id=$2 AND NOT deleted`, p.WorkspaceID, p.UserID).Scan(&count); err != nil {
		return MemoryReceipt{}, err
	}
	if count >= 128 {
		return MemoryReceipt{}, ErrMemoryFull
	}
	m, err := scanMemory(tx.QueryRow(ctx, `INSERT INTO personal_memories(id,workspace_id,user_id,project_id,kind,topic,content) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING `+memoryColumns, id, p.WorkspaceID, p.UserID, input.ProjectID, input.Kind, input.Topic, input.Content))
	if err != nil {
		return MemoryReceipt{}, memoryWriteError(err)
	}
	changeID, err := recordChange(ctx, tx, p, nil, m, nil)
	if err != nil {
		return MemoryReceipt{}, err
	}
	if err = barrier(ctx, tx, p); err != nil {
		return MemoryReceipt{}, err
	}
	return MemoryReceipt{Memory: m, ChangeID: changeID}, tx.Commit(ctx)
}

// Memory reads the current owned record, including a tombstone, so clients can
// reconcile an uncertain mutation without repeating a write. It is not a
// receipt proving that an arbitrary proposed payload was accepted.
func (s *Service) Memory(ctx context.Context, p auth.Principal, id uuid.UUID) (Memory, error) {
	if id == uuid.Nil {
		return Memory{}, ErrInvalid
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Memory{}, err
	}
	defer tx.Rollback(ctx)
	if err = authorize(ctx, tx, p); err != nil {
		return Memory{}, err
	}
	m, err := scanMemory(tx.QueryRow(ctx, `SELECT `+memoryColumns+` FROM personal_memories WHERE workspace_id=$1 AND user_id=$2 AND id=$3`, p.WorkspaceID, p.UserID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Memory{}, ErrNotFound
	}
	if err != nil {
		return Memory{}, err
	}
	return m, tx.Commit(ctx)
}

func (s *Service) UpdateMemory(ctx context.Context, p auth.Principal, id uuid.UUID, revision int64, content string, forget bool, details *MemoryDetails) (MemoryReceipt, error) {
	if id == uuid.Nil || revision < 1 || forget && details != nil || !forget && !bounded(content, 1, 600) {
		return MemoryReceipt{}, ErrInvalid
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return MemoryReceipt{}, err
	}
	defer tx.Rollback(ctx)
	if _, _, _, err = ensureThread(ctx, tx, p); err != nil {
		return MemoryReceipt{}, err
	}
	m, err := scanMemory(tx.QueryRow(ctx, `SELECT `+memoryColumns+` FROM personal_memories WHERE workspace_id=$1 AND user_id=$2 AND id=$3 FOR UPDATE`, p.WorkspaceID, p.UserID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return MemoryReceipt{}, ErrNotFound
	}
	if err != nil {
		return MemoryReceipt{}, err
	}
	if m.Revision != revision || m.Deleted {
		return MemoryReceipt{}, ErrConflict
	}
	before := m
	m.Content = strings.TrimSpace(content)
	m.Deleted = forget
	m.Automatic = false
	m.Evidence = ""
	m.SourceTurnID = nil
	if details != nil {
		m.Topic, m.Kind, m.ProjectID = strings.TrimSpace(details.Topic), details.Kind, details.ProjectID
		if !validMemory(m) {
			return MemoryReceipt{}, ErrInvalid
		}
		if err = checkMemoryProject(ctx, tx, p, m.ProjectID); err != nil {
			return MemoryReceipt{}, err
		}
	}
	if forget {
		m.Content = ""
	}
	m, err = writeMemory(ctx, tx, p, m)
	if err != nil {
		return MemoryReceipt{}, memoryWriteError(err)
	}
	changeID, err := recordChange(ctx, tx, p, &before, m, nil)
	if err != nil {
		return MemoryReceipt{}, err
	}
	if err = barrier(ctx, tx, p); err != nil {
		return MemoryReceipt{}, err
	}
	return MemoryReceipt{Memory: m, ChangeID: changeID}, tx.Commit(ctx)
}
