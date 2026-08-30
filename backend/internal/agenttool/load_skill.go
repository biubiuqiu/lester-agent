package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"regexp"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

var skillSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type LoadSkill struct{ DB *pgxpool.Pool }

type loadSkillInput struct {
	Name string `json:"name"`
}

func (LoadSkill) Definition() model.Tool {
	return model.Tool{Name: "load_skill", Description: "Load the full instructions for an installed Skill before applying it. Use only names listed in available_skills.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{
		"name": map[string]any{"type": "string", "description": "Installed Skill slug, for example code-review."},
	}, "required": []string{"name"}, "additionalProperties": false}}
}

func (tool LoadSkill) Execute(ctx context.Context, environment Environment, raw json.RawMessage) (any, error) {
	var input loadSkillInput
	if err := decodeArguments(raw, &input); err != nil {
		return nil, err
	}
	name, err := required(input.Name, "name")
	if err != nil {
		return nil, err
	}
	if !skillSlugPattern.MatchString(name) {
		return nil, errors.New("invalid skill name")
	}
	var installed bool
	if tool.DB == nil {
		return nil, errors.New("load_skill repository is unavailable")
	}
	err = tool.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_skills cs JOIN skills sk ON sk.id=cs.skill_id WHERE cs.conversation_id=$1 AND sk.slug=$2)`, environment.ConversationID, name).Scan(&installed)
	if err != nil {
		return nil, err
	}
	if !installed {
		return nil, errors.New("skill is not installed for this conversation")
	}
	data, err := environment.Sandboxes.ReadFile(ctx, environment.SandboxID, environment.WorkDir, path.Join(".agent/skills", name, "SKILL.md"))
	if err != nil {
		return nil, err
	}
	content, truncated, omitted := truncateText(string(data), outputCharacterLimit)
	result := map[string]any{"name": name, "content": content, "truncated": truncated}
	if truncated {
		result["omitted_characters"] = omitted
		result["notice"] = "Skill instructions were truncated at 30000 characters. Read the installed SKILL.md directly for the remainder."
	}
	return result, nil
}
