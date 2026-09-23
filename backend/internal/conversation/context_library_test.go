package conversation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/contextlibrary"
	"github.com/google/uuid"
)

func TestContextLibraryIsolationAndSnapshots(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	library := contextlibrary.Service{DB: f.service.db}
	entry, err := library.Save(ctx, f.workspaceID, uuid.Nil, contextlibrary.Entry{Title: "术语", Description: "项目定义", Content: "Lester means a personal agent workspace."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = library.Get(ctx, uuid.New(), entry.ID); err == nil {
		t.Fatal("cross-workspace read")
	}
	tx, err := f.service.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	snapshots, err := contextlibrary.Resolve(ctx, tx, f.workspaceID, []uuid.UUID{entry.ID, entry.ID})
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshot resolution: %v %v", snapshots, err)
	}
	if _, err = contextlibrary.Resolve(ctx, tx, uuid.New(), []uuid.UUID{entry.ID}); err == nil {
		t.Fatal("cross-workspace reference")
	}
	if _, err = contextlibrary.Resolve(ctx, tx, f.workspaceID, make([]uuid.UUID, 9)); err == nil {
		t.Fatal("reference limit not enforced")
	}
	raw, err := json.Marshal(map[string]any{"contexts": snapshots})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO messages(conversation_id,role,content,metadata) VALUES($1,'user','Explain the term',$2)`, f.conversationID, raw); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	entry.Content = "Changed definition"
	updated, err := library.Save(ctx, f.workspaceID, entry.ID, entry)
	if err != nil || updated.Version != 2 {
		t.Fatalf("update: %v", err)
	}
	if _, err = library.Save(ctx, f.workspaceID, entry.ID, entry); err == nil {
		t.Fatal("stale update accepted")
	}
	if err = library.Delete(ctx, f.workspaceID, entry.ID, entry.Version); err == nil {
		t.Fatal("stale delete accepted")
	}
	if err = library.Delete(ctx, f.workspaceID, entry.ID, updated.Version); err != nil {
		t.Fatal(err)
	}
	_, messages, err := f.service.Get(ctx, f.workspaceID, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	history := modelHistory(messages)
	if len(history) != 1 || !strings.Contains(history[0].Content, "Lester means") || strings.Contains(history[0].Content, "Changed definition") {
		t.Fatalf("historical snapshot changed: %#v", history)
	}
	if messages[0].Content != "Explain the term" {
		t.Fatal("snapshot overwrote user text")
	}
	if _, err = f.service.Send(ctx, f.workspaceID, f.userID, f.conversationID, "Use deleted term", nil, []uuid.UUID{entry.ID}); err == nil {
		t.Fatal("send accepted deleted reference")
	}
	var count int
	if err = f.service.db.QueryRow(ctx, `SELECT count(*) FROM runs WHERE conversation_id=$1`, f.conversationID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid reference persisted a run: %d %v", count, err)
	}
}

func TestContextLibraryValidation(t *testing.T) {
	for _, entry := range []contextlibrary.Entry{{Title: " ", Content: "x"}, {Title: "x", Content: " "}, {Title: "x", Content: strings.Repeat("中", 20001)}} {
		if err := contextlibrary.Validate(entry); err == nil {
			t.Fatal("invalid entry accepted")
		}
	}
}
