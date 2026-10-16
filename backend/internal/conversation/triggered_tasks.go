package conversation

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

type TriggeredTask struct {
	ID, WorkspaceID, UserID, ConversationID, ProjectID, ModelID uuid.UUID
	Name, Agent, Prompt, Permission                             string
}

// A trigger owns a preallocated conversation and submission identity. Recovery
// looks up the receipt; it must never turn an uncertain trigger into a new run.
func (s *Service) StartTriggeredTask(ctx context.Context, input TriggeredTask) (uuid.UUID, error) {
	if input.ID == uuid.Nil || input.ConversationID == uuid.Nil || input.ProjectID == uuid.Nil || input.ModelID == uuid.Nil {
		return uuid.Nil, errors.New("invalid trigger identity")
	}
	authority, err := s.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer authority.Rollback(ctx)
	var actor uuid.UUID
	// Serialize acceptance with account disable and workspace membership removal.
	if err = authority.QueryRow(ctx, `SELECT u.id FROM users u JOIN workspace_members wm ON wm.user_id=u.id WHERE u.id=$1 AND wm.workspace_id=$2 AND NOT u.disabled AND (NOT u.email_verification_required OR u.email_verified) FOR SHARE OF u,wm`, input.UserID, input.WorkspaceID).Scan(&actor); err != nil {
		return uuid.Nil, errors.New("trigger authorization changed")
	}
	if receipt, err := s.Submission(ctx, input.WorkspaceID, input.UserID, input.ConversationID, input.ID); err != nil {
		return uuid.Nil, err
	} else if receipt != nil {
		return receipt.RunID, nil
	}
	if _, err = s.createWithID(ctx, input.ConversationID, input.WorkspaceID, input.UserID, input.Agent, input.Name, input.ModelID, input.ProjectID); err != nil {
		return uuid.Nil, err
	}
	return s.SendSubmission(ctx, input.WorkspaceID, input.UserID, input.ConversationID, input.ID, input.Prompt, nil, nil, nil, "execute", input.Permission)
}
