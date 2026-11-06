package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/telemetry"
	"go.opentelemetry.io/otel/trace"
)

func TestLogCorrelationSurvivesAttrsGroupsAndMissingTrace(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(telemetry.NewLogHandler(slog.NewJSONHandler(&output, nil))).With("service", "fixture")
	traceID, _ := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
	spanID, _ := trace.SpanIDFromHex("0123456789abcdef")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled}))
	logger.InfoContext(ctx, "request", "route", "/runs/{id}")
	logger.WithGroup("operation").With("kind", "fixture").InfoContext(ctx, "nested", "outcome", "completed")
	logger.InfoContext(context.Background(), "outside")
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d entries", len(lines))
	}
	values := make([]map[string]any, 3)
	for i, line := range lines {
		if err := json.Unmarshal([]byte(line), &values[i]); err != nil {
			t.Fatal(err)
		}
	}
	if values[0]["trace_id"] != traceID.String() || values[0]["span_id"] != spanID.String() {
		t.Fatal("root correlation missing")
	}
	operation, ok := values[1]["operation"].(map[string]any)
	if !ok || operation["trace_id"] != traceID.String() || operation["span_id"] != spanID.String() || operation["kind"] != "fixture" || values[1]["service"] != "fixture" {
		t.Fatalf("group or bound attrs lost: %#v", values[1])
	}
	if _, ok = values[2]["trace_id"]; ok {
		t.Fatal("trace leaked into unrelated log")
	}
}
