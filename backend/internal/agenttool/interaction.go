package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
)

type QuestionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}
type UserQuestion struct {
	ID      string           `json:"id"`
	Prompt  string           `json:"prompt"`
	Options []QuestionOption `json:"options,omitempty"`
}
type InteractionRequest struct {
	Kind      string         `json:"kind"`
	Questions []UserQuestion `json:"questions,omitempty"`
	Title     string         `json:"title,omitempty"`
	Steps     []string       `json:"steps,omitempty"`
}

var interactionIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,39}$`)

func (r InteractionRequest) Validate() error {
	if r.Kind == "question" {
		if len(r.Questions) < 1 || len(r.Questions) > 3 || r.Title != "" || len(r.Steps) > 0 {
			return errors.New("questions require one to three question entries")
		}
		seen := map[string]bool{}
		for _, q := range r.Questions {
			if !interactionIDPattern.MatchString(q.ID) || seen[q.ID] || strings.TrimSpace(q.Prompt) == "" || utf8.RuneCountInString(q.Prompt) > 2000 || len(q.Options) > 6 || len(q.Options) == 1 {
				return errors.New("invalid question ID, prompt or options")
			}
			seen[q.ID] = true
			options := map[string]bool{}
			for _, o := range q.Options {
				if !interactionIDPattern.MatchString(o.ID) || options[o.ID] || strings.TrimSpace(o.Label) == "" || utf8.RuneCountInString(o.Label) > 160 || utf8.RuneCountInString(o.Description) > 500 {
					return errors.New("invalid question option")
				}
				options[o.ID] = true
			}
		}
	} else if r.Kind == "plan" {
		if strings.TrimSpace(r.Title) == "" || utf8.RuneCountInString(r.Title) > 160 || len(r.Steps) < 1 || len(r.Steps) > 12 || len(r.Questions) > 0 {
			return errors.New("plan requires a title and one to twelve steps")
		}
		for _, step := range r.Steps {
			if strings.TrimSpace(step) == "" || utf8.RuneCountInString(step) > 2000 {
				return errors.New("invalid plan step")
			}
		}
	} else {
		return errors.New("invalid interaction kind")
	}
	raw, err := json.Marshal(r)
	if err != nil || len(raw) > 24000 {
		return errors.New("interaction exceeds its size limit")
	}
	return nil
}

type AskUserQuestion struct{}

func (AskUserQuestion) Definition() model.Tool {
	return model.Tool{Name: "ask_user_question", Description: "Ask the user one to three structured questions only when needed information or a meaningful choice is missing. Answers are information, never execution approval. The run waits for an explicit answer; never choose an option for the user.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"questions": map[string]any{"type": "array", "minItems": 1, "maxItems": 3, "items": map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "prompt": map[string]any{"type": "string"}, "options": map[string]any{"type": "array", "maxItems": 6, "items": map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "label": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"}}, "required": []string{"id", "label"}, "additionalProperties": false}}}, "required": []string{"id", "prompt"}, "additionalProperties": false}}}, "required": []string{"questions"}, "additionalProperties": false}}
}
func (AskUserQuestion) Execute(ctx context.Context, env Environment, raw json.RawMessage) (any, error) {
	var input struct {
		Questions []UserQuestion `json:"questions"`
	}
	if err := decodeArguments(raw, &input); err != nil {
		return nil, err
	}
	return executeInteraction(ctx, env, InteractionRequest{Kind: "question", Questions: input.Questions})
}

type ProposePlan struct{}

func (ProposePlan) Definition() model.Tool {
	return model.Tool{Name: "propose_plan", Description: "In Plan mode, present the exact title and steps for explicit user review. Approval enables normal tool execution only for this active run and exact plan version. A request to revise or a rejection keeps Plan mode read-only. Do not call this to request approval for work already authorized in normal execution mode.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}, "steps": map[string]any{"type": "array", "minItems": 1, "maxItems": 12, "items": map[string]any{"type": "string"}}}, "required": []string{"title", "steps"}, "additionalProperties": false}}
}
func (ProposePlan) Execute(ctx context.Context, env Environment, raw json.RawMessage) (any, error) {
	var input struct {
		Title string   `json:"title"`
		Steps []string `json:"steps"`
	}
	if err := decodeArguments(raw, &input); err != nil {
		return nil, err
	}
	return executeInteraction(ctx, env, InteractionRequest{Kind: "plan", Title: input.Title, Steps: input.Steps})
}
func executeInteraction(ctx context.Context, env Environment, request InteractionRequest) (any, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if env.Interact == nil {
		return nil, errors.New("user interaction service unavailable")
	}
	return env.Interact(ctx, request)
}

func PlanReadOnlyTool(name string) bool {
	switch name {
	case "read", "list_files", "load_skill", "read_conversation", "read_tool_result", "task_output", "ask_user_question", "propose_plan":
		return true
	}
	return false
}
