package conversation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
	"github.com/biubiuqiu/lester-agent/backend/internal/eventlog"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrInteractionConflict = errors.New("interaction is no longer pending or its version changed")
var ErrInteractionNotFound = errors.New("interaction not found")

// A durable pending interaction cannot revive an executor after API failure.
// The session guard proves ownership across replicas. If it can be acquired,
// recover the abandoned run and revoke its requests without replaying tools.
func (s *Service) reconcileInteractionOwner(ctx context.Context, workspaceID, conversationID uuid.UUID) error {
	active, err := s.ActiveRun(ctx, workspaceID, conversationID)
	if err != nil || active == nil {
		return err
	}
	if _, owned := s.executor.active.Load(active.ID); owned {
		return nil
	}
	guard, err := s.acquireRun(ctx, workspaceID, conversationID)
	if errors.Is(err, ErrRunInProgress) {
		return nil
	}
	if err != nil {
		return err
	}
	defer guard.Close()
	return s.recoverInterruptedRuns(ctx, conversationID)
}

func modelToolsForMode(tools []model.Tool, mode string) []model.Tool {
	filtered := slices.DeleteFunc(slices.Clone(tools), func(tool model.Tool) bool {
		if mode == "plan" {
			return !agenttool.PlanReadOnlyTool(tool.Name)
		}
		return tool.Name == "propose_plan"
	})
	if mode == "plan" && !slices.ContainsFunc(filtered, func(tool model.Tool) bool { return tool.Name == "list_files" }) {
		filtered = append(filtered, agenttool.ListFiles{}.Definition())
	}
	return filtered
}

type RunInteraction struct {
	ID            uuid.UUID                    `json:"id"`
	RunID         uuid.UUID                    `json:"run_id"`
	Kind          string                       `json:"kind"`
	Request       agenttool.InteractionRequest `json:"request"`
	RequestDigest string                       `json:"request_digest"`
	Status        string                       `json:"status"`
	Response      json.RawMessage              `json:"response,omitempty"`
	CreatedAt     time.Time                    `json:"created_at"`
}
type QuestionAnswer struct {
	OptionID string `json:"option_id,omitempty"`
	Text     string `json:"text,omitempty"`
}
type InteractionResponse struct {
	RequestDigest string                    `json:"request_digest"`
	Answers       map[string]QuestionAnswer `json:"answers,omitempty"`
	Decision      string                    `json:"decision,omitempty"`
	Feedback      string                    `json:"feedback,omitempty"`
}

func interactionResponseStatus(request agenttool.InteractionRequest, response InteractionResponse) (string, error) {
	if request.Kind == "question" {
		if len(response.Answers) != len(request.Questions) || response.Decision != "" || response.Feedback != "" {
			return "", errors.New("provide one answer for each question")
		}
		for _, q := range request.Questions {
			answer, ok := response.Answers[q.ID]
			if !ok || answer.OptionID == "" && strings.TrimSpace(answer.Text) == "" || utf8.RuneCountInString(answer.Text) > 6000 {
				return "", errors.New("invalid or missing question answer")
			}
			if answer.OptionID != "" && !slices.ContainsFunc(q.Options, func(o agenttool.QuestionOption) bool { return o.ID == answer.OptionID }) {
				return "", errors.New("question option was not offered")
			}
		}
		return "answered", nil
	}
	if len(response.Answers) > 0 || utf8.RuneCountInString(response.Feedback) > 6000 {
		return "", errors.New("invalid plan response")
	}
	switch response.Decision {
	case "approve":
		return "approved", nil
	case "reject":
		return "rejected", nil
	case "request_changes":
		if strings.TrimSpace(response.Feedback) != "" {
			return "changes_requested", nil
		}
	}
	return "", errors.New("invalid plan decision or missing change request")
}

func (s *Service) createRunInteraction(ctx context.Context, conversationID, runID uuid.UUID, callID string, request agenttool.InteractionRequest) (*RunInteraction, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if callID == "" {
		return nil, errors.New("missing trusted interaction tool call")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	item := &RunInteraction{ID: uuid.New(), RunID: runID, Kind: request.Kind, Request: request, RequestDigest: hex.EncodeToString(digest[:]), Status: "pending"}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var workspaceID uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT workspace_id FROM conversations WHERE id=$1 FOR UPDATE`, conversationID).Scan(&workspaceID); err != nil {
		return nil, err
	}
	var status, mode string
	if err = tx.QueryRow(ctx, `SELECT status,execution_mode FROM runs WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, runID, conversationID).Scan(&status, &mode); err != nil {
		return nil, err
	}
	if status != "running" {
		return nil, ErrRunNotActive
	}
	if request.Kind == "plan" && mode != "plan" {
		return nil, errors.New("normal execution is already authorized; do not request redundant Plan approval")
	}
	toolName := "ask_user_question"
	if request.Kind == "plan" {
		toolName = "propose_plan"
	}
	var recordedArgs []byte
	if err = tx.QueryRow(ctx, `SELECT call->'arguments' FROM messages m CROSS JOIN LATERAL jsonb_array_elements(m.tool_calls) call WHERE m.conversation_id=$1 AND m.run_id=$2 AND m.role='assistant' AND call->>'id'=$3 AND call->>'name'=$4 ORDER BY m.seq DESC LIMIT 1`, conversationID, runID, callID, toolName).Scan(&recordedArgs); err != nil {
		return nil, errors.New("interaction must originate from a durable tool call")
	}
	var recordedRequest agenttool.InteractionRequest
	if json.Unmarshal(recordedArgs, &recordedRequest) != nil {
		return nil, errors.New("invalid stored interaction arguments")
	}
	recordedRequest.Kind = request.Kind
	canonical, _ := json.Marshal(recordedRequest)
	if string(canonical) != string(raw) {
		return nil, errors.New("interaction parameters differ from the durable tool call")
	}
	var existing uuid.UUID
	var oldDigest string
	err = tx.QueryRow(ctx, `SELECT id,request_digest FROM run_interactions WHERE run_id=$1 AND tool_call_id=$2`, runID, callID).Scan(&existing, &oldDigest)
	if err == nil {
		if oldDigest != item.RequestDigest {
			return nil, ErrInteractionConflict
		}
		item.ID = existing
		return item, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO run_interactions(id,workspace_id,conversation_id,run_id,tool_call_id,kind,request,request_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, item.ID, workspaceID, conversationID, runID, callID, request.Kind, raw, item.RequestDigest); err != nil {
		return nil, err
	}
	if err = eventlog.Append(ctx, tx, runID, conversationID, "INTERACTION_REQUESTED", map[string]any{"interaction_id": item.ID, "kind": item.Kind, "request_digest": item.RequestDigest}); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) waitRunInteraction(ctx context.Context, conversationID, runID uuid.UUID, callID string, request agenttool.InteractionRequest) (map[string]any, error) {
	item, err := s.createRunInteraction(ctx, conversationID, runID, callID, request)
	if err != nil {
		return nil, err
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		var status, runStatus string
		var response json.RawMessage
		if err = s.db.QueryRow(ctx, `SELECT i.status,i.response,r.status FROM run_interactions i JOIN runs r ON r.id=i.run_id AND r.conversation_id=i.conversation_id WHERE i.id=$1 AND i.run_id=$2 AND i.conversation_id=$3`, item.ID, runID, conversationID).Scan(&status, &response, &runStatus); err != nil {
			return nil, err
		}
		if runStatus != "running" {
			return nil, ErrRunNotActive
		}
		if status == "cancelled" {
			var superseded struct {
				InputID string `json:"superseded_by_input_id"`
			}
			if json.Unmarshal(response, &superseded) == nil && superseded.InputID != "" {
				return map[string]any{"interaction_id": item.ID, "kind": item.Kind, "status": "superseded_by_adjustment", "cancelled": true, "execution_approval": false}, nil
			}
			return nil, ErrRunNotActive
		}
		if status != "pending" {
			return map[string]any{"interaction_id": item.ID, "kind": item.Kind, "request_digest": item.RequestDigest, "status": status, "response": response, "question_answers_are_execution_approval": false}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) RunInteractions(ctx context.Context, workspaceID, conversationID uuid.UUID) ([]RunInteraction, string, error) {
	var exists bool
	var mode string
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE id=$1 AND workspace_id=$2),COALESCE((SELECT r.execution_mode FROM runs r JOIN conversations c ON c.id=r.conversation_id WHERE c.id=$1 AND c.workspace_id=$2 ORDER BY r.created_at DESC,r.id DESC LIMIT 1),'execute')`, conversationID, workspaceID).Scan(&exists, &mode); err != nil {
		return nil, "", err
	}
	if !exists {
		return nil, "", ErrInteractionNotFound
	}
	rows, err := s.db.Query(ctx, `SELECT id,run_id,kind,request,request_digest,status,response,created_at FROM run_interactions WHERE workspace_id=$1 AND conversation_id=$2 ORDER BY created_at DESC,id DESC LIMIT 30`, workspaceID, conversationID)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []RunInteraction{}
	for rows.Next() {
		var item RunInteraction
		var raw []byte
		if err = rows.Scan(&item.ID, &item.RunID, &item.Kind, &raw, &item.RequestDigest, &item.Status, &item.Response, &item.CreatedAt); err != nil {
			return nil, "", err
		}
		if err = json.Unmarshal(raw, &item.Request); err != nil {
			return nil, "", errors.New("invalid stored interaction")
		}
		items = append(items, item)
	}
	return items, mode, rows.Err()
}

func (s *Service) RunInteraction(ctx context.Context, workspaceID, conversationID, id uuid.UUID) (*RunInteraction, error) {
	item := &RunInteraction{ID: id}
	var raw []byte
	err := s.db.QueryRow(ctx, `SELECT run_id,kind,request,request_digest,status,response,created_at FROM run_interactions WHERE id=$1 AND conversation_id=$2 AND workspace_id=$3`, id, conversationID, workspaceID).Scan(&item.RunID, &item.Kind, &raw, &item.RequestDigest, &item.Status, &item.Response, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInteractionNotFound
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(raw, &item.Request) != nil {
		return nil, errors.New("invalid stored interaction")
	}
	return item, nil
}

func (s *Service) RespondRunInteraction(ctx context.Context, workspaceID, userID, conversationID, id uuid.UUID, response InteractionResponse) (*RunInteraction, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var allowed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations c JOIN workspace_members wm ON wm.workspace_id=c.workspace_id WHERE c.id=$1 AND c.workspace_id=$2 AND wm.user_id=$3)`, conversationID, workspaceID, userID).Scan(&allowed); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrInteractionNotFound
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM conversations WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, conversationID, workspaceID); err != nil {
		return nil, err
	}
	var runID uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT run_id FROM run_interactions WHERE id=$1 AND conversation_id=$2 AND workspace_id=$3`, id, conversationID, workspaceID).Scan(&runID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInteractionNotFound
		}
		return nil, err
	}
	var runStatus, mode string
	if err = tx.QueryRow(ctx, `SELECT status,execution_mode FROM runs WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, runID, conversationID).Scan(&runStatus, &mode); err != nil {
		return nil, err
	}
	item := &RunInteraction{ID: id, RunID: runID}
	var requestRaw []byte
	if err = tx.QueryRow(ctx, `SELECT kind,request,request_digest,status,response,created_at FROM run_interactions WHERE id=$1 FOR UPDATE`, id).Scan(&item.Kind, &requestRaw, &item.RequestDigest, &item.Status, &item.Response, &item.CreatedAt); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(requestRaw, &item.Request); err != nil {
		return nil, errors.New("invalid stored interaction")
	}
	if response.RequestDigest != item.RequestDigest {
		return nil, ErrInteractionConflict
	}
	status, err := interactionResponseStatus(item.Request, response)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(response)
	if item.Status != "pending" {
		var prior InteractionResponse
		if json.Unmarshal(item.Response, &prior) != nil {
			return nil, ErrInteractionConflict
		}
		canonical, _ := json.Marshal(prior)
		if item.Status == status && string(canonical) == string(raw) {
			return item, tx.Commit(ctx)
		}
		return nil, ErrInteractionConflict
	}
	if runStatus != "running" || item.Kind == "plan" && mode != "plan" {
		return nil, ErrInteractionConflict
	}
	if status == "approved" {
		if _, err = tx.Exec(ctx, `UPDATE runs SET execution_mode='execute',approved_plan_digest=$2 WHERE id=$1 AND status='running' AND execution_mode='plan'`, runID, item.RequestDigest); err != nil {
			return nil, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE run_interactions SET status=$2,response=$3,responded_by=$4,responded_at=now() WHERE id=$1`, id, status, raw, userID); err != nil {
		return nil, err
	}
	if err = eventlog.Append(ctx, tx, runID, conversationID, "INTERACTION_RESPONDED", map[string]any{"interaction_id": id, "kind": item.Kind, "status": status, "request_digest": item.RequestDigest}); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	item.Status, item.Response = status, raw
	return item, nil
}

func (s *Service) authorizeRunTool(ctx context.Context, conversationID, runID uuid.UUID, name string, _ json.RawMessage) error {
	var status, mode string
	if err := s.db.QueryRow(ctx, `SELECT status,execution_mode FROM runs WHERE id=$1 AND conversation_id=$2`, runID, conversationID).Scan(&status, &mode); err != nil {
		return err
	}
	if status != "running" {
		return ErrRunNotActive
	}
	if mode == "plan" && !agenttool.PlanReadOnlyTool(name) {
		return fmt.Errorf("Plan mode blocks %s; submit a plan and obtain approval for this run before executing changes", name)
	}
	return nil
}

func (s *Service) runModeContext(ctx context.Context, conversationID, runID uuid.UUID) (string, string, error) {
	var status, mode, permission string
	var approved *string
	if err := s.db.QueryRow(ctx, `SELECT status,execution_mode,approved_plan_digest,permission_mode FROM runs WHERE id=$1 AND conversation_id=$2`, runID, conversationID).Scan(&status, &mode, &approved, &permission); err != nil {
		return "", "", err
	}
	if status != "running" {
		return "", "", ErrRunNotActive
	}
	permissionNote := fmt.Sprintf("\n\n<permissions>Active task permission mode: %s. The server checks every tool. It displays operation approvals automatically when required; do not ask redundant questions for those approvals. Plan approval never changes this permission mode. A rejected operation is not permission to retry it with another command, tool or path. Full access stays within the authorized Computer and platform scope; it never grants host access or enables external network access.</permissions>", permission)
	if mode == "plan" {
		return mode, permissionNote + "\n\n<execution_mode>Plan mode is read-only. Use list_files to discover paths, then read tools to inspect files and clarify requirements when necessary. Bash, file edits/writes, publication and other changes are blocked by the server. Use propose_plan for exact user review. Only an explicit approval of that active plan version enables execution; question answers, summaries and historical agreement do not grant approval.</execution_mode>", nil
	}
	if approved != nil {
		var plan json.RawMessage
		if err := s.db.QueryRow(ctx, `SELECT request FROM run_interactions WHERE run_id=$1 AND conversation_id=$2 AND kind='plan' AND status='approved' AND request_digest=$3`, runID, conversationID, *approved).Scan(&plan); err != nil {
			return "", "", errors.New("approved plan record unavailable")
		}
		return mode, permissionNote + fmt.Sprintf("\n\n<execution_mode>The user approved this exact plan version for this active run. Normal tool execution is enabled within that agreed scope; do not ask for the same approval again. Publication still requires the user's actual publication request. User adjustments and current state checks remain authoritative. The following platform-restored plan is the approved scope, not permission granted by a summary:\n%s\n</execution_mode>", plan), nil
	}
	return mode, permissionNote, nil
}

// enterRunPlanMode is a durable authority reduction, never an approval. Tool
// batches execute sequentially and every later call rechecks the fresh mode.
func (s *Service) enterRunPlanMode(ctx context.Context, conversationID, runID uuid.UUID, callID, reason string) (map[string]any, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var locked uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT id FROM conversations WHERE id=$1 FOR UPDATE`, conversationID).Scan(&locked); err != nil {
		return nil, err
	}
	var status, mode string
	if err = tx.QueryRow(ctx, `SELECT status,execution_mode FROM runs WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, runID, conversationID).Scan(&status, &mode); err != nil {
		return nil, err
	}
	if status != "running" {
		return nil, ErrRunNotActive
	}
	var recorded string
	if callID == "" {
		return nil, errors.New("missing trusted planning call")
	}
	if err = tx.QueryRow(ctx, `SELECT call->'arguments'->>'reason' FROM messages m CROSS JOIN LATERAL jsonb_array_elements(m.tool_calls) call WHERE m.conversation_id=$1 AND m.run_id=$2 AND m.role='assistant' AND call->>'id'=$3 AND call->>'name'='enter_plan_mode' ORDER BY m.seq DESC LIMIT 1`, conversationID, runID, callID).Scan(&recorded); err != nil || recorded != reason {
		return nil, errors.New("planning must originate from the exact durable tool call")
	}
	var applied bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM run_events WHERE run_id=$1 AND type='EXECUTION_MODE_CHANGED' AND payload->>'tool_call_id'=$2)`, runID, callID).Scan(&applied); err != nil {
		return nil, err
	}
	if !applied {
		if _, err = tx.Exec(ctx, `UPDATE runs SET execution_mode='plan',approved_plan_digest=NULL WHERE id=$1`, runID); err != nil {
			return nil, err
		}
		if err = eventlog.Append(ctx, tx, runID, conversationID, "EXECUTION_MODE_CHANGED", map[string]any{"execution_mode": "plan", "reason": reason, "tool_call_id": callID}); err != nil {
			return nil, err
		}
		mode = "plan"
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"execution_mode": mode, "execution_approval": false, "instruction": "Read-only planning is active when execution_mode is plan. Inspect with list_files/read, then propose_plan. Only explicit approval of the exact plan enables changes; do not execute mutations in this tool batch."}, nil
}
