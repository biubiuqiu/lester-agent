package conversation

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const executionTelemetryScope = "lester/conversation"

type runObservationKey struct{}
type runObservation struct {
	mu      sync.Mutex
	once    sync.Once
	runID   uuid.UUID
	outcome string
	started time.Time
}

// A task survives its submitting HTTP request. Carry only its trace identity:
// deadlines, cancellation, authentication values and baggage stay at the request.
func detachedExecutionContext(parent context.Context) context.Context {
	identity := trace.SpanContextFromContext(parent).WithTraceState(trace.TraceState{})
	return trace.ContextWithSpanContext(context.Background(), identity)
}

func observeRun(ctx context.Context, conversationID, runID uuid.UUID) (context.Context, func()) {
	ctx, span := otel.Tracer(executionTelemetryScope).Start(ctx, "lester.agent.run", trace.WithAttributes(
		attribute.String("lester.run.id", runID.String()),
		attribute.String("lester.conversation.id", conversationID.String()),
	))
	observation := &runObservation{runID: runID, outcome: "unknown", started: time.Now()}
	ctx = context.WithValue(ctx, runObservationKey{}, observation)
	return ctx, func() {
		observation.once.Do(func() {
			observation.mu.Lock()
			outcome := observation.outcome
			observation.mu.Unlock()
			setExecutionSpanOutcome(span, outcome, "agent execution")
			recordExecutionMetric(ctx, "run", "", outcome, time.Since(observation.started))
			span.End()
		})
	}
}

// Outcomes come from confirmed durable transitions, not a nil function return.
// A recovered panic remains a panic even when its failed-run receipt is saved.
func setRunOutcome(ctx context.Context, runID uuid.UUID, outcome string) {
	observation, ok := ctx.Value(runObservationKey{}).(*runObservation)
	if !ok || observation.runID != runID {
		return
	}
	switch outcome {
	case "completed":
		outcome = "success"
	case "success", "failed", "cancelled", "panic", "unknown":
	default:
		return
	}
	observation.mu.Lock()
	defer observation.mu.Unlock()
	if observation.outcome != "panic" {
		observation.outcome = outcome
	}
}

func setExecutionSpanOutcome(span trace.Span, outcome, operation string) {
	span.SetAttributes(attribute.String("lester.outcome", outcome))
	switch outcome {
	case "success":
		span.SetStatus(codes.Ok, "")
	case "failed":
		span.SetStatus(codes.Error, operation+" failed")
	case "panic":
		span.SetStatus(codes.Error, operation+" panicked")
	}
}

func recordExecutionMetric(ctx context.Context, operation, name, outcome string, elapsed time.Duration) {
	attributes := []attribute.KeyValue{attribute.String("lester.outcome", outcome)}
	description := "Duration of an Agent run, including model, tool and interaction waits."
	if operation == "tool" {
		attributes = append(attributes, attribute.String("tool.name", name))
		description = "Duration of an Agent tool invocation, including permission and user-interaction waits."
	}
	meter := otel.Meter(executionTelemetryScope)
	if duration, err := meter.Float64Histogram("lester.agent."+operation+".duration", metric.WithUnit("s"), metric.WithDescription(description), metric.WithExplicitBucketBoundaries(.01, .05, .1, .5, 1, 5, 10, 30, 60, 300, 900, 3600, 14400)); err == nil {
		duration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(attributes...))
	}
	if count, err := meter.Int64Counter("lester.agent."+operation+".count", metric.WithUnit("{invocation}")); err == nil {
		count.Add(ctx, 1, metric.WithAttributes(attributes...))
	}
}

func observeTool(ctx context.Context, name string, runID, conversationID uuid.UUID, invoke func(context.Context) (any, error)) (result any, err error) {
	ctx, span := otel.Tracer(executionTelemetryScope).Start(ctx, "lester.agent.tool", trace.WithAttributes(
		attribute.String("tool.name", name),
		attribute.String("lester.run.id", runID.String()),
		attribute.String("lester.conversation.id", conversationID.String()),
	))
	started := time.Now()
	defer func() {
		outcome := "success"
		if recovered := recover(); recovered != nil {
			setExecutionSpanOutcome(span, "panic", "tool invocation")
			recordExecutionMetric(ctx, "tool", name, "panic", time.Since(started))
			span.End()
			panic(recovered)
		}
		switch {
		case ctx.Err() != nil || errors.Is(err, context.Canceled):
			outcome = "cancelled"
		case err != nil || commandResultFailed(name, result):
			outcome = "failed"
		}
		if value, ok := result.(map[string]any); ok && err == nil && outcome == "failed" {
			if status, _ := value["status"].(string); status == "unknown" && commandResultFailed(name, result) {
				outcome = "unknown"
			}
		}
		setExecutionSpanOutcome(span, outcome, "tool invocation")
		recordExecutionMetric(ctx, "tool", name, outcome, time.Since(started))
		span.End()
	}()
	return invoke(ctx)
}
