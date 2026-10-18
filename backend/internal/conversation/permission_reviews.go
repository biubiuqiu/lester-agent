package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/biubiuqiu/lester-agent/backend/internal/eventlog"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/biubiuqiu/lester-agent/backend/internal/toolcontext"
	"github.com/biubiuqiu/lester-agent/backend/prompts"
	"github.com/google/uuid"
)

const permissionReviewTimeout = 25 * time.Second

type permissionReviewModelKey struct{}
type permissionReviewModel struct {
	run, workspace uuid.UUID
	client         model.ModelClient
	deployment     model.Deployment
}

type commandReview struct {
	Decision      string `json:"decision"`
	Reason        string `json:"reason"`
	RequestDigest string `json:"request_digest"`
}

func permissionReviewSchema(digest string) *model.JSONSchema {
	return &model.JSONSchema{Name: "command_permission_review", Schema: map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"decision", "reason", "request_digest"},
		"properties": map[string]any{
			"decision":       map[string]any{"type": "string", "enum": []string{"allow", "deny", "ask_user"}},
			"reason":         map[string]any{"type": "string"},
			"request_digest": map[string]any{"type": "string", "enum": []string{digest}},
		},
	}}
}

// Validate the entire JSON object independently of provider enforcement. Reject
// duplicate keys, missing/extra fields, tool calls, prose and trailing objects.
func parseCommandReview(response *model.ModelResponse, digest string) (commandReview, error) {
	var result commandReview
	if response == nil || len(response.ToolCalls) != 0 || len(response.Content) > 4096 || !utf8.ValidString(response.Content) {
		return result, errors.New("invalid review response")
	}
	decoder := json.NewDecoder(strings.NewReader(response.Content))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return result, errors.New("review must be a JSON object")
	}
	fields := map[string]string{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return result, err
		}
		name, ok := key.(string)
		if !ok {
			return result, errors.New("invalid review field")
		}
		if _, duplicate := fields[name]; duplicate {
			return result, errors.New("duplicate review field")
		}
		if name != "decision" && name != "reason" && name != "request_digest" {
			return result, errors.New("unexpected review field")
		}
		var value string
		if err = decoder.Decode(&value); err != nil {
			return result, err
		}
		fields[name] = value
	}
	if last, err := decoder.Token(); err != nil || last != json.Delim('}') {
		return result, errors.New("invalid review object")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return result, errors.New("trailing review content")
	}
	result = commandReview{Decision: fields["decision"], Reason: fields["reason"], RequestDigest: fields["request_digest"]}
	if len(fields) != 3 || result.RequestDigest != digest || strings.TrimSpace(result.Reason) == "" || utf8.RuneCountInString(result.Reason) > 300 {
		return commandReview{}, errors.New("review does not match schema or operation")
	}
	if result.Decision != "allow" && result.Decision != "deny" && result.Decision != "ask_user" {
		return commandReview{}, errors.New("unknown review decision")
	}
	return result, nil
}

// Claim once under conversation -> run locks. A mode change clears the claim;
// a stale or cancelled reviewer can never decide an operation under a new mode.
func (s *Service) claimPermissionReview(ctx context.Context, conversation, run, id uuid.UUID) (uuid.UUID, int64, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, 0, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM conversations WHERE id=$1 FOR UPDATE`, conversation); err != nil {
		return uuid.Nil, 0, err
	}
	var revision int64
	var mode, status string
	if err = tx.QueryRow(ctx, `SELECT permission_mode,permission_revision,status FROM runs WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, run, conversation).Scan(&mode, &revision, &status); err != nil {
		return uuid.Nil, 0, err
	}
	if mode != "auto" || status != "running" {
		return uuid.Nil, revision, tx.Commit(ctx)
	}
	token := uuid.New()
	tag, err := tx.Exec(ctx, `UPDATE run_permissions SET status='reviewing',review_token=$2 WHERE id=$1 AND run_id=$3 AND tool='bash' AND permission_revision=$4 AND review_decision IS NULL AND review_token IS NULL AND status IN ('pending','reviewing')`, id, token, run, revision)
	if err != nil {
		return uuid.Nil, 0, err
	}
	if tag.RowsAffected() == 0 {
		return uuid.Nil, revision, tx.Commit(ctx)
	}
	if err = eventlog.Append(ctx, tx, run, conversation, "PERMISSION_REVIEW_STARTED", map[string]any{"permission_id": id}); err != nil {
		return uuid.Nil, 0, err
	}
	return token, revision, tx.Commit(ctx)
}

func (s *Service) reviewPermission(ctx context.Context, conversation, run, id, token uuid.UUID, revision int64, digest string, args json.RawMessage) error {
	reviewCtx, cancel := context.WithTimeout(ctx, permissionReviewTimeout)
	// Stop the model request promptly on Stop, adjustment or a permission change,
	// including a change made by a different API replica or browser window.
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-reviewCtx.Done():
				return
			case <-ticker.C:
			}
			var current bool
			err := s.db.QueryRow(reviewCtx, `SELECT EXISTS(SELECT 1 FROM runs r JOIN run_permissions p ON p.run_id=r.id WHERE r.id=$1 AND r.conversation_id=$2 AND r.status='running' AND r.permission_mode='auto' AND r.permission_revision=$3 AND p.id=$4 AND p.status='reviewing' AND p.review_token=$5)`, run, conversation, revision, id, token).Scan(&current)
			if err != nil || !current {
				cancel()
				return
			}
		}
	}()
	decision, reviewer := s.generatePermissionReview(reviewCtx, conversation, run, id, token, digest, args)
	cancel()
	<-monitorDone
	if ctx.Err() != nil {
		return ctx.Err()
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM conversations WHERE id=$1 FOR UPDATE`, conversation); err != nil {
		return err
	}
	var currentRevision int64
	var mode, status string
	if err = tx.QueryRow(ctx, `SELECT permission_mode,permission_revision,status FROM runs WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, run, conversation).Scan(&mode, &currentRevision, &status); err != nil {
		return err
	}
	if mode != "auto" || status != "running" || currentRevision != revision {
		return tx.Commit(ctx)
	}
	permissionStatus, receipt := "pending", (*string)(nil)
	if decision.Decision == "allow" {
		permissionStatus = "approved"
		once := "once"
		receipt = &once
	}
	if decision.Decision == "deny" {
		permissionStatus = "rejected"
		reject := "reject"
		receipt = &reject
	}
	tag, err := tx.Exec(ctx, `UPDATE run_permissions SET status=$2,decision=$3,decision_source='llm',review_decision=$4,review_reason=$5,reviewer_model_deployment_id=$6,review_token=NULL WHERE id=$1 AND run_id=$7 AND status='reviewing' AND review_token=$8 AND permission_revision=$9 AND request_digest=$10`, id, permissionStatus, receipt, decision.Decision, decision.Reason, reviewer, run, token, revision, digest)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	if err = eventlog.Append(ctx, tx, run, conversation, "PERMISSION_REVIEW_COMPLETED", map[string]any{"permission_id": id, "decision": decision.Decision, "reviewer_model_deployment_id": reviewer}); err != nil {
		return err
	}
	if permissionStatus == "pending" {
		if err = eventlog.Append(ctx, tx, run, conversation, "PERMISSION_REQUESTED", map[string]any{"permission_id": id, "tool": "bash"}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Service) generatePermissionReview(ctx context.Context, conversation, run, id, token uuid.UUID, digest string, args json.RawMessage) (commandReview, *uuid.UUID) {
	fallback := commandReview{Decision: "ask_user", RequestDigest: digest, Reason: "自动审批不可用，请你确认这次操作。"}
	if len(args) > 32<<10 || !json.Valid(args) {
		fallback.Reason = "审批输入过大或无效，请你确认这次操作。"
		return fallback, nil
	}
	var effectiveArgs map[string]json.RawMessage
	if json.Unmarshal(args, &effectiveArgs) != nil || effectiveArgs == nil {
		return fallback, nil
	}
	var workspace uuid.UUID
	var deploymentID *uuid.UUID
	var goal string
	err := s.db.QueryRow(ctx, `SELECT c.workspace_id,COALESCE(r.model_deployment_id,c.model_deployment_id),COALESCE(m.content,'') FROM runs r JOIN conversations c ON c.id=r.conversation_id LEFT JOIN messages m ON m.id=r.input_message_id AND m.conversation_id=c.id WHERE r.id=$1 AND c.id=$2 AND r.status='running'`, run, conversation).Scan(&workspace, &deploymentID, &goal)
	if err != nil || deploymentID == nil {
		return fallback, nil
	}
	if len(goal) > 16<<10 {
		fallback.Reason = "审批输入过大或无效，请你确认这次操作。"
		return fallback, deploymentID
	}
	var client model.ModelClient
	var deployment model.Deployment
	if accepted, ok := ctx.Value(permissionReviewModelKey{}).(permissionReviewModel); ok && accepted.run == run && accepted.workspace == workspace && accepted.deployment.ID == *deploymentID {
		// Reuse the accepted provider client/configuration, with a completely
		// separate request. Later model edits cannot silently switch reviewers.
		client, deployment = accepted.client, accepted.deployment
	} else {
		clientFor := s.reviewClient
		if clientFor == nil && s.models != nil {
			clientFor = s.models.Client
		}
		if clientFor == nil {
			return fallback, deploymentID
		}
		client, deployment, err = clientFor(ctx, workspace, *deploymentID)
	}
	if err != nil || client == nil {
		return fallback, deploymentID
	}
	// Persist which actual scoped deployment was selected before contacting it.
	tag, err := s.db.Exec(ctx, `UPDATE run_permissions SET reviewer_model_deployment_id=$2 WHERE id=$1 AND review_token=$3 AND status='reviewing'`, id, deploymentID, token)
	if err != nil || tag.RowsAffected() != 1 {
		return fallback, deploymentID
	}
	denied := []json.RawMessage{}
	rows, err := s.db.Query(ctx, `SELECT summary FROM run_permissions WHERE run_id=$1 AND status='rejected' ORDER BY created_at DESC LIMIT 8`, run)
	if err != nil {
		return fallback, deploymentID
	}
	for rows.Next() {
		var summary json.RawMessage
		if err = rows.Scan(&summary); err != nil {
			break
		}
		denied = append(denied, summary)
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil || rowErr != nil {
		return fallback, deploymentID
	}
	adjustments := []string{}
	rows, err = s.db.Query(ctx, `SELECT m.content FROM messages m JOIN runs r ON r.id=m.run_id WHERE m.run_id=$1 AND m.conversation_id=$2 AND m.role='user' AND m.id IS DISTINCT FROM r.input_message_id ORDER BY m.seq LIMIT 9`, run, conversation)
	if err != nil {
		return fallback, deploymentID
	}
	adjustmentBytes := 0
	for rows.Next() {
		var content string
		if err = rows.Scan(&content); err != nil {
			break
		}
		adjustmentBytes += len(content)
		adjustments = append(adjustments, content)
	}
	rowErr = rows.Err()
	rows.Close()
	if err != nil || rowErr != nil || len(adjustments) > 8 || adjustmentBytes > 16<<10 {
		return fallback, deploymentID
	}
	// Decode the same effective argument values the handler executes. In
	// particular, duplicate JSON keys must not confuse the review model.
	data, _ := json.Marshal(map[string]any{"request_digest": digest, "task_goal": goal, "applied_user_adjustments": adjustments, "conversation_directory": conversationWorkDir(conversation), "operation": map[string]any{"tool": "bash", "arguments": effectiveArgs}, "previously_denied_operations": denied})
	if len(data) > 64<<10 {
		return fallback, deploymentID
	}
	system, err := prompts.PermissionReviewer()
	if err != nil {
		return fallback, deploymentID
	}
	request := model.ModelRequest{Model: deployment.ModelID, System: system, Messages: []model.Message{{Role: "user", Content: string(data)}}, ResponseSchema: permissionReviewSchema(digest), MaxTokens: 1024}
	var estimator toolcontext.Estimator
	budget, err := estimator.Evaluate(request, toolcontext.BudgetConfig{ContextWindow: deployment.ContextWindow, OutputReserve: deployment.OutputReserve, PolicyWindow: deployment.PolicyWindow})
	if err != nil || budget.Exceeded {
		return fallback, deploymentID
	}
	index, err := s.startModelRequest(ctx, run, "permission_review", budget)
	if err != nil {
		return fallback, deploymentID
	}
	response, requestErr := client.Generate(ctx, request)
	if requestErr == nil && ctx.Err() != nil {
		requestErr = ctx.Err()
	}
	var review commandReview
	if requestErr == nil {
		review, requestErr = parseCommandReview(response, digest)
	}
	// Preserve provider usage even when the response fails schema validation.
	var usage *modelruntime.NormalizedUsage
	if response != nil {
		usage = response.NormalizedUsage
	}
	if err = s.finishModelRequest(ctx, run, index, usage, requestErr); err != nil {
		return fallback, deploymentID
	}
	if requestErr != nil {
		fallback.Reason = "自动审批超时或返回格式无效，请你确认这次操作。"
		return fallback, deploymentID
	}
	return review, deploymentID
}
