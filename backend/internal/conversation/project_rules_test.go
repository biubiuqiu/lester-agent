package conversation

import (
	"context"
	"errors"
	"github.com/biubiuqiu/lester-agent/backend/internal/project"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestProjectRulesSnapshotSurvivesEditMoveAndQueueDelay(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	var projectID uuid.UUID
	if err := f.service.db.QueryRow(ctx, `SELECT project_id FROM conversations WHERE id=$1`, f.conversationID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	service := project.Service{DB: f.service.db}
	first, err := service.SaveRules(ctx, f.workspaceID, projectID, "Use the existing design tokens", 1)
	if err != nil {
		t.Fatal(err)
	}
	parent := f.startRun(t, "start")
	queued, err := f.service.AcceptRunInput(ctx, f.workspaceID, f.userID, f.conversationID, RunInputRequest{ID: uuid.New(), RunID: parent, Kind: "next", Content: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	next, err := service.SaveRules(ctx, f.workspaceID, projectID, "Later rules must not affect accepted work", first.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.SaveRules(ctx, f.workspaceID, projectID, "stale overwrite", first.Version); !errors.Is(err, project.ErrRulesConflict) {
		t.Fatal("stale editor overwrote rules", err)
	}
	if repeated, err := service.SaveRules(ctx, f.workspaceID, projectID, next.Content, first.Version); err != nil || repeated.Version != next.Version {
		t.Fatal("lost-response retry created another version", err)
	}
	if _, err = service.Rules(ctx, uuid.New(), projectID); err == nil {
		t.Fatal("foreign workspace read rules")
	}
	destination, err := service.Create(ctx, f.workspaceID, "destination")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.db.Exec(ctx, `UPDATE conversations SET project_id=$2 WHERE id=$1`, f.conversationID, destination.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.service.finishCompletedRun(ctx, parent, f.conversationID); err != nil {
		t.Fatal(err)
	}
	guard, err := f.service.acquireRun(ctx, f.workspaceID, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	run, err := f.service.claimQueuedInput(ctx, f.workspaceID, f.conversationID)
	if err != nil || run == uuid.Nil {
		t.Fatal(err)
	}
	text, err := f.service.runProjectRulesContext(ctx, f.conversationID, run)
	if err != nil || !strings.Contains(text, first.Content) || strings.Contains(text, next.Content) || !strings.Contains(text, first.SHA256) {
		t.Fatal("accepted project rules changed after edit/move", text, err)
	}
	item, err := f.service.RunInput(ctx, f.workspaceID, f.userID, f.conversationID, queued.ID)
	if err != nil || item.Status != "started" {
		t.Fatal(err)
	}
}
