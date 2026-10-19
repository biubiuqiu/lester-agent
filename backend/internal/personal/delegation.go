package personal

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/conversation"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Service) prepareDelegation(ctx context.Context, tx pgx.Tx, p auth.Principal, req Request, a taskAction) error {
	conversationID := uuid.New()
	projectID, _ := uuid.Parse(a.ProjectID)
	title := strings.TrimSpace(a.Title)
	if a.Action == "continue" {
		conversationID, _ = uuid.Parse(a.ConversationID)
		if err := tx.QueryRow(ctx, `SELECT project_id,title FROM conversations WHERE id=$1 AND workspace_id=$2 AND created_by=$3 FOR SHARE`, conversationID, p.WorkspaceID, p.UserID).Scan(&projectID, &title); err != nil {
			return ErrNotFound
		}
	}
	// Select facts for this specific task/project after all memory edits. No
	// other project facts, files, permissions or previous chat are transferred.
	rows, err := tx.Query(ctx, `SELECT `+memoryColumns+` FROM personal_memories WHERE workspace_id=$1 AND user_id=$2 AND NOT deleted AND (project_id IS NULL OR project_id=$3) ORDER BY updated_at DESC,id LIMIT 128`, p.WorkspaceID, p.UserID, projectID)
	if err != nil {
		return err
	}
	memories := []Memory{}
	for rows.Next() {
		m, e := scanMemory(rows)
		if e != nil {
			rows.Close()
			return e
		}
		memories = append(memories, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	prompt := strings.TrimSpace(a.Instructions) + scopedMemoryNotice(selectMemories(memories, a.Instructions))
	_, err = tx.Exec(ctx, `INSERT INTO personal_delegations(turn_id,workspace_id,user_id,conversation_id,submission_id,project_id,action,title,prompt,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'prepared')`, req.ID, p.WorkspaceID, p.UserID, conversationID, uuid.New(), projectID, a.Action, title, prompt)
	return err
}

func (s *Service) dispatch(ctx context.Context, p auth.Principal, turnID, modelID uuid.UUID, conn *pgx.Conn) {
	// Claim once before contacting the ordinary conversation acceptance path.
	// An interrupted dispatch is inspected by receipt, never replayed.
	tx, err := conn.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	if err = authorize(ctx, tx, p); err != nil {
		return
	}
	var conversationID, submissionID, projectID uuid.UUID
	var action, title, prompt string
	err = tx.QueryRow(ctx, `UPDATE personal_delegations SET status='dispatching' WHERE turn_id=$1 AND workspace_id=$2 AND user_id=$3 AND status='prepared' RETURNING conversation_id,submission_id,project_id,action,title,prompt`, turnID, p.WorkspaceID, p.UserID).Scan(&conversationID, &submissionID, &projectID, &action, &title, &prompt)
	if err != nil {
		return
	}
	if err = tx.Commit(ctx); err != nil {
		return
	}
	ctx = conversation.WithDelegationProject(ctx, projectID)
	status := "unknown"
	var runID, inputID *uuid.UUID
	if s.TasksService != nil {
		// Hold the live account/session/membership through acceptance; revocation
		// cannot race a validated model output into a new task.
		authority, e := conn.Begin(ctx)
		if e == nil {
			defer authority.Rollback(context.Background())
			if e = authorize(ctx, authority, p); e == nil {
				if action == "create" {
					var id uuid.UUID
					id, e = s.TasksService.StartTriggeredTask(ctx, conversation.TriggeredTask{ID: submissionID, WorkspaceID: p.WorkspaceID, UserID: p.UserID, ConversationID: conversationID, ProjectID: projectID, ModelID: modelID, Name: title, Agent: "lester", Prompt: prompt, Permission: "standard"})
					if e == nil {
						runID = &id
						status = "started"
					}
				} else {
					var active *uuid.UUID
					var state string
					e = s.DB.QueryRow(ctx, `SELECT r.id,COALESCE(r.status,'idle') FROM conversations c LEFT JOIN LATERAL(SELECT id,status FROM runs WHERE conversation_id=c.id ORDER BY created_at DESC,id DESC LIMIT 1)r ON true WHERE c.id=$1 AND c.workspace_id=$2 AND c.created_by=$3`, conversationID, p.WorkspaceID, p.UserID).Scan(&active, &state)
					if e == nil && state == "running" && active != nil {
						var input *conversation.RunInput
						input, e = s.TasksService.AcceptRunInput(ctx, p.WorkspaceID, p.UserID, conversationID, conversation.RunInputRequest{ID: submissionID, RunID: *active, Kind: "next", Content: prompt, ExecutionMode: "execute", PermissionMode: "standard"})
						if e == nil {
							inputID = &input.ID
							status = "queued"
						}
					} else if e == nil && state != "cancelling" {
						var id uuid.UUID
						id, e = s.TasksService.SendSubmission(ctx, p.WorkspaceID, p.UserID, conversationID, submissionID, prompt, nil, nil, nil, "execute", "standard")
						if e == nil {
							runID = &id
							status = "started"
						}
					} else if e == nil {
						e = ErrBusy
					}
				}
			}
			if e != nil && (errors.Is(e, ErrUnauthorized) || errors.Is(e, ErrBusy) || errors.Is(e, conversation.ErrRunInputRejected) || errors.Is(e, conversation.ErrDelegationProjectChanged) || errors.Is(e, conversation.ErrRunNotActive) || errors.Is(e, conversation.ErrRunInProgress)) {
				status = "failed"
			}
			_ = authority.Rollback(context.Background())
		}
	}
	store, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = s.DB.Exec(store, `UPDATE personal_delegations SET status=$2,run_id=$3,input_id=$4 WHERE turn_id=$1 AND status='dispatching'`, turnID, status, runID, inputID)
}

// reconcileDelegation only reads original acceptance receipts. It cannot start
// anything, even if a response or process disappeared before dispatch.
func (s *Service) reconcileDelegation(ctx context.Context, p auth.Principal, turn uuid.UUID) error {
	_, err := s.DB.Exec(ctx, `UPDATE personal_delegations d SET run_id=r.id,status='started' FROM runs r JOIN conversations c ON c.id=r.conversation_id WHERE d.turn_id=$1 AND d.workspace_id=$2 AND d.user_id=$3 AND d.status IN ('prepared','dispatching','unknown') AND r.conversation_id=d.conversation_id AND r.submission_id=d.submission_id AND r.submitted_by=d.user_id AND c.workspace_id=d.workspace_id`, turn, p.WorkspaceID, p.UserID)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, `UPDATE personal_delegations d SET input_id=i.id,run_id=i.started_run_id,status=CASE WHEN i.status='started' THEN 'started' ELSE 'queued' END FROM run_inputs i WHERE d.turn_id=$1 AND d.workspace_id=$2 AND d.user_id=$3 AND d.status IN ('prepared','dispatching','unknown','queued') AND i.id=d.submission_id AND i.workspace_id=d.workspace_id AND i.conversation_id=d.conversation_id AND i.submitted_by=d.user_id`, turn, p.WorkspaceID, p.UserID)
	return err
}
