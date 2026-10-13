package conversation

import (
	"context"
	"fmt"

	"github.com/biubiuqiu/lester-agent/backend/internal/eventlog"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Service) RunInputs(ctx context.Context, workspaceID, userID, conversationID uuid.UUID) ([]RunInput, error) {
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE id=$1 AND workspace_id=$2)`, conversationID, workspaceID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrRunInputNotFound
	}
	rows, err := s.db.Query(ctx, `SELECT id,after_run_id,kind,status,left(content,2000),request_digest,accepted_order,revision,message_id,started_run_id,held_reason,created_at FROM run_inputs WHERE workspace_id=$1 AND conversation_id=$2 AND submitted_by=$3 ORDER BY accepted_order DESC LIMIT 100`, workspaceID, conversationID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []RunInput{}
	for rows.Next() {
		var item RunInput
		if err = rows.Scan(&item.ID, &item.AfterRunID, &item.Kind, &item.Status, &item.Content, &item.RequestDigest, &item.AcceptedOrder, &item.Revision, &item.MessageID, &item.StartedRunID, &item.HeldReason, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) ChangeRunInput(ctx context.Context, workspaceID, userID, conversationID, id uuid.UUID, action string) (*RunInput, error) {
	if action != "cancel" && action != "resume" {
		return nil, fmt.Errorf("%w: invalid run input action", ErrRunInputRejected)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var owned uuid.UUID
	var allowed bool
	if err = tx.QueryRow(ctx, `SELECT c.id,EXISTS(SELECT 1 FROM workspace_members wm JOIN users u ON u.id=wm.user_id WHERE wm.workspace_id=c.workspace_id AND wm.user_id=$3 AND NOT u.disabled) FROM conversations c WHERE c.id=$1 AND c.workspace_id=$2 FOR UPDATE`, conversationID, workspaceID, userID).Scan(&owned, &allowed); err != nil || !allowed {
		return nil, ErrRunInputNotFound
	}
	var after uuid.UUID
	var kind, status string
	if err = tx.QueryRow(ctx, `SELECT after_run_id FROM run_inputs WHERE id=$1 AND workspace_id=$2 AND conversation_id=$3 AND submitted_by=$4`, id, workspaceID, conversationID, userID).Scan(&after); err != nil {
		return nil, ErrRunInputNotFound
	}
	// Preserve conversation -> run -> input -> workspace-event lock order.
	if _, err = tx.Exec(ctx, `SELECT id FROM runs WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, after, conversationID); err != nil {
		return nil, err
	}
	if err = tx.QueryRow(ctx, `SELECT kind,status FROM run_inputs WHERE id=$1 FOR UPDATE`, id).Scan(&kind, &status); err != nil {
		return nil, err
	}
	if action == "cancel" && status == "cancelled" || action == "resume" && kind == "next" && status == "queued" {
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		return s.RunInput(ctx, workspaceID, userID, conversationID, id)
	}
	if status == "started" || status == "applied" || status == "cancelled" {
		return nil, ErrRunInputConflict
	}
	newStatus := "cancelled"
	if action == "resume" {
		if kind != "next" || status != "held" {
			return nil, ErrRunInputConflict
		}
		var active bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runs WHERE conversation_id=$1 AND status IN ('running','cancelling'))`, conversationID).Scan(&active); err != nil {
			return nil, err
		}
		if active {
			return nil, ErrRunInProgress
		}
		newStatus = "queued"
	}
	if _, err = tx.Exec(ctx, `UPDATE run_inputs SET status=$2,resume_authorized=CASE WHEN $3 THEN true ELSE resume_authorized END,held_reason=NULL,revision=revision+1,updated_at=now() WHERE id=$1`, id, newStatus, action == "resume"); err != nil {
		return nil, err
	}
	if err = eventlog.Append(ctx, tx, after, conversationID, "RUN_INPUT_UPDATED", map[string]any{"input_id": id, "status": newStatus, "action": action}); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	if action == "resume" {
		s.scheduleInputQueue(conversationID)
	}
	return s.RunInput(ctx, workspaceID, userID, conversationID, id)
}

func queuedInputHasPriority(ctx context.Context, tx pgx.Tx, conversationID uuid.UUID) (bool, error) {
	var ready bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM run_inputs i JOIN runs r ON r.id=i.after_run_id WHERE i.conversation_id=$1 AND i.kind='next' AND i.status='queued' AND (r.status='completed' OR i.resume_authorized))`, conversationID).Scan(&ready)
	return ready, err
}
