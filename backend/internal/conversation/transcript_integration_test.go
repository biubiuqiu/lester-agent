package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/biubiuqiu/lester-agent/backend/internal/toolcontext"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type transcriptFixture struct {
	service                             *Service
	workspaceID, userID, conversationID uuid.UUID
}

func newTranscriptFixture(t *testing.T, legacy bool) transcriptFixture {
	t.Helper()
	url := os.Getenv("LESTER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set LESTER_TEST_DATABASE_URL to run PostgreSQL transcript integration tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	// All mutable test data is confined to a generated schema in a test database.
	schema := "transcript_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	for _, file := range []string{"000001_phase_0_4.up.sql", "000002_user_sandboxes.up.sql", "000003_skills_attachments.up.sql"} {
		applyTestMigration(t, db, file)
	}
	f := transcriptFixture{service: New(db, nil, nil, nil, agenttool.NewDefaultRegistry(db)), workspaceID: uuid.New(), userID: uuid.New(), conversationID: uuid.New()}
	for _, stmt := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users(id,email,display_name,password_hash) VALUES($1,'test@example.invalid','test','unused')`, []any{f.userID}},
		{`INSERT INTO workspaces(id,name) VALUES($1,'test')`, []any{f.workspaceID}},
		{`INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2)`, []any{f.workspaceID, f.userID}},
		{`INSERT INTO conversations(id,workspace_id,created_by,agent_slug) VALUES($1,$2,$3,'lester')`, []any{f.conversationID, f.workspaceID, f.userID}},
	} {
		if _, err = db.Exec(ctx, stmt.query, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	if legacy {
		_, err = db.Exec(ctx, `INSERT INTO messages(id,conversation_id,role,content,created_at) VALUES
		('00000000-0000-0000-0000-000000000002',$1,'assistant','legacy answer','2026-01-01'),
		('00000000-0000-0000-0000-000000000001',$1,'user','legacy question','2026-01-01')`, f.conversationID)
		if err != nil {
			t.Fatal(err)
		}
	}
	applyTestMigration(t, db, "000004_durable_transcript.up.sql")
	return f
}

func applyTestMigration(t *testing.T, db *pgxpool.Pool, name string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(context.Background(), string(data)); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func (f transcriptFixture) startRun(t *testing.T, content string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var runID, messageID uuid.UUID
	if err := f.service.db.QueryRow(ctx, `INSERT INTO runs(conversation_id,status) VALUES($1,'running') RETURNING id`, f.conversationID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.db.QueryRow(ctx, `INSERT INTO messages(conversation_id,run_id,role,content) VALUES($1,$2,'user',$3) RETURNING id`, f.conversationID, runID, content).Scan(&messageID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.db.Exec(ctx, `UPDATE runs SET input_message_id=$2 WHERE id=$1`, runID, messageID); err != nil {
		t.Fatal(err)
	}
	return runID
}

func (f transcriptFixture) messages(t *testing.T) []Message {
	t.Helper()
	_, messages, err := f.service.Get(context.Background(), f.workspaceID, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	return messages
}

type scriptedModel struct {
	step    int
	respond func(int, model.ModelRequest) []model.ModelEvent
}

func (m *scriptedModel) Stream(_ context.Context, r model.ModelRequest) (<-chan model.ModelEvent, error) {
	events := m.respond(m.step, r)
	m.step++
	ch := make(chan model.ModelEvent, len(events))
	for _, event := range events {
		ch <- event
	}
	close(ch)
	return ch, nil
}
func (*scriptedModel) Generate(context.Context, model.ModelRequest) (*model.ModelResponse, error) {
	return nil, errors.New("not used")
}
func (*scriptedModel) Capabilities(context.Context, string) (model.ModelCapabilities, error) {
	return model.ModelCapabilities{Tools: true}, nil
}

func TestTranscriptReadEditSurvivesReload(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	file := "port: 8080\nmode: development\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, file)
			return
		}
		body, _ := io.ReadAll(r.Body)
		file = string(body)
		w.WriteHeader(204)
	}))
	defer server.Close()
	f.service.sandboxes = sandbox.NewClient(server.URL)
	runID := f.startRun(t, "Read config.yaml and change port to 9090")
	request := model.ModelRequest{Model: "test", System: "test system", Messages: modelHistory(f.messages(t)), Tools: f.service.tools.Definitions(), MaxTokens: 4096}
	if err := f.service.saveRunContext(ctx, runID, f.messages(t), request); err != nil {
		t.Fatal(err)
	}
	client := &scriptedModel{respond: func(step int, r model.ModelRequest) []model.ModelEvent {
		switch step {
		case 0:
			return []model.ModelEvent{{Delta: "Reading configuration"}, {ToolCall: &model.ToolCall{ID: "read-1", Name: "read", Index: 1, Arguments: json.RawMessage(`{"file_path":"config.yaml"}`)}}}
		case 1:
			stored := f.messages(t)
			if len(stored) != 3 || stored[2].ToolCallID != "read-1" || !strings.Contains(stored[2].Content, `1\tport: 8080`) {
				t.Fatalf("read not persisted before next model call: %#v", stored)
			}
			// JSONB normalizes object whitespace/key order. Compare protocol
			// values rather than the original raw argument byte representation.
			if !sameModelMessages(modelHistory(stored), r.Messages) {
				t.Fatal("persisted context differs from live model input")
			}
			return []model.ModelEvent{{Delta: "Updating port"}, {ToolCall: &model.ToolCall{ID: "edit-1", Name: "edit", Index: 2, Arguments: json.RawMessage(`{"file_path":"config.yaml","old_string":"port: 8080","new_string":"port: 9090"}`)}}}
		case 2:
			if len(f.messages(t)) != 5 {
				t.Fatal("edit result not durable")
			}
			return []model.ModelEvent{{Delta: "Port changed to 9090"}}
		default:
			t.Fatal("unexpected model turn")
			return nil
		}
	}}
	f.service.executeTurns(ctx, f.conversationID, runID, &Computer{SandboxID: "test", WorkDir: conversationWorkDir(f.conversationID)}, client, request)
	stored := f.messages(t)
	if len(stored) != 6 || file != "port: 9090\nmode: development\n" {
		t.Fatalf("messages=%d file=%q", len(stored), file)
	}
	for i, m := range stored {
		if m.Seq != int64(i+1) || m.RunID == nil || *m.RunID != runID {
			t.Fatalf("message attribution/order: %#v", m)
		}
	}
	if len(visibleMessages(stored)) != 2 {
		t.Fatal("internal messages leaked into legacy chat projection")
	}
	var status string
	var snapshot []byte
	if err := f.service.db.QueryRow(ctx, `SELECT status,context FROM runs WHERE id=$1`, runID).Scan(&status, &snapshot); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || !strings.Contains(string(snapshot), "history_through_seq") {
		t.Fatalf("run=%s %s", status, snapshot)
	}
	var result, callID string
	if err := f.service.db.QueryRow(ctx, `SELECT payload->>'tool_call_id',payload->'result'->>'content' FROM run_events WHERE run_id=$1 AND type='TOOL_COMPLETED' AND payload->>'tool'='read'`, runID).Scan(&callID, &result); err != nil {
		t.Fatal(err)
	}
	if callID != "read-1" || result != "     1\tport: 8080\n     2\tmode: development" {
		t.Fatalf("read event result=%q id=%s", result, callID)
	}
	// New service instance models a later request after process restart.
	f.service = New(f.service.db, nil, nil, nil, agenttool.NewDefaultRegistry(f.service.db))
	f.startRun(t, "What was the old port?")
	restored := modelHistory(f.messages(t))
	if len(restored) != 7 || restored[2].ToolCallID != "read-1" || !strings.Contains(restored[2].Content, "8080") {
		t.Fatalf("restored=%#v", restored)
	}
}

func sameModelMessages(a, b []model.Message) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false
	}
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func TestToolContextProjectsEveryIterationAndReloadWithoutPruningStorage(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	file := strings.Repeat("original code line\n", 200)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, file)
	}))
	defer server.Close()
	f.service.sandboxes = sandbox.NewClient(server.URL)
	runID := f.startRun(t, "inspect files")
	computer := &Computer{SandboxID: "test", WorkDir: conversationWorkDir(f.conversationID)}
	client := &scriptedModel{respond: func(step int, request model.ModelRequest) []model.ModelEvent {
		switch step {
		case 0:
			var events []model.ModelEvent
			for i := 0; i < 12; i++ {
				events = append(events, model.ModelEvent{ToolCall: &model.ToolCall{ID: fmt.Sprintf("batch-%d", i), Name: "read", Index: i, Arguments: json.RawMessage(`{"file_path":"x.go"}`)}})
			}
			return events
		case 1:
			exchanges, err := toolcontext.Exchanges(request.Messages)
			if err != nil || len(exchanges) != 12 {
				t.Fatalf("unobserved 12-call batch was pruned: count=%d err=%v", len(exchanges), err)
			}
			return []model.ModelEvent{{ToolCall: &model.ToolCall{ID: "next-read", Name: "read", Arguments: json.RawMessage(`{"file_path":"y.go"}`)}}}
		case 2:
			stored := f.messages(t)
			projection, err := toolcontext.Build(modelHistory(stored))
			if err != nil || projection.Stats.Full != 10 || projection.Stats.Reference != 3 || !sameModelMessages(projection.Messages, request.Messages) {
				t.Fatalf("live projection differs from stored history: stats=%+v err=%v", projection.Stats, err)
			}
			return []model.ModelEvent{{Delta: "inspection complete"}}
		default:
			t.Fatal("unexpected iteration")
			return nil
		}
	}}
	initial := f.messages(t)
	request := model.ModelRequest{Messages: modelHistory(initial)}
	if err := f.service.saveRunContext(ctx, runID, initial, request); err != nil {
		t.Fatal(err)
	}
	f.service.executeTurns(ctx, f.conversationID, runID, computer, client, request)
	stored := f.messages(t)
	if client.step != 3 || len(stored) != 17 {
		t.Fatalf("steps=%d messages=%d", client.step, len(stored))
	}
	for _, message := range stored {
		if strings.Contains(message.Content, "Historical tool references") {
			t.Fatal("projected references overwrote source history")
		}
		if message.Role == "tool" && !strings.Contains(message.Content, "original code line") {
			t.Fatal("full tool result was lost")
		}
	}
	var references int
	var version string
	if err := f.service.db.QueryRow(ctx, `SELECT (payload->'tool_context'->>'reference')::int FROM run_events WHERE run_id=$1 AND type='MODEL_STARTED' ORDER BY id DESC LIMIT 1`, runID).Scan(&references); err != nil || references != 3 {
		t.Fatalf("context stats not recorded: references=%d err=%v", references, err)
	}
	if err := f.service.db.QueryRow(ctx, `SELECT context->>'tool_context_policy' FROM runs WHERE id=$1`, runID).Scan(&version); err != nil || version != toolcontext.PolicyVersion {
		t.Fatalf("policy snapshot: %s err=%v", version, err)
	}
	// A later run reconstructs exactly the same policy from the unpruned DB.
	f.service = New(f.service.db, nil, nil, nil, agenttool.NewDefaultRegistry(f.service.db))
	nextRun := f.startRun(t, "continue")
	client = &scriptedModel{respond: func(_ int, request model.ModelRequest) []model.ModelEvent {
		projection, err := toolcontext.Build(modelHistory(f.messages(t)))
		if err != nil || projection.Stats.Reference != 3 || !sameModelMessages(projection.Messages, request.Messages) {
			t.Fatalf("reload projection changed: %+v err=%v", projection.Stats, err)
		}
		return []model.ModelEvent{{Delta: "continued"}}
	}}
	f.service.executeTurns(ctx, f.conversationID, nextRun, computer, client, model.ModelRequest{Messages: modelHistory(f.messages(t))})
	if client.step != 1 {
		t.Fatal("reload did not reach the model")
	}
}

func TestTranscriptRecoveryClosesMissingResults(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	runID := f.startRun(t, "inspect files")
	assistant := model.Message{Role: "assistant", Content: "checking", ToolCalls: []model.ToolCall{
		{ID: "a", Name: "read", Arguments: json.RawMessage(`{}`)},
		{ID: "b", Name: "edit", Arguments: json.RawMessage(`{}`)},
	}}
	if err := f.service.appendMessage(ctx, f.conversationID, runID, assistant, "", nil); err != nil {
		t.Fatal(err)
	}
	if err := f.service.appendMessage(ctx, f.conversationID, runID, model.Message{Role: "tool", ToolCallID: "a", Content: `{"content":"known"}`}, "read", nil); err != nil {
		t.Fatal(err)
	}
	guard, err := f.service.acquireRun(ctx, f.workspaceID, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if err = f.service.recoverInterruptedRuns(ctx, f.conversationID); err != nil {
		t.Fatal(err)
	}
	stored := f.messages(t)
	if len(stored) != 4 || stored[3].ToolCallID != "b" || stored[3].Metadata["interrupted"] != true {
		t.Fatalf("recovered=%#v", stored)
	}
	if err = f.service.recoverInterruptedRuns(ctx, f.conversationID); err != nil {
		t.Fatal(err)
	}
	if len(f.messages(t)) != 4 {
		t.Fatal("recovery is not idempotent")
	}
	if err = f.service.appendMessage(ctx, f.conversationID, runID, model.Message{Role: "assistant", Content: "late old worker"}, "", nil); err == nil {
		t.Fatal("superseded worker wrote a message")
	}
}

func TestTranscriptToolFailureAndPartialStream(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	f.service.sandboxes = sandbox.NewClient("http://unused.invalid")
	runID := f.startRun(t, "use a tool")
	client := &scriptedModel{respond: func(step int, r model.ModelRequest) []model.ModelEvent {
		if step == 0 {
			return []model.ModelEvent{{ToolCall: &model.ToolCall{ID: "missing-1", Name: "nonexistent", Arguments: json.RawMessage(`{}`)}}}
		}
		if !strings.Contains(r.Messages[len(r.Messages)-1].Content, "unknown tool") {
			t.Fatal("tool error missing from context")
		}
		return []model.ModelEvent{{Delta: "partial response"}, {Err: errors.New("stream interrupted")}}
	}}
	f.service.executeTurns(ctx, f.conversationID, runID, &Computer{SandboxID: "test"}, client, model.ModelRequest{Messages: modelHistory(f.messages(t))})
	stored := f.messages(t)
	if len(stored) != 4 || stored[2].Metadata["is_error"] != true || stored[3].Metadata["incomplete"] != true {
		t.Fatalf("stored=%#v", stored)
	}
	if len(modelHistory(stored)) != 3 {
		t.Fatal("partial model response was replayed")
	}
}

func TestTranscriptGuardAndConcurrentSequence(t *testing.T) {
	f := newTranscriptFixture(t, true)
	ctx := context.Background()
	legacy := f.messages(t)
	if legacy[0].Content != "legacy question" || legacy[0].Seq != 1 || legacy[1].Seq != 2 {
		t.Fatalf("legacy order=%#v", legacy)
	}
	first, err := f.service.acquireRun(ctx, f.workspaceID, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := f.service.acquireRun(ctx, f.workspaceID, f.conversationID); !errors.Is(err, ErrRunInProgress) {
		if second != nil {
			second.Close()
		}
		first.Close()
		t.Fatalf("second guard=%v", err)
	}
	if _, err := f.service.Send(ctx, f.workspaceID, f.userID, f.conversationID, "must not persist", nil); !errors.Is(err, ErrRunInProgress) {
		first.Close()
		t.Fatalf("concurrent Send=%v", err)
	}
	if len(f.messages(t)) != 2 {
		first.Close()
		t.Fatal("rejected Send persisted a user message")
	}
	first.Close()
	third, err := f.service.acquireRun(ctx, f.workspaceID, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	third.Close()
	if guard, err := f.service.acquireRun(ctx, uuid.New(), f.conversationID); err == nil {
		guard.Close()
		t.Fatal("workspace scope bypassed")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.service.db.Exec(ctx, `INSERT INTO messages(conversation_id,role,content,created_at) VALUES($1,'user','concurrent','2020-01-01')`, f.conversationID)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	stored := f.messages(t)
	for i, m := range stored {
		if m.Seq != int64(i+1) {
			t.Fatalf("sequence=%d at %d", m.Seq, i)
		}
	}
	applyTestMigration(t, f.service.db, "000004_durable_transcript.down.sql")
	var count int
	if err := f.service.db.QueryRow(ctx, `SELECT count(*) FROM messages`).Scan(&count); err != nil || count != 14 {
		t.Fatalf("rollback lost messages: count=%d err=%v", count, err)
	}
	applyTestMigration(t, f.service.db, "000004_durable_transcript.up.sql")
}
