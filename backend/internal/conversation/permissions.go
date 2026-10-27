package conversation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
	"github.com/biubiuqiu/lester-agent/backend/internal/eventlog"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func permissionMode(value string) (string, error) {
	if value == "" {
		value = "standard"
	}
	if value != "standard" && value != "auto" && value != "full" {
		return "", errors.New("invalid permission mode")
	}
	return value, nil
}
func permissionTool(name string) string {
	switch name {
	case "computer_exec":
		return "bash"
	case "computer_read_file":
		return "read"
	case "computer_write_file":
		return "write"
	case "computer_list_files":
		return "list_files"
	}
	return name
}
func permissionDigest(name string, args json.RawMessage) (string, error) {
	var value any
	if !json.Valid(args) {
		return "", errors.New("invalid tool arguments")
	}
	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte(permissionTool(name)+"\x00"), canonical...))
	return hex.EncodeToString(sum[:]), nil
}

// Every command goes through operation review in auto mode. The independent
// reviewer can allow, deny or defer to the user; other modes use user consent.
func needsPermission(mode, name string, args json.RawMessage) (bool, bool) {
	name = permissionTool(name)
	if name == "github_publish" {
		return true, false
	}
	if mode == "full" {
		return false, false
	}
	var input struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
		Command  string `json:"command"`
	}
	if json.Unmarshal(args, &input) != nil {
		return true, false
	}
	target := strings.ToLower(strings.TrimSpace(input.FilePath))
	if target == "" {
		target = strings.ToLower(strings.TrimSpace(input.Path))
	}
	sensitive := false
	for _, part := range strings.Split(strings.ReplaceAll(target, "\\", "/"), "/") {
		if strings.HasPrefix(part, ".env") || part == ".ssh" || part == ".aws" || part == ".git" || strings.Contains(part, "credential") || strings.Contains(part, "secret") || strings.HasSuffix(part, ".pem") || strings.HasSuffix(part, ".key") {
			sensitive = true
		}
	}
	if sensitive {
		return true, false
	}
	switch name {
	case "read", "list_files", "load_skill", "read_conversation", "tool_result", "task_output", "ask_user_question", "propose_plan", "enter_plan_mode", "list_schedules", "propose_schedule", "list_app_connections", "read_app", "list_github_repositories", "github_prepare_publish":
		return false, false
	case "write", "edit", "github_checkout":
		return mode != "auto", name != "github_checkout"
	case "bash":
		return true, false
	default:
		return true, false
	}
}

type RunPermission struct {
	ID              uuid.UUID       `json:"id"`
	RunID           uuid.UUID       `json:"run_id"`
	Tool            string          `json:"tool"`
	RequestDigest   string          `json:"request_digest"`
	Summary         json.RawMessage `json:"summary"`
	AllowRun        bool            `json:"allow_run"`
	Status          string          `json:"status"`
	Decision        *string         `json:"decision,omitempty"`
	DecisionSource  string          `json:"decision_source"`
	ReviewDecision  *string         `json:"review_decision,omitempty"`
	ReviewReason    string          `json:"review_reason"`
	ReviewerModelID *uuid.UUID      `json:"reviewer_model_deployment_id,omitempty"`
	Revision        int64           `json:"permission_revision"`
}

func permissionSummary(name string, args json.RawMessage) json.RawMessage {
	var input map[string]json.RawMessage
	_ = json.Unmarshal(args, &input)
	summary := map[string]any{"tool": name}
	for _, key := range []string{"file_path", "path", "command", "task_id", "description", "repository", "ref", "directory", "branch", "summary", "proposal_id", "digest"} {
		if value, ok := input[key]; ok {
			var text string
			if json.Unmarshal(value, &text) == nil {
				summary[key] = text
			}
		}
	}
	if value, ok := input["content"]; ok {
		var text string
		if json.Unmarshal(value, &text) == nil {
			summary["content_bytes"] = len(text)
		}
	}
	// Do not expose file contents, credentials or environment values in chrome.
	raw, _ := json.Marshal(summary)
	return raw
}
func (s *Service) authorizePermission(ctx context.Context, conversationID, runID uuid.UUID, callID, name string, args json.RawMessage) error {
	name = permissionTool(name)
	var mode, status string
	var revision int64
	if err := s.db.QueryRow(ctx, `SELECT permission_mode,status,permission_revision FROM runs WHERE id=$1 AND conversation_id=$2`, runID, conversationID).Scan(&mode, &status, &revision); err != nil {
		return err
	}
	if status != "running" {
		return ErrRunNotActive
	}
	needed, allowRun := needsPermission(mode, name, args)
	if !needed {
		return nil
	}
	digest, err := permissionDigest(name, args)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var workspaceID uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT workspace_id FROM conversations WHERE id=$1 FOR UPDATE`, conversationID).Scan(&workspaceID); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT status,permission_mode,permission_revision FROM runs WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, runID, conversationID).Scan(&status, &mode, &revision); err != nil {
		return err
	}
	if status != "running" {
		return ErrRunNotActive
	}
	needed, allowRun = needsPermission(mode, name, args)
	if !needed {
		return tx.Commit(ctx)
	}
	// The prompt and its hash come from the actual durable call, never from a
	// model-created approval request or a context summary.
	var recordedName string
	var recordedArgs json.RawMessage
	if err = tx.QueryRow(ctx, `SELECT call->>'name',call->'arguments' FROM messages m CROSS JOIN LATERAL jsonb_array_elements(m.tool_calls) call WHERE m.conversation_id=$1 AND m.run_id=$2 AND m.role='assistant' AND call->>'id'=$3 ORDER BY m.seq DESC LIMIT 1`, conversationID, runID, callID).Scan(&recordedName, &recordedArgs); err != nil {
		return errors.New("permission requires a durable tool call")
	}
	recordedDigest, err := permissionDigest(recordedName, recordedArgs)
	if err != nil || recordedDigest != digest {
		return errors.New("permission parameters differ from recorded call")
	}
	if allowRun {
		tools := []string{name}
		if name == "write" || name == "edit" {
			tools = []string{"write", "edit"}
		}
		var granted bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM run_permissions WHERE run_id=$1 AND conversation_id=$2 AND tool=ANY($3::text[]) AND allow_run AND status='approved' AND decision='run' AND permission_revision=$4)`, runID, conversationID, tools, revision).Scan(&granted); err != nil {
			return err
		}
		if granted {
			return tx.Commit(ctx)
		}
	}
	id := uuid.New()
	initialStatus := "pending"
	if mode == "auto" && name == "bash" {
		initialStatus = "reviewing"
	}
	if _, err = tx.Exec(ctx, `INSERT INTO run_permissions(id,workspace_id,conversation_id,run_id,tool_call_id,tool,request_digest,summary,allow_run,permission_revision,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(run_id,tool_call_id) DO NOTHING`, id, workspaceID, conversationID, runID, callID, name, digest, permissionSummary(name, args), allowRun, revision, initialStatus); err != nil {
		return err
	}
	var storedDigest, storedStatus string
	if err = tx.QueryRow(ctx, `SELECT id,request_digest,status FROM run_permissions WHERE run_id=$1 AND tool_call_id=$2`, runID, callID).Scan(&id, &storedDigest, &storedStatus); err != nil {
		return err
	}
	if storedDigest != digest {
		return ErrInteractionConflict
	}
	if storedStatus == "pending" {
		if err = eventlog.Append(ctx, tx, runID, conversationID, "PERMISSION_REQUESTED", map[string]any{"permission_id": id, "tool": name}); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = s.db.Exec(cleanupCtx, `UPDATE run_permissions SET status='cancelled',review_token=NULL WHERE id=$1 AND status IN ('pending','reviewing')`, id)
	}()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		var current, runStatus string
		var reviewDecision *string
		if err = s.db.QueryRow(ctx, `SELECT p.status,r.status,r.permission_mode,r.permission_revision,p.review_decision FROM run_permissions p JOIN runs r ON r.id=p.run_id WHERE p.id=$1 AND r.conversation_id=$2`, id, conversationID).Scan(&current, &runStatus, &mode, &revision, &reviewDecision); err != nil {
			return err
		}
		if runStatus != "running" {
			return ErrRunNotActive
		}
		if current == "approved" {
			if err = s.authorizeRunTool(ctx, conversationID, runID, name, args); err != nil {
				return err
			}
			consumed, consumeErr := s.consumePermission(ctx, conversationID, runID, id, digest)
			if consumeErr != nil {
				return consumeErr
			}
			if consumed {
				return nil
			}
			continue
		}
		if current != "pending" && current != "reviewing" {
			return fmt.Errorf("operation %s was not authorized; do not retry or use another tool to perform the denied operation", name)
		}
		if mode == "auto" && name == "bash" && reviewDecision == nil {
			token, reviewRevision, claimErr := s.claimPermissionReview(ctx, conversationID, runID, id)
			if claimErr != nil {
				return claimErr
			}
			if token != uuid.Nil {
				if err = s.reviewPermission(ctx, conversationID, runID, id, token, reviewRevision, digest, args); err != nil {
					return err
				}
				continue
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (s *Service) runPermissions(ctx context.Context, workspaceID, conversationID uuid.UUID) ([]RunPermission, error) {
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE id=$1 AND workspace_id=$2)`, conversationID, workspaceID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrInteractionNotFound
	}
	rows, err := s.db.Query(ctx, `SELECT p.id,p.run_id,p.tool,p.request_digest,p.summary,p.allow_run,CASE WHEN p.status IN ('pending','reviewing') AND r.status!='running' THEN 'cancelled' ELSE p.status END,p.decision,p.decision_source,p.review_decision,p.review_reason,p.reviewer_model_deployment_id,p.permission_revision FROM run_permissions p JOIN runs r ON r.id=p.run_id WHERE p.workspace_id=$1 AND p.conversation_id=$2 ORDER BY p.created_at DESC LIMIT 30`, workspaceID, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []RunPermission{}
	for rows.Next() {
		var item RunPermission
		if err = rows.Scan(&item.ID, &item.RunID, &item.Tool, &item.RequestDigest, &item.Summary, &item.AllowRun, &item.Status, &item.Decision, &item.DecisionSource, &item.ReviewDecision, &item.ReviewReason, &item.ReviewerModelID, &item.Revision); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func (s *Service) runPermission(ctx context.Context, workspaceID, conversationID, id uuid.UUID) (*RunPermission, error) {
	item := &RunPermission{ID: id}
	err := s.db.QueryRow(ctx, `SELECT p.run_id,p.tool,p.request_digest,p.summary,p.allow_run,CASE WHEN p.status IN ('pending','reviewing') AND r.status!='running' THEN 'cancelled' ELSE p.status END,p.decision,p.decision_source,p.review_decision,p.review_reason,p.reviewer_model_deployment_id,p.permission_revision FROM run_permissions p JOIN runs r ON r.id=p.run_id WHERE p.id=$1 AND p.workspace_id=$2 AND p.conversation_id=$3`, id, workspaceID, conversationID).Scan(&item.RunID, &item.Tool, &item.RequestDigest, &item.Summary, &item.AllowRun, &item.Status, &item.Decision, &item.DecisionSource, &item.ReviewDecision, &item.ReviewReason, &item.ReviewerModelID, &item.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInteractionNotFound
	}
	return item, err
}

func (s *Service) respondPermission(ctx context.Context, workspaceID, userID, conversationID, id uuid.UUID, digest, decision string) (*RunPermission, error) {
	if decision != "once" && decision != "run" && decision != "reject" && decision != "full" {
		return nil, errors.New("invalid permission decision")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var allowed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations c JOIN workspace_members wm ON wm.workspace_id=c.workspace_id JOIN users u ON u.id=wm.user_id WHERE c.id=$1 AND c.workspace_id=$2 AND wm.user_id=$3 AND NOT u.disabled)`, conversationID, workspaceID, userID).Scan(&allowed); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrInteractionNotFound
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM conversations WHERE id=$1 FOR UPDATE`, conversationID); err != nil {
		return nil, err
	}
	var runID uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT run_id FROM run_permissions WHERE id=$1 AND workspace_id=$2 AND conversation_id=$3`, id, workspaceID, conversationID).Scan(&runID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInteractionNotFound
		}
		return nil, err
	}
	var status, mode string
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT status,execution_mode,permission_revision FROM runs WHERE id=$1 FOR UPDATE`, runID).Scan(&status, &mode, &revision); err != nil {
		return nil, err
	}
	item := &RunPermission{ID: id, RunID: runID}
	if err = tx.QueryRow(ctx, `SELECT tool,request_digest,summary,allow_run,status,decision,decision_source,review_decision,review_reason,reviewer_model_deployment_id,permission_revision FROM run_permissions WHERE id=$1 FOR UPDATE`, id).Scan(&item.Tool, &item.RequestDigest, &item.Summary, &item.AllowRun, &item.Status, &item.Decision, &item.DecisionSource, &item.ReviewDecision, &item.ReviewReason, &item.ReviewerModelID, &item.Revision); err != nil {
		return nil, err
	}
	if item.Tool == "github_publish" {
		var own bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runs r JOIN users u ON u.id=r.submitted_by JOIN workspace_members wm ON wm.workspace_id=$3 AND wm.user_id=u.id WHERE r.id=$1 AND r.submitted_by=$2 AND NOT u.disabled AND (NOT u.email_verification_required OR u.email_verified))`, runID, userID, workspaceID).Scan(&own); err != nil {
			return nil, err
		}
		if !own {
			return nil, ErrInteractionNotFound
		}
		if decision != "once" && decision != "reject" {
			return nil, ErrInteractionConflict
		}
	}
	if digest != item.RequestDigest || decision == "run" && !item.AllowRun {
		return nil, ErrInteractionConflict
	}
	target := "approved"
	if decision == "reject" {
		target = "rejected"
	}
	if item.Status != "pending" {
		if item.Status == target && item.Decision != nil && *item.Decision == decision {
			return item, tx.Commit(ctx)
		}
		return nil, ErrInteractionConflict
	}
	if status != "running" || mode == "plan" && !agenttool.PlanReadOnlyTool(item.Tool) {
		return nil, ErrInteractionConflict
	}
	if decision == "full" {
		revision++
		if err = setRunPermissionModeTx(ctx, tx, conversationID, runID, userID, "full", revision); err != nil {
			return nil, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE run_permissions SET status=$2,decision=$3,decision_source='user',responded_by=$4 WHERE id=$1`, id, target, decision, userID); err != nil {
		return nil, err
	}
	if decision == "full" {
		if err = eventlog.Append(ctx, tx, runID, conversationID, "PERMISSION_MODE_CHANGED", map[string]any{"permission_mode": "full", "revision": revision, "changed_by": userID}); err != nil {
			return nil, err
		}
	}
	if err = eventlog.Append(ctx, tx, runID, conversationID, "PERMISSION_RESPONDED", map[string]any{"permission_id": id, "status": target}); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	item.Status = target
	item.Decision = &decision
	item.DecisionSource = "user"
	item.Revision = revision
	return item, nil
}
