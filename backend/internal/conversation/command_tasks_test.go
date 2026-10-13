package conversation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestCommandTasksPersistPrivateIdentityAndIgnoreLateRunningState(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	runID := f.startRun(t, "command task")
	computer := &Computer{SandboxID: "fixture-computer", WorkDir: "/workspace/conversations/" + f.conversationID.String()}
	snapshot := &sandbox.CommandTaskSnapshot{Handle: sandbox.CommandTaskHandle{TaskID: uuid.NewString(), RunID: runID.String(), ConversationID: f.conversationID.String(), WorkDir: computer.WorkDir, OwnershipToken: "private-fixture-owner"}, Status: "starting", StartedAt: time.Now()}
	if err := f.service.recordCommandTask(ctx, runID, f.conversationID, computer, snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Handle.PID = 123
	snapshot.Handle.StartTime = 456
	snapshot.Status = "running"
	if err := f.service.recordCommandTask(ctx, runID, f.conversationID, computer, snapshot); err != nil {
		t.Fatal(err)
	}
	bad := *snapshot
	bad.Handle.StartTime++
	if err := f.service.recordCommandTask(ctx, runID, f.conversationID, computer, &bad); err == nil {
		t.Fatal("changed process identity accepted")
	}
	exit := 7
	snapshot.ExitCode = &exit
	snapshot.Status = "exited"
	snapshot.StoredBytes = 42
	if err := f.service.recordCommandTask(ctx, runID, f.conversationID, computer, snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Status = "running"
	if err := f.service.recordCommandTask(ctx, runID, f.conversationID, computer, snapshot); err != nil {
		t.Fatal(err)
	}
	views, err := f.service.commandTasks(ctx, f.workspaceID, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0]["status"] != "exited" || views[0]["exit_code"] != float64(7) {
		t.Fatalf("terminal state lost: %#v", views)
	}
	raw, _ := json.Marshal(views)
	for _, private := range []string{"private-fixture-owner", "ownership_token", "start_time", "\"pid\"", "\"handle\""} {
		if bytes.Contains(raw, []byte(private)) {
			t.Fatal("private handle leaked")
		}
	}
	var leaked bool
	if err = f.service.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM run_events WHERE conversation_id=$1 AND payload::text LIKE '%private-fixture-owner%')`, f.conversationID).Scan(&leaked); err != nil || leaked {
		t.Fatalf("event ownership leak: %v", err)
	}
}

func TestCommandTaskRecoveryInspectsOnlyOriginalScopeAndPreservesRawBytes(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	runID := f.startRun(t, "command task")
	computer := &Computer{SandboxID: "fixture-computer", WorkDir: "/workspace/conversations/" + f.conversationID.String()}
	snapshot := &sandbox.CommandTaskSnapshot{Handle: sandbox.CommandTaskHandle{TaskID: uuid.NewString(), RunID: runID.String(), ConversationID: f.conversationID.String(), WorkDir: computer.WorkDir, OwnershipToken: "test-owner"}, Status: "starting", StartedAt: time.Now()}
	if err := f.service.recordCommandTask(ctx, runID, f.conversationID, computer, snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Handle.PID = 123
	snapshot.Handle.StartTime = 456
	snapshot.Status = "running"
	if err := f.service.recordCommandTask(ctx, runID, f.conversationID, computer, snapshot); err != nil {
		t.Fatal(err)
	}
	bytesExpected := []byte{0, 255, 'a', 226, 130, 172}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.HasSuffix(r.URL.Path, "/command-tasks/inspect") {
			t.Error("recovery launched or stopped a command")
			w.WriteHeader(500)
			return
		}
		var input struct {
			Handle sandbox.CommandTaskHandle      `json:"handle"`
			Read   sandbox.CommandTaskReadOptions `json:"read"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || !sameTaskIdentity(snapshot.Handle, input.Handle) {
			t.Error("request changed original task handle")
			w.WriteHeader(400)
			return
		}
		exit := 0
		copy := *snapshot
		copy.Status = "exited"
		copy.ExitCode = &exit
		copy.StoredBytes = int64(len(bytesExpected))
		copy.Page = sandbox.CommandTaskPage{Data: bytesExpected, StoredBytes: int64(len(bytesExpected)), NextOffset: int64(len(bytesExpected))}
		_ = json.NewEncoder(w).Encode(copy)
	}))
	defer server.Close()
	// Simulate a new API service process reading durable state, not an in-memory
	// supervisor map. No automatic StartTask is permitted during recovery.
	f.service = New(f.service.db, nil, sandbox.NewClient(server.URL, "fixture-service-token"), nil)
	taskID := uuid.MustParse(snapshot.Handle.TaskID)
	if _, err := f.service.commandTask(ctx, uuid.New(), f.conversationID, taskID, sandbox.CommandTaskReadOptions{}, false); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("foreign workspace could inspect task")
	}
	if calls != 0 {
		t.Fatal("foreign scope reached provider")
	}
	view, err := f.service.commandTask(ctx, f.workspaceID, f.conversationID, taskID, sandbox.CommandTaskReadOptions{Limit: 4096}, false)
	if err != nil {
		t.Fatal(err)
	}
	page, ok := view["page"].(sandbox.CommandTaskPage)
	if !ok || !bytes.Equal(page.Data, bytesExpected) || view["status"] != "exited" || calls != 1 {
		t.Fatal("recovered output changed")
	}
}

func TestCommandTaskMustBeRecordedBeforeLaunchAndCannotStartAfterCancellation(t *testing.T) {
	f := newTranscriptFixture(t, false)
	runID := f.startRun(t, "command task")
	computer := &Computer{SandboxID: "fixture-computer", WorkDir: "/workspace/conversations/" + f.conversationID.String()}
	snapshot := &sandbox.CommandTaskSnapshot{Handle: sandbox.CommandTaskHandle{TaskID: uuid.NewString(), RunID: runID.String(), ConversationID: f.conversationID.String(), WorkDir: computer.WorkDir, OwnershipToken: "test-owner", PID: 100, StartTime: 123}, Status: "running"}
	if err := f.service.recordCommandTask(context.Background(), runID, f.conversationID, computer, snapshot); err == nil {
		t.Fatal("unrecorded command accepted")
	}
	snapshot.Handle.PID = 0
	snapshot.Status = "starting"
	if _, err := f.service.db.Exec(context.Background(), `UPDATE runs SET status='cancelling' WHERE id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.recordCommandTask(context.Background(), runID, f.conversationID, computer, snapshot); err == nil {
		t.Fatal("cancelled run could launch command")
	}
}
