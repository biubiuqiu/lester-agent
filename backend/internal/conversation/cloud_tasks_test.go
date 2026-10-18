package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/biubiuqiu/lester-agent/backend/internal/toolcontext"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func taskConversation(t *testing.T, f transcriptFixture) transcriptFixture {
	t.Helper()
	id := uuid.New()
	if _, err := f.service.db.Exec(context.Background(), `INSERT INTO conversations(id,workspace_id,created_by,agent_slug,project_id,title) SELECT $1,workspace_id,created_by,agent_slug,project_id,'cloud task' FROM conversations WHERE id=$2`, id, f.conversationID); err != nil {
		t.Fatal(err)
	}
	f.conversationID = id
	return f
}
func TestBackgroundRunMonitorRepairsOrphansButPreservesOtherReplica(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	live := f.startRun(t, "a live replica")
	guard, err := f.service.acquireRun(ctx, f.workspaceID, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	orphan := taskConversation(t, f)
	run := orphan.startRun(t, "an abandoned write")
	if err = f.service.appendMessage(ctx, orphan.conversationID, run, model.Message{Role: "assistant", ToolCalls: []model.ToolCall{{ID: "unknown-write", Name: "write", Arguments: json.RawMessage(`{"file_path":"result.txt","content":"unknown"}`)}}}, "", nil); err != nil {
		t.Fatal(err)
	}
	recordedInteraction(t, orphan, run, "question")
	if _, err = f.service.db.Exec(ctx, `INSERT INTO run_permissions(workspace_id,conversation_id,run_id,tool_call_id,tool,request_digest,summary) VALUES($1,$2,$3,'unknown-write','write',repeat('a',64),'{}')`, f.workspaceID, orphan.conversationID, run); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.startModelRequest(ctx, run, "execution", toolcontext.Budget{EstimatedInput: 99}); err != nil {
		t.Fatal(err)
	}
	stopping := taskConversation(t, f)
	stopped := stopping.startRun(t, "cancel before crash")
	if _, err = f.service.db.Exec(ctx, `UPDATE runs SET status='cancelling' WHERE id=$1`, stopped); err != nil {
		t.Fatal(err)
	}
	cursor := runMonitorCursor{}
	for i := 0; i < 5; i++ {
		cursor, err = f.service.reconcileActiveRunBatch(ctx, cursor, 1)
		if err != nil {
			t.Fatal(err)
		}
	}
	for id, want := range map[uuid.UUID]string{live: "running", run: "failed", stopped: "cancelled"} {
		var status string
		if err = f.service.db.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1`, id).Scan(&status); err != nil || status != want {
			t.Fatalf("%s status %s: %v", id, status, err)
		}
	}
	var pending, unknown, events int
	if err = f.service.db.QueryRow(ctx, `SELECT (SELECT count(*) FROM run_interactions WHERE run_id=$1 AND status='pending')+(SELECT count(*) FROM run_permissions WHERE run_id=$1 AND status='pending'),(SELECT count(*) FROM messages WHERE run_id=$1 AND role='tool' AND tool_call_id='unknown-write' AND metadata->>'interrupted'='true'),(SELECT count(*) FROM run_events WHERE run_id=$1 AND type='RUN_FAILED')`, run).Scan(&pending, &unknown, &events); err != nil || pending != 0 || unknown != 1 || events != 1 {
		t.Fatalf("recovery receipts %d %d %d: %v", pending, unknown, events, err)
	}
	usage, err := f.service.RunUsage(ctx, f.workspaceID, orphan.conversationID, run)
	if err != nil || usage.InFlight != 0 || usage.EstimatedInputRequests != 1 || usage.ActualInputRequests != 0 {
		t.Fatalf("usage after crash: %#v %v", usage, err)
	}
	// Repeated scans are idempotent; no model or sandbox is configured or invoked.
	if _, err = f.service.reconcileActiveRunBatch(ctx, runMonitorCursor{}, 50); err != nil {
		t.Fatal(err)
	}
}
func TestCloudTasksAreScopedLatestOnlyFilteredAndCursorPaged(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	old := f.startRun(t, "old failure")
	if err := f.service.finishFailedRun(ctx, old, f.conversationID, "old failure"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.db.Exec(ctx, `UPDATE runs SET created_at='2025-01-01' WHERE id=$1`, old); err != nil {
		t.Fatal(err)
	}
	latest := f.startRun(t, "new completed task")
	if err := f.service.finishCompletedRun(ctx, latest, f.conversationID); err != nil {
		t.Fatal(err)
	}
	waiting := taskConversation(t, f)
	waitID := waiting.startRun(t, "waiting for an answer")
	recordedInteraction(t, waiting, waitID, "question")
	failed := taskConversation(t, f)
	failID := failed.startRun(t, "needs checking")
	if err := f.service.finishFailedRun(ctx, failID, failed.conversationID, "failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.db.Exec(ctx, `UPDATE runs SET created_at='2026-01-01' WHERE id=ANY($1::uuid[])`, []uuid.UUID{latest, waitID, failID}); err != nil {
		t.Fatal(err)
	}
	seen := map[uuid.UUID]bool{}
	cursor := ""
	for i := 0; i < 3; i++ {
		feed, err := f.service.CloudTasks(ctx, f.workspaceID, "all", cursor, 1)
		if err != nil || len(feed.Tasks) != 1 || feed.Attention != 2 {
			t.Fatalf("page: %#v %v", feed, err)
		}
		id := feed.Tasks[0].RunID
		if seen[id] || id == old {
			t.Fatal("duplicate or superseded task")
		}
		seen[id] = true
		cursor = feed.Next
	}
	if len(seen) != 3 || cursor != "" {
		t.Fatal("unstable page boundary")
	}
	active, err := f.service.CloudTasks(ctx, f.workspaceID, "active", "", 20)
	if err != nil || len(active.Tasks) != 1 || active.Tasks[0].State != "waiting_question" {
		t.Fatal(active, err)
	}
	empty, err := f.service.CloudTasks(ctx, uuid.New(), "all", "", 20)
	if err != nil || len(empty.Tasks) != 0 || empty.Attention != 0 {
		t.Fatal("workspace leak", empty, err)
	}
	for _, bad := range []string{"not base64", "e30"} {
		if _, err = f.service.CloudTasks(ctx, f.workspaceID, "all", bad, 20); !errors.Is(err, errTaskCursor) {
			t.Fatal("invalid cursor accepted", err)
		}
	}
	if _, err = f.service.CloudTasks(ctx, f.workspaceID, "unknown", "", 20); !errors.Is(err, errTaskCursor) {
		t.Fatal("invalid filter accepted")
	}
	for _, status := range []string{"running", "unknown"} {
		if _, err = f.service.db.Exec(ctx, `INSERT INTO command_tasks(id,workspace_id,conversation_id,run_id,sandbox_ref,handle,snapshot,status) VALUES($1,$2,$3,$4,'fixture','{}','{}',$5)`, uuid.New(), f.workspaceID, f.conversationID, latest, status); err != nil {
			t.Fatal(err)
		}
	}
	active, err = f.service.CloudTasks(ctx, f.workspaceID, "active", "", 20)
	if err != nil || len(active.Tasks) != 2 {
		t.Fatal("background command disappeared after Agent completion", active, err)
	}
	attention, err := f.service.CloudTasks(ctx, f.workspaceID, "attention", "", 20)
	if err != nil || attention.Attention != 3 {
		t.Fatal("unknown command falsely treated as finished", attention, err)
	}

	found := false
	for _, item := range active.Tasks {
		if item.RunID == latest {
			found = item.BackgroundCommands == 1 && item.UnknownCommands == 1
		}
	}
	if !found {
		t.Fatal("background command facts were dropped", active)
	}

}
func TestRunUsageKeepsReportedAndEstimatedCountsSeparate(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	run := f.startRun(t, "usage")
	samples := []*modelruntime.NormalizedUsage{
		{Source: modelruntime.UsageActual, InputTokens: 100, CacheReadInputTokens: 60, CachedIncluded: true, InputReported: true, OutputTokens: 20, OutputReported: true},
		{Source: modelruntime.UsageActual, InputTokens: 10, CacheReadInputTokens: 60, CacheCreationInputTokens: 30, InputReported: true, OutputTokens: 0, OutputReported: true},
		nil,
	}
	for i, u := range samples {
		if _, err := f.service.startModelRequest(ctx, run, "execution", toolcontext.Budget{EstimatedInput: 123}); err != nil {
			t.Fatal(err)
		}
		if u != nil {
			if err := f.service.finishModelRequest(ctx, run, i+1, u, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	usage, err := f.service.RunUsage(ctx, f.workspaceID, f.conversationID, run)
	if err != nil {
		t.Fatal(err)
	}
	if usage.Requests != 3 || usage.ActualInput != 200 || usage.ActualOutput != 20 || usage.ActualInputRequests != 2 || usage.ActualOutputRequests != 2 || usage.EstimatedInput != 123 || usage.EstimatedInputRequests != 1 || usage.MissingOutputRequests != 1 || usage.InFlight != 1 {
		t.Fatalf("misleading usage %#v", usage)
	}
	if _, err = f.service.RunUsage(ctx, uuid.New(), f.conversationID, run); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("foreign workspace usage leaked")
	}
	if _, err = f.service.RunUsage(ctx, f.workspaceID, uuid.New(), run); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("foreign conversation usage leaked")
	}
	// No requests is unknown, not a measured free run. Elapsed wall time includes waits.
	other := taskConversation(t, f)
	emptyID := other.startRun(t, "not started model")
	if _, err = f.service.db.Exec(ctx, `UPDATE runs SET created_at=now()-interval '90 seconds',completed_at=now(),status='completed' WHERE id=$1`, emptyID); err != nil {
		t.Fatal(err)
	}
	empty, err := f.service.RunUsage(ctx, f.workspaceID, other.conversationID, emptyID)
	if err != nil || empty.Requests != 0 || empty.ActualOutputRequests != 0 || empty.ElapsedSeconds == nil || *empty.ElapsedSeconds < 89 || *empty.ElapsedSeconds > 91 || empty.ObservedAt.After(time.Now().Add(time.Second)) {
		t.Fatal(empty, err)
	}
	if _, err = f.service.db.Exec(ctx, `UPDATE runs SET completed_at=NULL WHERE id=$1`, emptyID); err != nil {
		t.Fatal(err)
	}
	unknown, err := f.service.RunUsage(ctx, f.workspaceID, other.conversationID, emptyID)
	if err != nil || unknown.ElapsedSeconds != nil {
		t.Fatal("missing finish time was fabricated", unknown, err)
	}

}

func TestRunMonitorStartsRecoveryWithoutAnHTTPVisit(t *testing.T) {
	f := newTranscriptFixture(t, false)
	run := f.startRun(t, "unattended")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); f.service.MonitorRuns(ctx) }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var state string
		if err := f.service.db.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1`, run).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "failed" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("startup monitor did not recover an abandoned task")
		case <-ticker.C:
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitor did not stop")
	}
}
