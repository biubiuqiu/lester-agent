package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/eventlog"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// claimQueuedInput must run under the existing conversation session guard.
// It creates a new explicit user run once; it never resumes/replays an old run.
func (s *Service) claimQueuedInput(ctx context.Context, workspaceID, conversationID uuid.UUID) (uuid.UUID, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)
	var ownedConversation uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT id FROM conversations WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, conversationID, workspaceID).Scan(&ownedConversation); err != nil {
		return uuid.Nil, err
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runs WHERE conversation_id=$1 AND status IN ('running','cancelling'))`, conversationID).Scan(&active); err != nil {
		return uuid.Nil, err
	}
	if active {
		return uuid.Nil, ErrRunInProgress
	}
	var id, after uuid.UUID
	var actor, modelID *uuid.UUID
	var content, mode, digest, permission string
	var metadata json.RawMessage
	var override bool
	err = tx.QueryRow(ctx, `SELECT id,after_run_id,submitted_by,content,metadata,model_deployment_id,execution_mode,request_digest,resume_authorized,permission_mode FROM run_inputs WHERE workspace_id=$1 AND conversation_id=$2 AND kind='next' AND status='queued' ORDER BY accepted_order LIMIT 1 FOR UPDATE`, workspaceID, conversationID).Scan(&id, &after, &actor, &content, &metadata, &modelID, &mode, &digest, &override, &permission)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	var previousStatus string
	if err = tx.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, after, conversationID).Scan(&previousStatus); err != nil {
		return uuid.Nil, err
	}
	if previousStatus != "completed" && !override {
		if previousStatus == "running" || previousStatus == "cancelling" {
			return uuid.Nil, nil
		}
		if _, err = tx.Exec(ctx, `UPDATE run_inputs SET status='held',held_reason='run_interrupted',revision=revision+1,updated_at=now() WHERE id=$1`, id); err != nil {
			return uuid.Nil, err
		}
		if err = eventlog.Append(ctx, tx, after, conversationID, "RUN_INPUT_HELD", map[string]any{"input_id": id, "status": "held"}); err != nil {
			return uuid.Nil, err
		}
		return uuid.Nil, tx.Commit(ctx)
	}
	var authorized bool
	if actor != nil {
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_members wm JOIN users u ON u.id=wm.user_id WHERE wm.workspace_id=$1 AND wm.user_id=$2 AND NOT u.disabled)`, workspaceID, *actor).Scan(&authorized); err != nil {
			return uuid.Nil, err
		}
	}
	if !authorized {
		if _, err = tx.Exec(ctx, `UPDATE run_inputs SET status='held',held_reason='authorization_changed',revision=revision+1,updated_at=now() WHERE id=$1`, id); err != nil {
			return uuid.Nil, err
		}
		if err = eventlog.Append(ctx, tx, after, conversationID, "RUN_INPUT_HELD", map[string]any{"input_id": id, "status": "held"}); err != nil {
			return uuid.Nil, err
		}
		return uuid.Nil, tx.Commit(ctx)
	}
	var runID, messageID uuid.UUID
	if err = tx.QueryRow(ctx, `INSERT INTO runs(conversation_id,status,model_deployment_id,submission_id,submitted_by,submission_sha256,execution_mode,permission_mode) VALUES($1,'running',$2,$3,$4,$5,$6,$7) RETURNING id`, conversationID, modelID, id, actor, digest, mode, permission).Scan(&runID); err != nil {
		return uuid.Nil, err
	}
	if err = tx.QueryRow(ctx, `INSERT INTO messages(conversation_id,run_id,role,content,metadata) VALUES($1,$2,'user',$3,$4::jsonb || jsonb_build_object('run_input_id',$5::text,'input_kind','next')) RETURNING id`, conversationID, runID, content, metadata, id).Scan(&messageID); err != nil {
		return uuid.Nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE runs SET input_message_id=$2 WHERE id=$1`, runID, messageID); err != nil {
		return uuid.Nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE run_inputs SET status='started',message_id=$2,started_run_id=$3,revision=revision+1,updated_at=now() WHERE id=$1`, id, messageID, runID); err != nil {
		return uuid.Nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE conversations SET updated_at=now() WHERE id=$1`, conversationID); err != nil {
		return uuid.Nil, err
	}
	if err = eventlog.Append(ctx, tx, runID, conversationID, "RUN_STARTED", map[string]any{"input_id": id, "input_message_id": messageID}); err != nil {
		return uuid.Nil, err
	}
	if err = eventlog.Append(ctx, tx, runID, conversationID, "RUN_INPUT_STARTED", map[string]any{"input_id": id, "status": "started"}); err != nil {
		return uuid.Nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return runID, nil
}

func (s *Service) dispatchInputQueue(ctx context.Context, workspaceID, conversationID uuid.UUID) {
	guard, err := s.acquireRun(ctx, workspaceID, conversationID)
	if err != nil {
		return
	}
	handedOff := false
	defer func() {
		if !handedOff {
			guard.Close()
		}
	}()
	if err = s.recoverInterruptedRuns(ctx, conversationID); err != nil {
		return
	}
	s.executor.mu.Lock()
	stopping := s.executor.stopping || s.executor.queueContext != nil && s.executor.queueContext.Err() != nil
	s.executor.mu.Unlock()
	if stopping {
		return
	}
	runID, err := s.claimQueuedInput(ctx, workspaceID, conversationID)
	if err != nil || runID == uuid.Nil {
		return
	}
	if s.executor.start(guard, workspaceID, conversationID, runID) {
		handedOff = true
		return
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.finishFailedRun(cleanup, runID, conversationID, "Queued input was accepted, but execution was interrupted during API shutdown. Tools were not replayed.")
}

func (s *Service) scheduleInputQueue(conversationID uuid.UUID) {
	e := s.executor
	e.mu.Lock()
	if e.stopping || e.queueContext != nil && e.queueContext.Err() != nil {
		e.mu.Unlock()
		return
	}
	if _, busy := e.queueDispatching.LoadOrStore(conversationID, true); busy {
		e.mu.Unlock()
		return
	}
	e.tasks.Add(1)
	parent := e.queueContext
	if parent == nil {
		parent = context.Background()
	}
	e.mu.Unlock()
	go func() {
		defer e.tasks.Done()
		defer e.queueDispatching.Delete(conversationID)
		ctx, cancel := context.WithTimeout(parent, 20*time.Second)
		defer cancel()
		var workspaceID uuid.UUID
		if err := s.db.QueryRow(ctx, `SELECT workspace_id FROM conversations WHERE id=$1 AND EXISTS(SELECT 1 FROM run_inputs WHERE conversation_id=$1 AND status='queued' AND kind='next')`, conversationID).Scan(&workspaceID); err == nil {
			s.dispatchInputQueue(ctx, workspaceID, conversationID)
		}
	}()
}

func (s *Service) MonitorInputQueue(ctx context.Context) {
	s.executor.mu.Lock()
	s.executor.queueContext = ctx
	s.executor.mu.Unlock()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	var cursorWorkspace, cursorConversation uuid.UUID
	for {
		rows, err := s.db.Query(ctx, `SELECT DISTINCT workspace_id,conversation_id FROM run_inputs WHERE kind='next' AND status='queued' AND (workspace_id,conversation_id)>($1,$2) ORDER BY workspace_id,conversation_id LIMIT 30`, cursorWorkspace, cursorConversation)
		if err == nil {
			type scope struct{ workspace, conversation uuid.UUID }
			items := []scope{}
			for rows.Next() {
				var item scope
				if rows.Scan(&item.workspace, &item.conversation) == nil {
					items = append(items, item)
				}
			}
			rows.Close()
			if len(items) == 30 {
				last := items[len(items)-1]
				cursorWorkspace, cursorConversation = last.workspace, last.conversation
			} else {
				cursorWorkspace, cursorConversation = uuid.Nil, uuid.Nil
			}
			for _, item := range items {
				if ctx.Err() != nil {
					return
				}
				s.scheduleInputQueue(item.conversation)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
