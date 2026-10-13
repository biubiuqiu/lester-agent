package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/biubiuqiu/lester-agent/backend/internal/toolboxfs"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func recordedFileChange(t *testing.T, f transcriptFixture, run uuid.UUID, before, after string, existing bool) FileChange {
	t.Helper()
	ctx := context.Background()
	call := uuid.NewString()
	args, _ := json.Marshal(map[string]any{"file_path": "note.txt", "content": after})
	if err := f.service.appendMessage(ctx, f.conversationID, run, model.Message{Role: "assistant", ToolCalls: []model.ToolCall{{ID: call, Name: "write", Arguments: args}}}, "", nil); err != nil {
		t.Fatal(err)
	}
	change := agenttool.FileChange{Path: "note.txt", Operation: "write", Status: "pending", BeforeExists: existing, BeforeSHA256: byteDigest([]byte(before)), AfterSHA256: byteDigest([]byte(after)), BeforeContent: []byte(before), AfterContent: []byte(after)}
	if err := f.service.recordFileChange(ctx, run, f.conversationID, call, &change); err != nil {
		t.Fatal(err)
	}
	change.Status = "confirmed"
	if err := f.service.recordFileChange(ctx, run, f.conversationID, call, &change); err != nil {
		t.Fatal(err)
	}
	items, err := f.service.FileChanges(ctx, f.workspaceID, f.conversationID)
	if err != nil || len(items) != 1 {
		t.Fatal(err)
	}
	item, err := f.service.FileChange(ctx, f.workspaceID, f.conversationID, items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return item
}
func TestFileReviewScopeOriginalBytesAndUnconfirmedCapture(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	run := f.startRun(t, "change")
	change := recordedFileChange(t, f, run, "before\n", "after\n", true)
	if change.BeforeContent == nil || *change.BeforeContent != "before\n" || change.AfterContent == nil || *change.AfterContent != "after\n" {
		t.Fatal("actual snapshot not retained")
	}
	if _, err := f.service.FileChange(ctx, uuid.New(), f.conversationID, change.ID); !errors.Is(err, ErrFileChangeNotFound) {
		t.Fatal("foreign review leaked")
	}
	if err := f.service.recordFileChange(ctx, run, f.conversationID, "invented-call", &agenttool.FileChange{Status: "pending", Path: "note.txt", Operation: "write"}); err == nil {
		t.Fatal("unrecorded tool generated review")
	}
	if _, err := f.service.db.Exec(ctx, `UPDATE file_changes SET status='unknown' WHERE id=$1`, change.ID); err != nil {
		t.Fatal(err)
	}
	unknown, err := f.service.FileChange(ctx, f.workspaceID, f.conversationID, change.ID)
	if err != nil || unknown.BeforeContent != nil || unknown.AfterContent != nil {
		t.Fatal("unconfirmed intended bytes were exposed as actual diff")
	}
}
func rollbackComputer(t *testing.T, f transcriptFixture, initial string, lost bool) (string, *int) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/files/content"):
			if err := toolboxfs.Read(root, r.URL.Query().Get("path"), w); err != nil {
				http.Error(w, "missing file", 404)
			}
		case strings.HasSuffix(r.URL.Path, "/files/write-checked"):
			writes++
			var request sandbox.FileWriteRequest
			if json.NewDecoder(r.Body).Decode(&request) != nil {
				http.Error(w, "invalid", 400)
				return
			}
			result, err := toolboxfs.WriteChecked(root, r.URL.Query().Get("path"), toolboxfs.CheckedWriteRequest{Content: request.Content, ExpectedSHA256: request.ExpectedSHA256, CreateOnly: request.CreateOnly})
			if err != nil {
				http.Error(w, "version conflict", 409)
				return
			}
			if lost {
				http.Error(w, "lost native receipt", 503)
				return
			}
			json.NewEncoder(w).Encode(result)
		case strings.HasSuffix(r.URL.Path, "/exec"):
			json.NewEncoder(w).Encode(sandbox.CommandResult{})
		default:
			json.NewEncoder(w).Encode(sandbox.Sandbox{ID: f.userID.String(), Provider: "docker", ProviderRef: "fixture", Status: "running"})
		}
	}))
	t.Cleanup(server.Close)
	f.service.sandboxes = sandbox.NewClient(server.URL, "")
	return root, &writes
}
func TestConditionalFileRestoreRefusesLaterEditsAndIsIdempotent(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	run := f.startRun(t, "change")
	change := recordedFileChange(t, f, run, "before\n", "after\n", true)
	if err := f.service.finishCompletedRun(ctx, run, f.conversationID); err != nil {
		t.Fatal(err)
	}
	root, writes := rollbackComputer(t, f, "user changed it", false)
	conflict, err := f.service.RollbackFileChange(ctx, f.workspaceID, f.userID, f.conversationID, change.ID, uuid.New())
	if err != nil || conflict.Status != "conflict" || *writes != 0 {
		t.Fatal("later edit was overwritten", err)
	}
	if err = os.WriteFile(filepath.Join(root, "note.txt"), []byte("after\n"), 0600); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	restored, err := f.service.RollbackFileChange(ctx, f.workspaceID, f.userID, f.conversationID, change.ID, id)
	if err != nil || restored.Status != "completed" {
		t.Fatal("restore failed", restored, err)
	}
	repeated, err := f.service.RollbackFileChange(ctx, f.workspaceID, f.userID, f.conversationID, change.ID, id)
	if err != nil || repeated.Status != "completed" || *writes != 1 {
		t.Fatal("receipt retry wrote twice", err)
	}
	bytes, err := os.ReadFile(filepath.Join(root, "note.txt"))
	if err != nil || string(bytes) != "before\n" {
		t.Fatal("original bytes not restored")
	}
	nextRun := f.startRun(t, "continue after restoration")
	facts, factsErr := f.service.fileReviewContext(ctx, f.conversationID, nextRun)
	if factsErr != nil || !strings.Contains(facts, change.BeforeSHA256) || !strings.Contains(facts, "completed") || strings.Contains(facts, "before\\n") {
		t.Fatal("confirmed restoration facts were lost or file bytes leaked into prompt", factsErr)
	}
	if _, err = f.service.FileRollback(ctx, uuid.New(), f.userID, f.conversationID, change.ID, id); !errors.Is(err, ErrFileChangeNotFound) {
		t.Fatal("foreign rollback receipt leaked")
	}
}
func TestLostNativeRestoreReceiptNeverReplaysOrInfersSuccess(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	run := f.startRun(t, "change")
	change := recordedFileChange(t, f, run, "before", "after", true)
	if err := f.service.finishCompletedRun(ctx, run, f.conversationID); err != nil {
		t.Fatal(err)
	}
	root, writes := rollbackComputer(t, f, "after", true)
	id := uuid.New()
	receipt, err := f.service.RollbackFileChange(ctx, f.workspaceID, f.userID, f.conversationID, change.ID, id)
	if err != nil || receipt.Status != "unknown" {
		t.Fatal("lost native receipt claimed success", err)
	}
	bytes, err := os.ReadFile(filepath.Join(root, "note.txt"))
	if err != nil || string(bytes) != "before" {
		t.Fatal("fixture mutation did not happen")
	}
	if again, err := f.service.RollbackFileChange(ctx, f.workspaceID, f.userID, f.conversationID, change.ID, id); err != nil || again.Status != "unknown" || *writes != 1 {
		t.Fatal("unknown restore replayed", err)
	}
	if _, err = f.service.RollbackFileChange(ctx, f.workspaceID, f.userID, f.conversationID, change.ID, uuid.New()); !errors.Is(err, ErrFileRollbackConflict) || *writes != 1 {
		t.Fatal("matching bytes inferred ownership or new ID replayed", err)
	}
}

func TestRecordedWriteThroughExecutorProducesActualReview(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	run := f.startRun(t, "write")
	root, writes := rollbackComputer(t, f, "before", false)
	computer, err := f.service.ComputerForConversation(ctx, f.workspaceID, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	state := agenttool.NewFileState(nil)
	state.ObserveRead(map[string]any{"read_receipt": agenttool.ReadReceipt{Path: "note.txt", SHA256: byteDigest([]byte("before")), StartLine: 1, LineCount: 1, TotalLines: 1, Complete: true}, "content": "     1\tbefore"})
	call := model.ToolCall{ID: uuid.NewString(), Name: "computer_write_file", Arguments: json.RawMessage(`{"path":"note.txt","content":"after"}`)}
	if err = f.service.appendMessage(ctx, f.conversationID, run, model.Message{Role: "assistant", ToolCalls: []model.ToolCall{call}}, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.executor.runTool(ctx, run, f.conversationID, computer, call, state); err != nil {
		t.Fatal("recorded alias write failed", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "note.txt"))
	if err != nil || string(data) != "after" || *writes != 1 {
		t.Fatal("actual protected write missing")
	}
	changes, err := f.service.FileChanges(ctx, f.workspaceID, f.conversationID)
	if err != nil || len(changes) != 1 || changes[0].Status != "confirmed" {
		t.Fatal("native receipt did not confirm review", err)
	}
	snapshot, err := f.service.FileChange(ctx, f.workspaceID, f.conversationID, changes[0].ID)
	if err != nil || snapshot.BeforeContent == nil || *snapshot.BeforeContent != "before" || snapshot.AfterContent == nil || *snapshot.AfterContent != "after" {
		t.Fatal("actual before/after not recorded", err)
	}
}

func TestFileRestoreCannotOverlapRunOrDeleteNewFile(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	run := f.startRun(t, "change")
	change := recordedFileChange(t, f, run, "before", "after", true)
	_, writes := rollbackComputer(t, f, "after", false)
	if _, err := f.service.RollbackFileChange(ctx, f.workspaceID, f.userID, f.conversationID, change.ID, uuid.New()); !errors.Is(err, ErrRunInProgress) || *writes != 0 {
		t.Fatal("restore overlapped active task", err)
	}
	if err := f.service.finishCompletedRun(ctx, run, f.conversationID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.RollbackFileChange(ctx, f.workspaceID, uuid.New(), f.conversationID, change.ID, uuid.New()); !errors.Is(err, ErrFileChangeNotFound) || *writes != 0 {
		t.Fatal("foreign actor accessed restoration", err)
	}
	if _, err := f.service.db.Exec(ctx, `UPDATE file_changes SET before_exists=false WHERE id=$1`, change.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.RollbackFileChange(ctx, f.workspaceID, f.userID, f.conversationID, change.ID, uuid.New()); !errors.Is(err, ErrFileRollbackConflict) || *writes != 0 {
		t.Fatal("new file removal was falsely supported", err)
	}
}
