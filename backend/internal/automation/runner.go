package automation

import (
	"context"
	"github.com/biubiuqiu/lester-agent/backend/internal/conversation"
	"github.com/google/uuid"
)

type ConversationRunner struct{ Service *conversation.Service }

func (r ConversationRunner) Start(ctx context.Context, i Input) (uuid.UUID, error) {
	return r.Service.StartTriggeredTask(ctx, conversation.TriggeredTask{ID: i.ID, WorkspaceID: i.WorkspaceID, UserID: i.UserID, ConversationID: i.ConversationID, ProjectID: i.Config.ProjectID, ModelID: i.Config.ModelID, Name: i.Config.Name, Agent: i.Config.Agent, Prompt: i.Config.Prompt, Permission: i.Config.Permission})
}
func (r ConversationRunner) Lookup(ctx context.Context, i Input) (uuid.UUID, error) {
	receipt, err := r.Service.Submission(ctx, i.WorkspaceID, i.UserID, i.ConversationID, i.ID)
	if err != nil || receipt == nil {
		return uuid.Nil, err
	}
	return receipt.RunID, nil
}
