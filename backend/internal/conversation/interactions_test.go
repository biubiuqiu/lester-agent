package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
)

func recordedInteraction(t *testing.T, f transcriptFixture, runID uuid.UUID, kind string) (*RunInteraction, agenttool.InteractionRequest) {
	t.Helper()
	request := agenttool.InteractionRequest{Kind: "question", Questions: []agenttool.UserQuestion{{ID: "choice", Prompt: "Which result do you need?", Options: []agenttool.QuestionOption{{ID: "html", Label: "HTML"}, {ID: "document", Label: "Document"}}}}}
	name := "ask_user_question"
	var args any = map[string]any{"questions": request.Questions}
	if kind == "plan" {
		request = agenttool.InteractionRequest{Kind: "plan", Title: "Review before changing files", Steps: []string{"Read the current file", "Make the requested change", "Verify the result"}}
		name = "propose_plan"
		args = map[string]any{"title": request.Title, "steps": request.Steps}
	}
	raw, _ := json.Marshal(args)
	call := model.ToolCall{ID: uuid.NewString(), Name: name, Arguments: raw}
	if err := f.service.appendMessage(context.Background(), f.conversationID, runID, model.Message{Role: "assistant", ToolCalls: []model.ToolCall{call}}, "", nil); err != nil {
		t.Fatal(err)
	}
	item, err := f.service.createRunInteraction(context.Background(), f.conversationID, runID, call.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	return item, request
}

func TestQuestionAnswerIsScopedIdempotentAndNeverApprovesExecution(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	runID := f.startRun(t, "plan only")
	if _, err := f.service.db.Exec(ctx, `UPDATE runs SET execution_mode='plan' WHERE id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	item, _ := recordedInteraction(t, f, runID, "question")
	response := InteractionResponse{RequestDigest: item.RequestDigest, Answers: map[string]QuestionAnswer{"choice": {OptionID: "html", Text: "approve"}}}
	for _, scope := range [][3]uuid.UUID{{uuid.New(), f.userID, f.conversationID}, {f.workspaceID, uuid.New(), f.conversationID}, {f.workspaceID, f.userID, uuid.New()}} {
		if _, err := f.service.RespondRunInteraction(ctx, scope[0], scope[1], scope[2], item.ID, response); !errors.Is(err, ErrInteractionNotFound) {
			t.Fatalf("foreign response: %v", err)
		}
	}
	if _, err := f.service.RespondRunInteraction(ctx, f.workspaceID, f.userID, f.conversationID, item.ID, InteractionResponse{RequestDigest: item.RequestDigest, Answers: map[string]QuestionAnswer{"choice": {OptionID: "unoffered"}}}); err == nil {
		t.Fatal("unoffered answer accepted")
	}
	for i := 0; i < 2; i++ {
		got, err := f.service.RespondRunInteraction(ctx, f.workspaceID, f.userID, f.conversationID, item.ID, response)
		if err != nil || got.Status != "answered" {
			t.Fatalf("answer: %#v %v", got, err)
		}
	}
	var mode string
	if err := f.service.db.QueryRow(ctx, `SELECT execution_mode FROM runs WHERE id=$1`, runID).Scan(&mode); err != nil || mode != "plan" {
		t.Fatal("question answer enabled execution")
	}
	response.Answers["choice"] = QuestionAnswer{OptionID: "document"}
	if _, err := f.service.RespondRunInteraction(ctx, f.workspaceID, f.userID, f.conversationID, item.ID, response); !errors.Is(err, ErrInteractionConflict) {
		t.Fatal("changed response overwrote answer")
	}
	items, _, err := f.service.RunInteractions(ctx, f.workspaceID, f.conversationID)
	if err != nil || len(items) != 1 || items[0].Status != "answered" {
		t.Fatal("answer did not survive reload")
	}
	var events int
	if err = f.service.db.QueryRow(ctx, `SELECT count(*) FROM run_events WHERE run_id=$1 AND type='INTERACTION_RESPONDED'`, runID).Scan(&events); err != nil || events != 1 {
		t.Fatal("answer retry emitted duplicate events")
	}
}

func TestPlanApprovalBindsVersionAndRunAndGateIsFresh(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	runID := f.startRun(t, "plan before any changes")
	if _, err := f.service.db.Exec(ctx, `UPDATE runs SET execution_mode='plan' WHERE id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bash", "write", "edit", "stop_task", "deploy_html", "save_agent", "register_deliverable", "custom_tool"} {
		if err := f.service.authorizeRunTool(ctx, f.conversationID, runID, name, nil); err == nil {
			t.Fatalf("Plan permitted %s", name)
		}
	}
	for _, name := range []string{"read", "list_files", "load_skill", "task_output", "ask_user_question", "propose_plan"} {
		if err := f.service.authorizeRunTool(ctx, f.conversationID, runID, name, nil); err != nil {
			t.Fatalf("Plan blocked %s: %v", name, err)
		}
	}
	item, _ := recordedInteraction(t, f, runID, "plan")
	if _, err := f.service.RespondRunInteraction(ctx, f.workspaceID, f.userID, f.conversationID, item.ID, InteractionResponse{RequestDigest: "wrong-version", Decision: "approve"}); !errors.Is(err, ErrInteractionConflict) {
		t.Fatal("stale approval accepted")
	}
	response := InteractionResponse{RequestDigest: item.RequestDigest, Decision: "approve"}
	for i := 0; i < 2; i++ {
		if _, err := f.service.RespondRunInteraction(ctx, f.workspaceID, f.userID, f.conversationID, item.ID, response); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.service.authorizeRunTool(ctx, f.conversationID, runID, "write", nil); err != nil {
		t.Fatal("approved run remained read-only")
	}
	var mode, digest string
	if err := f.service.db.QueryRow(ctx, `SELECT execution_mode,approved_plan_digest FROM runs WHERE id=$1`, runID).Scan(&mode, &digest); err != nil || mode != "execute" || digest != item.RequestDigest {
		t.Fatal("approval was not bound to exact plan")
	}
	if err := f.service.finishCompletedRun(ctx, runID, f.conversationID); err != nil {
		t.Fatal(err)
	}
	next := f.startRun(t, "another plan")
	if _, err := f.service.db.Exec(ctx, `UPDATE runs SET execution_mode='plan' WHERE id=$1`, next); err != nil {
		t.Fatal(err)
	}
	if err := f.service.authorizeRunTool(ctx, f.conversationID, next, "write", nil); err == nil {
		t.Fatal("old approval authorized a new run")
	}
}

func TestCancellationAndRecoveryRevokePendingInteractions(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "crash-recovery"}[recovery], func(t *testing.T) {
			f := newTranscriptFixture(t, false)
			ctx := context.Background()
			runID := f.startRun(t, "plan")
			if _, err := f.service.db.Exec(ctx, `UPDATE runs SET execution_mode='plan' WHERE id=$1`, runID); err != nil {
				t.Fatal(err)
			}
			item, _ := recordedInteraction(t, f, runID, "plan")
			var err error
			if recovery {
				err = f.service.recoverInterruptedRuns(ctx, f.conversationID)
			} else {
				err = f.service.CancelRun(ctx, f.workspaceID, f.conversationID, runID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.service.RespondRunInteraction(ctx, f.workspaceID, f.userID, f.conversationID, item.ID, InteractionResponse{RequestDigest: item.RequestDigest, Decision: "approve"}); !errors.Is(err, ErrInteractionConflict) {
				t.Fatal("cancelled interaction authorized execution")
			}
			items, _, err := f.service.RunInteractions(ctx, f.workspaceID, f.conversationID)
			if err != nil || len(items) != 1 || items[0].Status != "cancelled" {
				t.Fatal("pending interaction not revoked")
			}
		})
	}
}

func TestPendingQuestionRefreshDetectsAbandonedOwnerWithoutReplaying(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	guard, err := f.service.acquireRun(ctx, f.workspaceID, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(guard.Close)
	runID := f.startRun(t, "clarify")
	item, _ := recordedInteraction(t, f, runID, "question")
	if err = f.service.reconcileInteractionOwner(ctx, f.workspaceID, f.conversationID); err != nil {
		t.Fatal(err)
	}
	current, err := f.service.RunInteraction(ctx, f.workspaceID, f.conversationID, item.ID)
	if err != nil || current.Status != "pending" {
		t.Fatal("another replica's session guard was ignored")
	}
	guard.Close()
	if err = f.service.reconcileInteractionOwner(ctx, f.workspaceID, f.conversationID); err != nil {
		t.Fatal(err)
	}
	current, err = f.service.RunInteraction(ctx, f.workspaceID, f.conversationID, item.ID)
	if err != nil || current.Status != "cancelled" {
		t.Fatal("abandoned question remained actionable")
	}
	var state string
	if err = f.service.db.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1`, runID).Scan(&state); err != nil || state != "failed" {
		t.Fatal("abandoned run not reconciled")
	}
}

func TestPlanToolsCannotExecuteAliasesAndApprovalContinuesSameRun(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	server, files := newTranscriptSandboxServer(t, map[string]string{"old.txt": "original"})
	f.service.sandboxes = sandbox.NewClient(server.URL, "")
	runID := f.startRun(t, "plan first")
	if _, err := f.service.db.Exec(ctx, `UPDATE runs SET execution_mode='plan' WHERE id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	env := agenttool.Environment{RunID: runID, ConversationID: f.conversationID, SandboxID: "test", WorkDir: conversationWorkDir(f.conversationID), Sandboxes: f.service.sandboxes, AuthorizeTool: func(ctx context.Context, name string, args json.RawMessage) error {
		return f.service.authorizeRunTool(ctx, f.conversationID, runID, name, args)
	}}
	if _, err := f.service.tools.Execute(ctx, "computer_write_file", json.RawMessage(`{"file_path":"old.txt","content":"bad"}`), env); err == nil || files.content("old.txt") != "original" {
		t.Fatal("alias bypassed Plan gate")
	}
	steps := []string{"Create the requested file", "Verify it"}
	client := &scriptedModel{respond: func(step int, req model.ModelRequest) []model.ModelEvent {
		if step == 0 {
			if slices.ContainsFunc(req.Tools, func(tool model.Tool) bool { return tool.Name == "bash" || tool.Name == "write" }) || !slices.ContainsFunc(req.Tools, func(tool model.Tool) bool { return tool.Name == "list_files" }) {
				t.Fatal("Plan schemas expose mutations or cannot list files")
			}
			raw, _ := json.Marshal(map[string]any{"title": "Create result", "steps": steps})
			return []model.ModelEvent{{ToolCall: &model.ToolCall{ID: "plan-execution", Name: "propose_plan", Arguments: raw}}}
		}
		if !slices.ContainsFunc(req.Tools, func(tool model.Tool) bool { return tool.Name == "write" }) || slices.ContainsFunc(req.Tools, func(tool model.Tool) bool { return tool.Name == "propose_plan" }) {
			t.Fatal("approval did not restore normal schemas")
		}
		if step == 1 {
			return []model.ModelEvent{{ToolCall: &model.ToolCall{ID: "create", Name: "write", Arguments: json.RawMessage(`{"file_path":"new.txt","content":"approved result"}`)}}}
		}
		return []model.ModelEvent{{Delta: "done"}}
	}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.service.executor.executeTurns(ctx, f.conversationID, runID, &Computer{SandboxID: "test", WorkDir: env.WorkDir}, client, model.ModelRequest{Messages: modelHistory(f.messages(t)), Tools: f.service.tools.Definitions()})
	}()
	deadline := time.Now().Add(5 * time.Second)
	var pending *RunInteraction
	for time.Now().Before(deadline) {
		items, _, err := f.service.RunInteractions(ctx, f.workspaceID, f.conversationID)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) > 0 {
			pending = &items[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pending == nil {
		t.Fatal("plan never became pending")
	}
	if files.content("new.txt") != "" {
		t.Fatal("file created before approval")
	}
	if _, err := f.service.RespondRunInteraction(ctx, f.workspaceID, f.userID, f.conversationID, pending.ID, InteractionResponse{RequestDigest: pending.RequestDigest, Decision: "approve"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("approved plan did not continue")
	}
	if files.content("new.txt") != "approved result" || client.step != 3 {
		t.Fatal("approved execution was not observed")
	}
}
