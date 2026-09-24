package agenttool

import (
	"context"
	"encoding/json"

	"github.com/biubiuqiu/lester-agent/backend/internal/agent"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SaveAgent struct{ DB *pgxpool.Pool }

func (SaveAgent) Definition() model.Tool {
	return model.Tool{Name: "save_agent", Description: "Create or update the Agent designed in this conversation. Only use after learning enough about the user's goal, constraints and desired output. The Agent becomes visible in the right-hand configuration view. Calling again updates the same Agent; include the full current definition every time. Do not call before the user has agreed to a concrete direction.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{
		"name":         map[string]any{"type": "string", "description": "A short, distinct Agent name."},
		"description":  map[string]any{"type": "string", "description": "One-sentence purpose, max 500 characters."},
		"instructions": map[string]any{"type": "string", "description": "Complete, reusable system instructions, max 20000 characters."},
		"skill_slugs":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Only slugs listed in the available Skill catalog."},
	}, "required": []string{"name", "description", "instructions", "skill_slugs"}, "additionalProperties": false}}
}

func (tool SaveAgent) Execute(ctx context.Context, environment Environment, raw json.RawMessage) (any, error) {
	var input agent.Agent
	if err := decodeArguments(raw, &input); err != nil {
		return nil, err
	}
	saved, err := (&agent.Service{DB: tool.DB}).SaveFromDesigner(ctx, environment.ConversationID, input)
	if err != nil {
		return nil, err
	}
	if environment.Emit != nil {
		environment.Emit("AGENT_SAVED", map[string]any{"agent_id": saved.ID, "name": saved.Name, "slug": saved.Slug})
	}
	return map[string]any{"agent_id": saved.ID, "slug": saved.Slug, "name": saved.Name, "version": saved.Version}, nil
}
