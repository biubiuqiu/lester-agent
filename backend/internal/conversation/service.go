package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
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
type Attachment struct {
	ID             uuid.UUID `json:"id"`
	ConversationID uuid.UUID `json:"conversation_id"`
	OriginalName   string    `json:"original_name"`
	StoredPath     string    `json:"stored_path"`
	ContentType    string    `json:"content_type"`
	SizeBytes      int64     `json:"size_bytes"`
	CreatedAt      time.Time `json:"created_at"`
}

type installedSkill struct {
	Slug, Name, Description string
}
type Service struct {
	db        *pgxpool.Pool
	redis     *redis.Client
	models    *model.Store
	sandboxes *sandbox.Client
	tools     *agenttool.Registry
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

func New(db *pgxpool.Pool, redisClient *redis.Client, models *model.Store, sandboxes *sandbox.Client, tools *agenttool.Registry) *Service {
	return &Service{db: db, redis: redisClient, models: models, sandboxes: sandboxes, tools: tools}
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
func (s *Service) Send(ctx context.Context, workspaceID, userID, id uuid.UUID, content string, attachmentIDs []uuid.UUID) (uuid.UUID, error) {
	content = string([]byte(content))
	attachmentIDs = uniqueUUIDs(attachmentIDs)
	if strings.TrimSpace(content) == "" && len(attachmentIDs) == 0 {
		return uuid.Nil, errors.New("message is required")
	}
	var runID uuid.UUID
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)
	attachments := []Attachment{}
	if len(attachmentIDs) > 0 {
		rows, queryErr := tx.Query(ctx, `SELECT id,conversation_id,original_name,stored_path,content_type,size_bytes,created_at FROM attachments WHERE conversation_id=$1 AND uploaded_by=$2 AND id=ANY($3) ORDER BY created_at,id`, id, userID, attachmentIDs)
		if queryErr != nil {
			return uuid.Nil, queryErr
		}
		for rows.Next() {
			var attachment Attachment
			if queryErr = rows.Scan(&attachment.ID, &attachment.ConversationID, &attachment.OriginalName, &attachment.StoredPath, &attachment.ContentType, &attachment.SizeBytes, &attachment.CreatedAt); queryErr != nil {
				rows.Close()
				return uuid.Nil, queryErr
			}
			attachments = append(attachments, attachment)
		}
		queryErr = rows.Err()
		rows.Close()
		if queryErr != nil {
			return uuid.Nil, queryErr
		}
		if len(attachments) != len(attachmentIDs) {
			return uuid.Nil, errors.New("one or more attachments were not found")
		}
	}
	if strings.TrimSpace(content) == "" {
		content = "已上传附件：" + attachmentNames(attachments)
	}
	metadata, _ := json.Marshal(map[string]any{"attachments": attachments})
	result, err := tx.Exec(ctx, `INSERT INTO messages(conversation_id,role,content,metadata) SELECT id,'user',$3,$4 FROM conversations WHERE workspace_id=$1 AND id=$2`, workspaceID, id, content, metadata)
	if err != nil {
		return uuid.Nil, err
	}
	if result.RowsAffected() == 0 {
		return uuid.Nil, errors.New("conversation not found")
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

func (s *Service) UploadAttachment(ctx context.Context, workspaceID, userID, conversationID uuid.UUID, originalName, contentType string, data []byte) (Attachment, error) {
	if len(data) > 25<<20 {
		return Attachment{}, errors.New("attachment exceeds the 25 MiB limit")
	}
	computer, err := s.ComputerForConversation(ctx, workspaceID, conversationID)
	if err != nil {
		return Attachment{}, err
	}
	id := uuid.New()
	name := sanitizeFilename(originalName)
	storedPath := path.Join(".agent/upload", id.String()+"-"+name)
	if err = s.sandboxes.WriteFile(ctx, computer.SandboxID, computer.WorkDir, storedPath, data); err != nil {
		return Attachment{}, err
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	attachment := Attachment{ID: id, ConversationID: conversationID, OriginalName: originalName, StoredPath: storedPath, ContentType: contentType, SizeBytes: int64(len(data))}
	err = s.db.QueryRow(ctx, `INSERT INTO attachments(id,conversation_id,uploaded_by,original_name,stored_path,content_type,size_bytes)
		SELECT $1,id,$3,$4,$5,$6,$7 FROM conversations WHERE id=$2 AND workspace_id=$8 RETURNING created_at`, attachment.ID, conversationID, userID, originalName, storedPath, contentType, len(data), workspaceID).Scan(&attachment.CreatedAt)
	if err != nil {
		return Attachment{}, err
	}
	return attachment, nil
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
	skills, err := s.installedSkills(ctx, workspaceID, conversationID)
	if err != nil {
		s.fail(ctx, runID, conversationID, err)
		return
	}
	promptSkills := make([]prompts.Skill, 0, len(skills))
	for _, item := range skills {
		promptSkills = append(promptSkills, prompts.Skill{Slug: item.Slug, Name: item.Name, Description: item.Description})
	}
	system, err := prompts.Compose(conversation.AgentSlug, conversationID.String(), workspaceID.String(), deployment.Name, computer.Status, promptSkills)
	if err != nil {
		s.fail(ctx, runID, conversationID, err)
		return
	}
	history := make([]model.Message, 0, len(messages))
	for _, m := range messages {
		history = append(history, model.Message{Role: m.Role, Content: messageContentForModel(m)})
	}
	request := model.ModelRequest{Model: deployment.ModelID, System: system, Messages: history, Tools: s.tools.Definitions(), MaxTokens: 4096}
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
func (s *Service) runTool(ctx context.Context, runID, conversationID uuid.UUID, computer *Computer, call model.ToolCall) (any, error) {
	_, _ = s.db.Exec(ctx, `UPDATE sandboxes SET last_active_at=now() WHERE provider_ref=$1`, computer.SandboxID)
	environment := agenttool.Environment{
		RunID: runID, ConversationID: conversationID,
		SandboxID: computer.SandboxID, WorkDir: computer.WorkDir,
		Sandboxes: s.sandboxes,
		Emit: func(eventType string, payload map[string]any) {
			s.event(ctx, runID, conversationID, eventType, payload)
		},
	}
	return s.tools.Execute(ctx, call.Name, call.Arguments, environment)
}
func (s *Service) installedSkills(ctx context.Context, workspaceID, conversationID uuid.UUID) ([]installedSkill, error) {
	rows, err := s.db.Query(ctx, `SELECT sk.slug,sk.name,sk.description FROM conversation_skills cs JOIN skills sk ON sk.id=cs.skill_id JOIN conversations c ON c.id=cs.conversation_id WHERE cs.conversation_id=$2 AND c.workspace_id=$1 ORDER BY sk.slug`, workspaceID, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []installedSkill{}
	for rows.Next() {
		var item installedSkill
		if err = rows.Scan(&item.Slug, &item.Name, &item.Description); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func messageContentForModel(message Message) string {
	attachmentsValue, ok := message.Metadata["attachments"]
	if !ok || message.Role != "user" {
		return message.Content
	}
	attachments, ok := attachmentsValue.([]any)
	if !ok || len(attachments) == 0 {
		return message.Content
	}
	var notice strings.Builder
	notice.WriteString(message.Content)
	notice.WriteString("\n\n<attachments>\nThe user attached files. Their contents are not included in the conversation context. The files are available in this conversation workspace:\n")
	for _, value := range attachments {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		fmt.Fprintf(&notice, "- %v (original: %v, content_type: %v, size_bytes: %v)\n", item["stored_path"], item["original_name"], item["content_type"], item["size_bytes"])
	}
	notice.WriteString("Use read or bash only when the task requires inspecting a file.\n</attachments>")
	return notice.String()
}

func uniqueUUIDs(values []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]struct{}{}
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		if value == uuid.Nil {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func attachmentNames(items []Attachment) string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.OriginalName)
	}
	return strings.Join(names, "、")
}

func sanitizeFilename(value string) string {
	value = path.Base(strings.ReplaceAll(value, "\\", "/"))
	var result strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._-", r) {
			result.WriteRune(r)
		} else {
			result.WriteRune('_')
		}
	}
	name := strings.Trim(result.String(), ".")
	if name == "" {
		return "attachment"
	}
	return string([]rune(name)[:min(len([]rune(name)), 120)])
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
