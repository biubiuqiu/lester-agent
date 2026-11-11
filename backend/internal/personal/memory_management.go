package personal

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type MemoryCollection struct {
	UserID      uuid.UUID `json:"user_id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Memories    []Memory  `json:"memories"`
	AutoMemory  bool      `json:"auto_memory"`
}

// MemoryCollection is independent of personal turns, task discovery and model
// configuration. Reading the manager never creates a thread or starts a model.
func (s *Service) MemoryCollection(ctx context.Context, p auth.Principal) (MemoryCollection, error) {
	result := MemoryCollection{UserID: p.UserID, WorkspaceID: p.WorkspaceID, Memories: []Memory{}, AutoMemory: true}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if err = authorize(ctx, tx, p); err != nil {
		return result, err
	}
	err = tx.QueryRow(ctx, `SELECT auto_memory FROM personal_threads WHERE workspace_id=$1 AND user_id=$2`, p.WorkspaceID, p.UserID).Scan(&result.AutoMemory)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if result.Memories, err = readMemories(ctx, tx, p); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

// SetMemoryPaused changes availability without changing the fact's authored
// content or provenance. It is revision-checked and never retried internally.
func (s *Service) SetMemoryPaused(ctx context.Context, p auth.Principal, id uuid.UUID, revision int64, paused bool) (MemoryReceipt, error) {
	if id == uuid.Nil || revision < 1 {
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
	} else if err != nil {
		return MemoryReceipt{}, err
	}
	if m.Revision != revision || m.Deleted {
		return MemoryReceipt{}, ErrConflict
	}
	if err = checkMemoryProject(ctx, tx, p, m.ProjectID); err != nil {
		return MemoryReceipt{}, err
	}
	before := m
	m.Paused = paused
	m, err = writeMemory(ctx, tx, p, m)
	if err != nil {
		return MemoryReceipt{}, err
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

type MemoryHistoryChange struct {
	ID        uuid.UUID  `json:"id"`
	MemoryID  uuid.UUID  `json:"memory_id"`
	Revision  int64      `json:"revision"`
	Before    *Memory    `json:"before"`
	After     Memory     `json:"after"`
	Undone    bool       `json:"undone"`
	TurnID    *uuid.UUID `json:"turn_id"`
	CreatedAt time.Time  `json:"created_at"`
	UndoOf    *uuid.UUID `json:"undo_of,omitempty"`
}

type MemoryHistory struct {
	Memory  Memory                `json:"memory"`
	Changes []MemoryHistoryChange `json:"changes"`
	HasMore bool                  `json:"has_more"`
}

// MemoryHistory returns only the owned record and its saved changes, including
// tombstones. Revisions missing from older undo implementations stay missing.
func (s *Service) MemoryHistory(ctx context.Context, p auth.Principal, id uuid.UUID, beforeRevision int64) (MemoryHistory, error) {
	result := MemoryHistory{Changes: []MemoryHistoryChange{}}
	if id == uuid.Nil || beforeRevision < 0 {
		return result, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if err = authorize(ctx, tx, p); err != nil {
		return result, err
	}
	result.Memory, err = scanMemory(tx.QueryRow(ctx, `SELECT `+memoryColumns+` FROM personal_memories WHERE workspace_id=$1 AND user_id=$2 AND id=$3`, p.WorkspaceID, p.UserID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrNotFound
	} else if err != nil {
		return result, err
	}
	if err = checkMemoryProject(ctx, tx, p, result.Memory.ProjectID); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT id,memory_id,revision,before_value,after_value,undone,turn_id,created_at,undo_of FROM personal_memory_changes WHERE workspace_id=$1 AND user_id=$2 AND memory_id=$3 AND ($4::bigint=0 OR revision<$4) ORDER BY revision DESC,id DESC LIMIT 31`, p.WorkspaceID, p.UserID, id, beforeRevision)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var change MemoryHistoryChange
		var prior, next []byte
		if err = rows.Scan(&change.ID, &change.MemoryID, &change.Revision, &prior, &next, &change.Undone, &change.TurnID, &change.CreatedAt, &change.UndoOf); err != nil {
			rows.Close()
			return result, err
		}
		if err = json.Unmarshal(next, &change.After); err != nil {
			rows.Close()
			return result, err
		}
		if len(prior) > 0 {
			change.Before = new(Memory)
			if err = json.Unmarshal(prior, change.Before); err != nil {
				rows.Close()
				return result, err
			}
		}
		result.Changes = append(result.Changes, change)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	result.HasMore = len(result.Changes) > 30
	if result.HasMore {
		result.Changes = result.Changes[:30]
	}
	return result, tx.Commit(ctx)
}
