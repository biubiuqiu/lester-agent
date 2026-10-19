package personal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/conversation"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fakeClient struct {
	generate func(context.Context, model.ModelRequest) (*model.ModelResponse, error)
}

func (f fakeClient) Generate(c context.Context, r model.ModelRequest) (*model.ModelResponse, error) {
	return f.generate(c, r)
}
func (f fakeClient) Stream(context.Context, model.ModelRequest) (<-chan model.ModelEvent, error) {
	panic("personal assistant must not use execution streams")
}
func (f fakeClient) Capabilities(context.Context, string) (model.ModelCapabilities, error) {
	return model.ModelCapabilities{}, nil
}

type fixture struct {
	s       *Service
	p       auth.Principal
	project uuid.UUID
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	url := os.Getenv("LESTER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("LESTER_TEST_DATABASE_URL required for PostgreSQL personal assistant tests")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Exec(ctx, `BEGIN; SELECT pg_advisory_xact_lock(hashtextextended('lester:test:pgcrypto',0)); CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public; COMMIT;`); e != nil {
		t.Fatal(e)
	}
	schema := "personal_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(url)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	cfg.MaxConns = 2 // Model waits and nested task acceptance must not exhaust the pool.
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	files, e := filepath.Glob("../../migrations/*.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range files {
		raw, e := os.ReadFile(f)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(ctx, string(raw)); e != nil {
			t.Fatalf("migration %s: %v", f, e)
		}
	}
	p := auth.Principal{UserID: uuid.New(), WorkspaceID: uuid.New(), SessionHash: []byte("personal-fixture-session")}
	if _, e = db.Exec(ctx, `INSERT INTO users(id,email,display_name,password_hash) VALUES($1,'personal@example.test','Personal','unused');`, p.UserID); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `INSERT INTO workspaces(id,name) VALUES($1,'Personal workspace')`, p.WorkspaceID); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2)`, p.WorkspaceID, p.UserID); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `INSERT INTO sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 day')`, p.UserID, p.SessionHash); e != nil {
		t.Fatal(e)
	}
	f := fixture{s: &Service{DB: db}, p: p}
	if e = db.QueryRow(ctx, `SELECT id FROM projects WHERE workspace_id=$1 AND is_default`, p.WorkspaceID).Scan(&f.project); e != nil {
		t.Fatal(e)
	}
	return f
}
func (f fixture) client(t *testing.T, fn func(model.ModelRequest) modelReply) *atomic.Int32 {
	t.Helper()
	calls := &atomic.Int32{}
	f.s.Client = func(context.Context, uuid.UUID, uuid.UUID) (model.ModelClient, model.Deployment, error) {
		return fakeClient{generate: func(_ context.Context, r model.ModelRequest) (*model.ModelResponse, error) {
			calls.Add(1)
			if len(r.Tools) != 0 || r.ResponseSchema == nil || r.ResponseSchema.Name != "personal_assistant" {
				t.Error("missing tool-free schema boundary")
			}
			v := fn(r)
			raw, _ := json.Marshal(v)
			return &model.ModelResponse{Content: string(raw)}, nil
		}}, model.Deployment{ModelID: "fixture", ContextWindow: 32000}, nil
	}
	return calls
}
func request(content string) Request {
	return Request{ID: uuid.New(), ModelID: uuid.New(), Content: content}
}
func say(text string) modelReply {
	return modelReply{Reply: text, Changes: []memoryAction{}, Task: taskAction{Action: "none"}}
}

func TestPersonalMemoryProvenanceCorrectionsUndoAndIsolation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	req := request("记住我偏好极简设计")
	calls := f.client(t, func(model.ModelRequest) modelReply {
		v := say("我会记住你的设计偏好。")
		v.Changes = []memoryAction{{Operation: "remember", Scope: "personal", Kind: "preference", Topic: "设计风格", Content: "偏好极简设计", Evidence: req.Content}}
		return v
	})
	v, e := f.s.Chat(ctx, f.p, req)
	if e != nil {
		t.Fatal(e)
	}
	if len(v.Changes) != 1 || v.Status != "completed" {
		t.Fatalf("bad receipt: %+v", v)
	}
	if _, e = f.s.Chat(ctx, f.p, req); e != nil || calls.Load() != 1 {
		t.Fatal("same request replayed model", e)
	}
	req.Content = "different"
	if _, e = f.s.Chat(ctx, f.p, req); !errors.Is(e, ErrConflict) {
		t.Fatal("changed identity accepted", e)
	}
	items, e := f.s.Memories(ctx, f.p)
	if e != nil || len(items) != 1 {
		t.Fatal(e)
	}
	m := items[0]
	if m.SourceTurnID == nil || *m.SourceTurnID != v.ID || m.Evidence != "记住我偏好极简设计" {
		t.Fatal("unproven source")
	}
	stranger := f.p
	stranger.UserID = uuid.New()
	if got, e := f.s.Memories(ctx, stranger); e != nil || len(got) != 0 {
		t.Fatal("private memory leaked", e)
	}
	if _, e = f.s.Turn(ctx, stranger, v.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("private dialogue leaked", e)
	}
	m, e = f.s.EditMemory(ctx, f.p, m.ID, m.Revision, "更喜欢温暖的极简设计", false)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Undo(ctx, f.p, v.Changes[0].ID); !errors.Is(e, ErrConflict) {
		t.Fatal("stale undo replaced a correction", e)
	}
	if _, e = f.s.EditMemory(ctx, f.p, m.ID, 1, "", true); !errors.Is(e, ErrConflict) {
		t.Fatal("stale forget accepted", e)
	}
	if _, e = f.s.EditMemory(ctx, f.p, m.ID, m.Revision, "", true); e != nil {
		t.Fatal(e)
	}
	f.client(t, func(r model.ModelRequest) modelReply {
		if strings.Contains(r.System, "偏好极简设计") || strings.Contains(serialized(r.Messages), "记住我偏好") {
			t.Error("forgotten fact reentered context")
		}
		return say("有什么新目标？")
	})
	if _, e = f.s.Chat(ctx, f.p, request("你好")); e != nil {
		t.Fatal(e)
	}
	newReq := request("我偏好冷色调")
	f.client(t, func(model.ModelRequest) modelReply {
		r := say("了解。")
		r.Changes = []memoryAction{{Operation: "remember", Scope: "personal", Kind: "preference", Topic: "配色", Content: newReq.Content, Evidence: newReq.Content, Automatic: true}}
		return r
	})
	newTurn, e := f.s.Chat(ctx, f.p, newReq)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Undo(ctx, f.p, newTurn.Changes[0].ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Undo(ctx, f.p, newTurn.Changes[0].ID); e != nil {
		t.Fatal("undo is not idempotent", e)
	}
	items, e = f.s.Memories(ctx, f.p)
	if e != nil || len(items) != 0 {
		t.Fatal("undo did not remove inferred memory", e)
	}
}

func TestPersonalRejectsForeignEvidenceAndUnsupportedActions(t *testing.T) {
	for _, raw := range []string{`{"reply":"x","memory_changes":[],"task":{"action":"bash"}}`, `{"reply":"x","memory_changes":[],"task":{"action":"none"},"command":"id"}`, `{"reply":"x","memory_changes":[],"task":{"action":"none"}} {}`} {
		if _, e := decodeReply(raw); e == nil {
			t.Fatal("invalid structured output accepted")
		}
	}
	s := &Service{}
	req := request("remember my design preference")
	r := say("ok")
	r.Changes = []memoryAction{{Operation: "remember", Scope: "personal", Kind: "preference", Topic: "Design", Content: "minimal", Evidence: "made up"}}
	if e := s.validateActions(req, r, nil, nil, nil, true); e == nil {
		t.Fatal("invented source accepted")
	}
	r.Changes[0].Evidence = "design preference"
	r.Changes[0].Automatic = true
	if e := s.validateActions(req, r, nil, nil, nil, false); e == nil {
		t.Fatal("automatic memory bypassed opt out")
	}
	r.Changes = nil
	r.Task = taskAction{Action: "continue", ConversationID: uuid.NewString(), Instructions: "change", Evidence: req.Content}
	if e := s.validateActions(req, r, nil, nil, nil, true); e == nil {
		t.Fatal("unoffered conversation accepted")
	}
}

func TestPersonalConcurrentMemoryForgetDiscardsStaleModelResponse(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	entered := make(chan struct{})
	release := make(chan struct{})
	req := request("记住我喜欢安静的界面")
	f.client(t, func(model.ModelRequest) modelReply {
		close(entered)
		<-release
		r := say("了解")
		r.Changes = []memoryAction{{Operation: "remember", Scope: "personal", Kind: "preference", Topic: "界面", Content: req.Content, Evidence: req.Content, Automatic: true}}
		return r
	})
	done := make(chan error, 1)
	go func() { _, e := f.s.Chat(ctx, f.p, req); done <- e }()
	<-entered
	if _, e := f.s.Chat(ctx, f.p, request("duplicate overlap")); !errors.Is(e, ErrBusy) {
		t.Fatal("overlapping turn accepted", e)
	}
	if e := f.s.SetAutoMemory(ctx, f.p, false); e != nil {
		t.Fatal(e)
	}
	close(release)
	if e := <-done; !errors.Is(e, ErrConflict) {
		t.Fatal("stale model response survived setting change", e)
	}
	items, e := f.s.Memories(ctx, f.p)
	if e != nil || len(items) > 0 {
		t.Fatal("stale inference was persisted", e)
	}
}

func TestPersonalRevokedSessionAndInterruptedOwnerNeverReplay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	calls := f.client(t, func(model.ModelRequest) modelReply { return say("hello") })
	req := request("hello")
	if _, e := f.s.Chat(ctx, f.p, req); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.DB.Exec(ctx, `UPDATE personal_turns SET status='pending' WHERE id=$1`, req.ID); e != nil {
		t.Fatal(e)
	}
	if e := f.s.recoverTurn(ctx, f.p, req.ID); e != nil {
		t.Fatal(e)
	}
	v, e := f.s.Turn(ctx, f.p, req.ID)
	if e != nil || v.Status != "interrupted" || calls.Load() != 1 {
		t.Fatal("lost owner replayed", e)
	}
	if _, e = f.s.DB.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, f.p.UserID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.Chat(ctx, f.p, request("new message")); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("revoked actor accepted", e)
	}
}

func TestPersonalDelegationUsesStandardRealSubmissionAndNoReplay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var connection, modelID uuid.UUID
	if e := f.s.DB.QueryRow(ctx, `INSERT INTO model_connections(workspace_id,name,provider,protocol) VALUES($1,'fixture','openai-compatible','openai') RETURNING id`, f.p.WorkspaceID).Scan(&connection); e != nil {
		t.Fatal(e)
	}
	if e := f.s.DB.QueryRow(ctx, `INSERT INTO model_deployments(workspace_id,connection_id,name,model_id) VALUES($1,$2,'fixture','fixture') RETURNING id`, f.p.WorkspaceID, connection).Scan(&modelID); e != nil {
		t.Fatal(e)
	}
	service := conversation.New(f.s.DB, nil, nil, nil)
	stop, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if e := service.Shutdown(stop); e != nil {
		t.Fatal(e)
	}
	f.s.TasksService = service
	req := request("帮我制作介绍页")
	req.ModelID = modelID
	req.ProjectID = &f.project
	f.client(t, func(model.ModelRequest) modelReply {
		r := say("我会把介绍页交给任务会话处理。")
		r.Task = taskAction{Action: "create", ProjectID: f.project.String(), Title: "介绍页", Instructions: req.Content, Evidence: req.Content}
		return r
	})
	v, e := f.s.Chat(ctx, f.p, req)
	if e != nil {
		t.Fatal(e)
	}
	if v.Task == nil || v.Task.Status != "started" || v.Task.RunID == nil {
		t.Fatalf("missing delegation receipt: %+v", v.Task)
	}
	var permission string
	if e = f.s.DB.QueryRow(ctx, `SELECT permission_mode FROM runs WHERE id=$1`, v.Task.RunID).Scan(&permission); e != nil || permission != "standard" {
		t.Fatal("delegation changed permissions", e)
	}
	if _, e = f.s.Chat(ctx, f.p, req); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = f.s.DB.QueryRow(ctx, `SELECT count(*) FROM runs WHERE conversation_id=$1`, v.Task.ConversationID).Scan(&count); e != nil || count != 1 {
		t.Fatal("delegation replayed", e)
	}
	if e = f.s.reconcileDelegation(ctx, f.p, req.ID); e != nil {
		t.Fatal(e)
	}
}

func TestPersonalMemoryRetrievalAndProjectScope(t *testing.T) {
	items := []Memory{{Topic: "审美", Kind: "preference", Content: "极简"}, {Topic: "发布", Kind: "project", Content: "首页使用蓝色"}, {Topic: "账目", Kind: "work", Content: "报销统计"}, {Topic: "旧偏好", Kind: "preference", Content: "已删除", Deleted: true}}
	got := selectMemories(items, "首页怎么优化")
	if len(got) != 2 || got[0].Topic != "发布" || got[1].Topic != "审美" {
		t.Fatalf("bad multilingual retrieval: %+v", got)
	}
}

func TestPersonalProjectMemorySnapshotAndQueueKeepTaskIdentity(t *testing.T) {
	f := newFixture(t)
	ctx := conversation.WithClientTimezone(context.Background(), "Asia/Shanghai")
	var connection, modelID uuid.UUID
	if e := f.s.DB.QueryRow(ctx, `INSERT INTO model_connections(workspace_id,name,provider,protocol) VALUES($1,'fixture','openai-compatible','openai') RETURNING id`, f.p.WorkspaceID).Scan(&connection); e != nil {
		t.Fatal(e)
	}
	if e := f.s.DB.QueryRow(ctx, `INSERT INTO model_deployments(workspace_id,connection_id,name,model_id) VALUES($1,$2,'fixture','fixture') RETURNING id`, f.p.WorkspaceID, connection).Scan(&modelID); e != nil {
		t.Fatal(e)
	}
	service := conversation.New(f.s.DB, nil, nil, nil)
	f.s.TasksService = service
	task, e := service.Create(ctx, f.p.WorkspaceID, f.p.UserID, "franklin", "Existing design", modelID, f.project)
	if e != nil {
		t.Fatal(e)
	}
	var run uuid.UUID
	if e = f.s.DB.QueryRow(ctx, `INSERT INTO runs(conversation_id,status) VALUES($1,'running') RETURNING id`, task.ID).Scan(&run); e != nil {
		t.Fatal(e)
	}
	if e = f.s.SetAutoMemory(ctx, f.p, true); e != nil {
		t.Fatal(e)
	}
	other := uuid.New()
	if _, e = f.s.DB.Exec(ctx, `INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'private other project')`, other, f.p.WorkspaceID); e != nil {
		t.Fatal(e)
	}
	for _, v := range []struct {
		project uuid.UUID
		content string
	}{{f.project, "Use warm colors"}, {other, "Unrelated secret project decision"}} {
		if _, e = f.s.DB.Exec(ctx, `INSERT INTO personal_memories(workspace_id,user_id,project_id,kind,topic,content) VALUES($1,$2,$3,'project','Visual style',$4)`, f.p.WorkspaceID, f.p.UserID, v.project, v.content); e != nil {
			t.Fatal(e)
		}
	}
	req := request("继续优化设计")
	req.ProjectID = &f.project
	req.ModelID = modelID
	f.client(t, func(r model.ModelRequest) modelReply {
		if strings.Contains(r.System, "Unrelated secret") {
			t.Error("foreign project entered assistant context")
		}
		reply := say("补充要求会排队执行。")
		reply.Task = taskAction{Action: "continue", ConversationID: task.ID.String(), Instructions: "Continue the existing design", Evidence: req.Content}
		return reply
	})
	v, e := f.s.Chat(ctx, f.p, req)
	if e != nil {
		t.Fatal(e)
	}
	if v.Task == nil || v.Task.Status != "queued" || v.Task.InputID == nil {
		t.Fatalf("continuation not queued: %+v", v.Task)
	}
	var content, permission, agent, timezone string
	var acceptedModel uuid.UUID
	if e = f.s.DB.QueryRow(ctx, `SELECT i.content,i.permission_mode,i.model_deployment_id,c.agent_slug,i.metadata->>'client_timezone' FROM run_inputs i JOIN conversations c ON c.id=i.conversation_id WHERE i.id=$1`, v.Task.InputID).Scan(&content, &permission, &acceptedModel, &agent, &timezone); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(content, "Use warm colors") || strings.Contains(content, "Unrelated secret") || permission != "standard" || acceptedModel != modelID || agent != "franklin" || timezone != "Asia/Shanghai" {
		t.Fatal("continuation changed task authority or memory scope")
	}
	if _, e = f.s.DB.Exec(ctx, `INSERT INTO run_permissions(workspace_id,conversation_id,run_id,tool_call_id,tool,request_digest,summary) VALUES($1,$2,$3,'pending-command','bash',repeat('0',64),'{}')`, f.p.WorkspaceID, task.ID, run); e != nil {
		t.Fatal(e)
	}
	attention, e := f.s.Tasks(ctx, f.p)
	if e != nil || len(attention) != 1 || attention[0].Status != "awaiting_input" {
		t.Fatal("pending approval hidden as running", e)
	}
	if _, e = f.s.DB.Exec(ctx, `UPDATE run_permissions SET status='approved' WHERE run_id=$1`, run); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.DB.Exec(ctx, `INSERT INTO run_interactions(workspace_id,conversation_id,run_id,tool_call_id,kind,request,request_digest) VALUES($1,$2,$3,'pending-question','question','{}',repeat('0',64))`, f.p.WorkspaceID, task.ID, run); e != nil {
		t.Fatal(e)
	}
	attention, e = f.s.Tasks(ctx, f.p)
	if e != nil || attention[0].Status != "awaiting_input" {
		t.Fatal("pending question hidden as running", e)
	}
}

func TestPersonalSearchCanFindOlderOwnedWorkWithoutReadingFiles(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if e := f.s.SetAutoMemory(ctx, f.p, true); e != nil {
		t.Fatal(e)
	}
	id := uuid.New()
	if _, e := f.s.DB.Exec(ctx, `INSERT INTO conversations(id,workspace_id,created_by,agent_slug,title,updated_at) VALUES($1,$2,$3,'lester','Archived unique design','2020-01-01')`, id, f.p.WorkspaceID, f.p.UserID); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.DB.Exec(ctx, `INSERT INTO conversations(workspace_id,created_by,agent_slug,title) SELECT $1,$2,'lester','Recent '||i FROM generate_series(1,35)i`, f.p.WorkspaceID, f.p.UserID); e != nil {
		t.Fatal(e)
	}
	var passes int
	f.client(t, func(r model.ModelRequest) modelReply {
		passes++
		reply := say("我找到这项工作了。")
		if passes == 1 {
			if strings.Contains(r.System, id.String()) {
				t.Error("old work was not outside recent catalogue")
			}
			reply.Task = taskAction{Action: "search", Title: "Archived unique"}
		} else {
			if !strings.Contains(serialized(r.Messages), id.String()) {
				t.Error("search omitted original conversation")
			}
		}
		return reply
	})
	if _, e := f.s.Chat(ctx, f.p, request("找一下之前 Archived unique 的工作")); e != nil {
		t.Fatal(e)
	}
	if passes != 2 {
		t.Fatal("search did not remain bounded")
	}
}

func TestPersonalHistoryPagesAndRollbackPreserveTaskData(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if e := f.s.SetAutoMemory(ctx, f.p, true); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.DB.Exec(ctx, `INSERT INTO personal_turns(id,workspace_id,user_id,request_digest,content,status,model_id) SELECT gen_random_uuid(),$1,$2,'fixture','message '||i,'completed',gen_random_uuid() FROM generate_series(1,35)i`, f.p.WorkspaceID, f.p.UserID); e != nil {
		t.Fatal(e)
	}
	newest, more, e := f.s.History(ctx, f.p, 0)
	if e != nil || !more || len(newest) != 30 {
		t.Fatal("history unbounded", e)
	}
	older, more, e := f.s.History(ctx, f.p, newest[0].Seq)
	if e != nil || more || len(older) != 5 || older[4].Seq >= newest[0].Seq {
		t.Fatal("history cursor overlaps", e)
	}
	if _, e = f.s.DB.Exec(ctx, `INSERT INTO conversations(workspace_id,created_by,agent_slug,title) VALUES($1,$2,'lester','keep task')`, f.p.WorkspaceID, f.p.UserID); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("../../migrations/000031_personal_assistant.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.DB.Exec(ctx, string(raw)); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = f.s.DB.QueryRow(ctx, `SELECT count(*) FROM conversations WHERE workspace_id=$1`, f.p.WorkspaceID).Scan(&count); e != nil || count != 1 {
		t.Fatal("rollback deleted task data", e)
	}
}

func TestPersonalSessionRevokedDuringReplyAndMembershipRemoval(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req := request("记住我喜欢安静的界面")
	f.client(t, func(model.ModelRequest) modelReply {
		if _, e := f.s.DB.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, f.p.UserID); e != nil {
			t.Fatal(e)
		}
		r := say("已了解")
		r.Changes = []memoryAction{{Operation: "remember", Scope: "personal", Kind: "preference", Topic: "界面", Content: "安静", Evidence: req.Content}}
		return r
	})
	if _, e := f.s.Chat(ctx, f.p, req); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("late model reply survived session revocation", e)
	}
	if items, e := f.s.Memories(ctx, f.p); e != nil || len(items) > 0 {
		t.Fatal("revoked model reply stored memory", e)
	}
	if _, e := f.s.DB.Exec(ctx, `DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.p.WorkspaceID, f.p.UserID); e != nil {
		t.Fatal("private data cascade failed", e)
	}
	var count int
	if e := f.s.DB.QueryRow(ctx, `SELECT count(*) FROM personal_turns WHERE user_id=$1`, f.p.UserID).Scan(&count); e != nil || count != 0 {
		t.Fatal("removed member retained personal dialogue", e)
	}
}

func TestPersonalMemoryCapacityDoesNotBreakDialogueAndForgettingFreesSpace(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e := f.s.SetAutoMemory(ctx, f.p, true); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.DB.Exec(ctx, `INSERT INTO personal_memories(workspace_id,user_id,kind,topic,content) SELECT $1,$2,'preference','Topic '||i,'Preference '||i FROM generate_series(1,128)i`, f.p.WorkspaceID, f.p.UserID); e != nil {
		t.Fatal(e)
	}
	req := request("记住我喜欢柔和的动画")
	f.client(t, func(model.ModelRequest) modelReply {
		r := say("了解你的偏好。")
		r.Changes = []memoryAction{{Operation: "remember", Scope: "personal", Kind: "preference", Topic: "动画风格", Content: "柔和的动画", Evidence: req.Content}}
		return r
	})
	v, e := f.s.Chat(ctx, f.p, req)
	if e != nil || v.Status != "completed" || v.MemoryNotice == "" || len(v.Changes) != 0 {
		t.Fatal("capacity blocked dialogue or fabricated save", e)
	}
	items, e := f.s.Memories(ctx, f.p)
	if e != nil || len(items) != 128 {
		t.Fatal(e)
	}
	if _, e = f.s.EditMemory(ctx, f.p, items[0].ID, items[0].Revision, "", true); e != nil {
		t.Fatal(e)
	}
	req.ID = uuid.New()
	v, e = f.s.Chat(ctx, f.p, req)
	if e != nil || v.MemoryNotice != "" || len(v.Changes) != 1 {
		t.Fatal("forgetting did not free space", e)
	}
}
