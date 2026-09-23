package conversation

import (
	"context"
	"strings"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/agent"
	"github.com/biubiuqiu/lester-agent/backend/prompts"
	"github.com/google/uuid"
)

func TestCustomAgentConversationSnapshotsAndIsolation(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	service := &agent.Service{DB: f.service.db}
	created, err := service.Save(ctx, f.workspaceID, uuid.Nil, agent.Agent{Name: "Researcher", Description: "查证资料", Instructions: "Use citations and distinguish evidence from inference.", SkillSlugs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Resolve(ctx, uuid.New(), created.Slug); err == nil {
		t.Fatal("cross-workspace agent resolution succeeded")
	}
	conversation, err := f.service.Create(ctx, f.workspaceID, f.userID, created.Slug, "Research", uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	created.Instructions = "Different instructions"
	updated, err := service.Save(ctx, f.workspaceID, created.ID, created)
	if err != nil || updated.Version != 2 {
		t.Fatalf("update: %v", err)
	}
	if _, err = service.Save(ctx, f.workspaceID, created.ID, created); err == nil {
		t.Fatal("stale update succeeded")
	}
	if err = service.Delete(ctx, f.workspaceID, created.ID, updated.Version); err != nil {
		t.Fatal(err)
	}
	stored, _, err := f.service.Get(ctx, f.workspaceID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AgentName != "Researcher" || stored.AgentInstructions != "Use citations and distinguish evidence from inference." {
		t.Fatalf("conversation snapshot changed: %#v", stored)
	}
	if _, err = f.service.Create(ctx, f.workspaceID, f.userID, created.Slug, "New", uuid.Nil); err == nil {
		t.Fatal("deleted agent accepted for new conversation")
	}
	system, err := prompts.ComposeCustom(stored.AgentInstructions, stored.ID.String(), f.workspaceID.String(), "test", "running", nil)
	if err != nil || !strings.Contains(system, "Use citations") || strings.Contains(system, "Different instructions") {
		t.Fatalf("custom prompt: %v %s", err, system)
	}
}
