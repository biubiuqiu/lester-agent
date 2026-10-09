package conversation

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/deliverable"
	"github.com/biubiuqiu/lester-agent/backend/internal/eventlog"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type deliverableFiles struct {
	content []byte
	err     error
	calls   int
	read    func(context.Context)
}

func (f *deliverableFiles) ReadFile(ctx context.Context, _, _, _ string) ([]byte, error) {
	f.calls++
	if f.read != nil {
		f.read(ctx)
	}
	return f.content, f.err
}

func TestDeliverableRegistrationPersistsIdentityAndScopedProvenance(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	run := f.startRun(t, "Make a report")
	files := &deliverableFiles{content: []byte("# Real report")}
	service := &deliverable.Service{DB: f.service.db, Files: files}
	input := deliverable.Input{EntryPath: "report.md", Title: "需求评审报告", Summary: "已整理目标与验收项目，内容仍需复核。"}
	first, err := service.Register(ctx, f.workspaceID, f.conversationID, run, "computer", conversationWorkDir(f.conversationID), input)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(files.content)
	if first.ContentSHA256 != hex.EncodeToString(digest[:]) || first.RunID != run {
		t.Fatalf("incorrect file evidence: %#v", first)
	}
	items, err := service.List(ctx, f.workspaceID, f.conversationID)
	if err != nil || len(items) != 1 || items[0].Title != input.Title {
		t.Fatalf("list = %#v, %v", items, err)
	}
	before := files.calls
	if _, err = service.Register(ctx, uuid.New(), f.conversationID, run, "computer", conversationWorkDir(f.conversationID), input); !errors.Is(err, deliverable.ErrInactiveRun) {
		t.Fatalf("cross-workspace register: %v", err)
	}
	if files.calls != before {
		t.Fatal("unauthorized registration accessed files")
	}
	if _, err = service.List(ctx, uuid.New(), f.conversationID); !errors.Is(err, deliverable.ErrNotFound) {
		t.Fatalf("cross-workspace list: %v", err)
	}
	if err = f.service.finishCompletedRun(ctx, run, f.conversationID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Register(ctx, f.workspaceID, f.conversationID, run, "computer", conversationWorkDir(f.conversationID), input); !errors.Is(err, deliverable.ErrInactiveRun) {
		t.Fatalf("terminal run register: %v", err)
	}
	nextRun := f.startRun(t, "Refine the report")
	files.content = []byte("# Updated report")
	input.Title = "更新后的评审报告"
	next, err := service.Register(ctx, f.workspaceID, f.conversationID, nextRun, "computer", conversationWorkDir(f.conversationID), input)
	if err != nil || next.ID != first.ID || next.RunID != nextRun || next.ContentSHA256 == first.ContentSHA256 {
		t.Fatalf("stable update: %#v, %v", next, err)
	}
}

func TestDeliverableRegistrationRejectsMissingFilesAndCancellationRace(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	run := f.startRun(t, "Make a website")
	files := &deliverableFiles{err: errors.New("not found")}
	service := &deliverable.Service{DB: f.service.db, Files: files}
	input := deliverable.Input{EntryPath: "index.html", Title: "网站"}
	if _, err := service.Register(ctx, f.workspaceID, f.conversationID, run, "computer", conversationWorkDir(f.conversationID), input); err == nil {
		t.Fatal("missing file registered")
	}
	files.err = nil
	files.content = []byte("<h1>Website</h1>")
	files.read = func(context.Context) {
		if err := f.service.CancelRun(ctx, f.workspaceID, f.conversationID, run); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.Register(ctx, f.workspaceID, f.conversationID, run, "computer", conversationWorkDir(f.conversationID), input); !errors.Is(err, deliverable.ErrInactiveRun) {
		t.Fatalf("cancel race: %v", err)
	}
	var count int
	if err := f.service.db.QueryRow(ctx, `SELECT count(*) FROM deliverables`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("phantom deliverable count=%d err=%v", count, err)
	}
}

func TestCriticalRunTransitionsRollbackWhenDeliveryIntentCannotPersist(t *testing.T) {
	for _, state := range []string{"completed", "failed", "cancelled", "cancelling"} {
		t.Run(state, func(t *testing.T) {
			f := newTranscriptFixture(t, false)
			ctx := context.Background()
			run := f.startRun(t, "Execute task")
			transition := func() error {
				switch state {
				case "completed":
					return f.service.finishCompletedRun(ctx, run, f.conversationID)
				case "failed":
					return f.service.finishFailedRun(ctx, run, f.conversationID, "test failure")
				case "cancelled":
					_, err := f.service.finishCancelledRun(ctx, run, f.conversationID)
					return err
				default:
					return f.service.CancelRun(ctx, f.workspaceID, f.conversationID, run)
				}
			}
			_, err := f.service.db.Exec(ctx, `CREATE FUNCTION reject_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected delivery intent failure'; END $$; CREATE TRIGGER reject_outbox BEFORE INSERT ON run_event_outbox FOR EACH ROW EXECUTE FUNCTION reject_outbox()`)
			if err != nil {
				t.Fatal(err)
			}
			if err = transition(); err == nil {
				t.Fatal("transition succeeded without delivery intent")
			}
			var status string
			var count int
			if err = f.service.db.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1`, run).Scan(&status); err != nil || status != "running" {
				t.Fatalf("state changed without event: %s %v", status, err)
			}
			if err = f.service.db.QueryRow(ctx, `SELECT count(*) FROM run_events WHERE run_id=$1`, run).Scan(&count); err != nil || count != 0 {
				t.Fatalf("event survived rollback: %d %v", count, err)
			}
			if _, err = f.service.db.Exec(ctx, `DROP TRIGGER reject_outbox ON run_event_outbox`); err != nil {
				t.Fatal(err)
			}
			if err = transition(); err != nil {
				t.Fatal(err)
			}
			if err = transition(); err != nil {
				t.Fatal(err)
			}
			if err = f.service.db.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1`, run).Scan(&status); err != nil || status != state {
				t.Fatalf("status=%s err=%v", status, err)
			}
			if err = f.service.db.QueryRow(ctx, `SELECT count(*) FROM run_event_outbox`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("duplicated transition: %d %v", count, err)
			}
		})
	}
}

func TestOutboxRetriesPartialPublicationAndPreservesEventIDs(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	run := f.startRun(t, "Task")
	f.service.event(ctx, run, f.conversationID, "MODEL_STARTED", map[string]any{})
	if err := f.service.finishCompletedRun(ctx, run, f.conversationID); err != nil {
		t.Fatal(err)
	}
	calls := 0
	dispatcher := eventlog.Dispatcher{DB: f.service.db, Publish: func(context.Context, string, []byte) error {
		calls++
		if calls == 2 {
			return errors.New("Redis unavailable")
		}
		return nil
	}}
	if _, err := dispatcher.Flush(ctx); err == nil {
		t.Fatal("partial publish succeeded")
	}
	var count int
	if err := f.service.db.QueryRow(ctx, `SELECT count(*) FROM run_event_outbox`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("lost retry intents: %d %v", count, err)
	}
	var ids []int64
	dispatcher.Publish = func(_ context.Context, channel string, data []byte) error {
		if strings.HasPrefix(channel, "workspace:") {
			if channel != "workspace:"+f.workspaceID.String() {
				t.Fatal("wrong workspace channel")
			}
			var event RunEvent
			if err := json.Unmarshal(data, &event); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, event.ID)
		}
		return nil
	}
	if count, err := dispatcher.Flush(ctx); err != nil || count != 2 {
		t.Fatalf("retry: %d %v", count, err)
	}
	if len(ids) != 2 || ids[0] >= ids[1] {
		t.Fatalf("event order=%v", ids)
	}
	if count, err := dispatcher.Flush(ctx); err != nil || count != 0 {
		t.Fatalf("delivered intents retained: %d %v", count, err)
	}
}

func TestWorkspaceEventIDsFollowCommitOrder(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run := f.startRun(t, "Task")
	tx1, err := f.service.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx1.Rollback(ctx)
	if err = eventlog.Append(ctx, tx1, run, f.conversationID, "FIRST", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		tx2, err := f.service.db.Begin(ctx)
		if err != nil {
			done <- err
			return
		}
		defer tx2.Rollback(ctx)
		err = eventlog.Append(ctx, tx2, run, f.conversationID, "SECOND", map[string]any{})
		if err == nil {
			err = tx2.Commit(ctx)
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("second event committed before first: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	if err = tx1.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	rows, err := f.service.db.Query(ctx, `SELECT type FROM run_events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var kinds []string
	for rows.Next() {
		var kind string
		if err = rows.Scan(&kind); err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, kind)
	}
	if fmt.Sprint(kinds) != "[FIRST SECOND]" {
		t.Fatalf("order=%v", kinds)
	}
}

func TestExecutorShutdownPersistsInterruptionAndReleasesGuard(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	run := f.startRun(t, "Task")
	guard, err := f.service.acquireRun(ctx, f.workspaceID, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	if !f.service.executor.launch(guard, f.conversationID, run, func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		f.service.finishContext(ctx, run, f.conversationID)
	}) {
		t.Fatal("executor rejected initial run")
	}
	<-started
	shutdownCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err = f.service.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = f.service.db.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1`, run).Scan(&status); err != nil || status != "failed" {
		t.Fatalf("shutdown status=%s %v", status, err)
	}
	nextGuard, err := f.service.acquireRun(ctx, f.workspaceID, f.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	defer nextGuard.Close()
	if f.service.executor.launch(nextGuard, f.conversationID, run, func(context.Context) { t.Error("execution started after shutdown") }) {
		t.Fatal("executor accepted run after shutdown")
	}
}

func TestDeliverablesMigrationRollback(t *testing.T) {
	f := newTranscriptFixture(t, false)
	data, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000012_deliverables_events.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.db.Exec(context.Background(), string(data)); err != nil {
		t.Fatal(err)
	}
	applyTestMigration(t, f.service.db, "000012_deliverables_events.up.sql")
}

// Exercise the complete tool/transcript boundary and the authenticated metadata
// transport, including the fact that listing never re-reads a Computer file.
func TestDeliverableToolAndAuthenticatedList(t *testing.T) {
	f := newTranscriptFixture(t, false)
	applyTestMigration(t, f.service.db, "000005_user_profiles.up.sql")
	applyTestMigration(t, f.service.db, "000013_account_identity.up.sql")
	ctx := context.Background()
	files := &deliverableFiles{content: []byte("# Delivery report")}
	service := &deliverable.Service{DB: f.service.db, Files: files}
	f.service.tools.Register(agenttool.RegisterDeliverable{Service: service})
	run := f.startRun(t, "Make a report")
	request := model.ModelRequest{Model: "test", Messages: modelHistory(f.messages(t)), Tools: f.service.tools.Definitions()}
	client := &scriptedModel{respond: func(step int, request model.ModelRequest) []model.ModelEvent {
		if step == 0 {
			return []model.ModelEvent{{ToolCall: &model.ToolCall{ID: "register-1", Name: "register_deliverable", Arguments: json.RawMessage(`{"entry_path":"report.md","title":"交付报告","summary":"仍需复核内容"}`)}}}
		}
		stored := f.messages(t)
		if len(stored) != 3 || stored[2].ToolCallID != "register-1" || !strings.Contains(stored[2].Content, "content_sha256") {
			t.Fatalf("registration result not durable before next model turn: %#v", stored)
		}
		return []model.ModelEvent{{Delta: "报告已登记，内容仍需检查。"}}
	}}
	f.service.executor.executeTurns(ctx, f.conversationID, run, &Computer{SandboxID: "computer", WorkDir: conversationWorkDir(f.conversationID)}, client, request)
	var status string
	if err := f.service.db.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1`, run).Scan(&status); err != nil || status != "completed" {
		t.Fatalf("registration execution failed: %s, %v", status, err)
	}
	raw := []byte(uuid.NewString())
	sum := sha256.Sum256(raw)
	if _, err := f.service.db.Exec(ctx, `INSERT INTO sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 hour')`, f.userID, sum[:]); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	router.Use(auth.New(f.service.db, nil, time.Hour, false).Middleware)
	router.Get("/conversations/{id}/deliverables", (&deliverable.Handler{Service: service}).List)
	call := func(id string, authenticated bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest("GET", "/conversations/"+id+"/deliverables", nil)
		if authenticated {
			request.AddCookie(&http.Cookie{Name: "lester_session", Value: base64.RawURLEncoding.EncodeToString(raw)})
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	before := files.calls
	response := call(f.conversationID.String(), true)
	var body struct {
		Deliverables []deliverable.Deliverable `json:"deliverables"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != 200 || len(body.Deliverables) != 1 || body.Deliverables[0].RunID != run {
		t.Fatalf("authenticated metadata response: %d %s, %v", response.Code, response.Body, err)
	}
	if files.calls != before {
		t.Fatal("metadata list woke or read the Computer")
	}
	if got := call(f.conversationID.String(), false).Code; got != 401 {
		t.Fatalf("unauthenticated status=%d", got)
	}
	if got := call(uuid.NewString(), true).Code; got != 404 {
		t.Fatalf("inaccessible conversation status=%d", got)
	}
	if got := call("invalid", true).Code; got != 400 {
		t.Fatalf("invalid identifier status=%d", got)
	}
}
