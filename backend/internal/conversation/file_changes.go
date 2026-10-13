package conversation

import (
	"bytes"
	"context"
	"errors"
	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
	"github.com/biubiuqiu/lester-agent/backend/internal/eventlog"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
	"unicode/utf8"
)

const maxConversationReviewBytes = 64 << 20

var ErrFileChangeNotFound = errors.New("file change not found")
var ErrFileRollbackConflict = errors.New("file cannot be restored from this receipt")

type FileChange struct {
	ID                uuid.UUID `json:"id"`
	RunID             uuid.UUID `json:"run_id"`
	Path              string    `json:"path"`
	Operation         string    `json:"operation"`
	Status            string    `json:"status"`
	BeforeExists      bool      `json:"before_exists"`
	BeforeSHA256      string    `json:"before_sha256"`
	AfterSHA256       string    `json:"after_sha256"`
	Retained          bool      `json:"retained"`
	UnavailableReason string    `json:"unavailable_reason,omitempty"`
	RollbackStatus    string    `json:"rollback_status"`
	CreatedAt         time.Time `json:"created_at"`
	BeforeContent     *string   `json:"before_content,omitempty"`
	AfterContent      *string   `json:"after_content,omitempty"`
}

func (s *Service) recordFileChange(ctx context.Context, runID, conversationID uuid.UUID, callID string, change *agenttool.FileChange) error {
	if change == nil {
		return errors.New("missing file change")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var workspaceID uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT workspace_id FROM conversations WHERE id=$1 FOR UPDATE`, conversationID).Scan(&workspaceID); err != nil {
		return err
	}
	if change.Status == "pending" {
		var recorded bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runs r JOIN messages m ON m.run_id=r.id AND m.conversation_id=r.conversation_id CROSS JOIN LATERAL jsonb_array_elements(m.tool_calls) call WHERE r.id=$1 AND r.conversation_id=$2 AND r.status='running' AND call->>'id'=$3 AND call->>'name'=ANY($4))`, runID, conversationID, callID, []string{"write", "edit", "computer_write_file", "computer_edit_file"}).Scan(&recorded); err != nil || !recorded {
			return errors.New("file change is not an active recorded tool call")
		}
		before, after, reason := change.BeforeContent, change.AfterContent, change.UnavailableReason
		if after != nil && (len(before) > agenttool.MaxReviewFileBytes || len(after) > agenttool.MaxReviewFileBytes || !utf8.Valid(before) || !utf8.Valid(after) || bytes.IndexByte(before, 0) >= 0 || bytes.IndexByte(after, 0) >= 0 || byteDigest(after) != change.AfterSHA256 || change.BeforeExists && byteDigest(before) != change.BeforeSHA256) {
			before = nil
			after = nil
			reason = "snapshot_unavailable"
		}
		retained := int64(len(before) + len(after))
		var used int64
		if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(retained_bytes),0) FROM file_changes WHERE conversation_id=$1`, conversationID).Scan(&used); err != nil {
			return err
		}
		if after == nil || used+retained > maxConversationReviewBytes {
			before = nil
			after = nil
			retained = 0
			if reason == "" {
				reason = "review_quota"
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO file_changes(workspace_id,conversation_id,run_id,tool_call_id,path,operation,status,before_exists,before_sha256,after_sha256,before_content,after_content,retained_bytes,unavailable_reason) VALUES($1,$2,$3,$4,$5,$6,'pending',$7,$8,$9,$10,$11,$12,$13)`, workspaceID, conversationID, runID, callID, change.Path, change.Operation, change.BeforeExists, change.BeforeSHA256, change.AfterSHA256, before, after, retained, reason)
	} else {
		if change.Status != "confirmed" && change.Status != "unknown" {
			return errors.New("invalid file change status")
		}
		tag, updateErr := tx.Exec(ctx, `UPDATE file_changes SET status=$4,after_sha256=CASE WHEN $4='confirmed' THEN $5 ELSE after_sha256 END,updated_at=now() WHERE run_id=$1 AND conversation_id=$2 AND tool_call_id=$3 AND status='pending'`, runID, conversationID, callID, change.Status, change.AfterSHA256)
		err = updateErr
		if err == nil && tag.RowsAffected() != 1 {
			return errors.New("file change receipt is not pending")
		}
		if err == nil {
			err = eventlog.Append(ctx, tx, runID, conversationID, "FILE_CHANGE_RECORDED", map[string]any{"path": change.Path, "status": change.Status})
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Service) FileChanges(ctx context.Context, workspaceID, conversationID uuid.UUID) ([]FileChange, error) {
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE id=$1 AND workspace_id=$2)`, conversationID, workspaceID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrFileChangeNotFound
	}
	rows, err := s.db.Query(ctx, `SELECT id,run_id,path,operation,status,before_exists,before_sha256,after_sha256,after_content IS NOT NULL,unavailable_reason,rollback_status,created_at FROM file_changes WHERE workspace_id=$1 AND conversation_id=$2 ORDER BY created_at DESC,id DESC LIMIT 100`, workspaceID, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []FileChange{}
	for rows.Next() {
		var item FileChange
		if err = rows.Scan(&item.ID, &item.RunID, &item.Path, &item.Operation, &item.Status, &item.BeforeExists, &item.BeforeSHA256, &item.AfterSHA256, &item.Retained, &item.UnavailableReason, &item.RollbackStatus, &item.CreatedAt); err != nil {
			return nil, err
		}
		if item.Status != "confirmed" {
			item.AfterSHA256 = ""
			item.Retained = false
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Service) FileChange(ctx context.Context, workspaceID, conversationID, id uuid.UUID) (FileChange, error) {
	item := FileChange{ID: id}
	var before, after []byte
	err := s.db.QueryRow(ctx, `SELECT run_id,path,operation,status,before_exists,before_sha256,after_sha256,after_content IS NOT NULL,unavailable_reason,rollback_status,created_at,before_content,after_content FROM file_changes WHERE workspace_id=$1 AND conversation_id=$2 AND id=$3`, workspaceID, conversationID, id).Scan(&item.RunID, &item.Path, &item.Operation, &item.Status, &item.BeforeExists, &item.BeforeSHA256, &item.AfterSHA256, &item.Retained, &item.UnavailableReason, &item.RollbackStatus, &item.CreatedAt, &before, &after)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, ErrFileChangeNotFound
	}
	if item.Status == "confirmed" && item.Retained {
		a, b := string(before), string(after)
		item.BeforeContent = &a
		item.AfterContent = &b
	}
	if item.Status != "confirmed" {
		item.AfterSHA256 = ""
		item.Retained = false
	}
	return item, err
}
