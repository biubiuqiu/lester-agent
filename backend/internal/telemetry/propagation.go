package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type traceparentOnly struct{}

// TraceparentPropagator shares trace identity, never baggage, vendor state or credentials.
func TraceparentPropagator() propagation.TextMapPropagator { return traceparentOnly{} }

func (traceparentOnly) Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	span := trace.SpanContextFromContext(ctx).WithTraceState(trace.TraceState{})
	propagation.TraceContext{}.Inject(trace.ContextWithSpanContext(ctx, span), carrier)
}

func (traceparentOnly) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	return propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{"traceparent": carrier.Get("traceparent")})
}

func (traceparentOnly) Fields() []string { return []string{"traceparent"} }
