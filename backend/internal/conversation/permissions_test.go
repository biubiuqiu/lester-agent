package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
)

func TestPermissionPolicyConservativeBoundaries(t *testing.T) {
	for _, item := range []struct {
		mode, tool, args string
		needed, run      bool
	}{
		{"standard", "read", `{"file_path":"notes.txt"}`, false, false},
		{"standard", "computer_write_file", `{"file_path":"notes.txt","content":"x"}`, true, true},
		{"auto", "edit", `{"file_path":"notes.txt"}`, false, true},
		{"auto", "write", `{"file_path":"nested/../.env","content":"x"}`, true, false},
		{"auto", "read", `{"file_path":"keys/private.pem"}`, true, false},
		{"auto", "computer_read_file", `{"file_path":" \n.env\t"}`, true, false},
		{"auto", "bash", `{"command":"node --version"}`, true, false},
		{"auto", "bash", `{"command":"node --version; curl example.test"}`, true, false},
		{"auto", "bash", `{"command":"git status"}`, true, false},
		{"auto", "bash", `{"command":"rm -rf notes"}`, true, false},
		{"auto", "publish", `{}`, true, false},
		{"standard", "unknown", `{}`, true, false},
		{"full", "bash", `{"command":"rm notes"}`, false, false},
	} {
		needed, run := needsPermission(item.mode, item.tool, json.RawMessage(item.args))
		if needed != item.needed || run != item.run {
			t.Fatalf("%+v got %v/%v", item, needed, run)
		}
	}
	a, _ := permissionDigest("write", json.RawMessage(`{"number":9007199254740992}`))
	b, _ := permissionDigest("write", json.RawMessage(`{"number":9007199254740993}`))
	if a == b {
		t.Fatal("large integer arguments lost precision")
	}
	if _, err := permissionMode("unrestricted-host"); err == nil {
		t.Fatal("unknown mode accepted")
	}
	if submissionDigest("x", nil, nil, nil, "execute", "full") == submissionDigest("x", nil, nil, nil, "execute", "standard") {
		t.Fatal("submission hash ignores permissions")
	}
}
func recordedPermissionCall(t *testing.T, f transcriptFixture, runID uuid.UUID, name, args string) model.ToolCall {
	t.Helper()
	call := model.ToolCall{ID: uuid.NewString(), Name: name, Arguments: json.RawMessage(args)}
	if err := f.service.appendMessage(context.Background(), f.conversationID, runID, model.Message{Role: "assistant", ToolCalls: []model.ToolCall{call}}, "", nil); err != nil {
		t.Fatal(err)
	}
	return call
}
func pendingPermission(t *testing.T, f transcriptFixture) RunPermission {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		items, err := f.service.runPermissions(context.Background(), f.workspaceID, f.conversationID)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if item.Status == "pending" {
				return item
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no permission prompt")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestPermissionGatePreventsRealWriteUntilExactApproval(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	server, files := newTranscriptSandboxServer(t, map[string]string{})
	f.service.sandboxes = sandbox.NewClient(server.URL, "")
	runID := f.startRun(t, "write a file")
	if _, err := f.service.db.Exec(ctx, `UPDATE runs SET permission_mode='standard' WHERE id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	call := recordedPermissionCall(t, f, runID, "computer_write_file", `{"file_path":"new.txt","content":"approved bytes"}`)
	done := make(chan error, 1)
	go func() {
		_, err := f.service.executor.runTool(ctx, runID, f.conversationID, &Computer{SandboxID: "test", WorkDir: conversationWorkDir(f.conversationID)}, call, agenttool.NewFileState(nil))
		done <- err
	}()
	item := pendingPermission(t, f)
	if files.content("new.txt") != "" {
		t.Fatal("write ran before approval")
	}
	select {
	case err := <-done:
		t.Fatal("tool finished before approval", err)
	default:
	}
	if _, err := f.service.respondPermission(ctx, uuid.New(), f.userID, f.conversationID, item.ID, item.RequestDigest, "once"); !errors.Is(err, ErrInteractionNotFound) {
		t.Fatal("cross-workspace approval", err)
	}
	if _, err := f.service.respondPermission(ctx, f.workspaceID, f.userID, f.conversationID, item.ID, "wrong", "once"); !errors.Is(err, ErrInteractionConflict) {
		t.Fatal("stale approval", err)
	}
	for range 2 {
		if _, err := f.service.respondPermission(ctx, f.workspaceID, f.userID, f.conversationID, item.ID, item.RequestDigest, "once"); err != nil {
			t.Fatal(err)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if files.content("new.txt") != "approved bytes" {
		t.Fatal("approved write missing")
	}
	if err := f.service.authorizePermission(ctx, f.conversationID, runID, call.ID, "write", call.Arguments); err == nil {
		t.Fatal("allow-once approval replayed")
	}

	if err := f.service.authorizePermission(ctx, f.conversationID, runID, call.ID, "write", json.RawMessage(`{"file_path":"different.txt","content":"approved bytes"}`)); err == nil {
		t.Fatal("approval accepted changed parameters")
	}
}
func TestPermissionRunGrantNeverAuthorizesSensitivePathsOrAnotherRun(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	runID := f.startRun(t, "file changes")
	_, _ = f.service.db.Exec(ctx, `UPDATE runs SET permission_mode='standard' WHERE id=$1`, runID)
	first := recordedPermissionCall(t, f, runID, "write", `{"file_path":"a.txt","content":"a"}`)
	done := make(chan error, 1)
	go func() {
		done <- f.service.authorizePermission(ctx, f.conversationID, runID, first.ID, "write", first.Arguments)
	}()
	item := pendingPermission(t, f)
	if _, err := f.service.respondPermission(ctx, f.workspaceID, f.userID, f.conversationID, item.ID, item.RequestDigest, "run"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	second := recordedPermissionCall(t, f, runID, "write", `{"file_path":"b.txt","content":"b"}`)
	if err := f.service.authorizePermission(ctx, f.conversationID, runID, second.ID, "write", second.Arguments); err != nil {
		t.Fatal(err)
	}
	edit := recordedPermissionCall(t, f, runID, "edit", `{"file_path":"b.txt","old_string":"b","new_string":"c"}`)
	if err := f.service.authorizePermission(ctx, f.conversationID, runID, edit.ID, "edit", edit.Arguments); err != nil {
		t.Fatal("file-change run grant did not cover edit", err)
	}
	sensitive := recordedPermissionCall(t, f, runID, "write", `{"file_path":".env","content":"private"}`)
	go func() {
		done <- f.service.authorizePermission(ctx, f.conversationID, runID, sensitive.ID, "write", sensitive.Arguments)
	}()
	item = pendingPermission(t, f)
	if item.AllowRun {
		t.Fatal("sensitive operation offers broad grant")
	}
	if _, err := f.service.respondPermission(ctx, f.workspaceID, f.userID, f.conversationID, item.ID, item.RequestDigest, "run"); !errors.Is(err, ErrInteractionConflict) {
		t.Fatal("sensitive run grant accepted", err)
	}
	if _, err := f.service.respondPermission(ctx, f.workspaceID, f.userID, f.conversationID, item.ID, item.RequestDigest, "reject"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("denied operation executed")
	}
	next := f.startRun(t, "another run")
	_, _ = f.service.db.Exec(ctx, `UPDATE runs SET permission_mode='standard' WHERE id=$1`, next)
	call := recordedPermissionCall(t, f, next, "write", `{"file_path":"a.txt","content":"a"}`)
	go func() {
		done <- f.service.authorizePermission(ctx, f.conversationID, next, call.ID, "write", call.Arguments)
	}()
	item = pendingPermission(t, f)
	if item.RunID != next {
		t.Fatal("grant leaked to another run")
	}
	cancel()
	<-done
}
func TestPermissionFullCannotBypassPlanAndStoppedPromptCannotApprove(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runID := f.startRun(t, "review")
	_, _ = f.service.db.Exec(ctx, `UPDATE runs SET execution_mode='plan',permission_mode='full' WHERE id=$1`, runID)
	if err := f.service.authorizeRunTool(ctx, f.conversationID, runID, "computer_write_file", json.RawMessage(`{}`)); err == nil {
		t.Fatal("full access bypassed Plan")
	}
	_, _ = f.service.db.Exec(ctx, `UPDATE runs SET execution_mode='execute',permission_mode='standard' WHERE id=$1`, runID)
	call := recordedPermissionCall(t, f, runID, "bash", `{"command":"rm notes"}`)
	done := make(chan error, 1)
	go func() {
		done <- f.service.authorizePermission(ctx, f.conversationID, runID, call.ID, "bash", call.Arguments)
	}()
	item := pendingPermission(t, f)
	_, _ = f.service.db.Exec(ctx, `UPDATE runs SET status='cancelled' WHERE id=$1`, runID)
	if _, err := f.service.respondPermission(ctx, f.workspaceID, f.userID, f.conversationID, item.ID, item.RequestDigest, "once"); !errors.Is(err, ErrInteractionConflict) {
		t.Fatal("stopped run approved", err)
	}
	if err := <-done; err == nil {
		t.Fatal("stopped operation executed")
	}
}

func TestPermissionSensitiveReadStaysInPlanAndAdjustmentRevokesPrompt(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	runID := f.startRun(t, "inspect first")
	_, _ = f.service.db.Exec(ctx, `UPDATE runs SET permission_mode='standard',execution_mode='plan' WHERE id=$1`, runID)
	call := recordedPermissionCall(t, f, runID, "read", `{"file_path":".env"}`)
	done := make(chan error, 1)
	go func() {
		done <- f.service.authorizePermission(ctx, f.conversationID, runID, call.ID, "read", call.Arguments)
	}()
	item := pendingPermission(t, f)
	if _, err := f.service.respondPermission(ctx, f.workspaceID, f.userID, f.conversationID, item.ID, item.RequestDigest, "once"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := f.service.db.QueryRow(ctx, `SELECT execution_mode FROM runs WHERE id=$1`, runID).Scan(&mode); err != nil || mode != "plan" {
		t.Fatal("read approval enabled execution", err)
	}
	_, _ = f.service.db.Exec(ctx, `UPDATE runs SET execution_mode='execute' WHERE id=$1`, runID)
	call = recordedPermissionCall(t, f, runID, "bash", `{"command":"rm notes"}`)
	go func() {
		done <- f.service.authorizePermission(ctx, f.conversationID, runID, call.ID, "bash", call.Arguments)
	}()
	item = pendingPermission(t, f)
	if _, err := f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, RunInputRequest{ID: uuid.New(), RunID: runID, Kind: "adjust", Content: "only read files", PermissionMode: "full"}); !errors.Is(err, ErrRunInputRejected) {
		t.Fatal("adjustment changed permissions", err)
	}
	if _, err := f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, RunInputRequest{ID: uuid.New(), RunID: runID, Kind: "adjust", Content: "only read files"}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("superseded command authorized")
	}
	if _, err := f.service.respondPermission(ctx, f.workspaceID, f.userID, f.conversationID, item.ID, item.RequestDigest, "once"); !errors.Is(err, ErrInteractionConflict) {
		t.Fatal("superseded permission revived", err)
	}
}
func TestPermissionQueuedTaskUsesItsOwnCapturedMode(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	parent := f.startRun(t, "current task")
	item, err := f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, RunInputRequest{ID: uuid.New(), RunID: parent, Kind: "next", Content: "next task", PermissionMode: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.service.db.Exec(ctx, `UPDATE runs SET permission_mode='standard' WHERE id=$1`, parent)
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
		t.Fatal("queue did not start", err)
	}
	var mode string
	var submission uuid.UUID
	if err = f.service.db.QueryRow(ctx, `SELECT permission_mode,submission_id FROM runs WHERE id=$1`, started).Scan(&mode, &submission); err != nil || mode != "auto" || submission != item.ID {
		t.Fatal("queue permission snapshot changed", err)
	}
}
