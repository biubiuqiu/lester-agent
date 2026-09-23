package conversation

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/agent"
	"github.com/google/uuid"
)

type agentFileStore struct{ items map[string][]byte }

func (s *agentFileStore) Ensure(context.Context) error { return nil }
func (s *agentFileStore) Put(_ context.Context, key string, reader io.Reader, _ int64, _ string) error {
	data, err := io.ReadAll(reader)
	if err == nil {
		s.items[key] = data
	}
	return err
}
func (s *agentFileStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.items[key])), nil
}
func (s *agentFileStore) Delete(_ context.Context, key string) error {
	delete(s.items, key)
	return nil
}

func TestAgentFilesSnapshotIntoConversation(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	store := &agentFileStore{items: map[string][]byte{}}
	service := &agent.Service{DB: f.service.db, Objects: store}
	definition, err := service.Save(ctx, f.workspaceID, uuid.Nil, agent.Agent{Name: "Docs", Instructions: "Use the reference files."})
	if err != nil {
		t.Fatal(err)
	}
	file, err := service.UploadFile(ctx, f.workspaceID, definition.ID, "guide.txt", "text/plain", []byte("Version one"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ListFiles(ctx, uuid.New(), definition.ID); err == nil {
		t.Fatal("cross-workspace files visible")
	}
	if _, err = service.UploadFile(ctx, f.workspaceID, definition.ID, "../escape.txt", "text/plain", []byte("bad")); err == nil {
		t.Fatal("unsafe filename accepted")
	}
	conversation, err := f.service.Create(ctx, f.workspaceID, f.userID, definition.Slug, "Read guide", uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.DeleteFile(ctx, f.workspaceID, definition.ID, file.ID); err != nil {
		t.Fatal(err)
	}
	if len(store.items) != 1 {
		t.Fatal("snapshot object removed with Agent file")
	}
	var name, key string
	if err = f.service.db.QueryRow(ctx, `SELECT name,object_key FROM conversation_agent_files WHERE conversation_id=$1`, conversation.ID).Scan(&name, &key); err != nil {
		t.Fatal(err)
	}
	if name != "guide.txt" || !bytes.Equal(store.items[key], []byte("Version one")) {
		t.Fatal("conversation did not retain file snapshot")
	}
	if _, err = service.GetFile(ctx, f.workspaceID, definition.ID, file.ID); err == nil {
		t.Fatal("removed Agent file still listed")
	}
}
