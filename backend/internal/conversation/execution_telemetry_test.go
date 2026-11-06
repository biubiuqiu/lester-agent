package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func executionTelemetryTestProviders(t *testing.T) (*tracetest.SpanRecorder, *sdkmetric.ManualReader) {
	t.Helper()
	previousTraces, previousMetrics := otel.GetTracerProvider(), otel.GetMeterProvider()
	recorder := tracetest.NewSpanRecorder()
	traces := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	reader := sdkmetric.NewManualReader()
	meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetTracerProvider(traces)
	otel.SetMeterProvider(meters)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousTraces)
		otel.SetMeterProvider(previousMetrics)
		_ = traces.Shutdown(context.Background())
		_ = meters.Shutdown(context.Background())
	})
	return recorder, reader
}

func spanAttribute(span sdktrace.ReadOnlySpan, key string) string {
	for _, item := range span.Attributes() {
		if string(item.Key) == key {
			return item.Value.AsString()
		}
	}
	return ""
}

func findExecutionSpan(t *testing.T, recorder *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	var found sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if span.Name() == name {
			if found != nil {
				t.Fatalf("more than one ended %s span", name)
			}
			found = span
		}
	}
	if found == nil {
		t.Fatalf("missing ended %s span", name)
	}
	return found
}

func assertExecutionTelemetryPrivate(t *testing.T, recorder *tracetest.SpanRecorder, reader *sdkmetric.ManualReader, forbidden ...string) {
	t.Helper()
	var spans strings.Builder
	for _, span := range recorder.Ended() {
		fmt.Fprintln(&spans, span.Name(), span.Attributes(), span.Events(), span.Status())
	}
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	metrics, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range forbidden {
		if strings.Contains(spans.String(), secret) || strings.Contains(string(metrics), secret) {
			t.Fatalf("private data appeared in telemetry: %q", secret)
		}
	}
}

func TestRunTelemetryKeepsTraceParentButDetachesRequestAndEndsAtDurableOutcome(t *testing.T) {
	for _, test := range []struct{ outcome, status string }{{"success", "completed"}, {"failed", "failed"}, {"cancelled", "cancelled"}, {"panic", "failed"}, {"unknown", "running"}} {
		t.Run(test.outcome, func(t *testing.T) {
			recorder, reader := executionTelemetryTestProviders(t)
			f := newTranscriptFixture(t, false)
			run := f.startRun(t, "PRIVATE-PROMPT-NEVER-EXPORT")
			guard, err := f.service.acquireRun(context.Background(), f.workspaceID, f.conversationID)
			if err != nil {
				t.Fatal(err)
			}
			request, requestSpan := otel.Tracer("test").Start(context.Background(), "submitting.http")
			defer requestSpan.End()
			member, _ := baggage.NewMember("private", "PRIVATE-BAGGAGE-NEVER-EXPORT")
			bag, _ := baggage.New(member)
			request = baggage.ContextWithBaggage(request, bag)
			state, _ := trace.ParseTraceState("vendor=PRIVATE-TRACESTATE")
			request = trace.ContextWithSpanContext(request, trace.SpanContextFromContext(request).WithTraceState(state))
			type privateRequestKey struct{}
			request = context.WithValue(request, privateRequestKey{}, "PRIVATE-AUTH-VALUE")
			request, cancelRequest := context.WithCancel(request)
			defer cancelRequest()
			started, proceed, checked := make(chan struct{}), make(chan struct{}), make(chan struct{})
			if !f.service.executor.launch(guard, f.conversationID, run, func(ctx context.Context) {
				close(started)
				<-proceed
				if ctx.Err() != nil || ctx.Value(privateRequestKey{}) != nil || baggage.FromContext(ctx).Len() != 0 || trace.SpanContextFromContext(ctx).TraceState().Len() != 0 {
					t.Error("task inherited request cancellation or private context")
				}
				close(checked)
				switch test.outcome {
				case "success":
					if err := f.service.finishCompletedRun(ctx, run, f.conversationID); err != nil {
						t.Error(err)
					}
				case "failed":
					f.service.finishExecutionError(ctx, run, f.conversationID, errors.New("PRIVATE-ERROR-NEVER-EXPORT"))
				case "cancelled":
					<-ctx.Done()
					f.service.finishContext(ctx, run, f.conversationID)
				case "panic":
					panic("PRIVATE-PANIC-NEVER-EXPORT")
				}
			}, request) {
				guard.Close()
				t.Fatal("launch rejected")
			}
			<-started
			active, _ := f.service.executor.active.Load(run)
			done := active.(*activeExecution).done
			cancelRequest()
			close(proceed)
			<-checked
			if test.outcome == "cancelled" {
				if err := f.service.CancelRun(context.Background(), f.workspaceID, f.conversationID, run); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("task did not finish")
			}
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := f.service.Shutdown(shutdown); err != nil {
				t.Fatal(err)
			}
			var status string
			if err := f.service.db.QueryRow(context.Background(), `SELECT status FROM runs WHERE id=$1`, run).Scan(&status); err != nil || status != test.status {
				t.Fatalf("durable status=%s error=%v", status, err)
			}
			span := findExecutionSpan(t, recorder, "lester.agent.run")
			if span.Parent().SpanID() != requestSpan.SpanContext().SpanID() || span.SpanContext().TraceID() != requestSpan.SpanContext().TraceID() || spanAttribute(span, "lester.outcome") != test.outcome {
				t.Fatalf("incorrect parent/outcome: %v %s", span.Parent(), spanAttribute(span, "lester.outcome"))
			}
			if (test.outcome == "failed" || test.outcome == "panic") && span.Status().Code != codes.Error {
				t.Fatal("failed run did not report a bounded error status")
			}
			assertExecutionTelemetryPrivate(t, recorder, reader, "PRIVATE-PROMPT", "PRIVATE-ERROR", "PRIVATE-PANIC", "PRIVATE-BAGGAGE", "PRIVATE-AUTH", "PRIVATE-TRACESTATE")
			var metrics metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &metrics); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(metrics)
			if strings.Contains(string(raw), run.String()) || strings.Contains(string(raw), f.conversationID.String()) || !strings.Contains(string(raw), "lester.agent.run.duration") {
				t.Fatal("run metrics missing or contained entity labels")
			}
		})
	}
}

type telemetryCommandProbe struct{}

func (telemetryCommandProbe) Definition() model.Tool { return model.Tool{Name: "bash"} }
func (telemetryCommandProbe) Execute(context.Context, agenttool.Environment, json.RawMessage) (any, error) {
	return map[string]any{"exit_code": 42, "stdout": "PRIVATE-STDOUT-NEVER-EXPORT"}, nil
}

func TestToolTelemetryUsesCanonicalNamesAndRecognizesCommandFailureWithoutCapturingContent(t *testing.T) {
	for _, name := range []string{"computer_exec", "PRIVATE-UNKNOWN-TOOL"} {
		t.Run(name, func(t *testing.T) {
			recorder, reader := executionTelemetryTestProviders(t)
			f := newTranscriptFixture(t, false)
			run := f.startRun(t, "PRIVATE-USER-CONTENT")
			f.service.tools = agenttool.NewRegistry()
			f.service.tools.Register(telemetryCommandProbe{}, "computer_exec")
			f.service.sandboxes = sandbox.NewClient("http://unused.invalid", "PRIVATE-SANDBOX-TOKEN")
			ctx, parent := otel.Tracer("test").Start(context.Background(), "parent.run")
			defer parent.End()
			result, err := f.service.executor.runTool(ctx, run, f.conversationID, &Computer{SandboxID: "PRIVATE-SANDBOX", WorkDir: "/PRIVATE-WORKDIR"}, model.ToolCall{ID: "PRIVATE-CALL-ID", Name: name, Arguments: json.RawMessage(`{"command":"PRIVATE-COMMAND","file_path":"/PRIVATE-PATH"}`)})
			canonical := "bash"
			if name == "computer_exec" {
				if err != nil || !commandResultFailed("bash", result) {
					t.Fatalf("probe did not execute: %v %v", result, err)
				}
			} else {
				canonical = "unknown"
				if err == nil {
					t.Fatal("unknown tool unexpectedly succeeded")
				}
			}
			span := findExecutionSpan(t, recorder, "lester.agent.tool")
			if spanAttribute(span, "tool.name") != canonical || spanAttribute(span, "lester.outcome") != "failed" || span.Status().Code != codes.Error || span.Parent().SpanID() != parent.SpanContext().SpanID() {
				t.Fatalf("incorrect tool trace: %v", span.Attributes())
			}
			assertExecutionTelemetryPrivate(t, recorder, reader, "PRIVATE-USER", "PRIVATE-STDOUT", "PRIVATE-COMMAND", "PRIVATE-PATH", "PRIVATE-WORKDIR", "PRIVATE-SANDBOX", "PRIVATE-CALL", "PRIVATE-UNKNOWN")
			var data metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &data); err != nil {
				t.Fatal(err)
			}
			for _, scope := range data.ScopeMetrics {
				for _, metric := range scope.Metrics {
					if strings.HasPrefix(metric.Name, "lester.agent.tool.") {
						raw, _ := json.Marshal(metric)
						if strings.Contains(string(raw), run.String()) || strings.Contains(string(raw), f.conversationID.String()) {
							t.Fatal("tool metric contained entity IDs")
						}
					}
				}
			}
		})
	}
}
