package telemetry_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	collectormetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

type observedExport struct {
	method, path, authorization, contentType string
}

type exportCapture struct {
	mu       sync.Mutex
	requests []observedExport
	traces   []*collectortrace.ExportTraceServiceRequest
	metrics  []*collectormetrics.ExportMetricsServiceRequest
}

// These tests use actual OTLP wire messages and local receivers, with no
// Collector, model, database, or external network service required.
func (c *exportCapture) httpReceiver(t *testing.T, tracePath, metricPath string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil {
			t.Errorf("read OTLP request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.requests = append(c.requests, observedExport{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type")})
		w.Header().Set("Content-Type", "application/x-protobuf")
		switch r.URL.Path {
		case tracePath:
			request := new(collectortrace.ExportTraceServiceRequest)
			if err := proto.Unmarshal(body, request); err != nil {
				t.Errorf("decode OTLP trace protobuf: %v", err)
			}
			c.traces = append(c.traces, request)
		case metricPath:
			request := new(collectormetrics.ExportMetricsServiceRequest)
			if err := proto.Unmarshal(body, request); err != nil {
				t.Errorf("decode OTLP metric protobuf: %v", err)
			}
			c.metrics = append(c.metrics, request)
		default:
			// Respond successfully to avoid retry delays; the test below reports
			// the routing failure using the observed path.
			t.Errorf("unexpected OTLP request path %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func isolatedTelemetry(t *testing.T) {
	t.Helper()
	previousTraces := otel.GetTracerProvider()
	previousMetrics := otel.GetMeterProvider()
	previousPropagation := otel.GetTextMapPropagator()
	previousErrors := otel.GetErrorHandler()
	t.Cleanup(func() {
		otel.SetTracerProvider(previousTraces)
		otel.SetMeterProvider(previousMetrics)
		otel.SetTextMapPropagator(previousPropagation)
		otel.SetErrorHandler(previousErrors)
	})
	for _, binding := range os.Environ() {
		key, _, _ := strings.Cut(binding, "=")
		if strings.HasPrefix(key, "OTEL_") {
			t.Setenv(key, "")
		}
	}
	// Fresh instruments after Init exercise the providers installed by this
	// Runtime rather than a provider installed by another test.
	otel.SetTracerProvider(trace.NewNoopTracerProvider())
	otel.SetMeterProvider(noop.NewMeterProvider())
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "3600000")
	t.Setenv("OTEL_BSP_SCHEDULE_DELAY", "3600000")
	t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "1000")
}

func startRuntime(t *testing.T, service string) *telemetry.Runtime {
	t.Helper()
	runtime, err := telemetry.Init(context.Background(), service)
	if err != nil {
		t.Fatalf("initialize telemetry: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := runtime.Shutdown(ctx); err != nil {
			t.Errorf("shutdown telemetry: %v", err)
		}
	})
	return runtime
}

func emitOperation(t *testing.T) (trace.SpanContext, trace.SpanContext) {
	t.Helper()
	ctx, parent := otel.Tracer("lester/export-integration").Start(context.Background(), "fixture.request")
	_, child := otel.Tracer("lester/export-integration").Start(ctx, "fixture.agent")
	parentContext, childContext := parent.SpanContext(), child.SpanContext()
	child.End()
	parent.End()
	counter, err := otel.Meter("lester/export-integration").Int64Counter("fixture.completed")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(context.Background(), 7)
	return parentContext, childContext
}

func flushRuntime(t *testing.T, runtime *telemetry.Runtime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := runtime.ForceFlush(ctx); err != nil {
		t.Fatalf("flush telemetry: %v", err)
	}
}

func stringAttribute(attributes []*commonpb.KeyValue, key string) string {
	for _, item := range attributes {
		if item.Key == key {
			return item.Value.GetStringValue()
		}
	}
	return ""
}

func (c *exportCapture) checkData(t *testing.T, service, environment string, parent, child trace.SpanContext) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	spans := map[string]*tracepb.Span{}
	for _, batch := range c.traces {
		for _, resource := range batch.ResourceSpans {
			if got := stringAttribute(resource.Resource.Attributes, "service.name"); got != service {
				t.Errorf("trace service.name = %q, want %q", got, service)
			}
			if environment != "" && stringAttribute(resource.Resource.Attributes, "deployment.environment.name") != environment {
				t.Error("trace is missing configured resource attributes")
			}
			for _, scope := range resource.ScopeSpans {
				for _, span := range scope.Spans {
					spans[span.Name] = span
				}
			}
		}
	}
	if len(spans) != 2 || spans["fixture.request"] == nil || spans["fixture.agent"] == nil {
		t.Fatalf("exported spans = %v, want the request and agent operation", spans)
	}
	parentID, childID := parent.SpanID(), child.SpanID()
	traceID := parent.TraceID()
	if !bytes.Equal(spans["fixture.agent"].ParentSpanId, parentID[:]) || !bytes.Equal(spans["fixture.agent"].SpanId, childID[:]) || !bytes.Equal(spans["fixture.agent"].TraceId, traceID[:]) {
		t.Error("OTLP export did not retain the parent/child trace relationship")
	}
	if !bytes.Equal(spans["fixture.request"].TraceId, traceID[:]) || len(spans["fixture.request"].ParentSpanId) != 0 {
		t.Error("OTLP request span is not the expected root")
	}
	c.checkMetricLocked(t, service)
}

func (c *exportCapture) checkMetricLocked(t *testing.T, service string) {
	t.Helper()
	found := false
	for _, batch := range c.metrics {
		for _, resource := range batch.ResourceMetrics {
			if got := stringAttribute(resource.Resource.Attributes, "service.name"); got != service {
				t.Errorf("metric service.name = %q, want %q", got, service)
			}
			for _, scope := range resource.ScopeMetrics {
				for _, metric := range scope.Metrics {
					if metric.Name != "fixture.completed" {
						continue
					}
					found = true
					points := metric.GetSum().GetDataPoints()
					if len(points) != 1 || points[0].GetAsInt() != 7 || metric.GetSum().AggregationTemporality != metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE {
						t.Errorf("exported counter = %v, want one cumulative value of 7", metric.GetSum())
					}
				}
			}
		}
	}
	if !found {
		t.Error("no OTLP metric data was exported")
	}
}

func TestOTLPHTTPExportPathsResourcesAndSignalOverrides(t *testing.T) {
	for _, specific := range []bool{false, true} {
		name := "common_endpoint"
		if specific {
			name = "signal_overrides"
		}
		t.Run(name, func(t *testing.T) {
			isolatedTelemetry(t)
			tracePath, metricPath := "/collector/v1/traces", "/collector/v1/metrics"
			if specific {
				tracePath, metricPath = "/trace-input", "/metric-input"
			}
			capture := new(exportCapture)
			server := capture.httpReceiver(t, tracePath, metricPath)
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL+"/collector/")
			t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Bearer%20shared")
			t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.name=resource-service,deployment.environment.name=fixture")
			service := "resource-service"
			if specific {
				t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", server.URL+tracePath)
				t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", server.URL+metricPath)
				t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "Authorization=Bearer%20trace")
				t.Setenv("OTEL_EXPORTER_OTLP_METRICS_HEADERS", "Authorization=Bearer%20metric")
				t.Setenv("OTEL_SERVICE_NAME", "configured-api")
				service = "configured-api"
			}
			runtime := startRuntime(t, "default-api")
			if !runtime.Enabled() {
				t.Fatal("an explicit collector endpoint did not enable telemetry")
			}
			parent, child := emitOperation(t)
			flushRuntime(t, runtime)
			capture.checkData(t, service, "fixture", parent, child)
			capture.mu.Lock()
			defer capture.mu.Unlock()
			if len(capture.requests) != 2 {
				t.Fatalf("received %d export requests, want one for each signal", len(capture.requests))
			}
			for _, request := range capture.requests {
				wantHeader := "Bearer shared"
				if specific && request.path == tracePath {
					wantHeader = "Bearer trace"
				} else if specific {
					wantHeader = "Bearer metric"
				}
				if request.method != http.MethodPost || request.contentType != "application/x-protobuf" || request.authorization != wantHeader {
					t.Errorf("unexpected OTLP HTTP request: %+v", request)
				}
			}
		})
	}
}

type traceReceiver struct {
	collectortrace.UnimplementedTraceServiceServer
	capture *exportCapture
}

func (r *traceReceiver) Export(ctx context.Context, request *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	r.capture.mu.Lock()
	defer r.capture.mu.Unlock()
	r.capture.requests = append(r.capture.requests, observedExport{method: "grpc", path: "traces", authorization: incomingAuthorization(ctx)})
	r.capture.traces = append(r.capture.traces, request)
	return &collectortrace.ExportTraceServiceResponse{}, nil
}

type metricReceiver struct {
	collectormetrics.UnimplementedMetricsServiceServer
	capture *exportCapture
}

func (r *metricReceiver) Export(ctx context.Context, request *collectormetrics.ExportMetricsServiceRequest) (*collectormetrics.ExportMetricsServiceResponse, error) {
	r.capture.mu.Lock()
	defer r.capture.mu.Unlock()
	r.capture.requests = append(r.capture.requests, observedExport{method: "grpc", path: "metrics", authorization: incomingAuthorization(ctx)})
	r.capture.metrics = append(r.capture.metrics, request)
	return &collectormetrics.ExportMetricsServiceResponse{}, nil
}

func incomingAuthorization(ctx context.Context) string {
	values, _ := metadata.FromIncomingContext(ctx)
	return strings.Join(values.Get("authorization"), ",")
}

func (c *exportCapture) grpcReceiver(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	collectortrace.RegisterTraceServiceServer(server, &traceReceiver{capture: c})
	collectormetrics.RegisterMetricsServiceServer(server, &metricReceiver{capture: c})
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		if err := <-done; err != nil {
			t.Errorf("serve local OTLP gRPC receiver: %v", err)
		}
	})
	return "http://" + listener.Addr().String()
}

func TestOTLPGRPCExportAndPerSignalProtocol(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		name := "grpc"
		if mixed {
			name = "http_trace_overrides_grpc"
		}
		t.Run(name, func(t *testing.T) {
			isolatedTelemetry(t)
			capture := new(exportCapture)
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", capture.grpcReceiver(t))
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
			t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Bearer%20grpc")
			if mixed {
				httpServer := capture.httpReceiver(t, "/trace-input", "/unused-metrics")
				t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", httpServer.URL+"/trace-input")
				t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "http/protobuf")
				t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "Authorization=Bearer%20http")
			}
			runtime := startRuntime(t, "grpc-api")
			parent, child := emitOperation(t)
			flushRuntime(t, runtime)
			capture.checkData(t, "grpc-api", "", parent, child)
			capture.mu.Lock()
			defer capture.mu.Unlock()
			if len(capture.requests) != 2 {
				t.Fatalf("received %d OTLP exports, want two signals", len(capture.requests))
			}
			for _, request := range capture.requests {
				wantHeader := "Bearer grpc"
				if mixed && request.path == "/trace-input" {
					wantHeader = "Bearer http"
				}
				if request.authorization != wantHeader || (!mixed && request.method != "grpc") {
					t.Errorf("OTLP protocol/header precedence failed: %+v", request)
				}
			}
		})
	}
}

func TestTraceSamplingDoesNotDisableMetricExport(t *testing.T) {
	isolatedTelemetry(t)
	capture := new(exportCapture)
	server := capture.httpReceiver(t, "/v1/traces", "/v1/metrics")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL)
	t.Setenv("OTEL_TRACES_SAMPLER", "always_off")
	runtime := startRuntime(t, "sampled-api")
	emitOperation(t)
	flushRuntime(t, runtime)
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.traces) != 0 {
		t.Fatal("always_off sampling exported spans")
	}
	capture.checkMetricLocked(t, "sampled-api")
}

func TestTelemetryDefaultAndExplicitDisableDoNotContactCollector(t *testing.T) {
	for _, configured := range []bool{false, true} {
		name := "unconfigured"
		if configured {
			name = "explicitly_disabled"
		}
		t.Run(name, func(t *testing.T) {
			isolatedTelemetry(t)
			capture := new(exportCapture)
			server := capture.httpReceiver(t, "/v1/traces", "/v1/metrics")
			if configured {
				t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL)
				t.Setenv("OTEL_SDK_DISABLED", "true")
			}
			runtime := startRuntime(t, "disabled-api")
			if runtime.Enabled() {
				t.Fatal("telemetry enabled without an enabled configuration")
			}
			emitOperation(t)
			flushRuntime(t, runtime)
			capture.mu.Lock()
			defer capture.mu.Unlock()
			if len(capture.requests) != 0 {
				t.Fatal("disabled telemetry contacted the collector")
			}
		})
	}
}

func TestSignalOptOutOnlyExportsEnabledSignals(t *testing.T) {
	for _, disabled := range []string{"traces", "metrics", "both"} {
		t.Run(disabled, func(t *testing.T) {
			isolatedTelemetry(t)
			capture := new(exportCapture)
			server := capture.httpReceiver(t, "/v1/traces", "/v1/metrics")
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL)
			t.Setenv("OTEL_SDK_DISABLED", "false")
			if disabled != "metrics" {
				t.Setenv("OTEL_TRACES_EXPORTER", "none")
			}
			if disabled != "traces" {
				t.Setenv("OTEL_METRICS_EXPORTER", "none")
			}
			runtime := startRuntime(t, "partial-api")
			if runtime.Enabled() != (disabled != "both") {
				t.Fatal("runtime enabled state does not match the selected signals")
			}
			emitOperation(t)
			flushRuntime(t, runtime)
			capture.mu.Lock()
			defer capture.mu.Unlock()
			switch disabled {
			case "traces":
				if len(capture.traces) != 0 {
					t.Error("disabled trace exporter received data")
				}
				capture.checkMetricLocked(t, "partial-api")
			case "metrics":
				if len(capture.metrics) != 0 || len(capture.traces) != 1 {
					t.Errorf("signal opt-out produced %d trace batches and %d metric batches", len(capture.traces), len(capture.metrics))
				}
			case "both":
				if len(capture.requests) != 0 {
					t.Error("disabling both signals contacted the collector")
				}
			}
		})
	}
}

func TestInvalidSignalConfigurationDoesNotInstallPartialProviders(t *testing.T) {
	isolatedTelemetry(t)
	capture := new(exportCapture)
	server := capture.httpReceiver(t, "/v1/traces", "/v1/metrics")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", "unsupported")
	traces, metrics := otel.GetTracerProvider(), otel.GetMeterProvider()
	runtime, err := telemetry.Init(context.Background(), "invalid-api")
	if err == nil {
		if runtime != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = runtime.Shutdown(ctx)
		}
		t.Fatal("invalid protocol was accepted")
	}
	if otel.GetTracerProvider() != traces || otel.GetMeterProvider() != metrics {
		t.Error("failed initialization replaced a global provider")
	}
	// Instrumentation still behaves normally after the rejected setup.
	_, span := otel.Tracer("lester/export-integration").Start(context.Background(), "after.invalid.config", trace.WithAttributes(attribute.Bool("fixture", true)))
	span.End()
}
