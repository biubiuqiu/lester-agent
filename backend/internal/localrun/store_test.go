package localrun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/google/uuid"
)

func openTestStore(t *testing.T) (*Store, string, Project, Conversation) {
	t.Helper()
	state := t.TempDir()
	store, err := OpenStore(state, "profile-a", "device-a")
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.OpenProject(context.Background(), "", "Project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := store.CreateConversation(context.Background(), project.ID, "fixture", "Task", "standard")
	if err != nil {
		t.Fatal(err)
	}
	return store, state, project, c
}

func TestStoreRestartReconcilesUnknownToolsWithoutReplay(t *testing.T) {
	ctx := context.Background()
	store, state, _, conversation := openTestStore(t)
	requestID := uuid.NewString()
	run, accepted, _, err := store.Accept(ctx, conversation.ID, requestID, "执行一次")
	if err != nil || !accepted {
		t.Fatalf("accept: %v %v", accepted, err)
	}
	_, _, err = store.AppendMessage(ctx, conversation.ID, run.ID, modelruntime.Message{Role: "assistant", ToolCalls: []modelruntime.ToolCall{{ID: "call-1", Name: "bash", Arguments: []byte(`{"command":"do-once"}`)}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	permission, _, err := store.CreatePermission(ctx, Permission{ConversationID: conversation.ID, RunID: run.ID, Tool: "bash", Arguments: []byte(`{"command":"do-once"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenStore(state, "profile-a", "device-a")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snapshot, err := store.Snapshot(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Conversation.Status != "interrupted" || snapshot.Conversation.ActiveRunID != "" || len(snapshot.Permissions) != 0 || len(snapshot.Messages) != 3 {
		t.Fatalf("recovered snapshot: %+v", snapshot)
	}
	if snapshot.Messages[2].ToolCallID != "call-1" || !strings.Contains(snapshot.Messages[2].Content, `"status":"unknown"`) {
		t.Fatalf("missing unknown outcome: %+v", snapshot.Messages[2])
	}
	if _, err = store.RespondPermission(ctx, conversation.ID, run.ID, permission.ID, "allow"); !errors.Is(err, ErrReceiptConflict) {
		t.Fatalf("stale approval: %v", err)
	}
	receipt, accepted, _, err := store.Accept(ctx, conversation.ID, requestID, "执行一次")
	if err != nil || accepted || receipt.ID != run.ID || receipt.Status != "interrupted" {
		t.Fatalf("receipt replayed: %+v %v %v", receipt, accepted, err)
	}
	if _, _, _, err = store.Accept(ctx, conversation.ID, requestID, "different"); !errors.Is(err, ErrReceiptConflict) {
		t.Fatalf("receipt digest mismatch: %v", err)
	}
	_ = store.Close()
	store, err = OpenStore(state, "profile-a", "device-a")
	if err != nil {
		t.Fatal(err)
	}
	messages, err := store.Messages(ctx, conversation.ID)
	if err != nil || len(messages) != 3 {
		t.Fatalf("restart duplicated recovery: %d %v", len(messages), err)
	}
	_ = store.Close()
}

func TestStoreProfileAndPermissionIsolation(t *testing.T) {
	ctx := context.Background()
	store, state, _, c := openTestStore(t)
	run, _, _, err := store.Accept(ctx, c.ID, uuid.NewString(), "test")
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := store.CreatePermission(ctx, Permission{ConversationID: c.ID, RunID: run.ID, Tool: "bash", Arguments: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][3]string{{uuid.NewString(), run.ID, p.ID}, {c.ID, uuid.NewString(), p.ID}, {c.ID, run.ID, uuid.NewString()}} {
		if _, err = store.RespondPermission(ctx, ids[0], ids[1], ids[2], "allow"); err == nil {
			t.Fatal("cross-scope permission accepted")
		}
	}
	if _, err = store.RespondPermission(ctx, c.ID, run.ID, p.ID, "allow"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RespondPermission(ctx, c.ID, run.ID, p.ID, "deny"); !errors.Is(err, ErrReceiptConflict) {
		t.Fatalf("decision changed: %v", err)
	}
	if _, err = store.Finish(ctx, run.ID, "completed", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RespondPermission(ctx, c.ID, run.ID, p.ID, "allow"); !errors.Is(err, ErrReceiptConflict) {
		t.Fatalf("terminal run accepted repeated approval: %v", err)
	}
	_ = store.Close()
	if foreign, err := OpenStore(state, "profile-b", "device-a"); err == nil {
		foreign.Close()
		t.Fatal("different profile opened state")
	}
	if foreign, err := OpenStore(state, "profile-a", "device-b"); err == nil {
		foreign.Close()
		t.Fatal("different device opened state")
	}
}

func assertNoPrivateBytes(t *testing.T, directory, secret string) {
	t.Helper()
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), secret) {
			t.Errorf("private credential persisted in %s", entry.Name())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
