package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// NewLogHandler keeps logs on their original destination and attaches correlation
// IDs only when the call includes an active trace context. It exports no log body.
func NewLogHandler(next slog.Handler) slog.Handler { return &logHandler{next: next} }

type logHandler struct{ next slog.Handler }

func (h *logHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}
func (h *logHandler) Handle(ctx context.Context, record slog.Record) error {
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		record = record.Clone()
		record.AddAttrs(slog.String("trace_id", span.TraceID().String()), slog.String("span_id", span.SpanID().String()))
	}
	return h.next.Handle(ctx, record)
}
func (h *logHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	return &logHandler{next: h.next.WithAttrs(attributes)}
}
func (h *logHandler) WithGroup(name string) slog.Handler {
	return &logHandler{next: h.next.WithGroup(name)}
}
