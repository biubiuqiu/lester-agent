package integration

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type observedFixtureClient struct {
	generate func(context.Context, modelruntime.Request) (*modelruntime.Response, error)
	stream   func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error)
}

func (c observedFixtureClient) Generate(ctx context.Context, req modelruntime.Request) (*modelruntime.Response, error) {
	return c.generate(ctx, req)
}
func (c observedFixtureClient) Stream(ctx context.Context, req modelruntime.Request) (<-chan modelruntime.Event, error) {
	return c.stream(ctx, req)
}
func (c observedFixtureClient) Capabilities(context.Context, string) (modelruntime.Capabilities, error) {
	return modelruntime.Capabilities{Streaming: true}, nil
}

type modelTelemetryFixture struct {
	traces *sdktrace.TracerProvider
	spans  *tracetest.InMemoryExporter
	reader *metric.ManualReader
	meter  *metric.MeterProvider
}

func newModelTelemetryFixture(t *testing.T) modelTelemetryFixture {
	t.Helper()
	spans := tracetest.NewInMemoryExporter()
	traces := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	reader := metric.NewManualReader()
	meter := metric.NewMeterProvider(metric.WithReader(reader))
	t.Cleanup(func() { _ = traces.Shutdown(context.Background()); _ = meter.Shutdown(context.Background()) })
	return modelTelemetryFixture{traces, spans, reader, meter}
}
func (f modelTelemetryFixture) observe(c modelruntime.Client, provider string) *observedModelClient {
	return newObservedModelClient(c, provider, f.traces, f.meter)
}
func (f modelTelemetryFixture) metrics(t *testing.T) map[string]metricdata.Metrics {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := f.reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	result := map[string]metricdata.Metrics{}
	for _, scope := range data.ScopeMetrics {
		for _, item := range scope.Metrics {
			result[item.Name] = item
		}
	}
	return result
}
func modelSpanAttributes(span tracetest.SpanStub) map[string]attribute.Value {
	attrs := map[string]attribute.Value{}
	for _, attr := range span.Attributes {
		attrs[string(attr.Key)] = attr.Value
	}
	return attrs
}
func assertModelOutcome(t *testing.T, f modelTelemetryFixture, outcome, kind string) {
	t.Helper()
	spans := f.spans.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	attrs := modelSpanAttributes(spans[0])
	if attrs["lester.outcome"].AsString() != outcome || attrs["error.type"].AsString() != kind {
		t.Fatalf("wrong outcome: %+v", attrs)
	}
	if kind != "" && spans[0].Status.Code != codes.Error {
		t.Fatalf("failed operation status=%v", spans[0].Status)
	}
	requests, ok := f.metrics(t)["lester.model.requests"].Data.(metricdata.Sum[int64])
	if !ok || len(requests.DataPoints) != 1 || requests.DataPoints[0].Value != 1 {
		t.Fatalf("requests=%#v", requests)
	}
}
func assertModelTokens(t *testing.T, f modelTelemetryFixture, want map[string]int64) {
	t.Helper()
	item := f.metrics(t)["gen_ai.client.token.usage"]
	if item.Data == nil && len(want) == 0 {
		return
	}
	hist, ok := item.Data.(metricdata.Histogram[int64])
	if !ok || len(hist.DataPoints) != len(want) {
		t.Fatalf("token histogram=%#v, want=%v", hist, want)
	}
	for _, p := range hist.DataPoints {
		kind, _ := p.Attributes.Value(attribute.Key("gen_ai.token.type"))
		expected, exists := want[kind.AsString()]
		if !exists || p.Count != 1 || p.Sum != expected {
			t.Fatalf("sample=%+v, want=%v", p, want)
		}
	}
}

func TestObservedModelGenerateKeepsParentAndPrivateContentOutOfTelemetry(t *testing.T) {
	f := newModelTelemetryFixture(t)
	ctx, parent := f.traces.Tracer("fixture").Start(context.Background(), "request")
	defer parent.End()
	private := "private-prompt-credential-and-error"
	request := modelruntime.Request{Model: private, System: private, Messages: []modelruntime.Message{{Content: private}}}
	response := &modelruntime.Response{Content: private, NormalizedUsage: &modelruntime.NormalizedUsage{Source: modelruntime.UsageActual, InputTokens: 100, CacheReadInputTokens: 60, CachedIncluded: true, InputReported: true, OutputReported: true, OutputTokens: 12}}
	c := f.observe(observedFixtureClient{generate: func(ctx context.Context, got modelruntime.Request) (*modelruntime.Response, error) {
		if got.Model != private || got.System != private || got.Messages[0].Content != private {
			t.Fatal("request changed")
		}
		if trace.SpanContextFromContext(ctx).TraceID() != parent.SpanContext().TraceID() {
			t.Fatal("parent trace lost")
		}
		return response, nil
	}}, private)
	got, err := c.Generate(ctx, request)
	if err != nil || got != response {
		t.Fatalf("response changed: %p %v", got, err)
	}
	assertModelOutcome(t, f, "success", "")
	spans := f.spans.GetSpans()
	if spans[0].Parent.SpanID() != parent.SpanContext().SpanID() || spans[0].Name != "gen_ai.chat" {
		t.Fatal("wrong parent or name")
	}
	if modelSpanAttributes(spans[0])["gen_ai.provider.name"].AsString() != "other" {
		t.Fatal("custom provider name leaked")
	}
	encoded, err := json.Marshal(struct {
		Spans   any
		Metrics any
	}{spans, f.metrics(t)})
	if err != nil || strings.Contains(string(encoded), private) {
		t.Fatalf("private content leaked or JSON failed: %v", err)
	}
	assertModelTokens(t, f, map[string]int64{"input": 100, "output": 12})
}

func TestObservedModelStreamEndsAtClosureAndRecordsFinalUsageOnce(t *testing.T) {
	f := newModelTelemetryFixture(t)
	source := make(chan modelruntime.Event)
	c := f.observe(observedFixtureClient{stream: func(ctx context.Context, _ modelruntime.Request) (<-chan modelruntime.Event, error) {
		if !trace.SpanContextFromContext(ctx).IsValid() {
			t.Fatal("model span missing")
		}
		return source, nil
	}}, "anthropic")
	output, err := c.Stream(context.Background(), modelruntime.Request{})
	if err != nil {
		t.Fatal(err)
	}
	final := &modelruntime.NormalizedUsage{Source: modelruntime.UsageActual, InputTokens: 100, CacheReadInputTokens: 40, CacheCreationInputTokens: 10, InputReported: true, OutputTokens: 7, OutputReported: true}
	for _, event := range []modelruntime.Event{
		{Type: "MODEL_DELTA", Delta: "first", NormalizedUsage: &modelruntime.NormalizedUsage{Source: modelruntime.UsageActual, InputTokens: 10, InputReported: true}},
		{Type: "MODEL_DELTA", Delta: "second"},
		{Type: "MODEL_COMPLETED", NormalizedUsage: final},
	} {
		source <- event
		if got := <-output; got.Type != event.Type || got.Delta != event.Delta || got.NormalizedUsage != event.NormalizedUsage {
			t.Fatalf("event changed: %+v", got)
		}
		if len(f.spans.GetSpans()) != 0 {
			t.Fatal("span ended before original channel closed")
		}
	}
	final.InputTokens = 99999
	close(source)
	for range output {
		t.Fatal("unexpected extra event")
	}
	assertModelOutcome(t, f, "success", "")
	assertModelTokens(t, f, map[string]int64{"input": 150, "output": 7})
}

func TestObservedModelReadyProviderCannotStarveCancellation(t *testing.T) {
	f := newModelTelemetryFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := make(chan modelruntime.Event, 1024)
	for range 1024 {
		source <- modelruntime.Event{Type: "MODEL_DELTA", Delta: "private"}
	}
	c := f.observe(observedFixtureClient{stream: func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error) {
		cancel()
		return source, nil
	}}, "openai")
	output, err := c.Stream(ctx, modelruntime.Request{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for range output {
		count++
	}
	if count != 1 {
		t.Fatalf("cancelled ready stream forwarded %d events, want just current event", count)
	}
	assertModelOutcome(t, f, "cancelled", "cancelled")
}

func TestObservedModelKeepsReadyTerminalEventWhenProviderCancelsContext(t *testing.T) {
	for _, completed := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "completed"}[completed], func(t *testing.T) {
			f := newModelTelemetryFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			event := modelruntime.Event{Type: "RUN_FAILED", Err: context.Canceled}
			if completed {
				event = modelruntime.Event{Type: "MODEL_COMPLETED"}
			}
			c := f.observe(observedFixtureClient{stream: func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error) {
				source := make(chan modelruntime.Event, 1)
				source <- event
				close(source)
				cancel()
				return source, nil
			}}, "openai")
			output, err := c.Stream(ctx, modelruntime.Request{})
			if err != nil {
				t.Fatal(err)
			}
			got, open := <-output
			if !open || got.Type != event.Type || got.Err != event.Err {
				t.Fatalf("terminal event lost: %+v %v", got, open)
			}
			if _, open = <-output; open {
				t.Fatal("stream did not close")
			}
			if completed {
				assertModelOutcome(t, f, "success", "")
			} else {
				assertModelOutcome(t, f, "cancelled", "cancelled")
			}
		})
	}
}

func TestObservedModelCancellationUnblocksForwardingAndProvider(t *testing.T) {
	f := newModelTelemetryFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	providerStopped := make(chan struct{})
	c := f.observe(observedFixtureClient{stream: func(ctx context.Context, _ modelruntime.Request) (<-chan modelruntime.Event, error) {
		source := make(chan modelruntime.Event)
		go func() {
			defer close(source)
			defer close(providerStopped)
			for {
				select {
				case source <- modelruntime.Event{Type: "MODEL_DELTA", Delta: "private"}:
				case <-ctx.Done():
					return
				}
			}
		}()
		return source, nil
	}}, "openai")
	output, err := c.Stream(ctx, modelruntime.Request{})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-providerStopped:
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not observe cancellation")
	}
	done := make(chan struct{})
	go func() {
		for range output {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("forwarding goroutine leaked")
	}
	assertModelOutcome(t, f, "cancelled", "cancelled")
}

func TestObservedModelFailureShapesFinishOnceWithoutErrorText(t *testing.T) {
	private := &modelruntime.ProviderError{Kind: modelruntime.ErrorKind("private-kind"), Cause: errors.New("private-detail")}
	for _, test := range []struct {
		name, kind string
		stream     func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error)
	}{
		{"synchronous error", "provider", func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error) { return nil, private }},
		{"nil channel", "protocol", func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error) { return nil, nil }},
		{"empty channel", "protocol", func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error) {
			ch := make(chan modelruntime.Event)
			close(ch)
			return ch, nil
		}},
		{"event error", "rate_limit", func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error) {
			ch := make(chan modelruntime.Event, 1)
			ch <- modelruntime.Event{Err: &modelruntime.ProviderError{Kind: modelruntime.ErrorRateLimit, Cause: errors.New("private-detail")}}
			close(ch)
			return ch, nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newModelTelemetryFixture(t)
			output, err := f.observe(observedFixtureClient{stream: test.stream}, "openai").Stream(context.Background(), modelruntime.Request{})
			if test.name == "synchronous error" && err != private {
				t.Fatal("provider error changed")
			}
			if test.name == "nil channel" {
				var provider *modelruntime.ProviderError
				if !errors.As(err, &provider) || provider.Kind != modelruntime.ErrorProtocol {
					t.Fatalf("nil stream was not rejected: %v", err)
				}
			}
			if output != nil {
				for range output {
				}
			}
			assertModelOutcome(t, f, "error", test.kind)
			data, _ := json.Marshal(f.spans.GetSpans())
			if strings.Contains(string(data), "private-") {
				t.Fatal("provider detail exported")
			}
		})
	}
}

func TestObservedModelOnlyActualReportedUsageIsRecorded(t *testing.T) {
	for _, test := range []struct {
		name  string
		usage *modelruntime.NormalizedUsage
		want  map[string]int64
	}{
		{"missing", nil, nil},
		{"unknown", &modelruntime.NormalizedUsage{Source: modelruntime.UsageMissing}, nil},
		{"estimated", &modelruntime.NormalizedUsage{Source: modelruntime.UsageEstimated, InputTokens: 123, InputReported: true}, nil},
		{"unreported", &modelruntime.NormalizedUsage{Source: modelruntime.UsageActual, InputTokens: 123, OutputTokens: 5}, nil},
		{"explicit zero", &modelruntime.NormalizedUsage{Source: modelruntime.UsageActual, InputReported: true, OutputReported: true}, map[string]int64{"input": 0, "output": 0}},
		{"overflow", &modelruntime.NormalizedUsage{Source: modelruntime.UsageActual, InputReported: true, InputTokens: math.MaxInt, CacheReadInputTokens: 1, OutputReported: true, OutputTokens: 5}, map[string]int64{"output": 5}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newModelTelemetryFixture(t)
			c := f.observe(observedFixtureClient{generate: func(context.Context, modelruntime.Request) (*modelruntime.Response, error) {
				return &modelruntime.Response{NormalizedUsage: test.usage}, nil
			}}, "openai")
			if _, err := c.Generate(context.Background(), modelruntime.Request{}); err != nil {
				t.Fatal(err)
			}
			assertModelTokens(t, f, test.want)
		})
	}
}

func TestObservedModelGenerateFailuresAndPanicKeepOriginalContract(t *testing.T) {
	for _, test := range []struct {
		name, kind string
		err        error
	}{
		{"nil response", "protocol", nil},
		{"deadline", "timeout", context.DeadlineExceeded},
		{"cancelled", "cancelled", context.Canceled},
		{"unsafe provider kind", "provider", &modelruntime.ProviderError{Kind: "private-kind", Cause: errors.New("private-detail")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newModelTelemetryFixture(t)
			c := f.observe(observedFixtureClient{generate: func(context.Context, modelruntime.Request) (*modelruntime.Response, error) { return nil, test.err }}, "openai")
			response, err := c.Generate(context.Background(), modelruntime.Request{})
			if test.name == "nil response" {
				var provider *modelruntime.ProviderError
				if !errors.As(err, &provider) || provider.Kind != modelruntime.ErrorProtocol || response != nil {
					t.Fatalf("nil response was not rejected: %v", err)
				}
			} else if response != nil || err != test.err {
				t.Fatal("instrumentation changed valid original return values")
			}
			outcome := "error"
			if test.kind == "cancelled" {
				outcome = "cancelled"
			}
			assertModelOutcome(t, f, outcome, test.kind)
		})
	}
	t.Run("panic", func(t *testing.T) {
		f := newModelTelemetryFixture(t)
		private := errors.New("private-panic-detail")
		c := f.observe(observedFixtureClient{generate: func(context.Context, modelruntime.Request) (*modelruntime.Response, error) { panic(private) }}, "openai")
		func() {
			defer func() {
				if got := recover(); got != private {
					t.Fatalf("panic replaced: %v", got)
				}
			}()
			_, _ = c.Generate(context.Background(), modelruntime.Request{})
		}()
		assertModelOutcome(t, f, "error", "provider")
		data, _ := json.Marshal(f.spans.GetSpans())
		if strings.Contains(string(data), "private-") {
			t.Fatal("panic value exported")
		}
	})
}

func TestRegistryAddsModelObservationWithoutExposingSpecification(t *testing.T) {
	client, err := NewDefaultRegistry().NewClient(ClientSpec{Provider: "openai_compatible", Endpoint: "https://fixture.invalid/v1", Credential: []byte("private-key"), ModelID: "private-model"})
	if err != nil {
		t.Fatal(err)
	}
	observed, ok := client.(*observedModelClient)
	if !ok || observed.provider != "openai_compatible" {
		t.Fatalf("registry returned %T", client)
	}
}
