package automation

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type scheduleActor struct {
	workspace, user, project, model uuid.UUID
	agent, timezone                 string
}

// Resolve identity from the active accepted run, never from tool arguments or
// the conversation's original creator. Hints do not grant any authority.
func scheduleContext(ctx context.Context, q scheduleQuery, conversation, run uuid.UUID) (scheduleActor, error) {
	var a scheduleActor
	err := q.QueryRow(ctx, `SELECT c.workspace_id,r.submitted_by,c.project_id,COALESCE(r.model_deployment_id,c.model_deployment_id),c.agent_slug,COALESCE(m.metadata->>'client_timezone','') FROM runs r JOIN conversations c ON c.id=r.conversation_id JOIN users u ON u.id=r.submitted_by JOIN workspace_members wm ON wm.workspace_id=c.workspace_id AND wm.user_id=u.id LEFT JOIN messages m ON m.id=r.input_message_id AND m.conversation_id=c.id WHERE r.id=$1 AND c.id=$2 AND r.status='running' AND NOT u.disabled AND (NOT u.email_verification_required OR u.email_verified)`, run, conversation).Scan(&a.workspace, &a.user, &a.project, &a.model, &a.agent, &a.timezone)
	if err != nil {
		return a, ErrNotFound
	}
	return a, nil
}

func (s *Service) ListSchedules(ctx context.Context, conversation, run, id uuid.UUID) (map[string]any, error) {
	a, err := scheduleContext(ctx, s.DB, conversation, run)
	if err != nil {
		return nil, err
	}
	items, err := s.List(ctx, a.workspace, a.user)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"current_time_utc": time.Now().UTC(), "browser_timezone_hint": a.timezone, "default_project_id": a.project, "default_model_deployment_id": a.model, "default_agent_slug": a.agent, "new_schedule_permission_mode": "standard", "instruction": "Review card confirmation is required. Each occurrence starts a separate conversation with only its saved prompt, not this chat's history, files or references. Ask for a time zone if no hint or explicit zone is available."}
	if zone, err := time.LoadLocation(a.timezone); a.timezone != "" && err == nil {
		result["current_time_local"] = time.Now().In(zone).Format(time.RFC3339)
	}
	if id != uuid.Nil {
		item, err := s.Get(ctx, a.workspace, a.user, id)
		if err != nil {
			return nil, err
		}
		if item.Kind != "schedule" {
			return nil, ErrNotFound
		}
		result["schedule"] = item
	} else {
		list := []map[string]any{}
		for _, item := range items {
			if item.Kind == "schedule" {
				list = append(list, map[string]any{"id": item.ID, "name": item.Name, "project_id": item.ProjectID, "revision": item.Revision, "enabled": item.Enabled, "cron": item.Cron, "timezone": item.Timezone, "run_at": item.RunAt, "next_run_at": item.NextRunAt})
			}
		}
		result["schedules"] = list
	}
	rows, err := s.DB.Query(ctx, `SELECT id,name FROM projects WHERE workspace_id=$1 ORDER BY is_default DESC,created_at,id LIMIT 100`, a.workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err = rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		projects = append(projects, map[string]any{"id": id, "name": name})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	result["projects"] = projects
	return result, nil
}

func (s *Service) PrepareSchedule(ctx context.Context, tx pgx.Tx, conversation, run uuid.UUID, r agenttool.ScheduleRequest) (*agenttool.SchedulePreview, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	a, err := scheduleContext(ctx, tx, conversation, run)
	if err != nil {
		return nil, err
	}
	c := Config{Kind: "schedule", ProjectID: a.project, Agent: a.agent, ModelID: a.model, Permission: "standard", Enabled: true, Timezone: a.timezone}
	id := r.ID
	if r.Action == "create" {
		id = uuid.New()
	} else {
		item, err := scanDefinition(tx.QueryRow(ctx, `SELECT `+columns+` FROM automations WHERE workspace_id=$1 AND created_by=$2 AND id=$3 AND deleted_at IS NULL`, a.workspace, a.user, id))
		if err != nil {
			return nil, err
		}
		if item.Kind != "schedule" || item.Revision != r.Revision {
			return nil, ErrConflict
		}
		c = item.Config
	}
	if r.Name != "" {
		c.Name = r.Name
	}
	if r.Prompt != "" {
		c.Prompt = r.Prompt
	}
	if r.ProjectID != uuid.Nil {
		c.ProjectID = r.ProjectID
	}
	if r.Timezone != "" {
		c.Timezone = r.Timezone
	}
	if r.Cron != "" {
		c.Cron = r.Cron
		c.RunAt = nil
	}
	if r.RunAt != nil {
		c.RunAt = r.RunAt
		c.Cron = ""
	}
	if r.Action == "pause" {
		c.Enabled = false
	}
	if r.Action == "resume" {
		c.Enabled = true
	}
	// Do not fall back to the server's UTC when the user's zone is unknown.
	if strings.TrimSpace(c.Timezone) == "" {
		return nil, errors.New("time zone unknown: ask the user for an IANA time zone before proposing this schedule")
	}
	c = normalize(c)
	next, err := validateSchedule(ctx, tx, a.workspace, c)
	if err != nil {
		return nil, err
	}
	p := &agenttool.SchedulePreview{ID: id, Revision: r.Revision, Name: c.Name, Prompt: c.Prompt, ProjectID: c.ProjectID, ModelID: c.ModelID, Agent: c.Agent, Permission: c.Permission, Enabled: c.Enabled, Cron: c.Cron, Timezone: c.Timezone, RunAt: c.RunAt, NextRunAt: next}
	if err = tx.QueryRow(ctx, `SELECT p.name,m.name FROM projects p JOIN model_deployments m ON m.id=$3 WHERE p.workspace_id=$1 AND p.id=$2`, a.workspace, p.ProjectID, p.ModelID).Scan(&p.ProjectName, &p.ModelName); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) ApplySchedule(ctx context.Context, tx pgx.Tx, conversation, run, user uuid.UUID, p *agenttool.SchedulePreview) error {
	if p == nil {
		return ErrInvalid
	}
	a, err := scheduleContext(ctx, tx, conversation, run)
	if err != nil {
		return err
	}
	if a.user != user {
		return ErrNotFound
	}
	c := Config{Name: p.Name, Kind: "schedule", ProjectID: p.ProjectID, Agent: p.Agent, ModelID: p.ModelID, Prompt: p.Prompt, Permission: p.Permission, Enabled: p.Enabled, Cron: p.Cron, Timezone: p.Timezone, RunAt: p.RunAt}
	next, err := validateSchedule(ctx, tx, a.workspace, c)
	if err != nil {
		return err
	}
	// Never silently replace the occurrence that the user actually reviewed.
	if (next == nil) != (p.NextRunAt == nil) || next != nil && !next.Equal(*p.NextRunAt) {
		return ErrConflict
	}
	saved, _, err := s.saveTx(ctx, tx, a.workspace, user, p.ID, c, p.Revision)
	if err != nil {
		return err
	}
	if (saved.NextRunAt == nil) != (p.NextRunAt == nil) || saved.NextRunAt != nil && !saved.NextRunAt.Equal(*p.NextRunAt) {
		return ErrConflict
	}
	return nil
}
