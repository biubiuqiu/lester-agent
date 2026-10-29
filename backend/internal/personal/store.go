package personal

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func authorize(ctx context.Context, tx pgx.Tx, p auth.Principal) error {
	var id uuid.UUID
	// Same user -> session order as account revocation. A model response never
	// survives logout, membership removal or a disabled/unverified account.
	if err := tx.QueryRow(ctx, `SELECT u.id FROM users u JOIN workspace_members wm ON wm.user_id=u.id WHERE u.id=$1 AND wm.workspace_id=$2 AND NOT u.disabled AND (NOT u.email_verification_required OR u.email_verified) FOR SHARE OF u,wm`, p.UserID, p.WorkspaceID).Scan(&id); err != nil {
		return ErrUnauthorized
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM sessions WHERE user_id=$1 AND token_hash=$2 AND expires_at>now() FOR SHARE`, p.UserID, p.SessionHash).Scan(&id); err != nil {
		return ErrUnauthorized
	}
	return nil
}

func ensureThread(ctx context.Context, tx pgx.Tx, p auth.Principal) (int64, int64, bool, error) {
	if err := authorize(ctx, tx, p); err != nil {
		return 0, 0, false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO personal_threads(workspace_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, p.WorkspaceID, p.UserID); err != nil {
		return 0, 0, false, err
	}
	var revision, after int64
	var auto bool
	err := tx.QueryRow(ctx, `SELECT revision,context_after,auto_memory FROM personal_threads WHERE workspace_id=$1 AND user_id=$2 FOR UPDATE`, p.WorkspaceID, p.UserID).Scan(&revision, &after, &auto)
	return revision, after, auto, err
}

const memoryColumns = `id,project_id,kind,topic,content,evidence,source_turn_id,automatic,deleted,revision,updated_at`

func scanMemory(row pgx.Row) (m Memory, err error) {
	err = row.Scan(&m.ID, &m.ProjectID, &m.Kind, &m.Topic, &m.Content, &m.Evidence, &m.SourceTurnID, &m.Automatic, &m.Deleted, &m.Revision, &m.UpdatedAt)
	return
}

func (s *Service) Memories(ctx context.Context, p auth.Principal) ([]Memory, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+memoryColumns+` FROM personal_memories WHERE workspace_id=$1 AND user_id=$2 AND NOT deleted ORDER BY updated_at DESC,id LIMIT 128`, p.WorkspaceID, p.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Memory{}
	for rows.Next() {
		m, e := scanMemory(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func (s *Service) History(ctx context.Context, p auth.Principal, before int64) ([]Turn, bool, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,seq,content,reply,status,created_at,memory_notice FROM personal_turns WHERE workspace_id=$1 AND user_id=$2 AND ($3::bigint=0 OR seq<$3) ORDER BY seq DESC LIMIT 31`, p.WorkspaceID, p.UserID, before)
	if err != nil {
		return nil, false, err
	}
	items := []Turn{}
	for rows.Next() {
		var v Turn
		if err = rows.Scan(&v.ID, &v.Seq, &v.Content, &v.Reply, &v.Status, &v.CreatedAt, &v.MemoryNotice); err != nil {
			rows.Close()
			return nil, false, err
		}
		items = append(items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, false, err
	}
	more := len(items) > 30
	if more {
		items = items[:30]
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	for i := range items {
		if err = s.decorate(ctx, p, &items[i]); err != nil {
			return nil, false, err
		}
	}
	return items, more, nil
}
func (s *Service) Turn(ctx context.Context, p auth.Principal, id uuid.UUID) (Turn, error) {
	var v Turn
	err := s.DB.QueryRow(ctx, `SELECT id,seq,content,reply,status,created_at,memory_notice FROM personal_turns WHERE workspace_id=$1 AND user_id=$2 AND id=$3`, p.WorkspaceID, p.UserID, id).Scan(&v.ID, &v.Seq, &v.Content, &v.Reply, &v.Status, &v.CreatedAt, &v.MemoryNotice)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = s.decorate(ctx, p, &v)
	return v, err
}
func (s *Service) decorate(ctx context.Context, p auth.Principal, v *Turn) error {
	v.Changes = []Change{}
	rows, err := s.DB.Query(ctx, `SELECT id,memory_id,after_value->>'topic',(after_value->>'deleted')::boolean,(after_value->>'automatic')::boolean,undone FROM personal_memory_changes WHERE workspace_id=$1 AND user_id=$2 AND turn_id=$3 ORDER BY created_at,id`, p.WorkspaceID, p.UserID, v.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var c Change
		if err = rows.Scan(&c.ID, &c.MemoryID, &c.Topic, &c.Deleted, &c.Automatic, &c.Undone); err != nil {
			rows.Close()
			return err
		}
		v.Changes = append(v.Changes, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	d := Delegation{}
	err = s.DB.QueryRow(ctx, `SELECT conversation_id,project_id,title,status,run_id,input_id FROM personal_delegations WHERE workspace_id=$1 AND user_id=$2 AND turn_id=$3`, p.WorkspaceID, p.UserID, v.ID).Scan(&d.ConversationID, &d.ProjectID, &d.Title, &d.Status, &d.RunID, &d.InputID)
	if err == nil {
		v.Task = &d
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return nil
}

func (s *Service) Tasks(ctx context.Context, p auth.Principal) ([]Task, error) {
	return s.searchTasks(ctx, p, nil, "")
}
func (s *Service) searchTasks(ctx context.Context, p auth.Principal, project *uuid.UUID, query string) ([]Task, error) {
	pattern := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(query) + "%"
	rows, err := s.DB.Query(ctx, `SELECT `+taskColumns+` `+taskJoins+` WHERE c.workspace_id=$1 AND c.created_by=$2 AND ($3::uuid IS NULL OR c.project_id=$3) AND (c.title ILIKE $4 OR p.name ILIKE $4) ORDER BY c.updated_at DESC,c.id DESC LIMIT 30`, p.WorkspaceID, p.UserID, project, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Task{}
	for rows.Next() {
		v, e := scanTask(rows)
		if e != nil {
			return nil, e
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func recordChange(ctx context.Context, tx pgx.Tx, p auth.Principal, before *Memory, after Memory, turn *uuid.UUID) (uuid.UUID, error) {
	var prior any
	if before != nil {
		b, _ := json.Marshal(before)
		prior = b
	}
	next, _ := json.Marshal(after)
	var id uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO personal_memory_changes(workspace_id,user_id,memory_id,turn_id,revision,before_value,after_value) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, p.WorkspaceID, p.UserID, after.ID, turn, after.Revision, prior, next).Scan(&id)
	return id, err
}

func validMemory(m Memory) bool {
	return bounded(m.Topic, 1, 80) && bounded(m.Content, 1, 600) && (m.Kind == "preference" || m.Kind == "project" || m.Kind == "work") && (m.Kind != "project" || m.ProjectID != nil)
}

// Manual edits and forgetting advance a context barrier. Old chat stays visible,
// but it cannot be silently mined to resurrect a corrected/forgotten fact.
func barrier(ctx context.Context, tx pgx.Tx, p auth.Principal) error {
	_, err := tx.Exec(ctx, `UPDATE personal_threads SET revision=revision+1,context_after=COALESCE((SELECT max(seq) FROM personal_turns WHERE workspace_id=$1 AND user_id=$2),0) WHERE workspace_id=$1 AND user_id=$2`, p.WorkspaceID, p.UserID)
	return err
}

func (s *Service) EditMemory(ctx context.Context, p auth.Principal, id uuid.UUID, revision int64, content string, forget bool) (Memory, error) {
	if !forget && !bounded(content, 1, 600) {
		return Memory{}, ErrInvalid
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Memory{}, err
	}
	defer tx.Rollback(ctx)
	if _, _, _, err = ensureThread(ctx, tx, p); err != nil {
		return Memory{}, err
	}
	m, err := scanMemory(tx.QueryRow(ctx, `SELECT `+memoryColumns+` FROM personal_memories WHERE workspace_id=$1 AND user_id=$2 AND id=$3 FOR UPDATE`, p.WorkspaceID, p.UserID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	if m.Revision != revision || m.Deleted {
		return m, ErrConflict
	}
	before := m
	m.Content = strings.TrimSpace(content)
	m.Deleted = forget
	m.Automatic = false
	m.Evidence = ""
	m.SourceTurnID = nil
	m, err = writeMemory(ctx, tx, p, m)
	if err != nil {
		return m, err
	}
	if _, err = recordChange(ctx, tx, p, &before, m, nil); err != nil {
		return m, err
	}
	if err = barrier(ctx, tx, p); err != nil {
		return m, err
	}
	return m, tx.Commit(ctx)
}

func writeMemory(ctx context.Context, tx pgx.Tx, p auth.Principal, m Memory) (Memory, error) {
	return scanMemory(tx.QueryRow(ctx, `UPDATE personal_memories SET content=$4,evidence=$5,source_turn_id=$6,automatic=$7,deleted=$8,revision=revision+1,updated_at=now() WHERE workspace_id=$1 AND user_id=$2 AND id=$3 RETURNING `+memoryColumns, p.WorkspaceID, p.UserID, m.ID, m.Content, m.Evidence, m.SourceTurnID, m.Automatic, m.Deleted))
}

func (s *Service) Undo(ctx context.Context, p auth.Principal, id uuid.UUID) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, _, _, err = ensureThread(ctx, tx, p); err != nil {
		return err
	}
	var memoryID uuid.UUID
	var rev int64
	var prior []byte
	var undone bool
	if err = tx.QueryRow(ctx, `SELECT memory_id,revision,before_value,undone FROM personal_memory_changes WHERE workspace_id=$1 AND user_id=$2 AND id=$3 FOR UPDATE`, p.WorkspaceID, p.UserID, id).Scan(&memoryID, &rev, &prior, &undone); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if undone {
		return tx.Commit(ctx)
	}
	current, err := scanMemory(tx.QueryRow(ctx, `SELECT `+memoryColumns+` FROM personal_memories WHERE workspace_id=$1 AND user_id=$2 AND id=$3 FOR UPDATE`, p.WorkspaceID, p.UserID, memoryID))
	if err != nil {
		return err
	}
	if current.Revision != rev {
		return ErrConflict
	}
	restored := current
	if len(prior) > 0 {
		if err = json.Unmarshal(prior, &restored); err != nil {
			return err
		}
	} else {
		restored.Deleted = true
		restored.Content = ""
		restored.Evidence = ""
		restored.SourceTurnID = nil
	}
	if current.Deleted && !restored.Deleted {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM personal_memories WHERE workspace_id=$1 AND user_id=$2 AND NOT deleted`, p.WorkspaceID, p.UserID).Scan(&count); err != nil {
			return err
		}
		if count >= 128 {
			return ErrMemoryFull
		}
	}
	if _, err = writeMemory(ctx, tx, p, restored); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE personal_memory_changes SET undone=true WHERE id=$1`, id); err != nil {
		return err
	}
	if err = barrier(ctx, tx, p); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) SetAutoMemory(ctx context.Context, p auth.Principal, enabled bool) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, _, _, err = ensureThread(ctx, tx, p); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE personal_threads SET auto_memory=$3 WHERE workspace_id=$1 AND user_id=$2`, p.WorkspaceID, p.UserID, enabled); err != nil {
		return err
	}
	if err = barrier(ctx, tx, p); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
