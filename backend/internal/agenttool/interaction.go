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
	Recommended bool   `json:"recommended,omitempty"`
}
type UserQuestion struct {
	ID          string           `json:"id"`
	Prompt      string           `json:"prompt"`
	Options     []QuestionOption `json:"options,omitempty"`
	Header      string           `json:"header,omitempty"`
	MultiSelect bool             `json:"multi_select,omitempty"`
}
type InteractionRequest struct {
	Kind            string           `json:"kind"`
	Questions       []UserQuestion   `json:"questions,omitempty"`
	Title           string           `json:"title,omitempty"`
	Steps           []string         `json:"steps,omitempty"`
	Schedule        *ScheduleRequest `json:"schedule,omitempty"`
	SchedulePreview *SchedulePreview `json:"schedule_preview,omitempty"`
}

var interactionIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,39}$`)

func (r InteractionRequest) Validate() error {
	if r.Kind != "automation" && (r.Schedule != nil || r.SchedulePreview != nil) {
		return errors.New("unexpected schedule data")
	}
	if r.Kind == "automation" {
		if r.Schedule == nil || len(r.Questions) > 0 || len(r.Steps) > 0 || r.Title != "" {
			return errors.New("schedule proposal requires only schedule arguments")
		}
		if err := r.Schedule.Validate(); err != nil {
			return err
		}
	} else if r.Kind == "question" {
		if len(r.Questions) < 1 || len(r.Questions) > 3 || r.Title != "" || len(r.Steps) > 0 {
			return errors.New("questions require one to three question entries")
		}
		seen := map[string]bool{}
		for _, q := range r.Questions {
			if !interactionIDPattern.MatchString(q.ID) || seen[q.ID] || strings.TrimSpace(q.Prompt) == "" || utf8.RuneCountInString(q.Prompt) > 2000 || len(q.Options) > 6 || len(q.Options) == 1 || utf8.RuneCountInString(q.Header) > 12 || q.MultiSelect && len(q.Options) < 2 {
				return errors.New("invalid question ID, prompt or options")
			}
			seen[q.ID] = true
			options := map[string]bool{}
			recommended := 0
			for _, o := range q.Options {
				if o.Recommended {
					recommended++
				}
				if recommended > 1 {
					return errors.New("offer at most one recommended choice")
				}
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
	limit := 24000
	if r.Kind == "automation" {
		limit = 64000
	}
	raw, err := json.Marshal(r)
	if err != nil || len(raw) > limit {
		return errors.New("interaction exceeds its size limit")
	}
	return nil
}

type AskUserQuestion struct{}

func (AskUserQuestion) Definition() model.Tool {
	option := map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "label": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"}, "recommended": map[string]any{"type": "boolean", "description": "Recommend at most one option; never select it for the user."}}, "required": []string{"id", "label"}, "additionalProperties": false}
	question := map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "header": map[string]any{"type": "string", "maxLength": 12, "description": "Optional short navigation label."}, "prompt": map[string]any{"type": "string"}, "multi_select": map[string]any{"type": "boolean", "description": "Allow multiple offered options together with optional free text."}, "options": map[string]any{"type": "array", "maxItems": 6, "items": option}}, "required": []string{"id", "prompt"}, "additionalProperties": false}
	return model.Tool{Name: "ask_user_question", Description: "Ask one to three structured questions only when needed information or a meaningful choice is missing. Offer concise options with useful tradeoffs; use multi_select for independent choices. Free text remains valid alongside selections. Answers are information, never execution approval or an instruction to continue despite a request to wait. The run waits for an explicit answer; never preselect an option or assume a timeout is consent.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"questions": map[string]any{"type": "array", "minItems": 1, "maxItems": 3, "items": question}}, "required": []string{"questions"}, "additionalProperties": false}}
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
	case "read", "list_files", "load_skill", "read_conversation", "read_tool_result", "task_output", "ask_user_question", "propose_plan", "list_schedules", "list_app_connections", "read_app":
		return true
	}
	return false
}

// EnterPlan only reduces authority. Leaving Plan still requires the existing
// exact-version, user-approved propose_plan interaction.
type EnterPlan struct{}

func (EnterPlan) Definition() model.Tool {
	return model.Tool{Name: "enter_plan_mode", Description: "Enter read-only planning when the user requests it or before a complex, multi-file or architectural change that needs a reviewed approach. Explain briefly why. Use this before any mutations, as a standalone tool call. Then inspect with list_files/read and propose_plan for explicit review. Do small, clear tasks directly; do not re-enter for an already approved plan unless its scope materially changes.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"reason": map[string]any{"type": "string", "maxLength": 1000}}, "required": []string{"reason"}, "additionalProperties": false}}
}
func (EnterPlan) Execute(ctx context.Context, env Environment, raw json.RawMessage) (any, error) {
	var input struct {
		Reason string `json:"reason"`
	}
	if err := decodeArguments(raw, &input); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Reason) == "" || utf8.RuneCountInString(input.Reason) > 1000 {
		return nil, errors.New("provide a brief planning reason")
	}
	if env.EnterPlan == nil {
		return nil, errors.New("planning service unavailable")
	}
	return env.EnterPlan(ctx, input.Reason)
}
