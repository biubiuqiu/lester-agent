package conversation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var errTaskCursor = errors.New("invalid task filter or cursor")

type taskCursor struct {
	Created time.Time `json:"created"`
	ID      uuid.UUID `json:"id"`
}
type CloudTask struct {
	RunID              uuid.UUID  `json:"run_id"`
	ConversationID     uuid.UUID  `json:"conversation_id"`
	Title              string     `json:"title"`
	Project            string     `json:"project"`
	Agent              string     `json:"agent"`
	State              string     `json:"state"`
	StartedAt          time.Time  `json:"started_at"`
	CompletedAt        *time.Time `json:"completed_at"`
	LastActivityAt     time.Time  `json:"last_activity_at"`
	Queued             int        `json:"queued"`
	Deliverables       int        `json:"deliverables"`
	BackgroundCommands int        `json:"background_commands"`
	UnknownCommands    int        `json:"unknown_commands"`
}
type CloudTasks struct {
	Tasks      []CloudTask `json:"tasks"`
	Next       string      `json:"next_cursor"`
	Attention  int         `json:"attention_count"`
	ObservedAt time.Time   `json:"observed_at"`
}

func (s *Service) CloudTasks(ctx context.Context, workspace uuid.UUID, filter, cursor string, limit int) (CloudTasks, error) {
	result := CloudTasks{Tasks: []CloudTask{}, ObservedAt: time.Now().UTC()}
	if filter != "all" && filter != "active" && filter != "attention" && filter != "finished" {
		return result, errTaskCursor
	}
	if limit < 1 || limit > 50 || len(cursor) > 512 {
		return result, errTaskCursor
	}
	end := taskCursor{Created: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), ID: uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")}
	if cursor != "" {
		end = taskCursor{}
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || len(raw) > 256 || json.Unmarshal(raw, &end) != nil || end.ID == uuid.Nil || end.Created.IsZero() {
			return result, errTaskCursor
		}
	}
	var raw []byte
	err := s.db.QueryRow(ctx, `WITH latest AS (
 SELECT r.* FROM conversations c CROSS JOIN LATERAL (SELECT r.* FROM runs r WHERE r.conversation_id=c.id ORDER BY r.created_at DESC,r.id DESC LIMIT 1) r WHERE c.workspace_id=$1
 ), facts AS (
 SELECT r.id,r.conversation_id,c.title,p.name AS project,COALESCE(NULLIF(c.agent_name,''),c.agent_slug) AS agent,r.created_at,r.completed_at,
 CASE WHEN r.status='running' THEN CASE
 WHEN EXISTS(SELECT 1 FROM run_permissions a WHERE a.run_id=r.id AND a.status='pending') THEN 'waiting_permission'
 WHEN EXISTS(SELECT 1 FROM run_interactions a WHERE a.run_id=r.id AND a.status='pending' AND a.kind='plan') THEN 'waiting_plan'
 WHEN EXISTS(SELECT 1 FROM run_interactions a WHERE a.run_id=r.id AND a.status='pending') THEN 'waiting_question'
 ELSE 'running' END ELSE r.status END AS state,
 COALESCE((SELECT e.created_at FROM run_events e WHERE e.run_id=r.id ORDER BY e.created_at DESC LIMIT 1),r.created_at) AS last_activity,
 (SELECT count(*) FROM run_inputs i WHERE i.conversation_id=c.id AND i.kind='next' AND i.status IN ('queued','held')) AS queued,
 (SELECT count(*) FROM deliverables d WHERE d.workspace_id=$1 AND d.conversation_id=c.id AND d.run_id=r.id) AS deliverables,
 (SELECT count(*) FROM command_tasks t WHERE t.workspace_id=$1 AND t.conversation_id=c.id AND t.status IN ('starting','running','stopping')) AS background_commands,
 (SELECT count(*) FROM command_tasks t WHERE t.workspace_id=$1 AND t.conversation_id=c.id AND t.status='unknown') AS unknown_commands
 FROM latest r JOIN conversations c ON c.id=r.conversation_id JOIN projects p ON p.id=c.project_id
 ), selected AS (
 SELECT id AS run_id,conversation_id,title,project,agent,state,created_at AS started_at,completed_at,last_activity AS last_activity_at,queued,deliverables,background_commands,unknown_commands
 FROM facts WHERE (created_at,id)<($2,$3) AND ($4='all' OR $4='active' AND (state IN ('running','cancelling','waiting_permission','waiting_plan','waiting_question') OR background_commands>0) OR $4='attention' AND (state IN ('waiting_permission','waiting_plan','waiting_question','failed') OR unknown_commands>0) OR $4='finished' AND state IN ('completed','failed','cancelled') AND background_commands=0 AND unknown_commands=0)
 ORDER BY created_at DESC,id DESC LIMIT $5
 ) SELECT COALESCE((SELECT jsonb_agg(selected ORDER BY started_at DESC,run_id DESC) FROM selected),'[]'::jsonb),
 (SELECT count(*) FROM facts WHERE state IN ('waiting_permission','waiting_plan','waiting_question','failed') OR unknown_commands>0)`, workspace, end.Created, end.ID, filter, limit+1).Scan(&raw, &result.Attention)
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(raw, &result.Tasks); err != nil {
		return result, err
	}

	if len(result.Tasks) > limit {
		result.Tasks = result.Tasks[:limit]
		last := result.Tasks[limit-1]
		raw, _ := json.Marshal(taskCursor{last.StartedAt, last.RunID})
		result.Next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return result, nil
}

type RunUsage struct {
	RunID                  uuid.UUID  `json:"run_id"`
	Status                 string     `json:"status"`
	StartedAt              time.Time  `json:"started_at"`
	CompletedAt            *time.Time `json:"completed_at"`
	ObservedAt             time.Time  `json:"observed_at"`
	ElapsedSeconds         *int64     `json:"elapsed_seconds"`
	Requests               int        `json:"requests"`
	InFlight               int        `json:"in_flight"`
	ActualInput            int64      `json:"actual_input_tokens"`
	ActualInputRequests    int        `json:"actual_input_requests"`
	ActualOutput           int64      `json:"actual_output_tokens"`
	ActualOutputRequests   int        `json:"actual_output_requests"`
	EstimatedInput         int64      `json:"estimated_input_tokens"`
	EstimatedInputRequests int        `json:"estimated_input_requests"`
	MissingOutputRequests  int        `json:"missing_output_requests"`
}

func (s *Service) RunUsage(ctx context.Context, workspace, conversation, run uuid.UUID) (RunUsage, error) {
	result := RunUsage{RunID: run, ObservedAt: time.Now().UTC()}
	if err := s.db.QueryRow(ctx, `SELECT r.status,r.created_at,r.completed_at FROM runs r JOIN conversations c ON c.id=r.conversation_id WHERE c.workspace_id=$1 AND c.id=$2 AND r.id=$3`, workspace, conversation, run).Scan(&result.Status, &result.StartedAt, &result.CompletedAt); err != nil {
		return result, err
	}
	end := result.ObservedAt
	if result.CompletedAt != nil {
		end = *result.CompletedAt
	}
	if result.CompletedAt != nil || result.Status == "running" || result.Status == "cancelling" {
		elapsed := max(0, int64(end.Sub(result.StartedAt).Seconds()))
		result.ElapsedSeconds = &elapsed
	}
	rows, err := s.db.Query(ctx, `SELECT status,estimated_input_tokens,usage FROM model_request_usage WHERE run_id=$1 ORDER BY request_index`, run)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var estimated int
		var raw []byte
		if err = rows.Scan(&status, &estimated, &raw); err != nil {
			return result, err
		}
		var usage modelruntime.NormalizedUsage
		if len(raw) > 0 {
			if err = json.Unmarshal(raw, &usage); err != nil {
				return result, err
			}
		}
		result.Requests++
		if status == "started" {
			result.InFlight++
		}
		input, known := usage.RequestInputTokens()
		if usage.Source == modelruntime.UsageActual && usage.InputReported && known {
			result.ActualInput += int64(input)
			result.ActualInputRequests++
		} else {
			if usage.Source == modelruntime.UsageEstimated && known {
				estimated = input
			}
			result.EstimatedInput += int64(max(0, estimated))
			result.EstimatedInputRequests++
		}
		if usage.Source == modelruntime.UsageActual && usage.OutputReported && usage.OutputTokens >= 0 {
			result.ActualOutput += int64(usage.OutputTokens)
			result.ActualOutputRequests++
		} else {
			result.MissingOutputRequests++
		}
	}
	return result, rows.Err()
}
func (h *Handler) CloudTasks(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	filter := r.URL.Query().Get("filter")
	if filter == "" {
		filter = "all"
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil {
			httpapi.Error(w, 400, errTaskCursor)
			return
		}
	}
	if len(r.URL.Query().Get("cursor")) > 512 {
		httpapi.Error(w, 400, errTaskCursor)
		return
	}
	result, err := h.service.CloudTasks(r.Context(), p.WorkspaceID, filter, r.URL.Query().Get("cursor"), limit)
	if errors.Is(err, errTaskCursor) {
		httpapi.Error(w, 400, err)
		return
	}
	if err != nil {
		httpapi.Error(w, 500, errors.New("task list unavailable"))
		return
	}
	httpapi.JSON(w, 200, result)
}
func (h *Handler) RunUsage(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	conversation, ok := pathID(w, r)
	if !ok {
		return
	}
	run, err := uuid.Parse(chi.URLParam(r, "runID"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid run ID"))
		return
	}
	result, err := h.service.RunUsage(r.Context(), p.WorkspaceID, conversation, run)
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, 404, errors.New("run not found"))
		return
	}
	if err != nil {
		httpapi.Error(w, 500, errors.New("usage unavailable"))
		return
	}
	httpapi.JSON(w, 200, result)
}
