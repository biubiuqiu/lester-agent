package agenttool

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/biubiuqiu/lester-agent/backend/internal/deliverable"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/google/uuid"
)

type RegisterDeliverable struct{ Service *deliverable.Service }

func (RegisterDeliverable) Definition() model.Tool {
	return model.Tool{Name: "register_deliverable", Description: "Register an actual HTML page or Markdown document as a user-facing deliverable with a clear title and concise delivery summary. Call after writing the entry file; use the same entry_path to update its stable ID after edits. The service reads the entry and records its digest. Registration does not validate task requirements, save a historical version or publish files. Do not claim checks passed unless they actually ran.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{
		"entry_path": pathProperty(), "title": map[string]any{"type": "string", "description": "User-facing name, 1–120 characters."}, "summary": map[string]any{"type": "string", "description": "Concise description of the actual delivery and remaining limitations, up to 2000 characters."},
	}, "required": []string{"entry_path", "title", "summary"}, "additionalProperties": false}}
}
func (tool RegisterDeliverable) Execute(ctx context.Context, environment Environment, raw json.RawMessage) (any, error) {
	var input deliverable.Input
	if err := decodeArguments(raw, &input); err != nil {
		return nil, err
	}
	var workspaceID uuid.UUID
	if err := tool.Service.DB.QueryRow(ctx, `SELECT workspace_id FROM conversations WHERE id=$1`, environment.ConversationID).Scan(&workspaceID); err != nil {
		return nil, errors.New("conversation not found")
	}
	return tool.Service.Register(ctx, workspaceID, environment.ConversationID, environment.RunID, environment.SandboxID, environment.WorkDir, input)
}
