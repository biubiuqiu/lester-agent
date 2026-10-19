package automation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
	"github.com/biubiuqiu/lester-agent/backend/internal/conversation"
	"github.com/google/uuid"
)

func (f fixture) scheduleRun(t *testing.T) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	conv, run, input := uuid.New(), uuid.New(), uuid.New()
	if _, err := f.service.DB.Exec(ctx, `INSERT INTO conversations(id,workspace_id,created_by,agent_slug,project_id,model_deployment_id) VALUES($1,$2,$3,'lester',$4,$5)`, conv, f.workspace, f.user, f.project, f.model); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.DB.Exec(ctx, `INSERT INTO runs(id,conversation_id,status,submitted_by,model_deployment_id,permission_mode) VALUES($1,$2,'running',$3,$4,'full')`, run, conv, f.user, f.model); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.DB.Exec(ctx, `INSERT INTO messages(id,conversation_id,run_id,role,content,metadata) VALUES($1,$2,$3,'user','每周一九点生成评审清单','{"client_timezone":"Asia/Shanghai"}')`, input, conv, run); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.DB.Exec(ctx, `UPDATE runs SET input_message_id=$2 WHERE id=$1`, run, input); err != nil {
		t.Fatal(err)
	}
	return conv, run
}

// Seed the durable boundary exactly as createRunInteraction does; the separate
// conversation tests exercise originating-call binding and Plan enforcement.
func (f fixture) scheduleCard(t *testing.T, conv, run uuid.UUID, r agenttool.ScheduleRequest) *conversation.RunInteraction {
	t.Helper()
	ctx := context.Background()
	tx, err := f.service.DB.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	p, err := f.service.PrepareSchedule(ctx, tx, conv, run, r)
	if err != nil {
		t.Fatal(err)
	}
	req := agenttool.InteractionRequest{Kind: "automation", Schedule: &r, SchedulePreview: p}
	raw, _ := json.Marshal(req)
	digest := sha256.Sum256(raw)
	item := &conversation.RunInteraction{ID: uuid.New(), RunID: run, Kind: "automation", Request: req, RequestDigest: hex.EncodeToString(digest[:]), Status: "pending"}
	if _, err = tx.Exec(ctx, `INSERT INTO run_interactions(id,workspace_id,conversation_id,run_id,tool_call_id,kind,request,request_digest) VALUES($1,$2,$3,$4,$5,'automation',$6,$7)`, item.ID, f.workspace, conv, run, uuid.NewString(), raw, item.RequestDigest); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return item
}
func scheduleCreate() agenttool.ScheduleRequest {
	return agenttool.ScheduleRequest{Action: "create", Name: "评审清单", Prompt: "生成一份包含目标、范围、验收标准的需求评审清单。", Cron: "0 9 * * 1"}
}

func TestConversationalScheduleConfirmationIsAtomicScopedAndIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	conv, run := f.scheduleRun(t)
	s := conversation.New(f.service.DB, nil, nil, agenttool.NewDefaultRegistry(f.service.DB))
	s.SetSchedules(f.service)
	item := f.scheduleCard(t, conv, run, scheduleCreate())
	p := item.Request.SchedulePreview
	if p.Permission != "standard" || p.Timezone != "Asia/Shanghai" || p.ProjectID != f.project || p.ModelID != f.model || p.ProjectName == "" {
		t.Fatalf("unsafe defaults: %#v", p)
	}
	items, err := f.service.List(ctx, f.workspace, f.user)
	if err != nil || len(items) != 0 {
		t.Fatal("proposal mutated schedule", err)
	}
	other := uuid.New()
	if _, err = f.service.DB.Exec(ctx, `INSERT INTO users(id,email,password_hash,display_name) VALUES($1,'other@example.invalid','unused','Other')`, other); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.DB.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2)`, f.workspace, other); err != nil {
		t.Fatal(err)
	}
	answer := conversation.InteractionResponse{RequestDigest: item.RequestDigest, Decision: "approve"}
	if _, err = s.RespondRunInteraction(ctx, f.workspace, other, conv, item.ID, answer); !errors.Is(err, conversation.ErrInteractionNotFound) {
		t.Fatal("another member confirmed owner's schedule", err)
	}
	bad := answer
	bad.RequestDigest = "wrong"
	if _, err = s.RespondRunInteraction(ctx, f.workspace, f.user, conv, item.ID, bad); !errors.Is(err, conversation.ErrInteractionConflict) {
		t.Fatal(err)
	}
	// A failure after writing the definition must also roll back the definition,
	// not leave enabled work behind an unconfirmed interaction.
	if _, err = f.service.DB.Exec(ctx, `CREATE FUNCTION reject_schedule_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.type='INTERACTION_RESPONDED' THEN RAISE EXCEPTION 'fixture receipt failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_schedule_receipt BEFORE INSERT ON run_events FOR EACH ROW EXECUTE FUNCTION reject_schedule_receipt()`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RespondRunInteraction(ctx, f.workspace, f.user, conv, item.ID, answer); err == nil {
		t.Fatal("receipt failure was ignored")
	}
	items, err = f.service.List(ctx, f.workspace, f.user)
	if err != nil || len(items) != 0 {
		t.Fatal("failed receipt left an enabled schedule", err)
	}
	if _, err = f.service.DB.Exec(ctx, `DROP TRIGGER reject_schedule_receipt ON run_events; DROP FUNCTION reject_schedule_receipt()`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		confirmed, err := s.RespondRunInteraction(ctx, f.workspace, f.user, conv, item.ID, answer)
		if err != nil || confirmed.Status != "approved" {
			t.Fatal(confirmed, err)
		}
	}
	items, err = f.service.List(ctx, f.workspace, f.user)
	if err != nil || len(items) != 1 || items[0].ID != p.ID || items[0].Revision != 1 || !items[0].Enabled {
		t.Fatal("duplicated or missing confirmed schedule", items, err)
	}
	var mode string
	var approved *string
	if err = f.service.DB.QueryRow(ctx, `SELECT execution_mode,approved_plan_digest FROM runs WHERE id=$1`, run).Scan(&mode, &approved); err != nil || mode != "execute" || approved != nil {
		t.Fatal("schedule approval changed Plan authority", err)
	}
	// The response and configuration survive together even if the run later stops.
	if _, err = f.service.DB.Exec(ctx, `UPDATE runs SET status='cancelled' WHERE id=$1`, run); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RespondRunInteraction(ctx, f.workspace, f.user, conv, item.ID, answer); err != nil {
		t.Fatal("lost receipt could not be read idempotently", err)
	}
	if _, err = s.RespondRunInteraction(ctx, f.workspace, other, conv, item.ID, answer); !errors.Is(err, conversation.ErrInteractionNotFound) {
		t.Fatal("idempotency bypassed ownership", err)
	}
}

func TestConversationalScheduleChangesPauseResumeAndStaleReview(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	conv, run := f.scheduleRun(t)
	s := conversation.New(f.service.DB, nil, nil, agenttool.NewDefaultRegistry(f.service.DB))
	s.SetSchedules(f.service)
	created := f.scheduleCard(t, conv, run, scheduleCreate())
	respond := func(item *conversation.RunInteraction, decision string) error {
		_, err := s.RespondRunInteraction(ctx, f.workspace, f.user, conv, item.ID, conversation.InteractionResponse{RequestDigest: item.RequestDigest, Decision: decision})
		return err
	}
	if err := respond(created, "reject"); err != nil {
		t.Fatal(err)
	}
	items, _ := f.service.List(ctx, f.workspace, f.user)
	if len(items) != 0 {
		t.Fatal("rejection enabled schedule")
	}
	created = f.scheduleCard(t, conv, run, scheduleCreate())
	if err := respond(created, "approve"); err != nil {
		t.Fatal(err)
	}
	id := created.Request.SchedulePreview.ID
	stale := f.scheduleCard(t, conv, run, agenttool.ScheduleRequest{Action: "update", ID: id, Revision: 1, Cron: "30 10 * * 2"})
	d, _ := f.service.Get(ctx, f.workspace, f.user, id)
	d.Name = "Changed elsewhere"
	saved, _, err := f.service.Save(ctx, f.workspace, f.user, id, d.Config, d.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err = respond(stale, "approve"); !errors.Is(err, conversation.ErrInteractionConflict) {
		t.Fatal("stale configuration applied", err)
	}
	var status string
	if err = f.service.DB.QueryRow(ctx, `SELECT status FROM run_interactions WHERE id=$1`, stale.ID).Scan(&status); err != nil || status != "pending" {
		t.Fatal("failed confirmation left a false receipt", err)
	}
	if err = respond(stale, "reject"); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"update", "pause", "resume"} {
		r := agenttool.ScheduleRequest{Action: action, ID: id, Revision: saved.Revision}
		if action == "update" {
			r.Cron = "30 10 * * 2"
			r.Prompt = "Write the self-contained weekly review."
		}
		card := f.scheduleCard(t, conv, run, r)
		if err = respond(card, "approve"); err != nil {
			t.Fatal(action, err)
		}
		saved, err = f.service.Get(ctx, f.workspace, f.user, id)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Enabled != (action != "pause") || saved.ModelID != f.model || saved.Permission != "standard" || saved.Cron != "30 10 * * 2" {
			t.Fatal(action, saved)
		}
	}
}

func TestScheduleReviewExpiryRevocationAndTransactionRollback(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	conv, run := f.scheduleRun(t)
	s := conversation.New(f.service.DB, nil, nil, agenttool.NewDefaultRegistry(f.service.DB))
	s.SetSchedules(f.service)
	card := f.scheduleCard(t, conv, run, scheduleCreate())
	p := card.Request.SchedulePreview
	tx, _ := f.service.DB.Begin(ctx)
	if err := f.service.ApplySchedule(ctx, tx, conv, run, f.user, p); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback(ctx)
	items, _ := f.service.List(ctx, f.workspace, f.user)
	if len(items) != 0 {
		t.Fatal("rollback left an enabled task")
	}
	// Expire the reviewed occurrence without waiting for a real clock boundary.
	past := time.Now().Add(-time.Hour)
	p.NextRunAt = &past
	raw, _ := json.Marshal(card.Request)
	if _, err := f.service.DB.Exec(ctx, `UPDATE run_interactions SET request=$2 WHERE id=$1`, card.ID, raw); err != nil {
		t.Fatal(err)
	}
	answer := conversation.InteractionResponse{RequestDigest: card.RequestDigest, Decision: "approve"}
	if _, err := s.RespondRunInteraction(ctx, f.workspace, f.user, conv, card.ID, answer); !errors.Is(err, conversation.ErrInteractionConflict) {
		t.Fatal("expired occurrence silently replaced", err)
	}
	if _, err := f.service.DB.Exec(ctx, `UPDATE users SET disabled=true WHERE id=$1`, f.user); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ListSchedules(ctx, conv, run, uuid.Nil); err == nil {
		t.Fatal("disabled actor read schedules")
	}
	if _, err := s.RespondRunInteraction(ctx, f.workspace, f.user, conv, card.ID, answer); !errors.Is(err, conversation.ErrInteractionNotFound) {
		t.Fatal("disabled actor approved schedule", err)
	}
}

func TestScheduleContextRejectsUnknownZoneForeignProjectWebhookAndSchemaRollback(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	conv, run := f.scheduleRun(t)
	if _, err := f.service.DB.Exec(ctx, `UPDATE messages SET metadata='{}' WHERE run_id=$1`, run); err != nil {
		t.Fatal(err)
	}
	prepare := func(r agenttool.ScheduleRequest) error {
		tx, _ := f.service.DB.Begin(ctx)
		defer tx.Rollback(ctx)
		_, err := f.service.PrepareSchedule(ctx, tx, conv, run, r)
		return err
	}
	r := scheduleCreate()
	if err := prepare(r); err == nil {
		t.Fatal("unknown zone became UTC")
	}
	r.Timezone = "Asia/Shanghai"
	r.ProjectID = uuid.New()
	if err := prepare(r); err == nil {
		t.Fatal("foreign project accepted")
	}
	r.ProjectID = uuid.Nil
	r.Timezone = "Local"
	if err := prepare(r); err == nil {
		t.Fatal("server local zone accepted")
	}
	webhook, _ := f.save(t, "webhook")
	if err := prepare(agenttool.ScheduleRequest{Action: "pause", ID: webhook.ID, Revision: webhook.Revision}); err == nil {
		t.Fatal("schedule tool modified webhook")
	}
	r = scheduleCreate()
	r.Timezone = "America/New_York"
	card := f.scheduleCard(t, conv, run, r)
	if card.Request.SchedulePreview.Timezone != r.Timezone {
		t.Fatal("explicit zone lost")
	}
	query, err := f.service.ListSchedules(ctx, conv, run, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(query)
	var list []map[string]any
	if err = json.Unmarshal(mustJSON(t, query["schedules"]), &list); err != nil || len(list) != 0 {
		t.Fatal("webhook exposed in schedule list", string(encoded), err)
	}
	// A downgrade refuses to discard durable confirmations or imply undo.
	raw, err := os.ReadFile("../../migrations/000029_conversational_schedules.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, _ := f.service.DB.Begin(ctx)
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, string(raw)); err == nil {
		t.Fatal("rollback silently deleted confirmation history")
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
