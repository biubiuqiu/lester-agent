// Package eventlog stores ordered run events and durable delivery intents.
package eventlog

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Append must be the last domain operation before commit. Lock order is
// conversation -> run -> workspace event lock, matching terminal transitions.
// The short workspace lock makes event IDs follow commit order within a stream.
func Append(ctx context.Context, tx pgx.Tx, runID, conversationID uuid.UUID, kind string, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var workspaceID, lockedRun uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT workspace_id FROM conversations WHERE id=$1 FOR KEY SHARE`, conversationID).Scan(&workspaceID); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT id FROM runs WHERE id=$1 AND conversation_id=$2 FOR KEY SHARE`, runID, conversationID).Scan(&lockedRun); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "lester:events:"+workspaceID.String()); err != nil {
		return err
	}
	var eventID int64
	if err = tx.QueryRow(ctx, `INSERT INTO run_events(run_id,conversation_id,type,payload) VALUES($1,$2,$3,$4) RETURNING id`, runID, conversationID, kind, raw).Scan(&eventID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO run_event_outbox(event_id) VALUES($1)`, eventID)
	return err
}

type Publisher func(context.Context, string, []byte) error
type Dispatcher struct {
	DB      *pgxpool.Pool
	Publish Publisher
}

// Flush serializes dispatch across API replicas. A failed transaction retains
// every delivery intent; already-published events may repeat with the same ID.
func (d *Dispatcher) Flush(ctx context.Context) (int, error) {
	if d.Publish == nil {
		return 0, errors.New("event publisher is unavailable")
	}
	tx, err := d.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var locked bool
	if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('lester:event-dispatch',0))`).Scan(&locked); err != nil || !locked {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT e.id,e.run_id,e.conversation_id,e.type,e.payload,e.created_at,c.workspace_id
        FROM run_event_outbox o JOIN run_events e ON e.id=o.event_id JOIN conversations c ON c.id=e.conversation_id
        ORDER BY o.event_id LIMIT 100 FOR UPDATE OF o`)
	if err != nil {
		return 0, err
	}
	type event struct {
		ID             int64           `json:"id"`
		RunID          uuid.UUID       `json:"run_id"`
		ConversationID uuid.UUID       `json:"conversation_id"`
		Type           string          `json:"type"`
		Payload        json.RawMessage `json:"payload"`
		CreatedAt      time.Time       `json:"created_at"`
		WorkspaceID    uuid.UUID       `json:"-"`
	}
	var events []event
	for rows.Next() {
		var item event
		if err = rows.Scan(&item.ID, &item.RunID, &item.ConversationID, &item.Type, &item.Payload, &item.CreatedAt, &item.WorkspaceID); err != nil {
			rows.Close()
			return 0, err
		}
		events = append(events, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, item := range events {
		encoded, encodeErr := json.Marshal(item)
		if encodeErr != nil {
			return 0, encodeErr
		}
		for _, channel := range []string{"conversation:" + item.ConversationID.String(), "workspace:" + item.WorkspaceID.String()} {
			if err = d.Publish(ctx, channel, encoded); err != nil {
				return 0, err
			}
		}
		if _, err = tx.Exec(ctx, `DELETE FROM run_event_outbox WHERE event_id=$1`, item.ID); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(events), nil
}

func (d *Dispatcher) Run(ctx context.Context) {
	for ctx.Err() == nil {
		batchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		count, err := d.Flush(batchCtx)
		cancel()
		delay := 100 * time.Millisecond
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("run event delivery deferred", "error", err)
			delay = time.Second
		} else if count == 100 {
			continue
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
