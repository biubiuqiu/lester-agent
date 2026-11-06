package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestAutomationTelemetryTracksFiniteDispatchAndKeepsUnknownReceiptHonest(t *testing.T) {
	previous := otel.GetTracerProvider()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	f := newFixture(t)
	ctx, parent := provider.Tracer("test").Start(context.Background(), "scheduler.work")
	defer parent.End()
	config := f.config("webhook")
	config.Name, config.Prompt = "PRIVATE-TASK-NAME", "PRIVATE-TASK-PROMPT"
	definition, token, err := f.service.Save(ctx, f.workspace, f.user, uuid.New(), config, 0)
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := f.service.Webhook(ctx, definition.ID, token, "PRIVATE-EXTERNAL-EVENT", json.RawMessage(`{"data":"PRIVATE-WEBHOOK-PAYLOAD"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.dispatch(ctx, delivery.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := f.service.Delivery(ctx, f.workspace, f.user, definition.ID, delivery.ID)
	if err != nil || stored.State != "accepted" || stored.RunID == nil {
		t.Fatalf("dispatch receipt=%#v error=%v", stored, err)
	}
	orphan, err := f.service.Trigger(ctx, f.workspace, f.user, definition.ID, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.DB.Exec(ctx, `UPDATE automation_deliveries SET state='dispatching' WHERE id=$1`, orphan.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.service.dispatch(ctx, orphan.ID); err != nil {
		t.Fatal(err)
	}
	unknown, err := f.service.Delivery(ctx, f.workspace, f.user, definition.ID, orphan.ID)
	if err != nil || unknown.State != "unknown" || f.runner.calls.Load() != 1 {
		t.Fatalf("unknown receipt replayed: %#v error=%v", unknown, err)
	}
	if err = f.service.enqueueDue(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(recorder.Ended()) != 2 {
		t.Fatalf("empty scans or non-dispatch work produced spans: %d", len(recorder.Ended()))
	}
	for _, span := range recorder.Ended() {
		attrs := map[string]string{}
		for _, item := range span.Attributes() {
			attrs[string(item.Key)] = item.Value.AsString()
		}
		if span.Name() != "lester.automation.dispatch" || span.Parent().SpanID() != parent.SpanContext().SpanID() {
			t.Fatal("dispatch span missing or incorrectly parented")
		}
		switch attrs["lester.automation.delivery.id"] {
		case delivery.ID.String():
			if attrs["lester.outcome"] != "accepted" || attrs["lester.automation.trigger"] != "webhook" || span.Status().Code != codes.Ok {
				t.Fatalf("accepted dispatch telemetry=%v", attrs)
			}
		case orphan.ID.String():
			if attrs["lester.outcome"] != "unknown" || attrs["lester.automation.operation"] != "reconcile" || span.Status().Code != codes.Unset {
				t.Fatalf("uncertain dispatch overstated success: %v", attrs)
			}
		default:
			t.Fatal("unexpected delivery span")
		}
		observed := fmt.Sprint(span.Name(), span.Attributes(), span.Events(), span.Status())
		for _, private := range []string{token, "PRIVATE-TASK", "PRIVATE-EXTERNAL", "PRIVATE-WEBHOOK"} {
			if strings.Contains(observed, private) {
				t.Fatal("dispatch telemetry contained private definition or webhook data")
			}
		}
	}
}
