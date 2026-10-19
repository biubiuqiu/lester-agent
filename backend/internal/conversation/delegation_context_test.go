package conversation

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestDelegationMemoryProjectMustMatchAtImmediateAndQueuedAcceptance(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	project := uuid.New()
	if _, err := f.service.db.Exec(ctx, `INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'another project')`, project, f.workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SendSubmission(WithDelegationProject(ctx, project), f.workspaceID, f.userID, f.conversationID, uuid.New(), "private project memory", nil, nil, nil, "execute", "standard"); !errors.Is(err, ErrDelegationProjectChanged) {
		t.Fatal("moved project accepted delegated memory", err)
	}
	var count int
	if err := f.service.db.QueryRow(ctx, `SELECT count(*) FROM messages WHERE conversation_id=$1`, f.conversationID).Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected memory was persisted", err)
	}
	var run uuid.UUID
	if err := f.service.db.QueryRow(ctx, `INSERT INTO runs(conversation_id,status) VALUES($1,'running') RETURNING id`, f.conversationID).Scan(&run); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.AcceptRunInput(WithDelegationProject(ctx, project), f.workspaceID, f.userID, f.conversationID, RunInputRequest{ID: uuid.New(), RunID: run, Kind: "next", Content: "old-project facts", PermissionMode: "standard"}); !errors.Is(err, ErrRunInputRejected) {
		t.Fatal("queued moved project accepted", err)
	}
}
