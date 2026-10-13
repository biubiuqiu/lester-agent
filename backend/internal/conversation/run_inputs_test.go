package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/reference"
	"github.com/google/uuid"
)

func TestRunInputReceiptIsScopedAndConcurrentRetryDoesNotDuplicate(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	runID := f.startRun(t, "start")
	req := RunInputRequest{ID: uuid.New(), RunID: runID, Kind: "adjust", Content: "保留的调整🙂"}
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, req)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := f.service.db.QueryRow(ctx, `SELECT count(*) FROM run_inputs WHERE after_run_id=$1`, runID).Scan(&count); err != nil || count != 1 {
		t.Fatal("retry duplicated input")
	}
	changed := req
	changed.Content = "different"
	if _, err := f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, changed); !errors.Is(err, ErrRunInputConflict) {
		t.Fatal("changed input reused receipt")
	}
	for _, scope := range [][3]uuid.UUID{{uuid.New(), f.userID, f.conversationID}, {f.workspaceID, uuid.New(), f.conversationID}, {f.workspaceID, f.userID, uuid.New()}} {
		if _, err := f.service.RunInput(ctx, scope[0], scope[1], scope[2], req.ID); !errors.Is(err, ErrRunInputNotFound) {
			t.Fatal("foreign actor got receipt")
		}
	}
	withReference := req
	withReference.ID = uuid.New()
	withReference.References = []reference.Input{{Type: "conversation", ID: uuid.New()}}
	if _, err := f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, withReference); !errors.Is(err, ErrRunInputRejected) {
		t.Fatal("adjustment granted a new reference")
	}
	withMode := req
	withMode.ID = uuid.New()
	withMode.ExecutionMode = "execute"
	if _, err := f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, withMode); !errors.Is(err, ErrRunInputRejected) {
		t.Fatal("adjustment changed mode")
	}
}

func TestAdjustmentInFinalResponseCannotBeLostAtCompletion(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	runID := f.startRun(t, "initial")
	request := model.ModelRequest{Messages: modelHistory(f.messages(t))}
	var accepted *RunInput
	client := &scriptedModel{respond: func(step int, req model.ModelRequest) []model.ModelEvent {
		if step == 0 {
			var err error
			accepted, err = f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, RunInputRequest{ID: uuid.New(), RunID: runID, Kind: "adjust", Content: "Actually include the extra result🙂"})
			if err != nil {
				t.Fatal(err)
			}
			return []model.ModelEvent{{Delta: "first response"}}
		}
		if step != 1 || req.Messages[len(req.Messages)-1].Role != "user" || req.Messages[len(req.Messages)-1].Content != "Actually include the extra result🙂" {
			t.Fatal("accepted adjustment did not become literal input in same run")
		}
		return []model.ModelEvent{{Delta: "adjusted response"}}
	}}
	f.service.executor.executeTurns(ctx, f.conversationID, runID, nil, client, request)
	if client.step != 2 {
		t.Fatal("final response discarded pending adjustment")
	}
	item, err := f.service.RunInput(ctx, f.workspaceID, f.userID, f.conversationID, accepted.ID)
	if err != nil || item.Status != "applied" || item.MessageID == nil {
		t.Fatal("applied receipt missing")
	}
	audit := f.messages(t)
	if len(audit) != 4 || audit[2].Role != "user" || audit[2].RunID == nil || *audit[2].RunID != runID || audit[2].Seq != 3 {
		t.Fatal("adjustment broke order or created another run")
	}
	if err = f.service.finishCompletedRun(ctx, runID, f.conversationID); err != nil {
		t.Fatal(err)
	}
}

func TestAppliedAdjustmentsStayOrderedAndDoNotChangeOriginalGrants(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	runID := f.startRun(t, "plan")
	if _, err := f.service.db.Exec(ctx, `UPDATE runs SET execution_mode='plan' WHERE id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"one", "two", "three"} {
		if _, err := f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, RunInputRequest{ID: uuid.New(), RunID: runID, Kind: "adjust", Content: content}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.service.finishCompletedRun(ctx, runID, f.conversationID); !errors.Is(err, ErrPendingRunInputs) {
		t.Fatal("completion raced past accepted inputs")
	}
	messages, err := f.service.applyRunAdjustments(ctx, f.conversationID, runID)
	if err != nil || len(messages) != 3 {
		t.Fatal("adjustments missing")
	}
	for i, want := range []string{"one", "two", "three"} {
		if messages[i].Content != want || messages[i].Seq != int64(i+2) {
			t.Fatal("acceptance order changed")
		}
	}
	if again, err := f.service.applyRunAdjustments(ctx, f.conversationID, runID); err != nil || len(again) != 0 {
		t.Fatal("adjustments replayed")
	}
	var mode string
	var inputID uuid.UUID
	if err = f.service.db.QueryRow(ctx, `SELECT execution_mode,input_message_id FROM runs WHERE id=$1`, runID).Scan(&mode, &inputID); err != nil || mode != "plan" || inputID != f.messages(t)[0].ID {
		t.Fatal("adjustment changed Plan or initial @ authority")
	}
}

func TestQueueClaimPinsSnapshotAndStartsOnceAfterPredecessor(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	parent := f.startRun(t, "first")
	selected := uuid.New()
	alternative, connection := uuid.New(), uuid.New()
	if _, err := f.service.db.Exec(ctx, `INSERT INTO model_connections(id,workspace_id,name,provider,protocol) VALUES($1,$2,'queue-fixture','openai','openai')`, connection, f.workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.db.Exec(ctx, `INSERT INTO model_deployments(id,workspace_id,connection_id,name,model_id) VALUES($1,$3,$4,'selected','selected'),($2,$3,$4,'alternative','alternative')`, selected, alternative, f.workspaceID, connection); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.db.Exec(ctx, `UPDATE conversations SET model_deployment_id=$2 WHERE id=$1`, f.conversationID, selected); err != nil {
		t.Fatal(err)
	}
	first, err := f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, RunInputRequest{ID: uuid.New(), RunID: parent, Kind: "next", Content: "queued first", ExecutionMode: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, RunInputRequest{ID: uuid.New(), RunID: parent, Kind: "next", Content: "queued second"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.claimQueuedInput(ctx, f.workspaceID, f.conversationID); !errors.Is(err, ErrRunInProgress) {
		t.Fatal("queued work overlapped predecessor")
	}
	if err = f.service.finishCompletedRun(ctx, parent, f.conversationID); err != nil {
		t.Fatal(err)
	}
	guard, err := f.service.acquireRun(ctx, f.workspaceID, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if _, err = f.service.db.Exec(ctx, `UPDATE conversations SET model_deployment_id=$2 WHERE id=$1`, f.conversationID, alternative); err != nil {
		t.Fatal(err)
	}
	started, err := f.service.claimQueuedInput(ctx, f.workspaceID, f.conversationID)
	if err != nil || started == uuid.Nil {
		t.Fatalf("claim: %s %v", started, err)
	}
	var mode string
	var modelID, submission uuid.UUID
	if err = f.service.db.QueryRow(ctx, `SELECT execution_mode,model_deployment_id,submission_id FROM runs WHERE id=$1`, started).Scan(&mode, &modelID, &submission); err != nil || mode != "plan" || modelID != selected || submission != first.ID {
		t.Fatal("queued snapshot changed at dispatch")
	}
	if _, err = f.service.claimQueuedInput(ctx, f.workspaceID, f.conversationID); !errors.Is(err, ErrRunInProgress) {
		t.Fatal("claimed same queued task twice")
	}
	if err = f.service.finishFailedRun(ctx, started, f.conversationID, "fixture fail"); err != nil {
		t.Fatal(err)
	}
	item, err := f.service.RunInput(ctx, f.workspaceID, f.userID, f.conversationID, second.ID)
	if err != nil || item.Status != "held" {
		t.Fatal("failure started subsequent work")
	}
	if next, err := f.service.claimQueuedInput(ctx, f.workspaceID, f.conversationID); err != nil || next != uuid.Nil {
		t.Fatal("held queue launched automatically")
	}
	var metadata []byte
	if err = f.service.db.QueryRow(ctx, `SELECT metadata FROM messages WHERE run_id=$1 AND role='user'`, started).Scan(&metadata); err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if json.Unmarshal(metadata, &values) != nil || values["input_kind"] != "next" {
		t.Fatal("queued message lost source receipt")
	}
}

func TestCancellationHoldsInputsAndManualCancelNeverApplies(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	runID := f.startRun(t, "first")
	adjust, err := f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, RunInputRequest{ID: uuid.New(), RunID: runID, Kind: "adjust", Content: "later"})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, RunInputRequest{ID: uuid.New(), RunID: runID, Kind: "next", Content: "next"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.ChangeRunInput(ctx, f.workspaceID, f.userID, f.conversationID, adjust.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	if applied, err := f.service.applyRunAdjustments(ctx, f.conversationID, runID); err != nil || len(applied) != 0 {
		t.Fatal("cancelled input applied")
	}
	if err = f.service.CancelRun(ctx, f.workspaceID, f.conversationID, runID); err != nil {
		t.Fatal(err)
	}
	item, err := f.service.RunInput(ctx, f.workspaceID, f.userID, f.conversationID, queued.ID)
	if err != nil || item.Status != "held" {
		t.Fatal("Stop did not hold future work")
	}
}

func TestQueuedContextSnapshotAndRevokedActorCannotSilentlyStart(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	parent := f.startRun(t, "first")
	entry := uuid.New()
	if _, err := f.service.db.Exec(ctx, `INSERT INTO context_entries(id,workspace_id,title,content) VALUES($1,$2,'queue-context','original accepted content')`, entry, f.workspaceID); err != nil {
		t.Fatal(err)
	}
	item, err := f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, RunInputRequest{ID: uuid.New(), RunID: parent, Kind: "next", Content: "use accepted context", ContextIDs: []uuid.UUID{entry}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.db.Exec(ctx, `UPDATE context_entries SET content='later edit',version=2 WHERE id=$1`, entry); err != nil {
		t.Fatal(err)
	}
	if err = f.service.finishCompletedRun(ctx, parent, f.conversationID); err != nil {
		t.Fatal(err)
	}
	guard, err := f.service.acquireRun(ctx, f.workspaceID, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	started, err := f.service.claimQueuedInput(ctx, f.workspaceID, f.conversationID)
	if err != nil || started == uuid.Nil {
		t.Fatal(err)
	}
	var metadata []byte
	if err = f.service.db.QueryRow(ctx, `SELECT metadata FROM messages WHERE run_id=$1 AND role='user'`, started).Scan(&metadata); err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Contexts []struct {
			Content string
			Version int
		}
	}
	if json.Unmarshal(metadata, &snapshot) != nil || len(snapshot.Contexts) != 1 || snapshot.Contexts[0].Content != "original accepted content" || snapshot.Contexts[0].Version != 1 {
		t.Fatalf("accepted snapshot changed: %s", metadata)
	}
	next, err := f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, RunInputRequest{ID: uuid.New(), RunID: started, Kind: "next", Content: "must not execute after disable"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.finishCompletedRun(ctx, started, f.conversationID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.db.Exec(ctx, `UPDATE users SET disabled=true WHERE id=$1`, f.userID); err != nil {
		t.Fatal(err)
	}
	if run, err := f.service.claimQueuedInput(ctx, f.workspaceID, f.conversationID); err != nil || run != uuid.Nil {
		t.Fatal("revoked actor launched work", err)
	}
	revoked, err := f.service.RunInput(ctx, f.workspaceID, f.userID, f.conversationID, next.ID)
	if err != nil || revoked.Status != "held" || revoked.HeldReason == nil || *revoked.HeldReason != "authorization_changed" {
		t.Fatal("revocation not reflected in receipt", err)
	}
	original, err := f.service.RunInput(ctx, f.workspaceID, f.userID, f.conversationID, item.ID)
	if err != nil || original.Status != "started" {
		t.Fatal(err)
	}
}

func TestAdjustmentSupersedesPendingQuestionWithoutApprovingPlan(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	run := f.startRun(t, "plan")
	if _, err := f.service.db.Exec(ctx, `UPDATE runs SET execution_mode='plan' WHERE id=$1`, run); err != nil {
		t.Fatal(err)
	}
	question, _ := recordedInteraction(t, f, run, "question")
	if _, err := f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, RunInputRequest{ID: uuid.New(), RunID: run, Kind: "adjust", Content: "Use the new requirement instead"}); err != nil {
		t.Fatal(err)
	}
	current, err := f.service.RunInteraction(ctx, f.workspaceID, f.conversationID, question.ID)
	if err != nil || current.Status != "cancelled" {
		t.Fatal("obsolete question remained pending", err)
	}
	if err = f.service.authorizeRunTool(ctx, f.conversationID, run, "bash", nil); err == nil {
		t.Fatal("adjustment granted execution")
	}
	if _, err = f.service.RespondRunInteraction(ctx, f.workspaceID, f.userID, f.conversationID, question.ID, InteractionResponse{RequestDigest: question.RequestDigest, Answers: map[string]QuestionAnswer{"choice": {OptionID: "html"}}}); !errors.Is(err, ErrInteractionConflict) {
		t.Fatal("old question response accepted", err)
	}
}

func TestExplicitQueueResumeAndCancellationRespectIdleAndFIFO(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	parent := f.startRun(t, "start")
	first, err := f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, RunInputRequest{ID: uuid.New(), RunID: parent, Kind: "next", Content: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, RunInputRequest{ID: uuid.New(), RunID: parent, Kind: "next", Content: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.finishFailedRun(ctx, parent, f.conversationID, "interrupted"); err != nil {
		t.Fatal(err)
	}
	f.service.executor.mu.Lock()
	f.service.executor.stopping = true
	f.service.executor.mu.Unlock()
	if _, err = f.service.ChangeRunInput(ctx, f.workspaceID, f.userID, f.conversationID, first.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if item, err := f.service.ChangeRunInput(ctx, f.workspaceID, f.userID, f.conversationID, second.ID, "resume"); err != nil || item.Status != "queued" {
			t.Fatal("explicit resume not idempotent", err)
		}
	}
	if _, err = f.service.SendSubmission(ctx, f.workspaceID, f.userID, f.conversationID, uuid.New(), "manual", nil, nil, nil); !errors.Is(err, ErrQueuedInputPriority) {
		t.Fatal("manual send overtook accepted queue", err)
	}
	guard, err := f.service.acquireRun(ctx, f.workspaceID, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	run, err := f.service.claimQueuedInput(ctx, f.workspaceID, f.conversationID)
	if err != nil || run == uuid.Nil {
		t.Fatal("explicit held resume not started", err)
	}
	var submission uuid.UUID
	if err = f.service.db.QueryRow(ctx, `SELECT submission_id FROM runs WHERE id=$1`, run).Scan(&submission); err != nil || submission != second.ID {
		t.Fatal("cancelled first input launched", err)
	}
	if _, err = f.service.ChangeRunInput(ctx, f.workspaceID, f.userID, f.conversationID, second.ID, "cancel"); !errors.Is(err, ErrRunInputConflict) {
		t.Fatal("started run cancelled through queue instead of Stop", err)
	}
}
