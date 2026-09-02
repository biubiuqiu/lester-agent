package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/toolcontext"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrRunInProgress = errors.New("a run is already active in this conversation")

// The connection must stay open for the entire run. PostgreSQL releases its
// advisory lock if the process crashes; the next owner repairs interrupted runs.
// This requires a direct/session-pooled database connection (not transaction pooling).
type runGuard struct{ conn *pgx.Conn }

func (s *Service) acquireRun(ctx context.Context, workspaceID, conversationID uuid.UUID) (*runGuard, error) {
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2)`, workspaceID, conversationID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New("conversation not found")
	}
	conn, err := pgx.ConnectConfig(ctx, s.db.Config().ConnConfig.Copy())
	if err != nil {
		return nil, err
	}
	guard := &runGuard{conn: conn}
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, "lester:conversation:"+conversationID.String()).Scan(&locked); err != nil {
		guard.Close()
		return nil, err
	}
	if !locked {
		guard.Close()
		return nil, ErrRunInProgress
	}
	return guard, nil
}

func (g *runGuard) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = g.conn.Close(ctx)
}

// Cancel the worker if ownership is lost. Stop joins the watchdog before Close.
func (g *runGuard) Watch(ctx context.Context, cancelRun context.CancelFunc) func() {
	watchCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-ticker.C:
				checkCtx, cancel := context.WithTimeout(watchCtx, 5*time.Second)
				err := g.conn.Ping(checkCtx)
				cancel()
				if err != nil {
					cancelRun()
					return
				}
			}
		}
	}()
	return func() { stop(); <-done }
}

func modelHistory(messages []Message) []model.Message {
	history := make([]model.Message, 0, len(messages))
	for _, message := range messages {
		if message.Metadata["incomplete"] == true {
			continue
		}
		calls := message.ToolCalls
		if len(calls) == 0 {
			calls = nil
		}
		item := model.Message{Role: message.Role, Content: messageContentForModel(message), ToolCalls: calls, ToolCallID: message.ToolCallID}
		if message.RunID != nil {
			item.RunID = message.RunID.String()
		}
		history = append(history, item)
	}
	return history
}

// Keep the existing chat UI contract. Full records are available with
// GET /conversations/{id}?include_internal=true. Model requests separately project
// this full transcript through the tool context policy.
func visibleMessages(messages []Message) []Message {
	visible := make([]Message, 0, len(messages))
	for _, message := range messages {
		if message.Role == "tool" || len(message.ToolCalls) > 0 || message.Metadata["incomplete"] == true {
			continue
		}
		visible = append(visible, message)
	}
	return visible
}

// Stream indices need not start at zero (e.g. Anthropic content block indices).
func assembledToolCalls(fragments map[int]*model.ToolCall) ([]model.ToolCall, error) {
	indices := make([]int, 0, len(fragments))
	for index := range fragments {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	calls := make([]model.ToolCall, 0, len(indices))
	seen := map[string]bool{}
	for _, index := range indices {
		call := *fragments[index]
		if call.ID == "" || call.Name == "" || seen[call.ID] {
			return nil, errors.New("incomplete or duplicate model tool call")
		}
		if len(call.Arguments) == 0 {
			call.Arguments = json.RawMessage(`{}`)
		}
		if !json.Valid(call.Arguments) {
			return nil, errors.New("invalid model tool arguments")
		}
		seen[call.ID] = true
		call.Index = 0 // Transport-only index; slice order is now canonical.
		calls = append(calls, call)
	}
	return calls, nil
}

func (s *Service) appendMessage(ctx context.Context, conversationID, runID uuid.UUID, message model.Message, toolName string, metadata map[string]any) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Use the same lock order as recovery. A superseded worker cannot write into
	// a transcript after another owner has marked its run failed.
	if _, err = tx.Exec(ctx, `SELECT id FROM conversations WHERE id=$1 FOR UPDATE`, conversationID); err != nil {
		return err
	}
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, runID, conversationID).Scan(&status); err != nil {
		return err
	}
	if status != "running" {
		return errors.New("run is no longer active")
	}
	if err = insertMessage(ctx, tx, conversationID, runID, message, toolName, metadata); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertMessage(ctx context.Context, tx pgx.Tx, conversationID, runID uuid.UUID, message model.Message, toolName string, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	calls := message.ToolCalls
	if calls == nil {
		calls = []model.ToolCall{}
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	callsJSON, err := json.Marshal(calls)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO messages(conversation_id,run_id,role,content,metadata,tool_calls,tool_call_id,tool_name)
		VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''))`, conversationID, runID, message.Role, message.Content, metadataJSON, callsJSON, message.ToolCallID, toolName)
	return err
}

func (s *Service) saveRunContext(ctx context.Context, runID uuid.UUID, messages []Message, request model.ModelRequest) error {
	var through int64
	for _, message := range messages {
		if message.Seq > through {
			through = message.Seq
		}
	}
	// Record the dynamic prompt/tool snapshot once. Durable messages through the
	// cursor reconstruct the initial history; this contains no provider credentials.
	contextSnapshot := map[string]any{"model": request.Model, "system": request.System, "tools": request.Tools, "temperature": request.Temperature, "history_through_seq": through, "tool_context_policy": toolcontext.PolicyVersion, "recent_full_tool_exchanges": toolcontext.RecentFullToolExchanges}
	if request.MaxTokens > 0 {
		contextSnapshot["max_tokens"] = request.MaxTokens
	}
	snapshot, err := json.Marshal(contextSnapshot)
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `UPDATE runs SET context=$2 WHERE id=$1 AND status='running'`, runID, snapshot)
	if err == nil && tag.RowsAffected() != 1 {
		return errors.New("run is no longer active")
	}
	return err
}

func (s *Service) savePartialResponse(runID, conversationID uuid.UUID, text string, fragments map[int]*model.ToolCall) {
	if text == "" && len(fragments) == 0 {
		return
	}
	// Partial arguments may not be valid JSON. Preserve them as strings, never
	// as runnable tool_calls, and exclude this record from future model requests.
	parts := map[int]any{}
	for index, call := range fragments {
		parts[index] = map[string]any{"id": call.ID, "name": call.Name, "arguments_fragment": string(call.Arguments)}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.appendMessage(ctx, conversationID, runID, model.Message{Role: "assistant", Content: text}, "", map[string]any{"incomplete": true, "tool_call_fragments": parts}); err != nil {
		slog.Error("persist partial model response", "run_id", runID, "error", err)
	}
}

func (s *Service) recoverInterruptedRuns(ctx context.Context, conversationID uuid.UUID) error {
	// Only call after acquiring the conversation guard. A running row without
	// its guard is left over from an interrupted process; never replay its tools.
	rows, err := s.db.Query(ctx, `SELECT id FROM runs WHERE conversation_id=$1 AND status='running' ORDER BY created_at,id`, conversationID)
	if err != nil {
		return err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = s.finishFailedRun(ctx, id, conversationID, "Run interrupted before completion; tools were not automatically retried."); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) finishFailedRun(ctx context.Context, runID, conversationID uuid.UUID, reason string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM conversations WHERE id=$1 FOR UPDATE`, conversationID); err != nil {
		return err
	}
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, runID, conversationID).Scan(&status); err != nil {
		return err
	}
	if status != "running" {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT call->>'id',call->>'name'
		FROM messages m CROSS JOIN LATERAL jsonb_array_elements(m.tool_calls) WITH ORDINALITY AS t(call,n)
		WHERE m.run_id=$1 AND m.conversation_id=$2 AND m.role='assistant'
		AND NOT EXISTS(SELECT 1 FROM messages r WHERE r.run_id=m.run_id AND r.role='tool' AND r.tool_call_id=call->>'id')
		ORDER BY m.seq,t.n`, runID, conversationID)
	if err != nil {
		return err
	}
	var missing []model.ToolCall
	for rows.Next() {
		var call model.ToolCall
		if err = rows.Scan(&call.ID, &call.Name); err != nil {
			rows.Close()
			return err
		}
		missing = append(missing, call)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, call := range missing {
		result, _ := json.Marshal(map[string]any{"error": "Tool execution was interrupted and its result is unavailable. Side effects may have occurred; inspect current state before retrying.", "interrupted": true})
		if err = insertMessage(ctx, tx, conversationID, runID, model.Message{Role: "tool", Content: string(result), ToolCallID: call.ID}, call.Name, map[string]any{"is_error": true, "interrupted": true}); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE runs SET status='failed',completed_at=now() WHERE id=$1`, runID); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("finish failed run: %w", err)
	}
	s.event(ctx, runID, conversationID, "RUN_FAILED", map[string]any{"error": reason})
	return nil
}
