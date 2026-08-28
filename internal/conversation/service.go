package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/biubiuqiu/lester-agent/internal/model"
	"github.com/biubiuqiu/lester-agent/internal/sandbox"
	"github.com/biubiuqiu/lester-agent/packages/prompts"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Conversation struct {
	ID                uuid.UUID `json:"id"`
	WorkspaceID       uuid.UUID `json:"workspace_id"`
	CreatedBy         uuid.UUID `json:"created_by"`
	AgentSlug         string    `json:"agent_slug"`
	ModelDeploymentID uuid.UUID `json:"model_deployment_id"`
	Title             string    `json:"title"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
type Message struct {
	ID             uuid.UUID      `json:"id"`
	ConversationID uuid.UUID      `json:"conversation_id"`
	Role           string         `json:"role"`
	Content        string         `json:"content"`
	Metadata       map[string]any `json:"metadata"`
	CreatedAt      time.Time      `json:"created_at"`
}
type RunEvent struct {
	ID             int64          `json:"id"`
	RunID          uuid.UUID      `json:"run_id"`
	ConversationID uuid.UUID      `json:"conversation_id"`
	Type           string         `json:"type"`
	Payload        map[string]any `json:"payload"`
	CreatedAt      time.Time      `json:"created_at"`
}
type Service struct {
	db        *pgxpool.Pool
	redis     *redis.Client
	models    *model.Store
	sandboxes *sandbox.Client
}

func New(db *pgxpool.Pool, redisClient *redis.Client, models *model.Store, sandboxes *sandbox.Client) *Service {
	return &Service{db: db, redis: redisClient, models: models, sandboxes: sandboxes}
}

func (s *Service) Create(ctx context.Context, workspaceID, userID uuid.UUID, agent, title string, modelID uuid.UUID) (Conversation, error) {
	if agent == "" {
		agent = "lester"
	}
	if title == "" {
		title = "新对话"
	}
	var c Conversation
	err := s.db.QueryRow(ctx, `INSERT INTO conversations(workspace_id,created_by,agent_slug,title,model_deployment_id) VALUES($1,$2,$3,$4,COALESCE(NULLIF($5,'00000000-0000-0000-0000-000000000000'::uuid),(SELECT id FROM model_deployments WHERE workspace_id=$1 AND is_default LIMIT 1))) RETURNING id,workspace_id,created_by,agent_slug,COALESCE(model_deployment_id,'00000000-0000-0000-0000-000000000000'),title,created_at,updated_at`, workspaceID, userID, agent, title, modelID).Scan(&c.ID, &c.WorkspaceID, &c.CreatedBy, &c.AgentSlug, &c.ModelDeploymentID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}
func (s *Service) List(ctx context.Context, workspaceID uuid.UUID) ([]Conversation, error) {
	rows, err := s.db.Query(ctx, `SELECT id,workspace_id,created_by,agent_slug,COALESCE(model_deployment_id,'00000000-0000-0000-0000-000000000000'),title,created_at,updated_at FROM conversations WHERE workspace_id=$1 ORDER BY updated_at DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Conversation{}
	for rows.Next() {
		var c Conversation
		if err = rows.Scan(&c.ID, &c.WorkspaceID, &c.CreatedBy, &c.AgentSlug, &c.ModelDeploymentID, &c.Title, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}
func (s *Service) Get(ctx context.Context, workspaceID, id uuid.UUID) (Conversation, []Message, error) {
	var c Conversation
	err := s.db.QueryRow(ctx, `SELECT id,workspace_id,created_by,agent_slug,COALESCE(model_deployment_id,'00000000-0000-0000-0000-000000000000'),title,created_at,updated_at FROM conversations WHERE id=$2 AND workspace_id=$1`, workspaceID, id).Scan(&c.ID, &c.WorkspaceID, &c.CreatedBy, &c.AgentSlug, &c.ModelDeploymentID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return c, nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT id,conversation_id,role,content,metadata,created_at FROM messages WHERE conversation_id=$1 ORDER BY created_at,id`, id)
	if err != nil {
		return c, nil, err
	}
	defer rows.Close()
	messages := []Message{}
	for rows.Next() {
		var m Message
		var raw []byte
		if err = rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &raw, &m.CreatedAt); err != nil {
			return c, nil, err
		}
		_ = json.Unmarshal(raw, &m.Metadata)
		messages = append(messages, m)
	}
	return c, messages, rows.Err()
}
func (s *Service) UpdateModel(ctx context.Context, workspaceID, id, modelID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `UPDATE conversations SET model_deployment_id=$3,updated_at=now() WHERE workspace_id=$1 AND id=$2 AND EXISTS(SELECT 1 FROM model_deployments WHERE id=$3 AND workspace_id=$1)`, workspaceID, id, modelID)
	if err == nil && tag.RowsAffected() == 0 {
		return errors.New("conversation or model not found")
	}
	return err
}
func (s *Service) Send(ctx context.Context, workspaceID, id uuid.UUID, content string) (uuid.UUID, error) {
	content = string([]byte(content))
	if content == "" {
		return uuid.Nil, errors.New("message is required")
	}
	var runID uuid.UUID
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO messages(conversation_id,role,content) SELECT id,'user',$3 FROM conversations WHERE workspace_id=$1 AND id=$2`, workspaceID, id, content); err != nil {
		return uuid.Nil, err
	}
	if err = tx.QueryRow(ctx, `INSERT INTO runs(conversation_id,status) VALUES($1,'running') RETURNING id`, id).Scan(&runID); err != nil {
		return uuid.Nil, err
	}
	_, _ = tx.Exec(ctx, `UPDATE conversations SET updated_at=now(),title=CASE WHEN title='新对话' THEN left($2,60) ELSE title END WHERE id=$1`, id, content)
	if err = tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	go s.execute(context.Background(), workspaceID, id, runID)
	return runID, nil
}

func (s *Service) execute(ctx context.Context, workspaceID, conversationID, runID uuid.UUID) {
	s.event(ctx, runID, conversationID, "RUN_STARTED", map[string]any{})
	conversation, messages, err := s.Get(ctx, workspaceID, conversationID)
	if err != nil {
		s.fail(ctx, runID, conversationID, err)
		return
	}
	if conversation.ModelDeploymentID == uuid.Nil {
		s.fail(ctx, runID, conversationID, errors.New("configure a default model deployment first"))
		return
	}
	client, deployment, err := s.models.Client(ctx, workspaceID, conversation.ModelDeploymentID)
	if err != nil {
		s.fail(ctx, runID, conversationID, err)
		return
	}
	computer, err := s.ensureComputer(ctx, conversationID)
	if err != nil {
		s.fail(ctx, runID, conversationID, err)
		return
	}
	system, err := prompts.Compose(conversation.AgentSlug, conversationID.String(), workspaceID.String(), deployment.Name, computer.Status)
	if err != nil {
		s.fail(ctx, runID, conversationID, err)
		return
	}
	history := make([]model.Message, 0, len(messages))
	for _, m := range messages {
		history = append(history, model.Message{Role: m.Role, Content: m.Content})
	}
	request := model.ModelRequest{Model: deployment.ModelID, System: system, Messages: history, Tools: computerTools(), MaxTokens: 4096}
	for turn := 0; turn < 12; turn++ {
		s.event(ctx, runID, conversationID, "MODEL_STARTED", map[string]any{"turn": turn + 1})
		stream, err := client.Stream(ctx, request)
		if err != nil {
			s.fail(ctx, runID, conversationID, err)
			return
		}
		text := ""
		callsByIndex := map[int]*model.ToolCall{}
		for event := range stream {
			if event.Err != nil {
				s.fail(ctx, runID, conversationID, event.Err)
				return
			}
			if event.Delta != "" {
				text += event.Delta
				s.event(ctx, runID, conversationID, "MODEL_DELTA", map[string]any{"delta": event.Delta})
			}
			if event.ToolCall != nil {
				call := callsByIndex[event.ToolCall.Index]
				if call == nil {
					call = &model.ToolCall{Index: event.ToolCall.Index}
					callsByIndex[event.ToolCall.Index] = call
				}
				if event.ToolCall.ID != "" {
					call.ID = event.ToolCall.ID
				}
				if event.ToolCall.Name != "" {
					call.Name = event.ToolCall.Name
				}
				if fragment := event.ToolCall.Arguments; len(fragment) > 0 && string(fragment) != "{}" {
					call.Arguments = append(call.Arguments, fragment...)
				}
			}
		}
		calls := make([]model.ToolCall, 0, len(callsByIndex))
		for index := 0; index < len(callsByIndex); index++ {
			if call := callsByIndex[index]; call != nil && call.Name != "" {
				if len(call.Arguments) == 0 {
					call.Arguments = json.RawMessage(`{}`)
				}
				calls = append(calls, *call)
			}
		}
		s.event(ctx, runID, conversationID, "MODEL_COMPLETED", map[string]any{})
		if len(calls) == 0 {
			if text != "" {
				_, err = s.db.Exec(ctx, `INSERT INTO messages(conversation_id,role,content) VALUES($1,'assistant',$2)`, conversationID, text)
				if err != nil {
					s.fail(ctx, runID, conversationID, err)
					return
				}
			}
			_, _ = s.db.Exec(ctx, `UPDATE runs SET status='completed',completed_at=now() WHERE id=$1`, runID)
			s.event(ctx, runID, conversationID, "RUN_COMPLETED", map[string]any{})
			return
		}
		request.Messages = append(request.Messages, model.Message{Role: "assistant", Content: text, ToolCalls: calls})
		for _, call := range calls {
			result, toolErr := s.runTool(ctx, runID, conversationID, call)
			if toolErr != nil {
				result = map[string]any{"error": toolErr.Error()}
			}
			raw, _ := json.Marshal(result)
			request.Messages = append(request.Messages, model.Message{Role: "tool", Content: string(raw), ToolCallID: call.ID})
		}
	}
	s.fail(ctx, runID, conversationID, errors.New("tool loop limit reached"))
}
func (s *Service) ensureComputer(ctx context.Context, conversationID uuid.UUID) (*sandbox.Sandbox, error) {
	var providerRef, status string
	err := s.db.QueryRow(ctx, `SELECT provider_ref,status FROM sandboxes WHERE conversation_id=$1`, conversationID).Scan(&providerRef, &status)
	if err == nil {
		if status != "running" {
			if err = s.sandboxes.Action(ctx, providerRef, "resume"); err != nil {
				return nil, err
			}
			status = "running"
			_, _ = s.db.Exec(ctx, `UPDATE sandboxes SET status='running',last_active_at=now() WHERE conversation_id=$1`, conversationID)
		}
		return &sandbox.Sandbox{ID: providerRef, ProviderRef: providerRef, Status: status}, nil
	}
	item, err := s.sandboxes.Create(ctx, conversationID.String())
	if err != nil {
		return nil, err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO sandboxes(conversation_id,provider_ref,status) VALUES($1,$2,'running') ON CONFLICT(conversation_id) DO UPDATE SET status='running',last_active_at=now()`, conversationID, conversationID.String())
	return item, err
}
func computerTools() []model.Tool {
	return []model.Tool{{Name: "computer_exec", Description: "Run a shell command inside this conversation's Computer", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"command": map[string]string{"type": "string"}}, "required": []string{"command"}}}, {Name: "computer_list_files", Description: "List files under /workspace", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]string{"type": "string"}}}}, {Name: "computer_read_file", Description: "Read a text file under /workspace", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]string{"type": "string"}}, "required": []string{"path"}}}, {Name: "computer_write_file", Description: "Write a text file under /workspace", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]string{"type": "string"}, "content": map[string]string{"type": "string"}}, "required": []string{"path", "content"}}}}
}
func (s *Service) runTool(ctx context.Context, runID, conversationID uuid.UUID, call model.ToolCall) (any, error) {
	var input map[string]any
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &input); err != nil {
			return nil, err
		}
	}
	switch call.Name {
	case "computer_exec":
		command := fmt.Sprint(input["command"])
		s.event(ctx, runID, conversationID, "COMMAND_STARTED", map[string]any{"command": command})
		result, err := s.sandboxes.Exec(ctx, conversationID.String(), sandbox.Command{Command: command})
		if err == nil {
			s.event(ctx, runID, conversationID, "COMMAND_OUTPUT", map[string]any{"stdout": result.Stdout, "stderr": result.Stderr})
			s.event(ctx, runID, conversationID, "COMMAND_COMPLETED", map[string]any{"exit_code": result.ExitCode})
		}
		return result, err
	case "computer_list_files":
		return s.sandboxes.ListFiles(ctx, conversationID.String(), fmt.Sprint(input["path"]))
	case "computer_read_file":
		data, err := s.sandboxes.ReadFile(ctx, conversationID.String(), fmt.Sprint(input["path"]))
		return map[string]string{"content": string(data)}, err
	case "computer_write_file":
		path := fmt.Sprint(input["path"])
		err := s.sandboxes.WriteFile(ctx, conversationID.String(), path, []byte(fmt.Sprint(input["content"])))
		if err == nil {
			s.event(ctx, runID, conversationID, "FILE_UPDATED", map[string]any{"path": path})
		}
		return map[string]bool{"ok": err == nil}, err
	}
	return nil, errors.New("unknown computer tool")
}
func (s *Service) event(ctx context.Context, runID, conversationID uuid.UUID, eventType string, payload map[string]any) {
	raw, _ := json.Marshal(payload)
	var event RunEvent
	event.RunID = runID
	event.ConversationID = conversationID
	event.Type = eventType
	event.Payload = payload
	if err := s.db.QueryRow(ctx, `INSERT INTO run_events(run_id,conversation_id,type,payload) VALUES($1,$2,$3,$4) RETURNING id,created_at`, runID, conversationID, eventType, raw).Scan(&event.ID, &event.CreatedAt); err == nil {
		encoded, _ := json.Marshal(event)
		_ = s.redis.Publish(ctx, "conversation:"+conversationID.String(), encoded).Err()
	}
}
func (s *Service) fail(ctx context.Context, runID, conversationID uuid.UUID, err error) {
	_, _ = s.db.Exec(ctx, `UPDATE runs SET status='failed',completed_at=now() WHERE id=$1`, runID)
	s.event(ctx, runID, conversationID, "RUN_FAILED", map[string]any{"error": err.Error()})
}

func (s *Service) SuspendIdle(ctx context.Context, idle time.Duration) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rows, err := s.db.Query(ctx, `SELECT conversation_id,provider_ref FROM sandboxes WHERE status='running' AND last_active_at<now()-$1::interval`, fmt.Sprintf("%f seconds", idle.Seconds()))
			if err != nil {
				continue
			}
			for rows.Next() {
				var conversationID uuid.UUID
				var ref string
				if rows.Scan(&conversationID, &ref) == nil && s.sandboxes.Action(ctx, ref, "suspend") == nil {
					_, _ = s.db.Exec(ctx, `UPDATE sandboxes SET status='suspended' WHERE conversation_id=$1`, conversationID)
				}
			}
			rows.Close()
		}
	}
}
