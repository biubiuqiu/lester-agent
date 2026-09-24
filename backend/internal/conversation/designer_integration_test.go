package conversation

import (
	"context"
	"errors"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/agent"
	"github.com/google/uuid"
)

func TestDesignerConversationOwnsOneAgent(t *testing.T) {
	fixture := newTranscriptFixture(t, false)
	ctx := context.Background()
	service := &agent.Service{DB: fixture.service.db}
	input := agent.Agent{Name: "Research Helper", Description: "Researches a topic", Instructions: "Investigate sources and summarize findings."}
	if _, err := service.SaveFromDesigner(ctx, fixture.conversationID, input); !errors.Is(err, agent.ErrNotDesigner) {
		t.Fatalf("ordinary conversation saved Agent: %v", err)
	}
	if _, err := fixture.service.db.Exec(ctx, `UPDATE conversations SET agent_slug='agent-designer' WHERE id=$1`, fixture.conversationID); err != nil {
		t.Fatal(err)
	}
	created, err := service.SaveFromDesigner(ctx, fixture.conversationID, input)
	if err != nil {
		t.Fatal(err)
	}
	if created.BuilderConversationID == nil || *created.BuilderConversationID != fixture.conversationID {
		t.Fatal("Agent not linked to design conversation")
	}
	input.Name = "Research Analyst"
	updated, err := service.SaveFromDesigner(ctx, fixture.conversationID, input)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID || updated.Version != created.Version+1 {
		t.Fatal("follow-up created a second Agent rather than updating the first")
	}
	loaded, err := service.Get(ctx, fixture.workspaceID, created.ID)
	if err != nil || loaded.BuilderConversationID == nil || *loaded.BuilderConversationID != fixture.conversationID || loaded.Name != "Research Analyst" {
		t.Fatalf("linked Agent detail missing: %v", err)
	}
	listed, err := service.List(ctx, fixture.workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, item := range listed {
		if item.ID == created.ID {
			found = item.BuilderConversationID != nil && *item.BuilderConversationID == fixture.conversationID
		}
	}
	if !found {
		t.Fatal("linked Agent missing from catalog")
	}
	otherWorkspace := uuid.New()
	if _, err = service.Get(ctx, otherWorkspace, created.ID); err == nil {
		t.Fatal("Agent visible across workspaces")
	}
	conversation, _, err := fixture.service.Get(ctx, fixture.workspaceID, fixture.conversationID)
	if err != nil || conversation.CreatedAgentID == nil || *conversation.CreatedAgentID != created.ID {
		t.Fatalf("conversation link missing: %v", err)
	}
}
