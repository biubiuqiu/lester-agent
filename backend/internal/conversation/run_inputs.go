package conversation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/biubiuqiu/lester-agent/backend/internal/eventlog"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/reference"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrRunInputConflict = errors.New("run input conflicts with the accepted request or current state")
var ErrRunInputNotFound = errors.New("run input not found")
var ErrPendingRunInputs = errors.New("accepted adjustments remain pending")
var ErrQueuedInputPriority = errors.New("an accepted queued task is waiting to start")
var ErrRunInputRejected = errors.New("run input was not accepted")

type RunInputRequest struct {
	ID             uuid.UUID         `json:"id"`
	RunID          uuid.UUID         `json:"run_id"`
	Kind           string            `json:"kind"`
	Content        string            `json:"content"`
	AttachmentIDs  []uuid.UUID       `json:"attachment_ids,omitempty"`
	ContextIDs     []uuid.UUID       `json:"context_ids,omitempty"`
	References     []reference.Input `json:"references,omitempty"`
	ExecutionMode  string            `json:"execution_mode,omitempty"`
	PermissionMode string            `json:"permission_mode,omitempty"`
}

type RunInput struct {
	ID            uuid.UUID       `json:"id"`
	AfterRunID    uuid.UUID       `json:"after_run_id"`
	Kind          string          `json:"kind"`
	Status        string          `json:"status"`
	Content       string          `json:"content"`
	Request       json.RawMessage `json:"request,omitempty"`
	RequestDigest string          `json:"request_digest"`
	AcceptedOrder int64           `json:"accepted_order"`
	Revision      int64           `json:"revision"`
	MessageID     *uuid.UUID      `json:"message_id,omitempty"`
	StartedRunID  *uuid.UUID      `json:"started_run_id,omitempty"`
	HeldReason    *string         `json:"held_reason,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

func (s *Service) RunInput(ctx context.Context, workspaceID, userID, conversationID, id uuid.UUID) (*RunInput, error) {
	item := &RunInput{ID: id}
	err := s.db.QueryRow(ctx, `SELECT i.after_run_id,i.kind,i.status,i.content,i.request,i.request_digest,i.accepted_order,i.revision,i.message_id,i.started_run_id,i.held_reason,i.created_at FROM run_inputs i JOIN conversations c ON c.id=i.conversation_id AND c.workspace_id=i.workspace_id WHERE i.id=$1 AND i.workspace_id=$2 AND i.conversation_id=$3 AND i.submitted_by=$4`, id, workspaceID, conversationID, userID).Scan(&item.AfterRunID, &item.Kind, &item.Status, &item.Content, &item.Request, &item.RequestDigest, &item.AcceptedOrder, &item.Revision, &item.MessageID, &item.StartedRunID, &item.HeldReason, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRunInputNotFound
	}
	return item, err
}

func (s *Service) AcceptRunInput(ctx context.Context, workspaceID, userID, conversationID uuid.UUID, request RunInputRequest) (*RunInput, error) {
	if request.ID == uuid.Nil || request.RunID == uuid.Nil || request.Kind != "adjust" && request.Kind != "next" || utf8.RuneCountInString(request.Content) > 100000 {
		return nil, fmt.Errorf("%w: invalid run input", ErrRunInputRejected)
	}
	if request.Kind == "adjust" && (len(request.AttachmentIDs) > 0 || len(request.ContextIDs) > 0 || len(request.References) > 0 || request.ExecutionMode != "" || request.PermissionMode != "") {
		return nil, fmt.Errorf("%w: current-task adjustments accept text only and cannot change execution mode", ErrRunInputRejected)
	}
	mode := "execute"
	if request.ExecutionMode != "" {
		mode = request.ExecutionMode
	}
	if mode != "execute" && mode != "plan" {
		return nil, fmt.Errorf("%w: invalid next-task execution mode", ErrRunInputRejected)
	}
	permission, permissionErr := permissionMode(request.PermissionMode)
	if permissionErr != nil {
		return nil, fmt.Errorf("%w: invalid permission mode", ErrRunInputRejected)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	if existing, lookupErr := s.RunInput(ctx, workspaceID, userID, conversationID, request.ID); lookupErr == nil {
		if existing.RequestDigest != hash {
			return nil, ErrRunInputConflict
		}
		return existing, nil
	} else if !errors.Is(lookupErr, ErrRunInputNotFound) {
		return nil, lookupErr
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var modelID *uuid.UUID
	var allowed bool
	if err = tx.QueryRow(ctx, `SELECT c.model_deployment_id,EXISTS(SELECT 1 FROM workspace_members wm JOIN users u ON u.id=wm.user_id WHERE wm.workspace_id=c.workspace_id AND wm.user_id=$3 AND NOT u.disabled) FROM conversations c WHERE c.id=$1 AND c.workspace_id=$2 FOR UPDATE`, conversationID, workspaceID, userID).Scan(&modelID, &allowed); err != nil || !allowed {
		return nil, ErrRunInputNotFound
	}
	// Recheck identity after the conversation lock for concurrent same-ID retries.
	var existingHash string
	err = tx.QueryRow(ctx, `SELECT request_digest FROM run_inputs WHERE id=$1 AND workspace_id=$2 AND conversation_id=$3 AND submitted_by=$4`, request.ID, workspaceID, conversationID, userID).Scan(&existingHash)
	if err == nil {
		if existingHash != hash {
			return nil, ErrRunInputConflict
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		return s.RunInput(ctx, workspaceID, userID, conversationID, request.ID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	var usedIdentity bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runs WHERE conversation_id=$1 AND submission_id=$2)`, conversationID, request.ID).Scan(&usedIdentity); err != nil {
		return nil, err
	}
	if usedIdentity {
		return nil, ErrRunInputConflict
	}
	var runStatus string
	if err = tx.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, request.RunID, conversationID).Scan(&runStatus); err != nil {
		return nil, ErrRunInputNotFound
	}
	if runStatus != "running" {
		return nil, ErrRunNotActive
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM run_inputs WHERE conversation_id=$1 AND status IN ('pending','queued','held')`, conversationID).Scan(&count); err != nil {
		return nil, err
	}
	if count >= 20 {
		return nil, fmt.Errorf("%w: too many pending inputs; cancel or finish an existing input first", ErrRunInputRejected)
	}
	content, metadata, err := prepareRunInput(ctx, tx, workspaceID, userID, conversationID, request.Content, request.AttachmentIDs, request.ContextIDs, request.References)
	if err != nil {
		return nil, fmt.Errorf("%w: check the selected references and attachments", ErrRunInputRejected)
	}
	status := "pending"
	if request.Kind == "next" {
		status = "queued"
	}
	item := &RunInput{ID: request.ID, AfterRunID: request.RunID, Kind: request.Kind, Status: status, Content: content, Request: raw, RequestDigest: hash}
	if err = tx.QueryRow(ctx, `INSERT INTO run_inputs(id,workspace_id,conversation_id,after_run_id,submitted_by,kind,request,request_digest,content,metadata,model_deployment_id,execution_mode,status,permission_mode) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING accepted_order,revision,created_at`, request.ID, workspaceID, conversationID, request.RunID, userID, request.Kind, raw, hash, content, metadata, modelID, mode, status, permission).Scan(&item.AcceptedOrder, &item.Revision, &item.CreatedAt); err != nil {
		return nil, err
	}
	if request.Kind == "adjust" {
		if _, err = tx.Exec(ctx, `UPDATE run_permissions SET status='cancelled' WHERE run_id=$1 AND status='pending'`, request.RunID); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `UPDATE run_interactions SET status='cancelled',response=jsonb_build_object('superseded_by_input_id',$2::text),responded_at=now() WHERE run_id=$1 AND status='pending'`, request.RunID, request.ID); err != nil {
			return nil, err
		}
	}
	if err = eventlog.Append(ctx, tx, request.RunID, conversationID, "RUN_INPUT_ACCEPTED", map[string]any{"input_id": request.ID, "kind": request.Kind, "status": status}); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return item, nil
}

// Apply only after the preceding complete assistant/tool batch. This appends
// real user messages, preserves their accepted order and never changes the
// original run's @ grants or execution mode.
func (s *Service) applyRunAdjustments(ctx context.Context, conversationID, runID uuid.UUID) ([]model.Message, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM conversations WHERE id=$1 FOR UPDATE`, conversationID); err != nil {
		return nil, err
	}
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, runID, conversationID).Scan(&status); err != nil {
		return nil, err
	}
	if status != "running" {
		return nil, ErrRunNotActive
	}
	rows, err := tx.Query(ctx, `SELECT id,content,submitted_by FROM run_inputs WHERE after_run_id=$1 AND conversation_id=$2 AND kind='adjust' AND status='pending' ORDER BY accepted_order FOR UPDATE`, runID, conversationID)
	if err != nil {
		return nil, err
	}
	type input struct {
		id      uuid.UUID
		content string
		actor   *uuid.UUID
	}
	items := []input{}
	for rows.Next() {
		var item input
		if err = rows.Scan(&item.id, &item.content, &item.actor); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	output := []model.Message{}
	ids := []uuid.UUID{}
	for _, item := range items {
		message, insertErr := insertMessageReceipt(ctx, tx, conversationID, runID, model.Message{Role: "user", Content: item.content}, "", map[string]any{"run_input_id": item.id, "submitted_by": item.actor, "input_kind": "adjust"})
		if insertErr != nil {
			return nil, insertErr
		}
		if _, err = tx.Exec(ctx, `UPDATE run_inputs SET status='applied',message_id=$2,revision=revision+1,updated_at=now() WHERE id=$1`, item.id, message.MessageID); err != nil {
			return nil, err
		}
		output = append(output, message)
		ids = append(ids, item.id)
	}
	if len(items) > 0 {
		messageIDs := []string{}
		for _, message := range output {
			messageIDs = append(messageIDs, message.MessageID)
		}
		if err = eventlog.Append(ctx, tx, runID, conversationID, "RUN_INPUT_APPLIED", map[string]any{"input_ids": ids, "message_ids": messageIDs}); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return output, nil
}

// Cancellation/failure does not silently launch accepted future work. Held
// inputs retain their exact request and require an explicit user decision.
func holdRunInputs(ctx context.Context, tx pgx.Tx, runID uuid.UUID, reason string) error {
	_, err := tx.Exec(ctx, `UPDATE run_inputs SET status='held',held_reason=$2,revision=revision+1,updated_at=now() WHERE (after_run_id=$1 AND status='pending') OR (conversation_id=(SELECT conversation_id FROM runs WHERE id=$1) AND kind='next' AND status='queued')`, runID, reason)
	return err
}
