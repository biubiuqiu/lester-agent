package conversation

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type scheduleStub struct{ calls int }

func (s *scheduleStub) ListSchedules(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (map[string]any, error) {
	return nil, nil
}
func (s *scheduleStub) PrepareSchedule(_ context.Context, _ pgx.Tx, _, _ uuid.UUID, r agenttool.ScheduleRequest) (*agenttool.SchedulePreview, error) {
	s.calls++
	next := time.Now().Add(time.Hour)
	return &agenttool.SchedulePreview{ID: uuid.New(), Name: r.Name, Prompt: r.Prompt, Timezone: r.Timezone, Cron: r.Cron, NextRunAt: &next}, nil
}
func (s *scheduleStub) ApplySchedule(context.Context, pgx.Tx, uuid.UUID, uuid.UUID, uuid.UUID, *agenttool.SchedulePreview) error {
	panic("unexpected apply during proposal")
}

func TestScheduleProposalBindsDurableArgumentsAndTrustedPreview(t *testing.T) {
	f := newTranscriptFixture(t, false)
	applyTestMigration(t, f.service.db, "000029_conversational_schedules.up.sql")
	stub := &scheduleStub{}
	f.service.SetSchedules(stub)
	ctx := context.Background()
	run := f.startRun(t, "schedule review")
	input := agenttool.ScheduleRequest{Action: "create", Name: "Weekly review", Prompt: "Write an independent weekly review.", Cron: "0 9 * * 1", Timezone: "Asia/Shanghai"}
	args, _ := json.Marshal(input)
	call := uuid.NewString()
	if err := f.service.appendMessage(ctx, f.conversationID, run, model.Message{Role: "assistant", ToolCalls: []model.ToolCall{{ID: call, Name: "propose_schedule", Arguments: args}}}, "", nil); err != nil {
		t.Fatal(err)
	}
	request := agenttool.InteractionRequest{Kind: "automation", Schedule: &input}
	if _, err := f.service.db.Exec(ctx, `UPDATE runs SET execution_mode='plan' WHERE id=$1`, run); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.createRunInteraction(ctx, f.conversationID, run, call, request); err == nil || stub.calls != 0 {
		t.Fatal("Plan created schedule review", err)
	}
	if _, err := f.service.db.Exec(ctx, `UPDATE runs SET execution_mode='execute' WHERE id=$1`, run); err != nil {
		t.Fatal(err)
	}
	altered := input
	altered.Prompt = "Changed behind the durable call"
	if _, err := f.service.createRunInteraction(ctx, f.conversationID, run, call, agenttool.InteractionRequest{Kind: "automation", Schedule: &altered}); err == nil {
		t.Fatal("unbound prompt accepted")
	}
	item, err := f.service.createRunInteraction(ctx, f.conversationID, run, call, request)
	if err != nil || item.Request.SchedulePreview == nil {
		t.Fatal(item, err)
	}
	again, err := f.service.createRunInteraction(ctx, f.conversationID, run, call, request)
	if err != nil || again.ID != item.ID || again.RequestDigest != item.RequestDigest || stub.calls != 1 {
		t.Fatal("re-prepared a durable proposal", again, err)
	}
	feed, err := f.service.CloudTasks(ctx, f.workspaceID, "attention", "", 20)
	if err != nil || feed.Attention != 1 || len(feed.Tasks) != 1 || feed.Tasks[0].State != "waiting_schedule" {
		t.Fatal("schedule absent from task attention", feed, err)
	}
	request.SchedulePreview = &agenttool.SchedulePreview{Permission: "full"}
	if _, err = f.service.createRunInteraction(ctx, f.conversationID, run, call, request); err == nil {
		t.Fatal("model provided platform preview")
	}
	if !agenttool.PlanReadOnlyTool("list_schedules") || agenttool.PlanReadOnlyTool("propose_schedule") {
		t.Fatal("schedule tools cross Plan boundary")
	}
	for _, mode := range []string{"standard", "auto", "full"} {
		if needed, _ := needsPermission(mode, "propose_schedule", args); needed {
			t.Fatal("duplicate approval before schedule review", mode)
		}
	}
}

func TestBrowserTimezoneIsOptionalSnapshotData(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := withClientTimezone(context.Background(), "America/New_York")
	tx, err := f.service.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	_, raw, err := prepareRunInput(ctx, tx, f.workspaceID, f.userID, f.conversationID, "tomorrow at 9", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err = json.Unmarshal(raw, &metadata); err != nil || metadata["client_timezone"] != "America/New_York" {
		t.Fatal("time zone not snapshotted", err)
	}
	for _, zone := range []string{"Local", "invalid/zone", ""} {
		if withClientTimezone(context.Background(), zone).Value(clientTimezoneKey{}) != nil {
			t.Fatal("invalid hint retained", zone)
		}
	}
}
