package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrRunCancelled     = errors.New("run cancelled by user")
	ErrRunNotFound      = errors.New("run not found")
	ErrRunNotActive     = errors.New("run is no longer active")
	errRunOwnershipLost = errors.New("run ownership was lost")
)

type activeExecution struct {
	cancel context.CancelCauseFunc
	done   chan struct{}
}

type ActiveRun struct {
	ID     uuid.UUID `json:"id"`
	Status string    `json:"status"`
}

func (s *Service) ActiveRun(ctx context.Context, workspaceID, conversationID uuid.UUID) (*ActiveRun, error) {
	var run ActiveRun
	err := s.db.QueryRow(ctx, `SELECT r.id,r.status FROM runs r
		JOIN conversations c ON c.id=r.conversation_id
		WHERE c.workspace_id=$1 AND c.id=$2 AND r.status IN ('running','cancelling')
		ORDER BY r.created_at DESC LIMIT 1`, workspaceID, conversationID).Scan(&run.ID, &run.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &run, err
}

func (s *Service) CancelRun(ctx context.Context, workspaceID, conversationID, runID uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2)`, workspaceID, conversationID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrRunNotFound
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM conversations WHERE id=$1 FOR UPDATE`, conversationID); err != nil {
		return err
	}
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, runID, conversationID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRunNotFound
		}
		return err
	}
	switch status {
	case "running":
		if _, err = tx.Exec(ctx, `UPDATE runs SET status='cancelling' WHERE id=$1`, runID); err != nil {
			return err
		}
	case "cancelling", "cancelled":
		// Idempotent retries are safe.
	default:
		return ErrRunNotActive
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}

	if value, ok := s.activeRuns.Load(runID); ok {
		execution := value.(*activeExecution)
		execution.cancel(ErrRunCancelled)
		select {
		case <-execution.done:
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
			// The cancellation is durable and the worker will still observe it.
		}
	}
	return nil
}

func (s *Service) finishCancelledRun(ctx context.Context, runID, conversationID uuid.UUID) (bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM conversations WHERE id=$1 FOR UPDATE`, conversationID); err != nil {
		return false, err
	}
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, runID, conversationID).Scan(&status); err != nil {
		return false, err
	}
	if status == "cancelled" {
		return false, nil
	}
	if status != "running" && status != "cancelling" {
		return false, nil
	}
	rows, err := tx.Query(ctx, `SELECT call->>'id',call->>'name'
		FROM messages m CROSS JOIN LATERAL jsonb_array_elements(m.tool_calls) WITH ORDINALITY AS t(call,n)
		WHERE m.run_id=$1 AND m.conversation_id=$2 AND m.role='assistant'
		AND NOT EXISTS(SELECT 1 FROM messages r WHERE r.run_id=m.run_id AND r.role='tool' AND r.tool_call_id=call->>'id')
		ORDER BY m.seq,t.n`, runID, conversationID)
	if err != nil {
		return false, err
	}
	var missing []model.ToolCall
	for rows.Next() {
		var call model.ToolCall
		if err = rows.Scan(&call.ID, &call.Name); err != nil {
			rows.Close()
			return false, err
		}
		missing = append(missing, call)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	for _, call := range missing {
		result, _ := json.Marshal(map[string]any{"error": "Tool execution was cancelled before a result was recorded. Side effects may have occurred; inspect current state before retrying.", "cancelled": true})
		if err = insertMessage(ctx, tx, conversationID, runID, model.Message{Role: "tool", Content: string(result), ToolCallID: call.ID}, call.Name, map[string]any{"is_error": true, "cancelled": true}); err != nil {
			return false, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE runs SET status='cancelled',completed_at=now() WHERE id=$1`, runID); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("finish cancelled run: %w", err)
	}
	s.event(ctx, runID, conversationID, "RUN_CANCELLED", map[string]any{"reason": "user_requested"})
	return true, nil
}

func (s *Service) finishContext(ctx context.Context, runID, conversationID uuid.UUID) {
	cause := context.Cause(ctx)
	if cause == nil {
		cause = ctx.Err()
	}
	if errors.Is(cause, ErrRunCancelled) {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := s.finishCancelledRun(cleanupCtx, runID, conversationID); err != nil {
			slog.Error("persist cancelled run", "run_id", runID, "error", err)
		}
		return
	}
	if cause == nil {
		cause = errRunOwnershipLost
	}
	s.finishExecutionError(ctx, runID, conversationID, cause)
}

// finishExecutionError resolves the durable run state before classifying an
// execution error. This closes the small cross-replica race where the database
// has already recorded "cancelling" but the local watchdog has not fired yet.
func (s *Service) finishExecutionError(ctx context.Context, runID, conversationID uuid.UUID, runErr error) {
	if errors.Is(context.Cause(ctx), ErrRunCancelled) {
		s.finishContext(ctx, runID, conversationID)
		return
	}
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var status string
	if err := s.db.QueryRow(checkCtx, `SELECT status FROM runs WHERE id=$1 AND conversation_id=$2`, runID, conversationID).Scan(&status); err == nil && (status == "cancelling" || status == "cancelled") {
		if _, err = s.finishCancelledRun(checkCtx, runID, conversationID); err != nil {
			slog.Error("persist cancelled run", "run_id", runID, "error", err)
		}
		return
	}
	s.fail(ctx, runID, conversationID, runErr)
}
