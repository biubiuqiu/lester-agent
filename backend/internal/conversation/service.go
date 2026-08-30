package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/biubiuqiu/lester-agent/backend/prompts"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	locks     sync.Map
}

type Computer struct {
	SandboxID string
	WorkDir   string
	Status    string
}

type ComputerState struct {
	ConversationID uuid.UUID  `json:"conversation_id"`
	UserID         uuid.UUID  `json:"user_id"`
	Provider       string     `json:"provider,omitempty"`
	ProviderRef    string     `json:"provider_ref,omitempty"`
	Status         string     `json:"status"`
	LastError      string     `json:"last_error,omitempty"`
	LastCheckedAt  *time.Time `json:"last_checked_at,omitempty"`
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
	computer, err := s.ensureComputer(ctx, conversation)
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
	request := model.ModelRequest{Model: deployment.ModelID, System: system, Messages: history, Tools: agentTools(), MaxTokens: 4096}
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
			s.event(ctx, runID, conversationID, "TOOL_STARTED", map[string]any{"tool": call.Name, "arguments": string(call.Arguments)})
			result, toolErr := s.runTool(ctx, runID, conversationID, computer, call)
			if toolErr != nil {
				s.event(ctx, runID, conversationID, "TOOL_FAILED", map[string]any{"tool": call.Name, "error": toolErr.Error()})
				result = map[string]any{"error": toolErr.Error()}
			} else {
				s.event(ctx, runID, conversationID, "TOOL_COMPLETED", map[string]any{"tool": call.Name})
			}
			raw, _ := json.Marshal(result)
			request.Messages = append(request.Messages, model.Message{Role: "tool", Content: string(raw), ToolCallID: call.ID})
		}
	}
	s.fail(ctx, runID, conversationID, errors.New("tool loop limit reached"))
}
func conversationWorkDir(conversationID uuid.UUID) string {
	return "/workspace/conversations/" + conversationID.String()
}

func (s *Service) userLock(userID uuid.UUID) *sync.Mutex {
	lock, _ := s.locks.LoadOrStore(userID, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func (s *Service) ensureComputer(ctx context.Context, conversation Conversation) (*Computer, error) {
	lock := s.userLock(conversation.CreatedBy)
	lock.Lock()
	defer lock.Unlock()

	providerID := conversation.CreatedBy.String()
	var status string
	err := s.db.QueryRow(ctx, `SELECT provider_ref,status FROM sandboxes WHERE workspace_id=$1 AND user_id=$2`, conversation.WorkspaceID, conversation.CreatedBy).Scan(&providerID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.db.QueryRow(ctx, `INSERT INTO sandboxes(workspace_id,user_id,provider_ref,status) VALUES($1,$2,$3,'creating') ON CONFLICT(user_id) DO UPDATE SET last_active_at=now() RETURNING provider_ref,status`, conversation.WorkspaceID, conversation.CreatedBy, providerID).Scan(&providerID, &status)
	}
	if err != nil {
		return nil, fmt.Errorf("load user computer: %w", err)
	}

	actual, err := s.sandboxes.Inspect(ctx, providerID)
	if err != nil {
		s.recordComputerError(ctx, conversation.CreatedBy, err)
		return nil, fmt.Errorf("inspect user computer: %w", err)
	}
	switch actual.Status {
	case "missing":
		actual, err = s.sandboxes.Create(ctx, providerID)
	case "running":
		// Already ready.
	case "unhealthy":
		if destroyErr := s.sandboxes.Action(ctx, providerID, "destroy"); destroyErr != nil {
			err = destroyErr
			break
		}
		actual, err = s.sandboxes.Create(ctx, providerID)
	default:
		err = s.sandboxes.Action(ctx, providerID, "resume")
		if err == nil {
			actual, err = s.sandboxes.Inspect(ctx, providerID)
		}
	}
	if err != nil {
		s.recordComputerError(ctx, conversation.CreatedBy, err)
		return nil, fmt.Errorf("recover user computer: %w", err)
	}
	if actual.Status != "running" {
		err = fmt.Errorf("user computer is %s after recovery", actual.Status)
		s.recordComputerError(ctx, conversation.CreatedBy, err)
		return nil, err
	}
	workDir := conversationWorkDir(conversation.ID)
	if _, err = s.sandboxes.Exec(ctx, providerID, sandbox.Command{Command: "true", WorkDir: workDir}); err != nil {
		s.recordComputerError(ctx, conversation.CreatedBy, err)
		return nil, fmt.Errorf("prepare conversation directory: %w", err)
	}
	_, _ = s.db.Exec(ctx, `UPDATE sandboxes SET status='running',last_error=NULL,last_checked_at=now(),last_active_at=now() WHERE workspace_id=$1 AND user_id=$2`, conversation.WorkspaceID, conversation.CreatedBy)
	return &Computer{SandboxID: providerID, WorkDir: workDir, Status: "running"}, nil
}

func (s *Service) recordComputerError(ctx context.Context, userID uuid.UUID, err error) {
	_, _ = s.db.Exec(ctx, `UPDATE sandboxes SET status='error',last_error=$2,last_checked_at=now() WHERE user_id=$1`, userID, err.Error())
}

func (s *Service) ComputerForConversation(ctx context.Context, workspaceID, conversationID uuid.UUID) (*Computer, error) {
	conversation, _, err := s.Get(ctx, workspaceID, conversationID)
	if err != nil {
		return nil, err
	}
	return s.ensureComputer(ctx, conversation)
}

func (s *Service) ComputerStatus(ctx context.Context, workspaceID, conversationID uuid.UUID) (ComputerState, error) {
	conversation, _, err := s.Get(ctx, workspaceID, conversationID)
	if err != nil {
		return ComputerState{}, err
	}
	state := ComputerState{ConversationID: conversationID, UserID: conversation.CreatedBy, Status: "not_created"}
	var checkedAt *time.Time
	err = s.db.QueryRow(ctx, `SELECT provider,provider_ref,status,COALESCE(last_error,''),last_checked_at FROM sandboxes WHERE workspace_id=$1 AND user_id=$2`, workspaceID, conversation.CreatedBy).Scan(&state.Provider, &state.ProviderRef, &state.Status, &state.LastError, &checkedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return ComputerState{}, err
	}
	actual, inspectErr := s.sandboxes.Inspect(ctx, state.ProviderRef)
	if inspectErr != nil {
		s.recordComputerError(ctx, conversation.CreatedBy, inspectErr)
		state.Status = "error"
		state.LastError = inspectErr.Error()
		now := time.Now()
		state.LastCheckedAt = &now
		return state, nil
	}
	state.Status = actual.Status
	state.LastError = ""
	_, _ = s.db.Exec(ctx, `UPDATE sandboxes SET status=$2,last_error=NULL,last_checked_at=now() WHERE user_id=$1`, conversation.CreatedBy, state.Status)
	now := time.Now()
	state.LastCheckedAt = &now
	return state, nil
}
func agentTools() []model.Tool {
	pathProperty := map[string]any{"type": "string", "description": "Path relative to the current conversation directory. Do not prefix it with /workspace."}
	return []model.Tool{
		{Name: "bash", Description: "Run a Bash command in the current conversation directory. Use it for listing, searching, tests, builds, and long-running processes.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"command":           map[string]any{"type": "string", "description": "The Bash command to execute."},
			"description":       map[string]any{"type": "string", "description": "A short description of what the command does."},
			"timeout":           map[string]any{"type": "integer", "minimum": 1000, "maximum": 600000, "description": "Foreground timeout in milliseconds. Defaults to 120000. For background tasks, only applied when explicitly provided."},
			"run_in_background": map[string]any{"type": "boolean", "description": "Run asynchronously and return a task ID plus a log file path."},
		}, "required": []string{"command"}, "additionalProperties": false}},
		{Name: "read", Description: "Read a text file from the current conversation directory, optionally by line range.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"file_path": pathProperty,
			"offset":    map[string]any{"type": "integer", "minimum": 1, "description": "One-based first line to return. Defaults to 1."},
			"limit":     map[string]any{"type": "integer", "minimum": 1, "maximum": 2000, "description": "Maximum number of lines to return. Defaults to 2000."},
		}, "required": []string{"file_path"}, "additionalProperties": false}},
		{Name: "write", Description: "Create or completely overwrite a text file in the current conversation directory.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"file_path": pathProperty, "content": map[string]any{"type": "string", "description": "The complete file content."}}, "required": []string{"file_path", "content"}, "additionalProperties": false}},
		{Name: "edit", Description: "Edit a text file by exact string replacement. This is not a regular expression or AST edit.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"file_path":   pathProperty,
			"old_string":  map[string]any{"type": "string", "description": "The exact text to replace. Include enough context to make a single match."},
			"new_string":  map[string]any{"type": "string", "description": "The replacement text. It may be empty."},
			"replace_all": map[string]any{"type": "boolean", "description": "Replace every exact match. Defaults to false; otherwise multiple matches are rejected as ambiguous."},
		}, "required": []string{"file_path", "old_string", "new_string"}, "additionalProperties": false}},
	}
}
func (s *Service) runTool(ctx context.Context, runID, conversationID uuid.UUID, computer *Computer, call model.ToolCall) (any, error) {
	_, _ = s.db.Exec(ctx, `UPDATE sandboxes SET last_active_at=now() WHERE provider_ref=$1`, computer.SandboxID)
	var input map[string]any
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &input); err != nil {
			return nil, err
		}
	}
	switch call.Name {
	case "bash", "computer_exec":
		command, err := requiredToolStringArgument(input, "command")
		if err != nil {
			return nil, err
		}
		background, err := toolBoolArgument(input, "run_in_background")
		if err != nil {
			return nil, err
		}
		timeoutMS, hasTimeout, err := toolIntArgument(input, "timeout")
		if err != nil {
			return nil, err
		}
		if hasTimeout && (timeoutMS < 1000 || timeoutMS > 600000) {
			return nil, errors.New("tool argument \"timeout\" must be between 1000 and 600000 milliseconds")
		}
		s.event(ctx, runID, conversationID, "COMMAND_STARTED", map[string]any{"command": command})
		if background {
			return s.startBackgroundCommand(ctx, runID, conversationID, computer, command, timeoutMS, hasTimeout)
		}
		if !hasTimeout {
			timeoutMS = 120000
		}
		result, err := s.sandboxes.Exec(ctx, computer.SandboxID, sandbox.Command{Command: command, WorkDir: computer.WorkDir, TimeoutSeconds: millisecondsToSeconds(timeoutMS)})
		if err == nil {
			s.event(ctx, runID, conversationID, "COMMAND_OUTPUT", map[string]any{"stdout": result.Stdout, "stderr": result.Stderr})
			s.event(ctx, runID, conversationID, "COMMAND_COMPLETED", map[string]any{"exit_code": result.ExitCode})
		}
		return result, err
	case "computer_list_files":
		filePath, err := toolStringArgument(input, "path", false)
		if err != nil {
			return nil, err
		}
		return s.sandboxes.ListFiles(ctx, computer.SandboxID, computer.WorkDir, filePath)
	case "read", "computer_read_file":
		filePath, err := toolFilePathArgument(input)
		if err != nil {
			return nil, err
		}
		data, err := s.sandboxes.ReadFile(ctx, computer.SandboxID, computer.WorkDir, filePath)
		if err != nil {
			return nil, err
		}
		offset, hasOffset, err := toolIntArgument(input, "offset")
		if err != nil {
			return nil, err
		}
		if !hasOffset {
			offset = 1
		}
		limit, hasLimit, err := toolIntArgument(input, "limit")
		if err != nil {
			return nil, err
		}
		if !hasLimit {
			limit = 2000
		}
		if offset < 1 || limit < 1 || limit > 2000 {
			return nil, errors.New("read offset must be at least 1 and limit must be between 1 and 2000")
		}
		content, totalLines, returnedLines := sliceFileLines(string(data), offset, limit)
		return map[string]any{"content": content, "start_line": offset, "line_count": returnedLines, "total_lines": totalLines}, nil
	case "write", "computer_write_file":
		filePath, err := toolFilePathArgument(input)
		if err != nil {
			return nil, err
		}
		content, err := toolStringArgument(input, "content", true)
		if err != nil {
			return nil, err
		}
		err = s.sandboxes.WriteFile(ctx, computer.SandboxID, computer.WorkDir, filePath, []byte(content))
		if err == nil {
			s.event(ctx, runID, conversationID, "FILE_UPDATED", map[string]any{"path": filePath, "operation": "write"})
		}
		return map[string]bool{"ok": err == nil}, err
	case "edit":
		filePath, err := toolFilePathArgument(input)
		if err != nil {
			return nil, err
		}
		oldString, err := requiredToolStringArgument(input, "old_string")
		if err != nil {
			return nil, err
		}
		newString, err := toolStringArgument(input, "new_string", true)
		if err != nil {
			return nil, err
		}
		replaceAll, err := toolBoolArgument(input, "replace_all")
		if err != nil {
			return nil, err
		}
		data, err := s.sandboxes.ReadFile(ctx, computer.SandboxID, computer.WorkDir, filePath)
		if err != nil {
			return nil, err
		}
		updated, replacements, err := replaceExactString(string(data), oldString, newString, replaceAll)
		if err != nil {
			return nil, err
		}
		if err = s.sandboxes.WriteFile(ctx, computer.SandboxID, computer.WorkDir, filePath, []byte(updated)); err != nil {
			return nil, err
		}
		s.event(ctx, runID, conversationID, "FILE_UPDATED", map[string]any{"path": filePath, "operation": "edit", "replacements": replacements})
		return map[string]any{"ok": true, "replacements": replacements}, nil
	}
	return nil, errors.New("unknown tool")
}

func (s *Service) startBackgroundCommand(ctx context.Context, runID, conversationID uuid.UUID, computer *Computer, command string, timeoutMS int, hasTimeout bool) (any, error) {
	taskID := uuid.NewString()
	logPath := ".lester/tasks/" + taskID + ".log"
	inner := command + "\nexit_code=$?\nprintf '\\n[Lester background task exited with code %s]\\n' \"$exit_code\"\nexit \"$exit_code\""
	runner := "sh -lc " + shellQuoteArgument(inner)
	if hasTimeout {
		runner = fmt.Sprintf("timeout %ds %s", millisecondsToSeconds(timeoutMS), runner)
	}
	launch := "mkdir -p .lester/tasks; nohup " + runner + " > " + shellQuoteArgument(logPath) + " 2>&1 < /dev/null & echo $!"
	result, err := s.sandboxes.Exec(ctx, computer.SandboxID, sandbox.Command{Command: launch, WorkDir: computer.WorkDir, TimeoutSeconds: 30})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("start background command: %s", strings.TrimSpace(result.Stderr))
	}
	payload := map[string]any{"task_id": taskID, "pid": strings.TrimSpace(result.Stdout), "log_path": logPath, "status": "running"}
	s.event(ctx, runID, conversationID, "BACKGROUND_STARTED", payload)
	return payload, nil
}

func toolStringArgument(input map[string]any, name string, required bool) (string, error) {
	value, exists := input[name]
	if !exists || value == nil {
		if required {
			return "", fmt.Errorf("missing required tool argument %q", name)
		}
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("tool argument %q must be a string", name)
	}
	return text, nil
}

func requiredToolStringArgument(input map[string]any, name string) (string, error) {
	text, err := toolStringArgument(input, name, true)
	if err == nil && strings.TrimSpace(text) == "" {
		err = fmt.Errorf("tool argument %q cannot be empty", name)
	}
	return text, err
}

func toolFilePathArgument(input map[string]any) (string, error) {
	if _, exists := input["file_path"]; exists {
		return requiredToolStringArgument(input, "file_path")
	}
	return requiredToolStringArgument(input, "path")
}

func toolBoolArgument(input map[string]any, name string) (bool, error) {
	value, exists := input[name]
	if !exists || value == nil {
		return false, nil
	}
	result, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("tool argument %q must be a boolean", name)
	}
	return result, nil
}

func toolIntArgument(input map[string]any, name string) (int, bool, error) {
	value, exists := input[name]
	if !exists || value == nil {
		return 0, false, nil
	}
	number, ok := value.(float64)
	if !ok || number != float64(int(number)) {
		return 0, true, fmt.Errorf("tool argument %q must be an integer", name)
	}
	return int(number), true, nil
}

func millisecondsToSeconds(milliseconds int) int {
	return (milliseconds + 999) / 1000
}

func sliceFileLines(content string, offset, limit int) (string, int, int) {
	lines := strings.Split(content, "\n")
	if content == "" {
		lines = nil
	}
	total := len(lines)
	start := min(offset-1, total)
	end := min(start+limit, total)
	return strings.Join(lines[start:end], "\n"), total, end - start
}

func replaceExactString(content, oldString, newString string, replaceAll bool) (string, int, error) {
	if oldString == "" {
		return "", 0, errors.New("tool argument \"old_string\" cannot be empty")
	}
	matches := strings.Count(content, oldString)
	if matches == 0 {
		return "", 0, errors.New("old_string was not found in the file")
	}
	if matches > 1 && !replaceAll {
		return "", 0, fmt.Errorf("old_string matched %d locations; include more context or set replace_all", matches)
	}
	if replaceAll {
		return strings.ReplaceAll(content, oldString, newString), matches, nil
	}
	return strings.Replace(content, oldString, newString, 1), 1, nil
}

func shellQuoteArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
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
			rows, err := s.db.Query(ctx, `SELECT id,provider_ref FROM sandboxes WHERE status='running' AND last_active_at<now()-$1::interval`, fmt.Sprintf("%f seconds", idle.Seconds()))
			if err != nil {
				continue
			}
			for rows.Next() {
				var sandboxID uuid.UUID
				var ref string
				if rows.Scan(&sandboxID, &ref) == nil && s.sandboxes.Action(ctx, ref, "suspend") == nil {
					_, _ = s.db.Exec(ctx, `UPDATE sandboxes SET status='suspended',last_checked_at=now() WHERE id=$1`, sandboxID)
				}
			}
			rows.Close()
		}
	}
}

func (s *Service) MonitorSandboxes(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rows, err := s.db.Query(ctx, `SELECT id,provider_ref FROM sandboxes WHERE status<>'not_created'`)
			if err != nil {
				continue
			}
			for rows.Next() {
				var id uuid.UUID
				var ref string
				if rows.Scan(&id, &ref) != nil {
					continue
				}
				checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				actual, inspectErr := s.sandboxes.Inspect(checkCtx, ref)
				cancel()
				if inspectErr != nil {
					_, _ = s.db.Exec(ctx, `UPDATE sandboxes SET status='error',last_error=$2,last_checked_at=now() WHERE id=$1`, id, inspectErr.Error())
					continue
				}
				_, _ = s.db.Exec(ctx, `UPDATE sandboxes SET status=$2,last_error=NULL,last_checked_at=now() WHERE id=$1`, id, actual.Status)
			}
			rows.Close()
		}
	}
}
