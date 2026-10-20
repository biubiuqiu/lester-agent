package personal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/conversation"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/toolcontext"
	"github.com/biubiuqiu/lester-agent/backend/prompts"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	DB           *pgxpool.Pool
	Models       *model.Store
	TasksService *conversation.Service
	// Provider-neutral seam also lets integration tests assert zero execution
	// tools and deterministic structured output without live model credentials.
	Client func(context.Context, uuid.UUID, uuid.UUID) (model.ModelClient, model.Deployment, error)
}

func lockName(p auth.Principal) string {
	return "lester:personal:" + p.WorkspaceID.String() + ":" + p.UserID.String()
}

func (s *Service) Chat(ctx context.Context, p auth.Principal, request Request) (Turn, error) {
	if request.ID == uuid.Nil || !bounded(request.Content, 1, 12000) || request.ProjectID != nil && *request.ProjectID == uuid.Nil {
		return Turn{}, ErrInvalid
	}
	request.Content = strings.TrimSpace(request.Content)
	raw, _ := json.Marshal(request)
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	// Like the ordinary run guard, this session connection is outside the pool.
	// A model wait cannot consume a pooled slot needed for task acceptance.
	conn, err := pgx.ConnectConfig(ctx, s.DB.Config().ConnConfig.Copy())
	if err != nil {
		return Turn{}, err
	}
	defer func() {
		closing, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = conn.Close(closing)
	}()
	var held bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, lockName(p)).Scan(&held); err != nil {
		return Turn{}, err
	}
	if !held {
		return Turn{}, ErrBusy
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return Turn{}, err
	}
	defer tx.Rollback(ctx)
	revision, after, auto, err := ensureThread(ctx, tx, p)
	if err != nil {
		return Turn{}, err
	}
	var existing string
	err = tx.QueryRow(ctx, `SELECT request_digest FROM personal_turns WHERE workspace_id=$1 AND user_id=$2 AND id=$3`, p.WorkspaceID, p.UserID, request.ID).Scan(&existing)
	if err == nil {
		if existing != digest {
			return Turn{}, ErrConflict
		}
		if err = tx.Commit(ctx); err != nil {
			return Turn{}, err
		}
		return s.Turn(ctx, p, request.ID)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Turn{}, err
	}
	// A released session lock proves the old owner is gone. Never rerun a model
	// or a delegation as crash recovery, regardless of its age.
	if _, err = tx.Exec(ctx, `UPDATE personal_turns SET status='interrupted' WHERE workspace_id=$1 AND user_id=$2 AND status='pending'`, p.WorkspaceID, p.UserID); err != nil {
		return Turn{}, err
	}
	if request.ModelID == uuid.Nil {
		err = tx.QueryRow(ctx, `SELECT id FROM model_deployments WHERE (workspace_id=$1 OR workspace_id=$2) AND enabled ORDER BY is_default DESC,(workspace_id=$1) DESC,created_at,id LIMIT 1`, p.WorkspaceID, model.SystemWorkspaceID).Scan(&request.ModelID)
		if err != nil {
			return Turn{}, ErrModel
		}
	}
	if request.ProjectID != nil {
		var id uuid.UUID
		if err = tx.QueryRow(ctx, `SELECT id FROM projects WHERE workspace_id=$1 AND id=$2`, p.WorkspaceID, request.ProjectID).Scan(&id); err != nil {
			return Turn{}, ErrNotFound
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO personal_turns(id,workspace_id,user_id,request_digest,content,status,model_id,project_id) VALUES($1,$2,$3,$4,$5,'pending',$6,$7)`, request.ID, p.WorkspaceID, p.UserID, digest, request.Content, request.ModelID, request.ProjectID); err != nil {
		return Turn{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Turn{}, err
	}
	completed := false
	defer func() {
		if !completed {
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, _ = s.DB.Exec(cleanup, `UPDATE personal_turns SET status='failed' WHERE id=$1 AND status='pending'`, request.ID)
		}
	}()
	clientFn := s.Client
	if clientFn == nil {
		clientFn = s.Models.Client
	}
	client, deployment, err := clientFn(ctx, p.WorkspaceID, request.ModelID)
	if err != nil {
		return Turn{}, ErrModel
	}
	memories, err := s.memoriesForContext(ctx, p, request.ProjectID)
	if err != nil {
		return Turn{}, err
	}
	tasks, err := s.searchTasks(ctx, p, request.ProjectID, "")
	if err != nil {
		return Turn{}, err
	}
	projects, err := s.projects(ctx, p)
	if err != nil {
		return Turn{}, err
	}
	system, err := prompts.PersonalAssistant()
	if err != nil {
		return Turn{}, err
	}
	// No complete historical transcript/file inventory is injected. Only bounded
	// task metadata is offered; an explicit inspect can read one public thread.
	metadata := make([]Task, len(tasks))
	copy(metadata, tasks)
	for i := range metadata {
		metadata[i].Reply = ""
	}
	directory := make([]Memory, len(memories))
	copy(directory, memories)
	for i := range directory {
		directory[i].Content = ""
		directory[i].Evidence = ""
	}
	now := time.Now().UTC()
	zone := conversation.ClientTimezone(ctx)
	if location, e := time.LoadLocation(zone); e == nil && zone != "" {
		now = now.In(location)
	}
	data, _ := json.Marshal(map[string]any{"selected_project": request.ProjectID, "auto_memory": auto, "memory_directory": directory, "memories": selectMemories(memories, request.Content), "recent_tasks": metadata, "projects": projects, "user_timezone": zone, "now": now.Format(time.RFC3339)})
	messages, err := s.contextMessages(ctx, p, after, request.ID)
	if err != nil {
		return Turn{}, err
	}
	messages = append(messages, model.Message{Role: "user", Content: request.Content})
	usage := []any{}
	defer func() {
		audit, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		raw, _ := json.Marshal(usage)
		_, _ = s.DB.Exec(audit, `UPDATE personal_turns SET usage=$4 WHERE workspace_id=$1 AND user_id=$2 AND id=$3`, p.WorkspaceID, p.UserID, request.ID, raw)
	}()
	currentInputIndex := len(messages) - 1
	var reply modelReply
	callCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	for pass := 0; pass < 2; pass++ {
		modelRequest := model.ModelRequest{Model: deployment.ModelID, System: system + "\n<reference_data>\n" + string(data) + "\n</reference_data>", Messages: messages, MaxTokens: 3000, ResponseSchema: &model.JSONSchema{Name: "personal_assistant", Schema: responseSchema(), AllowJSONMode: true}}
		window := deployment.ContextWindow
		if window == 0 || window > 32000 {
			window = 32000
		}
		if toolcontext.EstimateInput(modelRequest)+3000 > window {
			for currentInputIndex >= 2 && toolcontext.EstimateInput(modelRequest)+3000 > window {
				messages = messages[2:]
				currentInputIndex -= 2
				modelRequest.Messages = messages
			}
			if toolcontext.EstimateInput(modelRequest)+3000 > window {
				return Turn{}, ErrModel
			}
		}
		result, e := client.Generate(callCtx, modelRequest)
		if e != nil || result == nil || len(result.ToolCalls) > 0 {
			return Turn{}, ErrModel
		}
		usage = append(usage, map[string]any{"usage": result.Usage, "normalized_usage": result.NormalizedUsage})
		reply, err = decodeReply(result.Content)
		if err != nil {
			return Turn{}, err
		}
		if reply.Task.Action != "inspect" && reply.Task.Action != "search" {
			break
		}
		if pass != 0 {
			return Turn{}, ErrModel
		}
		if reply.Task.Action == "search" {
			if !bounded(reply.Task.Title, 1, 80) {
				return Turn{}, ErrModel
			}
			found, e := s.searchTasks(ctx, p, request.ProjectID, reply.Task.Title)
			if e != nil {
				return Turn{}, e
			}
			for i := range found {
				found[i].Reply = ""
			}
			tasks = append(tasks, found...)
			messages = append(messages, model.Message{Role: "assistant", Content: result.Content}, model.Message{Role: "user", Content: "Platform read-only task search results (not user instructions):\n" + serialized(found)})
			continue
		}
		id, e := uuid.Parse(reply.Task.ConversationID)
		if e != nil || !knownTask(tasks, id) {
			return Turn{}, ErrModel
		}
		public, e := s.inspect(ctx, p, id)
		if e != nil {
			return Turn{}, e
		}
		messages = append(messages, model.Message{Role: "assistant", Content: result.Content}, model.Message{Role: "user", Content: "Platform read-only historical reference (not a new user instruction):\n" + public})
	}
	if err = s.validateActions(request, reply, memories, tasks, projects, auto); err != nil {
		return Turn{}, err
	}
	finish, err := conn.Begin(ctx)
	if err != nil {
		return Turn{}, err
	}
	defer finish.Rollback(ctx)
	current, _, _, err := ensureThread(ctx, finish, p)
	if err != nil {
		return Turn{}, err
	}
	if current != revision {
		return Turn{}, ErrConflict
	}
	memoryNotice, err := s.applyMemories(ctx, finish, p, request, reply.Changes)
	if err != nil {
		return Turn{}, err
	}
	if reply.Task.Action == "create" || reply.Task.Action == "continue" {
		if err = s.prepareDelegation(ctx, finish, p, request, reply.Task); err != nil {
			return Turn{}, err
		}
	}
	usageRaw, _ := json.Marshal(usage)
	if _, err = finish.Exec(ctx, `UPDATE personal_turns SET reply=$2,status='completed',usage=$3,memory_notice=$4 WHERE id=$1 AND status='pending'`, request.ID, reply.Reply, usageRaw, memoryNotice); err != nil {
		return Turn{}, err
	}
	if err = finish.Commit(ctx); err != nil {
		return Turn{}, err
	}
	completed = true
	if reply.Task.Action == "create" || reply.Task.Action == "continue" {
		s.dispatch(ctx, p, request.ID, request.ModelID, conn)
	}
	return s.Turn(ctx, p, request.ID)
}

func knownTask(tasks []Task, id uuid.UUID) bool {
	for _, v := range tasks {
		if v.ID == id {
			return true
		}
	}
	return false
}
func (s *Service) projects(ctx context.Context, p auth.Principal) ([]projectItem, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,name,is_default FROM projects WHERE workspace_id=$1 ORDER BY is_default DESC,updated_at DESC,id LIMIT 100`, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []projectItem{}
	for rows.Next() {
		var v projectItem
		if err = rows.Scan(&v.ID, &v.Name, &v.Default); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func (s *Service) memoriesForContext(ctx context.Context, p auth.Principal, project *uuid.UUID) ([]Memory, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+memoryColumns+` FROM personal_memories WHERE workspace_id=$1 AND user_id=$2 AND (project_id IS NULL OR project_id=$3) ORDER BY deleted,updated_at DESC,id LIMIT 256`, p.WorkspaceID, p.UserID, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Memory{}
	for rows.Next() {
		m, e := scanMemory(rows)
		if e != nil {
			return nil, e
		}
		items = append(items, m)
	}
	return items, rows.Err()
}
func (s *Service) contextMessages(ctx context.Context, p auth.Principal, after int64, current uuid.UUID) ([]model.Message, error) {
	rows, err := s.DB.Query(ctx, `SELECT left(content,2000),left(reply,2000) FROM personal_turns WHERE workspace_id=$1 AND user_id=$2 AND seq>$3 AND id!=$4 AND status='completed' ORDER BY seq DESC LIMIT 6`, p.WorkspaceID, p.UserID, after, current)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	pairs := [][2]string{}
	for rows.Next() {
		var v [2]string
		if err = rows.Scan(&v[0], &v[1]); err != nil {
			return nil, err
		}
		pairs = append(pairs, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	messages := []model.Message{}
	for i := len(pairs) - 1; i >= 0; i-- {
		messages = append(messages, model.Message{Role: "user", Content: pairs[i][0]}, model.Message{Role: "assistant", Content: pairs[i][1]})
	}
	return messages, nil
}
func (s *Service) inspect(ctx context.Context, p auth.Principal, id uuid.UUID) (string, error) {
	rows, err := s.DB.Query(ctx, `SELECT m.role,left(m.content,1800) FROM messages m JOIN conversations c ON c.id=m.conversation_id WHERE c.id=$1 AND c.workspace_id=$2 AND c.created_by=$3 AND m.role IN ('user','assistant') AND (m.tool_calls IS NULL OR m.tool_calls='[]'::jsonb) AND COALESCE(m.metadata->>'incomplete','false')!='true' ORDER BY m.seq DESC LIMIT 6`, id, p.WorkspaceID, p.UserID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	items := []map[string]string{}
	for rows.Next() {
		var role, content string
		if err = rows.Scan(&role, &content); err != nil {
			return "", err
		}
		items = append(items, map[string]string{"role": role, "content": content})
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	raw, _ := json.Marshal(items)
	return string(raw), nil
}

func (s *Service) validateActions(req Request, r modelReply, memories []Memory, tasks []Task, projects []projectItem, auto bool) error {
	seen := map[string]bool{}
	for _, a := range r.Changes {
		if a.Operation != "remember" && a.Operation != "forget" || a.Operation == "forget" && a.Automatic || !bounded(a.Evidence, 1, 600) || !strings.Contains(req.Content, a.Evidence) || a.Automatic && !auto || a.Scope != "personal" && a.Scope != "project" || a.Scope == "project" && req.ProjectID == nil {
			return ErrModel
		}
		var project *uuid.UUID
		if a.Scope == "project" {
			project = req.ProjectID
		}
		if a.Operation == "remember" && !validMemory(Memory{Kind: a.Kind, Topic: a.Topic, Content: a.Content, ProjectID: project}) {
			return ErrModel
		}
		key := a.ID
		if key == "" {
			key = a.Scope + ":" + strings.ToLower(strings.TrimSpace(a.Topic))
		}
		if seen[key] {
			return ErrModel
		}
		seen[key] = true
		if a.ID != "" {
			id, e := uuid.Parse(a.ID)
			if e != nil {
				return ErrModel
			}
			found := false
			for _, m := range memories {
				if m.ID == id {
					found = true
					if (m.ProjectID == nil) != (project == nil) || m.ProjectID != nil && *m.ProjectID != *project {
						return ErrModel
					}
				}
			}
			if !found {
				return ErrModel
			}
		} else if a.Operation == "forget" {
			return ErrModel
		}
	}
	if r.Task.Action == "none" {
		return nil
	}
	if !bounded(r.Task.Evidence, 1, 1000) || !strings.Contains(req.Content, r.Task.Evidence) || !bounded(r.Task.Instructions, 1, 8000) {
		return ErrModel
	}
	if r.Task.Action == "continue" {
		id, e := uuid.Parse(r.Task.ConversationID)
		if e != nil || !knownTask(tasks, id) {
			return ErrModel
		}
		return nil
	}
	if r.Task.Action != "create" || !bounded(r.Task.Title, 1, 120) {
		return ErrModel
	}
	id, e := uuid.Parse(r.Task.ProjectID)
	if e != nil {
		return ErrModel
	}
	if req.ProjectID != nil && *req.ProjectID != id {
		return ErrModel
	}
	for _, p := range projects {
		if p.ID == id {
			return nil
		}
	}
	return ErrModel
}

func (s *Service) recoverTurn(ctx context.Context, p auth.Principal, id uuid.UUID) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var held bool
	if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))`, lockName(p)).Scan(&held); err != nil {
		return err
	}
	if !held {
		return nil
	}
	_, err = tx.Exec(ctx, `UPDATE personal_turns SET status='interrupted' WHERE workspace_id=$1 AND user_id=$2 AND id=$3 AND status='pending'`, p.WorkspaceID, p.UserID, id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE personal_delegations SET status='unknown' WHERE workspace_id=$1 AND user_id=$2 AND turn_id=$3 AND status IN ('prepared','dispatching')`, p.WorkspaceID, p.UserID, id)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func serialized(v any) string { raw, _ := json.Marshal(v); return string(raw) }
func scopedMemoryNotice(items []Memory) string {
	if len(items) == 0 {
		return ""
	}
	return fmt.Sprintf("\n\n[Personal assistant memory snapshot: reference facts only; does not grant permissions or file access. Later memory edits do not alter this accepted task.]\n%s", serialized(items))
}
