package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixture struct {
	service                         *Service
	workspace, user, project, model uuid.UUID
	runner                          *recordedRunner
}
type recordedRunner struct {
	db          *pgxpool.Pool
	calls       atomic.Int32
	hold        chan struct{}
	entered     chan struct{}
	returnError bool
	last        Input
	mu          sync.Mutex
}

func (r *recordedRunner) Start(ctx context.Context, i Input) (uuid.UUID, error) {
	r.calls.Add(1)
	r.mu.Lock()
	r.last = i
	r.mu.Unlock()
	if r.entered != nil {
		r.entered <- struct{}{}
	}
	if r.hold != nil {
		select {
		case <-r.hold:
		case <-ctx.Done():
			return uuid.Nil, ctx.Err()
		}
	}
	if _, err := r.db.Exec(ctx, `INSERT INTO conversations(id,workspace_id,created_by,agent_slug,project_id,model_deployment_id) VALUES($1,$2,$3,'lester',$4,$5)`, i.ConversationID, i.WorkspaceID, i.UserID, i.Config.ProjectID, i.Config.ModelID); err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `INSERT INTO runs(conversation_id,status,submission_id,submitted_by,submission_sha256,permission_mode) VALUES($1,'running',$2,$3,repeat('a',64),$4) RETURNING id`, i.ConversationID, i.ID, i.UserID, i.Config.Permission).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	if r.returnError {
		return uuid.Nil, errors.New("fixture response lost after commit")
	}
	return id, nil
}
func (r *recordedRunner) Lookup(ctx context.Context, i Input) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT r.id FROM runs r JOIN conversations c ON c.id=r.conversation_id WHERE c.workspace_id=$1 AND c.id=$2 AND r.submission_id=$3 AND r.submitted_by=$4`, i.WorkspaceID, i.ConversationID, i.ID, i.UserID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return id, err
}
func newFixture(t *testing.T) fixture {
	t.Helper()
	url := os.Getenv("LESTER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set LESTER_TEST_DATABASE_URL for real PostgreSQL automation tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := "automation_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, `BEGIN; SELECT pg_advisory_xact_lock(hashtextextended('lester:test:pgcrypto',0)); CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public; COMMIT;`); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	migrations, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range migrations {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, string(raw)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	f := fixture{workspace: uuid.New(), user: uuid.New(), model: uuid.New(), runner: &recordedRunner{db: db}}
	f.service = &Service{DB: db, Runner: f.runner}
	if _, err = db.Exec(ctx, `INSERT INTO users(id,email,display_name,password_hash) VALUES($1,'automation@example.invalid','Automation','unused');`, f.user); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO workspaces(id,name) VALUES($1,'Automation');`, f.workspace); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2)`, f.workspace, f.user); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `SELECT id FROM projects WHERE workspace_id=$1 AND is_default`, f.workspace).Scan(&f.project); err != nil {
		t.Fatal(err)
	}
	var connection uuid.UUID
	if err = db.QueryRow(ctx, `INSERT INTO model_connections(workspace_id,name,provider,protocol) VALUES($1,'fixture','openai-compatible','openai') RETURNING id`, f.workspace).Scan(&connection); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO model_deployments(id,workspace_id,connection_id,name,model_id) VALUES($1,$2,$3,'fixture','fixture')`, f.model, f.workspace, connection); err != nil {
		t.Fatal(err)
	}
	return f
}
func (f fixture) config(kind string) Config {
	return Config{Name: "Daily brief", Kind: kind, ProjectID: f.project, Agent: "lester", ModelID: f.model, Prompt: "Summarize the project", Permission: "standard", Enabled: true, Cron: map[string]string{"schedule": "0 9 * * *"}[kind], Timezone: "Asia/Shanghai"}
}
func (f fixture) save(t *testing.T, kind string) (Definition, string) {
	t.Helper()
	d, secret, err := f.service.Save(context.Background(), f.workspace, f.user, uuid.New(), f.config(kind), 0)
	if err != nil {
		t.Fatal(err)
	}
	return d, secret
}

func TestScheduleCalendarTimezoneDSTAndValidation(t *testing.T) {
	cases := []struct{ expression, zone, after, want string }{{"0 9 * * *", "Asia/Shanghai", "2026-10-16T00:00:00Z", "2026-10-16T01:00:00Z"}, {"30 9 * * 1", "Asia/Tokyo", "2026-10-16T00:00:00Z", "2026-10-19T00:30:00Z"}, {"0 9 * * *", "America/New_York", "2026-03-07T15:00:00Z", "2026-03-08T13:00:00Z"}, {"30 2 * * *", "America/New_York", "2026-03-08T05:00:00Z", "2026-03-09T06:30:00Z"}}
	for _, c := range cases {
		after, _ := time.Parse(time.RFC3339, c.after)
		next, err := nextOccurrence(Config{Kind: "schedule", Cron: c.expression, Timezone: c.zone, Enabled: true}, after)
		if err != nil || next == nil || next.Format(time.RFC3339) != c.want {
			t.Fatalf("%#v: %v %v", c, next, err)
		}
	}
	for _, c := range []Config{{Kind: "schedule", Cron: "* * * * * *", Timezone: "UTC"}, {Kind: "schedule", Cron: "@every 1s", Timezone: "UTC"}, {Kind: "schedule", Cron: "0 9 * * *", Timezone: "Missing/Zone"}, {Kind: "schedule", Timezone: "UTC"}, {Kind: "other"}, {Kind: "schedule", Cron: "0 9 * * *", Timezone: "Local"}, {Kind: "schedule", Cron: "CRON_TZ=UTC 0 9 * * *", Timezone: "Asia/Tokyo"}} {
		if _, err := nextOccurrence(c, time.Now()); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid config: %#v", c)
		}
	}
	past := time.Now().Add(-time.Minute)
	if _, err := nextOccurrence(Config{Kind: "schedule", Timezone: "UTC", Enabled: true, RunAt: &past}, time.Now()); !errors.Is(err, ErrInvalid) {
		t.Fatal("enabled past one-shot accepted")
	}
}
func TestDefinitionScopeRevisionSecretAndCreationReceipt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, token := f.save(t, "webhook")
	if !strings.HasPrefix(token, "lwh_") {
		t.Fatal("missing one-time secret")
	}
	same, again, err := f.service.Save(ctx, f.workspace, f.user, d.ID, d.Config, 0)
	if err != nil || same.ID != d.ID || again != "" {
		t.Fatalf("creation receipt: %v", err)
	}
	list, err := f.service.List(ctx, f.workspace, f.user)
	raw, _ := json.Marshal(list)
	if err != nil || len(list) != 1 || strings.Contains(string(raw), token) || strings.Contains(string(raw), "webhook_hash") {
		t.Fatalf("secret leaked/list failed: %v", err)
	}
	if _, err = f.service.Get(ctx, uuid.New(), f.user, d.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("workspace leak")
	}
	if _, err = f.service.Get(ctx, f.workspace, uuid.New(), d.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("owner leak")
	}
	d.Config.Prompt = "Changed"
	updated, _, err := f.service.Save(ctx, f.workspace, f.user, d.ID, d.Config, d.Revision)
	if err != nil || updated.Revision != 2 {
		t.Fatal(err)
	}
	if _, _, err = f.service.Save(ctx, f.workspace, f.user, d.ID, d.Config, d.Revision); !errors.Is(err, ErrConflict) {
		t.Fatal("stale write accepted")
	}
	invalid := f.config("schedule")
	invalid.ProjectID = uuid.New()
	if _, _, err = f.service.Save(ctx, f.workspace, f.user, uuid.New(), invalid, 0); !errors.Is(err, ErrInvalid) {
		t.Fatal("foreign project accepted")
	}
	invalid = f.config("schedule")
	invalid.ModelID = uuid.New()
	if _, _, err = f.service.Save(ctx, f.workspace, f.user, uuid.New(), invalid, 0); !errors.Is(err, ErrInvalid) {
		t.Fatal("foreign model accepted")
	}
}
func TestWebhookDuplicateConflictRotationPauseAndActorRevocation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, token := f.save(t, "webhook")
	payload := json.RawMessage(`{"issue":123}`)
	if _, err := f.service.Webhook(ctx, d.ID, "wrong", "event-1", payload); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("bad token accepted")
	}
	const clients = 8
	results := make(chan uuid.UUID, clients)
	errs := make(chan error, clients)
	var workers sync.WaitGroup
	for range clients {
		workers.Go(func() {
			item, err := f.service.Webhook(ctx, d.ID, token, "event-1", payload)
			results <- item.ID
			errs <- err
		})
	}
	workers.Wait()
	close(results)
	close(errs)
	var accepted uuid.UUID
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for id := range results {
		if accepted == uuid.Nil {
			accepted = id
		}
		if id != accepted {
			t.Fatal("duplicate event accepted twice")
		}
	}
	if _, err := f.service.Webhook(ctx, d.ID, token, "event-1", json.RawMessage(`{"issue":456}`)); !errors.Is(err, ErrConflict) {
		t.Fatal("changed duplicate accepted")
	}
	rotated, newToken, err := f.service.Rotate(ctx, f.workspace, f.user, d.ID, d.Revision)
	if err != nil || newToken == token {
		t.Fatal(err)
	}
	if _, err = f.service.Webhook(ctx, d.ID, token, "old-token", payload); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("old token accepted")
	}
	if _, err = f.service.Webhook(ctx, d.ID, newToken, "event-2", payload); err != nil {
		t.Fatal(err)
	}
	rotated.Enabled = false
	paused, _, err := f.service.Save(ctx, f.workspace, f.user, d.ID, rotated.Config, rotated.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Webhook(ctx, d.ID, newToken, "event-3", payload); !errors.Is(err, ErrPaused) {
		t.Fatal("paused webhook accepted")
	}
	history, err := f.service.History(ctx, f.workspace, f.user, d.ID)
	if err != nil || len(history) != 2 || history[0].State != "cancelled" {
		t.Fatalf("pause history: %#v %v", history, err)
	}
	paused.Enabled = true
	if _, _, err = f.service.Save(ctx, f.workspace, f.user, d.ID, paused.Config, paused.Revision); err != nil {
		t.Fatal(err)
	}
	item, err := f.service.Webhook(ctx, d.ID, newToken, "event-4", payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.DB.Exec(ctx, `UPDATE users SET disabled=true WHERE id=$1`, f.user); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Webhook(ctx, d.ID, newToken, "event-5", payload); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("disabled actor accepted")
	}
	if err = f.service.dispatch(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := f.service.Delivery(ctx, f.workspace, f.user, d.ID, item.ID)
	if err != nil || stored.State != "failed" || stored.ErrorCode != "authorization_changed" || f.runner.calls.Load() != 0 {
		t.Fatalf("revoked dispatch: %#v %v", stored, err)
	}
}
func TestScheduledOccurrencesCoalesceBusyAndKeepAcceptedSnapshot(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, _ := f.save(t, "schedule")
	now := time.Now()
	due := now.Add(-48 * time.Hour)
	if _, err := f.service.DB.Exec(ctx, `UPDATE automations SET next_run_at=$2 WHERE id=$1`, d.ID, due); err != nil {
		t.Fatal(err)
	}
	if err := f.service.enqueueDue(ctx, now); err != nil {
		t.Fatal(err)
	}
	if err := f.service.enqueueDue(ctx, now); err != nil {
		t.Fatal(err)
	}
	history, err := f.service.History(ctx, f.workspace, f.user, d.ID)
	if err != nil || len(history) != 1 || history[0].ScheduledAt == nil {
		t.Fatalf("coalesce: %#v %v", history, err)
	}
	latest, err := f.service.Get(ctx, f.workspace, f.user, d.ID)
	if err != nil || latest.NextRunAt == nil || !latest.NextRunAt.After(now) {
		t.Fatal("next occurrence not advanced")
	}
	latest.Prompt = "Future tasks only"
	if _, _, err = f.service.Save(ctx, f.workspace, f.user, d.ID, latest.Config, latest.Revision); err != nil {
		t.Fatal(err)
	}
	if err = f.service.dispatch(ctx, history[0].ID); err != nil {
		t.Fatal(err)
	}
	if f.runner.last.Config.Prompt != d.Prompt {
		t.Fatal("accepted snapshot changed")
	}
	if _, err = f.service.DB.Exec(ctx, `UPDATE automations SET next_run_at=$2 WHERE id=$1`, d.ID, now); err != nil {
		t.Fatal(err)
	}
	if err = f.service.enqueueDue(ctx, now); err != nil {
		t.Fatal(err)
	}
	history, err = f.service.History(ctx, f.workspace, f.user, d.ID)
	if err != nil || len(history) != 2 || history[0].State != "skipped" || history[0].ErrorCode != "busy" {
		t.Fatalf("overlap: %#v %v", history, err)
	}
}
func TestDispatchReplicaOwnershipAndUncertainReceiptsNeverReplay(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, _ := f.save(t, "webhook")
	item, err := f.service.Trigger(ctx, f.workspace, f.user, d.ID, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	f.runner.hold = make(chan struct{})
	f.runner.entered = make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() { done <- f.service.dispatch(ctx, item.ID) }()
	<-f.runner.entered
	if err = f.service.dispatch(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if f.runner.calls.Load() != 1 {
		t.Fatal("another replica launched a duplicate")
	}
	close(f.runner.hold)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	f.runner.hold = nil
	f.runner.entered = nil
	if _, err = f.service.DB.Exec(ctx, `UPDATE automation_deliveries SET state='dispatching',run_id=NULL WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.service.dispatch(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := f.service.Delivery(ctx, f.workspace, f.user, d.ID, item.ID)
	if err != nil || stored.State != "accepted" || stored.RunID == nil || f.runner.calls.Load() != 1 {
		t.Fatalf("receipt recovery replayed: %#v %v", stored, err)
	}
	orphan, err := f.service.Trigger(ctx, f.workspace, f.user, d.ID, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.DB.Exec(ctx, `UPDATE automation_deliveries SET state='dispatching' WHERE id=$1`, orphan.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.service.dispatch(ctx, orphan.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.service.dispatch(ctx, orphan.ID); err != nil {
		t.Fatal(err)
	}
	stored, err = f.service.Delivery(ctx, f.workspace, f.user, d.ID, orphan.ID)
	if err != nil || stored.State != "unknown" || f.runner.calls.Load() != 1 {
		t.Fatalf("uncertain delivery replayed: %#v %v", stored, err)
	}
}

func TestLostStartResponseUsesReceiptAndManualQueueWaitsForCompletion(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, _ := f.save(t, "webhook")
	f.runner.returnError = true
	first, err := f.service.Trigger(ctx, f.workspace, f.user, d.ID, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.dispatch(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	first, err = f.service.Delivery(ctx, f.workspace, f.user, d.ID, first.ID)
	if err != nil || first.State != "accepted" || first.RunID == nil {
		t.Fatalf("lost start receipt: %#v %v", first, err)
	}
	next, err := f.service.Trigger(ctx, f.workspace, f.user, d.ID, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.dispatch(ctx, next.ID); err != nil {
		t.Fatal(err)
	}
	if f.runner.calls.Load() != 1 {
		t.Fatal("overlapping delivery launched")
	}
	if _, err = f.service.DB.Exec(ctx, `UPDATE runs SET status='completed',completed_at=now() WHERE id=$1`, first.RunID); err != nil {
		t.Fatal(err)
	}
	if err = f.service.dispatch(ctx, next.ID); err != nil {
		t.Fatal(err)
	}
	if f.runner.calls.Load() != 2 {
		t.Fatal("queued independent event did not dispatch")
	}
}
func TestOneShotAndMonitorStartWithoutBrowser(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	config := f.config("schedule")
	config.Cron = ""
	when := time.Now().Add(time.Minute)
	config.RunAt = &when
	d, _, err := f.service.Save(ctx, f.workspace, f.user, uuid.New(), config, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.DB.Exec(ctx, `UPDATE automations SET next_run_at=now()-interval '1 minute' WHERE id=$1`, d.ID); err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { f.service.Run(parent); close(done) }()
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var accepted bool
		if err = f.service.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM automation_deliveries WHERE automation_id=$1 AND state='accepted' AND run_id IS NOT NULL)`, d.ID).Scan(&accepted); err != nil {
			cancel()
			<-done
			t.Fatal(err)
		}
		if accepted {
			break
		}
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("startup required an HTTP visit")
		case <-ticker.C:
		}
	}
	cancel()
	<-done
	if err = f.service.enqueueDue(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	history, err := f.service.History(ctx, f.workspace, f.user, d.ID)
	if err != nil || len(history) != 1 {
		t.Fatalf("one shot repeated: %#v %v", history, err)
	}
	latest, err := f.service.Get(ctx, f.workspace, f.user, d.ID)
	if err != nil || latest.NextRunAt != nil {
		t.Fatal("one shot was scheduled twice")
	}
}

func TestConcurrentScheduleClaimsAndLateReceiptRecovery(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, _ := f.save(t, "schedule")
	now := time.Now()
	if _, err := f.service.DB.Exec(ctx, `UPDATE automations SET next_run_at=$2 WHERE id=$1`, d.ID, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	errs := make(chan error, 6)
	for range 6 {
		group.Go(func() { errs <- f.service.enqueueDue(ctx, now) })
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	history, err := f.service.History(ctx, f.workspace, f.user, d.ID)
	if err != nil || len(history) != 1 {
		t.Fatal("schedule duplicated", err)
	}
	var raw json.RawMessage
	if err = f.service.DB.QueryRow(ctx, `UPDATE automation_deliveries SET state='unknown',error_code='receipt_unknown' WHERE id=$1 RETURNING snapshot`, history[0].ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var input Input
	if err = json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	// Simulate the original owner's delayed commit; recovery must only link it.
	original, err := f.runner.Start(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.dispatch(ctx, input.ID); err != nil {
		t.Fatal(err)
	}
	record, err := f.service.Delivery(ctx, f.workspace, f.user, d.ID, input.ID)
	if err != nil || record.State != "accepted" || record.RunID == nil || *record.RunID != original || f.runner.calls.Load() != 1 {
		t.Fatalf("late receipt replayed: %#v %v", record, err)
	}
}
func TestPendingAndRateBoundsKeepDuplicateReceiptsReadable(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, secret := f.save(t, "webhook")
	payload := json.RawMessage(`{"x":1}`)
	for index := range 20 {
		if _, err := f.service.Webhook(ctx, d.ID, secret, fmt.Sprintf("event-%d", index), payload); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.service.Webhook(ctx, d.ID, secret, "queue-full", payload); !errors.Is(err, ErrLimit) {
		t.Fatal("queue unbounded")
	}
	if _, err := f.service.Webhook(ctx, d.ID, secret, "event-0", payload); err != nil {
		t.Fatal("duplicate consumed a new queue slot")
	}
	if _, err := f.service.DB.Exec(ctx, `UPDATE automation_deliveries SET state='cancelled' WHERE automation_id=$1`, d.ID); err != nil {
		t.Fatal(err)
	}
	for index := 20; index < 30; index++ {
		if _, err := f.service.Webhook(ctx, d.ID, secret, fmt.Sprintf("event-%d", index), payload); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.service.Webhook(ctx, d.ID, secret, "rate-full", payload); !errors.Is(err, ErrLimit) {
		t.Fatal("rate unbounded")
	}
	if _, err := f.service.Webhook(ctx, d.ID, secret, "event-0", payload); err != nil {
		t.Fatal("duplicate consumed a new rate slot")
	}
}

func TestDeliveryOrderSurvivesTransactionTimestampInversion(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, _ := f.save(t, "webhook")
	first, err := f.service.Trigger(ctx, f.workspace, f.user, d.ID, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.Trigger(ctx, f.workspace, f.user, d.ID, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	// now() is a transaction's start time, not its acceptance order. A blocked
	// request or wall-clock adjustment must not reorder the durable queue.
	if _, err = f.service.DB.Exec(ctx, `UPDATE automation_deliveries SET created_at=now()+interval '1 hour' WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.service.dispatch(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if f.runner.calls.Load() != 0 {
		t.Fatal("later event overtook first accepted event")
	}
	if err = f.service.dispatch(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := f.service.Delivery(ctx, f.workspace, f.user, d.ID, first.ID)
	if err != nil || stored.RunID == nil {
		t.Fatal(err)
	}
	if _, err = f.service.DB.Exec(ctx, `UPDATE runs SET status='completed',completed_at=now() WHERE id=$1`, stored.RunID); err != nil {
		t.Fatal(err)
	}
	if err = f.service.dispatch(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if f.runner.calls.Load() != 2 {
		t.Fatal("accepted sequence did not resume")
	}
}
func TestAutomationMigrationRollbackPreservesAcceptedConversation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, _ := f.save(t, "webhook")
	event, err := f.service.Trigger(ctx, f.workspace, f.user, d.ID, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.dispatch(ctx, event.ID); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../migrations/000027_automations.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.DB.Exec(ctx, string(raw)); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err = f.service.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2)`, f.workspace, event.ConversationID).Scan(&exists); err != nil || !exists {
		t.Fatal("rollback deleted conversation", err)
	}
	raw, err = os.ReadFile("../../migrations/000027_automations.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.DB.Exec(ctx, string(raw)); err != nil {
		t.Fatal(err)
	}
	items, err := f.service.List(ctx, f.workspace, f.user)
	if err != nil || len(items) != 0 {
		t.Fatal("reapply failed", err)
	}
}
