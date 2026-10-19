package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ScheduleRequest contains model arguments only. Identity, model and permission
// are resolved by the server, never accepted from model-authored arguments.
type ScheduleRequest struct {
	Action    string     `json:"action"`
	ID        uuid.UUID  `json:"automation_id,omitempty"`
	Revision  int64      `json:"revision,omitempty"`
	Name      string     `json:"name,omitempty"`
	Prompt    string     `json:"prompt,omitempty"`
	ProjectID uuid.UUID  `json:"project_id,omitempty"`
	Cron      string     `json:"cron,omitempty"`
	Timezone  string     `json:"timezone,omitempty"`
	RunAt     *time.Time `json:"run_at,omitempty"`
}

func (r ScheduleRequest) Validate() error {
	if r.Action != "create" && r.Action != "update" && r.Action != "pause" && r.Action != "resume" {
		return errors.New("invalid schedule action")
	}
	if r.Action == "create" {
		if r.ID != uuid.Nil || r.Revision != 0 || strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.Prompt) == "" || (r.Cron == "") == (r.RunAt == nil) {
			return errors.New("create requires name, prompt and exactly one of cron/run_at; omit ID and revision")
		}
	} else if r.ID == uuid.Nil || r.Revision < 1 {
		return errors.New("read the schedule first and provide its ID and current revision")
	}
	if r.Action == "update" && r.Name == "" && r.Prompt == "" && r.ProjectID == uuid.Nil && r.Cron == "" && r.Timezone == "" && r.RunAt == nil {
		return errors.New("update requires at least one changed field")
	}
	if r.Cron != "" && r.RunAt != nil {
		return errors.New("choose cron or run_at, not both")
	}
	if (r.Action == "pause" || r.Action == "resume") && (r.Name != "" || r.Prompt != "" || r.ProjectID != uuid.Nil || r.Cron != "" || r.Timezone != "" || r.RunAt != nil) {
		return errors.New("pause/resume cannot change task configuration")
	}
	if len(r.Name) > 480 || len(r.Prompt) > 16000 || len(r.Cron) > 100 || len(r.Timezone) > 100 {
		return errors.New("schedule arguments exceed limits")
	}
	return nil
}

type SchedulePreview struct {
	ID          uuid.UUID  `json:"id"`
	Revision    int64      `json:"revision"`
	Name        string     `json:"name"`
	Prompt      string     `json:"prompt"`
	ProjectID   uuid.UUID  `json:"project_id"`
	ProjectName string     `json:"project_name"`
	ModelID     uuid.UUID  `json:"model_deployment_id"`
	ModelName   string     `json:"model_name"`
	Agent       string     `json:"agent_slug"`
	Permission  string     `json:"permission_mode"`
	Enabled     bool       `json:"enabled"`
	Cron        string     `json:"cron"`
	Timezone    string     `json:"timezone"`
	RunAt       *time.Time `json:"run_at"`
	NextRunAt   *time.Time `json:"next_run_at"`
}

// The implementation lives with the automation service. Apply shares the
// interaction response transaction so confirmation and mutation commit together.
type ScheduleService interface {
	ListSchedules(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (map[string]any, error)
	PrepareSchedule(context.Context, pgx.Tx, uuid.UUID, uuid.UUID, ScheduleRequest) (*SchedulePreview, error)
	ApplySchedule(context.Context, pgx.Tx, uuid.UUID, uuid.UUID, uuid.UUID, *SchedulePreview) error
}

type ListSchedules struct{}

func (ListSchedules) Definition() model.Tool {
	return model.Tool{Name: "list_schedules", Description: "List this task actor's scheduled tasks (IDs, revisions, project and timing), current project/model defaults, current server time, browser time zone hint and available projects. Supply automation_id to read a schedule's complete task prompt before editing. Read-only; does not run tasks. Never infer the user's time zone from server UTC.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"automation_id": map[string]any{"type": "string", "format": "uuid"}}, "additionalProperties": false}}
}
func (ListSchedules) Execute(ctx context.Context, env Environment, raw json.RawMessage) (any, error) {
	var input struct {
		ID uuid.UUID `json:"automation_id"`
	}
	if err := decodeArguments(raw, &input); err != nil {
		return nil, err
	}
	if env.ListSchedules == nil {
		return nil, errors.New("schedule service unavailable")
	}
	return env.ListSchedules(ctx, input.ID)
}

type ProposeSchedule struct{}

func (ProposeSchedule) Definition() model.Tool {
	p := map[string]any{"action": map[string]any{"type": "string", "enum": []string{"create", "update", "pause", "resume"}}, "automation_id": map[string]any{"type": "string", "format": "uuid"}, "revision": map[string]any{"type": "integer", "minimum": 1}, "name": map[string]any{"type": "string", "maxLength": 120}, "prompt": map[string]any{"type": "string", "maxLength": 16000, "description": "Self-contained future task. No automatic inheritance of this chat's files, history, attachments or @ references."}, "project_id": map[string]any{"type": "string", "format": "uuid", "description": "Omit to use the current project on create or retain the existing project on update."}, "cron": map[string]any{"type": "string", "description": "Five-field cron, e.g. 0 9 * * 1. Mutually exclusive with run_at."}, "timezone": map[string]any{"type": "string", "description": "Real IANA time zone. Uses browser hint on create when omitted; ask if unavailable or ambiguous."}, "run_at": map[string]any{"type": "string", "format": "date-time", "description": "One-off RFC3339 instant with explicit offset. Mutually exclusive with cron."}}
	return model.Tool{Name: "propose_schedule", Description: "Propose creating, updating, pausing or resuming a server-side scheduled task. First list_schedules to resolve context, time zone, IDs and current revisions. Create requires name, self-contained prompt and cron or run_at; defaults to current Agent/model/project with standard permissions. Update accepts only changed fields, retaining model/Agent/permissions and enabled state. Pause/resume require only ID/revision. Shows an exact server-validated review card and waits for explicit user confirmation; no mutation before approval, even in full access. Rejection or change requests do not apply it. Only report success from the confirmed tool result. Runs in a new conversation per occurrence even with browser closed; never use sandbox cron or promise to remember a timer. Plan mode blocks this tool.", InputSchema: map[string]any{"type": "object", "properties": p, "required": []string{"action"}, "additionalProperties": false}}
}
func (ProposeSchedule) Execute(ctx context.Context, env Environment, raw json.RawMessage) (any, error) {
	var input ScheduleRequest
	if err := decodeArguments(raw, &input); err != nil {
		return nil, err
	}
	return executeInteraction(ctx, env, InteractionRequest{Kind: "automation", Schedule: &input})
}
