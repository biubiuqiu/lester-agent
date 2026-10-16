package automation

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Run polls persisted intent, not browser state. PostgreSQL row/session guards
// coordinate replicas; elapsed time is never used as proof of abandoned work.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	var cursor int64
	for {
		if err := s.enqueueDue(ctx, time.Now()); err != nil && ctx.Err() == nil {
			slog.Error("schedule scan failed")
		}
		rows, err := s.DB.Query(ctx, `SELECT id,accepted_order FROM automation_deliveries WHERE state IN ('pending','dispatching','unknown') AND accepted_order>$1 ORDER BY accepted_order LIMIT 30`, cursor)
		if err == nil {
			type item struct {
				id    uuid.UUID
				order int64
			}
			items := []item{}
			for rows.Next() {
				var row item
				if err = rows.Scan(&row.id, &row.order); err != nil {
					break
				}
				items = append(items, row)
			}
			if err == nil {
				err = rows.Err()
			}
			rows.Close()
			if len(items) == 30 {
				last := items[len(items)-1]
				cursor = last.order
			} else {
				cursor = 0
			}
			if err == nil {
				for _, row := range items {
					if ctx.Err() != nil {
						return
					}
					if err = s.dispatch(ctx, row.id); err != nil && ctx.Err() == nil {
						slog.Error("automation dispatch needs reconciliation", "delivery_id", row.id)
					}
				}
			}
		} else if ctx.Err() == nil {
			slog.Error("automation queue scan failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func busy(ctx context.Context, tx pgx.Tx, automationID uuid.UUID) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM automation_deliveries d LEFT JOIN runs r ON r.id=d.run_id WHERE d.automation_id=$1 AND (d.state='dispatching' OR (d.state='accepted' AND (r.status IN ('running','cancelling') OR EXISTS(SELECT 1 FROM command_tasks c WHERE c.workspace_id=d.workspace_id AND c.conversation_id=d.conversation_id AND c.status IN ('starting','running','stopping','unknown'))))))`, automationID).Scan(&exists)
	return exists, err
}
func (s *Service) enqueueDue(ctx context.Context, now time.Time) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT `+columns+` FROM automations WHERE kind='schedule' AND enabled AND deleted_at IS NULL AND next_run_at<=$1 ORDER BY next_run_at,id LIMIT 20 FOR UPDATE SKIP LOCKED`, now)
	if err != nil {
		return err
	}
	items := []Definition{}
	for rows.Next() {
		d, scanErr := scanDefinition(rows)
		if scanErr != nil {
			rows.Close()
			return scanErr
		}
		items = append(items, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, d := range items {
		occupied, err := busy(ctx, tx, d.ID)
		if err != nil {
			return err
		}
		var pending bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM automation_deliveries WHERE automation_id=$1 AND state='pending')`, d.ID).Scan(&pending); err != nil {
			return err
		}
		state, code := "pending", ""
		if occupied || pending {
			state, code = "skipped", "busy"
		}
		scheduled := d.NextRunAt.UTC()
		input := newInput(d)
		if err = insertDelivery(ctx, tx, d, input, "schedule:"+scheduled.Format(time.RFC3339Nano), "schedule", "schedule", state, code, &scheduled); err != nil {
			return err
		}
		var next *time.Time
		if d.Cron != "" {
			next, err = nextOccurrence(d.Config, now)
			if err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE automations SET next_run_at=$2,revision=revision+1,updated_at=now() WHERE id=$1 AND workspace_id=$3`, d.ID, next, d.WorkspaceID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Service) dispatch(ctx context.Context, id uuid.UUID) error {
	// A separate connection keeps ownership across the short claim commit. If a
	// replica disappears after dispatching was persisted, the next owner only
	// looks up the original submission receipt. It never calls Start again.
	conn, err := pgx.ConnectConfig(ctx, s.DB.Config().ConnConfig.Copy())
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = conn.Close(cleanup)
	}()
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, "lester:automation:"+id.String()).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var raw json.RawMessage
	var state, source string
	var definitionID, workspace uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT automation_id,workspace_id FROM automation_deliveries WHERE id=$1`, id).Scan(&definitionID, &workspace); errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	// All writers lock the definition before its deliveries, including pause.
	var enabled bool
	var deleted *time.Time
	if err = tx.QueryRow(ctx, `SELECT enabled,deleted_at FROM automations WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspace, definitionID).Scan(&enabled, &deleted); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT snapshot,state,source FROM automation_deliveries WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspace, id).Scan(&raw, &state, &source); err != nil {
		return err
	}
	if state != "pending" && state != "dispatching" && state != "unknown" {
		return nil
	}
	var input Input
	if err = json.Unmarshal(raw, &input); err != nil {
		return err
	}
	if input.ID != id || input.WorkspaceID != workspace {
		return errors.New("invalid stored trigger identity")
	}
	if state == "dispatching" || state == "unknown" {
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		return s.reconcile(ctx, input)
	}
	if deleted != nil || !enabled && source != "manual" {
		if _, err = tx.Exec(ctx, `UPDATE automation_deliveries SET state='cancelled',error_code='paused',updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspace, id); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	occupied, err := busy(ctx, tx, definitionID)
	if err != nil || occupied {
		return err
	}
	var authorized bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN workspace_members wm ON wm.user_id=u.id WHERE u.id=$1 AND wm.workspace_id=$2 AND NOT u.disabled AND (NOT u.email_verification_required OR u.email_verified))`, input.UserID, workspace).Scan(&authorized); err != nil {
		return err
	}
	if !authorized {
		if _, err = tx.Exec(ctx, `UPDATE automation_deliveries SET state='failed',error_code='authorization_changed',updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspace, id); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	// Preserve acceptance order while an earlier occurrence is being dispatched.
	var earlier bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM automation_deliveries a JOIN automation_deliveries b ON b.id=$2 WHERE a.automation_id=$1 AND a.state='pending' AND a.accepted_order<b.accepted_order)`, definitionID, id).Scan(&earlier); err != nil || earlier {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE automation_deliveries SET state='dispatching',updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspace, id); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if err = conn.Ping(ctx); err != nil {
		return err
	}
	runID, startErr := s.Runner.Start(ctx, input)
	if startErr == nil && runID != uuid.Nil {
		return s.acceptReceipt(ctx, input, runID)
	}
	// Even an error can follow a committed run. Read the receipt before deciding
	// what is known; raw provider/database errors never enter public history.
	return s.reconcile(ctx, input)
}
func (s *Service) acceptReceipt(ctx context.Context, input Input, runID uuid.UUID) error {
	_, err := s.DB.Exec(ctx, `UPDATE automation_deliveries SET state='accepted',run_id=$3,error_code='',updated_at=now() WHERE workspace_id=$1 AND id=$2 AND state IN ('dispatching','unknown')`, input.WorkspaceID, input.ID, runID)
	return err
}
func (s *Service) reconcile(ctx context.Context, input Input) error {
	runID, err := s.Runner.Lookup(ctx, input)
	if err != nil {
		return err
	}
	if runID != uuid.Nil {
		return s.acceptReceipt(ctx, input, runID)
	}
	_, err = s.DB.Exec(ctx, `UPDATE automation_deliveries SET state='unknown',error_code='receipt_unknown',updated_at=now() WHERE workspace_id=$1 AND id=$2 AND state='dispatching'`, input.WorkspaceID, input.ID)
	return err
}
