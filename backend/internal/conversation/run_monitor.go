package conversation

import (
	"context"
	"github.com/google/uuid"
	"log/slog"
	"time"
)

type runMonitorCursor struct{ workspace, conversation uuid.UUID }

// A released session guard is the ownership proof. Age/quietness alone must
// never interrupt a live executor, including one on another API replica.
func (s *Service) reconcileActiveRunBatch(ctx context.Context, after runMonitorCursor, limit int) (runMonitorCursor, error) {
	rows, err := s.db.Query(ctx, `SELECT DISTINCT c.workspace_id,c.id FROM runs r JOIN conversations c ON c.id=r.conversation_id WHERE r.status IN ('running','cancelling') AND (c.workspace_id,c.id)>($1,$2) ORDER BY c.workspace_id,c.id LIMIT $3`, after.workspace, after.conversation, limit)
	if err != nil {
		return after, err
	}
	var scopes []runMonitorCursor
	for rows.Next() {
		var item runMonitorCursor
		if err = rows.Scan(&item.workspace, &item.conversation); err != nil {
			rows.Close()
			return after, err
		}
		scopes = append(scopes, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return after, err
	}
	for _, item := range scopes {
		check, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = s.reconcileInteractionOwner(check, item.workspace, item.conversation)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return after, ctx.Err()
			}
			slog.Warn("run ownership reconciliation failed", "conversation_id", item.conversation, "error", err)
		}
	}
	if len(scopes) == limit {
		return scopes[len(scopes)-1], nil
	}
	return runMonitorCursor{}, nil
}

// MonitorRuns repairs abandoned runs without a browser/API request and never
// replays model or tool calls. Run at startup, then scan bounded, fair pages.
func (s *Service) MonitorRuns(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	cursor := runMonitorCursor{}
	for {
		if ctx.Err() != nil {
			return
		}
		next, err := s.reconcileActiveRunBatch(ctx, cursor, 30)
		if err == nil {
			cursor = next
		} else if ctx.Err() == nil {
			slog.Warn("run monitor scan failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
