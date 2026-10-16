package conversation

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestTriggeredTaskUsesRealSubmissionAndImmutableProjectRulesWithoutReplay(t *testing.T) {
	f := newTranscriptFixture(t, false)
	applyTestMigration(t, f.service.db, "000013_account_identity.up.sql")
	ctx := context.Background()
	var project, connection, modelID uuid.UUID
	if err := f.service.db.QueryRow(ctx, `SELECT id FROM projects WHERE workspace_id=$1 AND is_default`, f.workspaceID).Scan(&project); err != nil {
		t.Fatal(err)
	}
	if err := f.service.db.QueryRow(ctx, `INSERT INTO model_connections(workspace_id,name,provider,protocol) VALUES($1,'trigger fixture','openai-compatible','openai') RETURNING id`, f.workspaceID).Scan(&connection); err != nil {
		t.Fatal(err)
	}
	if err := f.service.db.QueryRow(ctx, `INSERT INTO model_deployments(workspace_id,connection_id,name,model_id) VALUES($1,$2,'fixture','fixture') RETURNING id`, f.workspaceID, connection).Scan(&modelID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.db.Exec(ctx, `UPDATE projects SET rules_content='Use the accepted project rules',rules_version=2 WHERE id=$1`, project); err != nil {
		t.Fatal(err)
	}
	// Exercise acceptance during shutdown: a real run/receipt is persisted and
	// becomes interrupted, with no provider/sandbox call or automatic relaunch.
	f.service.executor.mu.Lock()
	f.service.executor.stopping = true
	f.service.executor.mu.Unlock()
	input := TriggeredTask{ID: uuid.New(), ConversationID: uuid.New(), WorkspaceID: f.workspaceID, UserID: f.userID, ProjectID: project, ModelID: modelID, Name: "Scheduled brief", Agent: "lester", Prompt: "Prepare a brief", Permission: "auto"}
	first, err := f.service.StartTriggeredTask(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.StartTriggeredTask(ctx, input)
	if err != nil || first != second {
		t.Fatal("trigger replayed", err)
	}
	var count int
	var permission, status string
	if err = f.service.db.QueryRow(ctx, `SELECT count(*) FROM runs WHERE conversation_id=$1`, input.ConversationID).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate run", err)
	}
	if err = f.service.db.QueryRow(ctx, `SELECT permission_mode,status FROM runs WHERE id=$1`, first).Scan(&permission, &status); err != nil || permission != "auto" || status != "failed" {
		t.Fatalf("receipt/permissions: %s %s %v", permission, status, err)
	}
	_, messages, err := f.service.Get(ctx, f.workspaceID, input.ConversationID)
	if err != nil || len(messages) != 1 || messages[0].Content != input.Prompt {
		t.Fatal("input was not accepted", err)
	}
	rules := messages[0].Metadata["project_rules"].(map[string]any)
	if rules["content"] != "Use the accepted project rules" {
		t.Fatal("missing project rules snapshot")
	}
	denied := input
	denied.ID = uuid.New()
	denied.ConversationID = uuid.New()
	denied.WorkspaceID = uuid.New()
	if _, err = f.service.StartTriggeredTask(ctx, denied); err == nil {
		t.Fatal("foreign workspace accepted")
	}
	if _, err = f.service.db.Exec(ctx, `UPDATE users SET disabled=true WHERE id=$1`, f.userID); err != nil {
		t.Fatal(err)
	}
	denied = input
	denied.ID = uuid.New()
	denied.ConversationID = uuid.New()
	if _, err = f.service.StartTriggeredTask(ctx, denied); err == nil {
		t.Fatal("disabled user accepted")
	}
}
