package conversation

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/reference"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
)

func referenceSource(t *testing.T, f transcriptFixture, title string) Conversation {
	t.Helper()
	item, err := f.service.Create(context.Background(), f.workspaceID, f.userID, "lester", title, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	return item
}
func sourceText(t *testing.T, f transcriptFixture, source uuid.UUID, role, content string) {
	t.Helper()
	if _, err := f.service.db.Exec(context.Background(), `INSERT INTO messages(conversation_id,role,content) VALUES($1,$2,$3)`, source, role, content); err != nil {
		t.Fatal(err)
	}
}
func saveReferenceMetadata(t *testing.T, f transcriptFixture, run uuid.UUID, metadata map[string]any) {
	t.Helper()
	raw, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.db.Exec(context.Background(), `UPDATE messages SET metadata=$2 FROM runs WHERE messages.id=runs.input_message_id AND runs.id=$1`, run, raw); err != nil {
		t.Fatal(err)
	}
}

func TestConversationReferencesEnforceGrantsCutoffPagingAndWorkspace(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	source := referenceSource(t, f, "Saved source title")
	long := strings.Repeat("猫🙂", 8000)
	sourceText(t, f, source.ID, "user", "Initial source request")
	sourceText(t, f, source.ID, "assistant", long)
	if _, err := f.service.db.Exec(ctx, `INSERT INTO messages(conversation_id,role,content,tool_calls) VALUES($1,'assistant','INTERNAL ASSISTANT','[{"id":"internal","name":"bash","arguments":{}}]')`, source.ID); err != nil {
		t.Fatal(err)
	}
	sourceText(t, f, source.ID, "tool", "INTERNAL TOOL SECRET")
	if _, err := f.service.db.Exec(ctx, `INSERT INTO messages(conversation_id,role,content,metadata) VALUES($1,'assistant','INCOMPLETE OUTPUT','{"incomplete":true}')`, source.ID); err != nil {
		t.Fatal(err)
	}
	sourceText(t, f, source.ID, "assistant", "Confirmed decision")
	tx, err := f.service.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	saved, _, err := reference.Resolve(ctx, tx, f.workspaceID, f.conversationID, []reference.Input{{Type: "conversation", ID: source.ID}})
	_ = tx.Rollback(ctx)
	if err != nil || len(saved) != 1 {
		t.Fatalf("resolve: %#v %v", saved, err)
	}
	run := f.startRun(t, "Use the selected conversation")
	saveReferenceMetadata(t, f, run, map[string]any{"conversations": saved})
	sourceText(t, f, source.ID, "assistant", "LATER MESSAGE MUST NOT LEAK")
	if _, err = f.service.db.Exec(ctx, `UPDATE conversations SET title='Changed source title' WHERE id=$1`, source.ID); err != nil {
		t.Fatal(err)
	}
	var parts []string
	seq, character := int64(1), 0
	for count := 0; count < 10; count++ {
		page, err := reference.ReadRunConversation(ctx, f.service.db, run, f.conversationID, source.ID, seq, character, 1)
		if err != nil {
			t.Fatal(err)
		}
		if page.Conversation.Title != "Saved source title" || page.Conversation.MaxSeq != saved[0].MaxSeq {
			t.Fatal("snapshot labels or cutoff changed")
		}
		n := 0
		for _, message := range page.Messages {
			n += len([]rune(message.Content))
			parts = append(parts, message.Content)
		}
		if n > 12000 {
			t.Fatal("unbounded page")
		}
		if !page.HasMore {
			break
		}
		if page.NextSeq < seq || (page.NextSeq == seq && page.NextCharacter <= character) {
			t.Fatal("cursor did not advance")
		}
		seq, character = page.NextSeq, page.NextCharacter
		if count == 9 {
			t.Fatal("pagination failed to terminate")
		}
	}
	if strings.Join(parts, "") != "Initial source request"+long+"Confirmed decision" {
		t.Fatal("paging lost Unicode text or leaked late/internal content")
	}
	if _, err = reference.ReadRunConversation(ctx, f.service.db, uuid.New(), f.conversationID, source.ID, 1, 0, 10); err == nil {
		t.Fatal("forged run grant accepted")
	}
	if _, err = reference.ReadRunConversation(ctx, f.service.db, run, uuid.New(), source.ID, 1, 0, 10); err == nil {
		t.Fatal("forged caller conversation accepted")
	}
	unselected := referenceSource(t, f, "Unselected source")
	if _, err = reference.ReadRunConversation(ctx, f.service.db, run, f.conversationID, unselected.ID, 1, 0, 10); err == nil {
		t.Fatal("unselected source accepted")
	}
	foreignWorkspace, foreignSource := uuid.New(), uuid.New()
	if _, err = f.service.db.Exec(ctx, `INSERT INTO workspaces(id,name) VALUES($1,'other')`, foreignWorkspace); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.db.Exec(ctx, `INSERT INTO conversations(id,workspace_id,created_by,agent_slug) VALUES($1,$2,$3,'lester')`, foreignSource, foreignWorkspace, f.userID); err != nil {
		t.Fatal(err)
	}
	saveReferenceMetadata(t, f, run, map[string]any{"conversations": []reference.Conversation{{ID: foreignSource, MaxSeq: 99}}})
	if _, err = reference.ReadRunConversation(ctx, f.service.db, run, f.conversationID, foreignSource, 1, 0, 10); err == nil {
		t.Fatal("forged stored metadata bypassed workspace scope")
	}
	saveReferenceMetadata(t, f, run, map[string]any{"conversations": saved})
	if err = f.service.finishCompletedRun(ctx, run, f.conversationID); err != nil {
		t.Fatal(err)
	}
	next := f.startRun(t, "New task without a reference")
	if _, err = reference.ReadRunConversation(ctx, f.service.db, next, f.conversationID, source.ID, 1, 0, 10); err == nil {
		t.Fatal("historical reference granted new-task authority")
	}
	saveReferenceMetadata(t, f, next, map[string]any{"conversations": saved})
	if _, err = f.service.db.Exec(ctx, `DELETE FROM conversations WHERE id=$1`, source.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = reference.ReadRunConversation(ctx, f.service.db, next, f.conversationID, source.ID, 1, 0, 10); err == nil {
		t.Fatal("deleted source remained readable")
	}
}

func TestReferenceValidationDoesNotCreateRunsOrMutateUserText(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	for _, inputs := range [][]reference.Input{
		{{Type: "conversation", ID: uuid.New()}}, {{Type: "conversation", ID: f.conversationID}}, {{Type: "skill", ID: uuid.New()}}, {{Type: "unknown", ID: uuid.New()}}, {{Type: "context", ID: uuid.Nil}}, make([]reference.Input, 9),
	} {
		if _, err := f.service.SendWithReferences(ctx, f.workspaceID, f.userID, f.conversationID, "Keep this draft", nil, nil, inputs); err == nil {
			t.Fatalf("invalid references accepted: %#v", inputs)
		}
	}
	var runs, messages int
	if err := f.service.db.QueryRow(ctx, `SELECT (SELECT count(*) FROM runs),(SELECT count(*) FROM messages)`).Scan(&runs, &messages); err != nil || runs != 0 || messages != 0 {
		t.Fatalf("invalid request persisted data: %d %d %v", runs, messages, err)
	}
	source := referenceSource(t, f, "Authoritative source label")
	sourceText(t, f, source.ID, "user", "Source fact")
	run, err := f.service.SendWithReferences(ctx, f.workspaceID, f.userID, f.conversationID, "Original user text", nil, nil, []reference.Input{{Type: "conversation", ID: source.ID}})
	if err != nil {
		t.Fatal(err)
	}
	stopCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err = f.service.Shutdown(stopCtx); err != nil {
		t.Fatal(err)
	}
	var content string
	var raw []byte
	if err = f.service.db.QueryRow(ctx, `SELECT m.content,m.metadata FROM runs r JOIN messages m ON m.id=r.input_message_id WHERE r.id=$1`, run).Scan(&content, &raw); err != nil {
		t.Fatal(err)
	}
	if content != "Original user text" || !strings.Contains(string(raw), "Authoritative source label") || strings.Contains(string(raw), "Source fact") {
		t.Fatalf("user text changed or source body eagerly injected: %s", raw)
	}
}

func TestReferenceSearchIsScopedLiteralAndExcludesCurrentConversation(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	visible := referenceSource(t, f, "budget_2026%")
	foreignWorkspace, foreignSource := uuid.New(), uuid.New()
	if _, err := f.service.db.Exec(ctx, `INSERT INTO workspaces(id,name) VALUES($1,'other')`, foreignWorkspace); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.db.Exec(ctx, `INSERT INTO conversations(id,workspace_id,created_by,agent_slug,title) VALUES($1,$2,$3,'lester','private budget')`, foreignSource, foreignWorkspace, f.userID); err != nil {
		t.Fatal(err)
	}
	service := reference.Service{DB: f.service.db}
	items, err := service.Search(ctx, f.workspaceID, f.conversationID, "conversation", "%")
	if err != nil || len(items) != 1 || items[0].ID != visible.ID {
		t.Fatalf("literal/scoped search: %#v %v", items, err)
	}
	items, err = service.Search(ctx, f.workspaceID, f.conversationID, "conversation", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == f.conversationID || item.ID == foreignSource {
			t.Fatal("current or foreign conversation leaked")
		}
	}
	if _, err = service.Preview(ctx, f.workspaceID, foreignSource); err == nil {
		t.Fatal("foreign preview accepted")
	}
	if _, err = service.Search(ctx, f.workspaceID, uuid.Nil, "unknown", ""); err == nil {
		t.Fatal("unknown search category accepted")
	}
}

func TestSelectedSkillInstallsOnceAndConversationReadingIsDurable(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	source := referenceSource(t, f, "Product decisions")
	sourceText(t, f, source.ID, "assistant", "The confirmed audience is independent developers.")
	skillID := uuid.New()
	if _, err := f.service.db.Exec(ctx, `INSERT INTO skills(id,slug,name,description,version,object_key) VALUES($1,'conversation-reader','Conversation Reader','Read selected conversations','1.0.0','test/reader.zip')`, skillID); err != nil {
		t.Fatal(err)
	}
	tx, err := f.service.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conversations, skills, err := reference.Resolve(ctx, tx, f.workspaceID, f.conversationID, []reference.Input{{Type: "conversation", ID: source.ID}, {Type: "skill", ID: skillID}})
	_ = tx.Rollback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	run := f.startRun(t, "Summarize the referenced decisions with the selected Skill")
	saveReferenceMetadata(t, f, run, map[string]any{"conversations": conversations, "skills": skills})
	server, _ := newTranscriptSandboxServer(t, nil)
	f.service.sandboxes = sandbox.NewClient(server.URL, "")
	computer := &Computer{SandboxID: "test", WorkDir: conversationWorkDir(f.conversationID)}
	installs := 0
	instructions, err := os.ReadFile("../skill/defaults/conversation-reader/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	f.service.SetAgentSkillInstaller(func(ctx context.Context, workspace, user, conversation uuid.UUID, sandboxID, workDir, slug string) error {
		installs++
		if workspace != f.workspaceID || user != f.userID || conversation != f.conversationID || slug != "conversation-reader" {
			t.Fatal("incorrect install scope")
		}
		if err := f.service.sandboxes.WriteFile(ctx, sandboxID, workDir, ".agent/skills/conversation-reader/SKILL.md", instructions); err != nil {
			return err
		}
		_, err := f.service.db.Exec(ctx, `INSERT INTO conversation_skills(conversation_id,skill_id,installed_by) VALUES($1,$2,$3)`, conversation, skillID, user)
		return err
	})
	conversation, messages, err := f.service.Get(ctx, f.workspaceID, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = f.service.installSelectedSkills(ctx, f.workspaceID, conversation, run, messages, computer); err != nil {
			t.Fatal(err)
		}
	}
	if installs != 1 {
		t.Fatal("already-installed Skill was overwritten")
	}
	request := model.ModelRequest{Model: "test", System: "Use only explicit references", Messages: modelHistory(messages), Tools: f.service.tools.Definitions()}
	if strings.Contains(request.Messages[0].Content, "independent developers") {
		t.Fatal("source contents loaded before the tool call")
	}
	client := &scriptedModel{respond: func(step int, r model.ModelRequest) []model.ModelEvent {
		if step == 0 {
			raw, _ := json.Marshal(map[string]any{"conversation_id": source.ID})
			return []model.ModelEvent{{ToolCall: &model.ToolCall{ID: "load-ref-skill", Name: "load_skill", Index: 0, Arguments: json.RawMessage(`{"name":"conversation-reader"}`)}}, {ToolCall: &model.ToolCall{ID: "read-ref-source", Name: "read_conversation", Index: 1, Arguments: raw}}}
		}
		if step != 1 {
			t.Fatal("unexpected model iteration")
		}
		if len(r.Messages) < 4 || !strings.Contains(r.Messages[len(r.Messages)-2].Content, "Conversation Reader") || !strings.Contains(r.Messages[len(r.Messages)-1].Content, "independent developers") {
			t.Fatal("actual Skill/content tool results missing")
		}
		return []model.ModelEvent{{Delta: "The source confirms independent developers as the audience."}}
	}}
	f.service.executor.executeTurns(ctx, f.conversationID, run, computer, client, request)
	stored := f.messages(t)
	if len(stored) != 5 || stored[2].ToolName != "load_skill" || stored[3].ToolName != "read_conversation" || !strings.Contains(stored[3].Content, "independent developers") {
		t.Fatalf("tool results not durable: %#v", stored)
	}
	if _, err = f.service.db.Exec(ctx, `DELETE FROM conversation_skills WHERE conversation_id=$1`, f.conversationID); err != nil {
		t.Fatal(err)
	}
	next := f.startRun(t, "A new task without a Skill selection")
	_, messages, err = f.service.Get(ctx, f.workspaceID, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.installSelectedSkills(ctx, f.workspaceID, conversation, next, messages, computer); err != nil {
		t.Fatal(err)
	}
	if installs != 1 {
		t.Fatal("historical Skill selection activated in a new task")
	}
}
