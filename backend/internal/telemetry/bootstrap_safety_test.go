package telemetry_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/telemetry"
	"go.opentelemetry.io/otel"
)

func TestInvalidTelemetryConfigurationNeverEchoesCredentials(t *testing.T) {
	for _, sample := range []struct{ key, value string }{
		{"OTEL_EXPORTER_OTLP_ENDPOINT", "https://private-marker:private-marker@example.test/otlp"},
		{"OTEL_EXPORTER_OTLP_ENDPOINT", "https://example.test/otlp?token=private-marker"},
		{"OTEL_EXPORTER_OTLP_TRACES_HEADERS", "Authorization=private-marker%0AInjected%3Avalue"},
		{"OTEL_EXPORTER_OTLP_PROTOCOL", "private-marker"},
		{"OTEL_TRACES_SAMPLER", "private-marker"},
		{"OTEL_TRACES_SAMPLER_ARG", "NaN"},
		{"OTEL_METRIC_EXPORT_INTERVAL", "-1"},
		{"OTEL_SDK_DISABLED", "private-marker"},
	} {
		t.Run(sample.key, func(t *testing.T) {
			isolatedTelemetry(t)
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
			t.Setenv("OTEL_TRACES_SAMPLER", "parentbased_traceidratio")
			t.Setenv(sample.key, sample.value)
			traces, metrics := otel.GetTracerProvider(), otel.GetMeterProvider()
			runtime, err := telemetry.Init(context.Background(), "fixture")
			if err == nil || runtime != nil {
				t.Fatal("invalid exporter configuration was accepted")
			}
			if strings.Contains(err.Error(), "private-marker") {
				t.Fatal("configuration error exposed private input")
			}
			if otel.GetTracerProvider() != traces || otel.GetMeterProvider() != metrics {
				t.Fatal("invalid configuration changed active providers")
			}
		})
	}
}

func TestExplicitDisableDoesNotInitializeInvalidExporter(t *testing.T) {
	isolatedTelemetry(t)
	t.Setenv("OTEL_SDK_DISABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://private-marker:private-marker@example.test")
	runtime, err := telemetry.Init(context.Background(), "fixture")
	if err != nil || runtime.Enabled() {
		t.Fatal("explicitly disabled telemetry initialized an exporter")
	}
	if err := runtime.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestServiceInstancesDoNotMergeReplicaMetrics(t *testing.T) {
	isolatedTelemetry(t)
	var capture exportCapture
	server := capture.httpReceiver(t, "/v1/traces", "/v1/metrics")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL)
	for range 2 {
		runtime, err := telemetry.Init(context.Background(), "fixture-replica")
		if err != nil {
			t.Fatal(err)
		}
		emitOperation(t)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err = runtime.Shutdown(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	instances := map[string]bool{}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	for _, batch := range capture.metrics {
		for _, metrics := range batch.ResourceMetrics {
			for _, attribute := range metrics.Resource.Attributes {
				if attribute.Key == "service.instance.id" {
					instances[attribute.Value.GetStringValue()] = true
				}
			}
		}
	}
	if len(instances) != 2 || instances[""] {
		t.Fatal("replica resource identity was missing or reused")
	}
}

func TestCollectorFailureHasBoundedFlushAndSanitizedWarnings(t *testing.T) {
	isolatedTelemetry(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("private-marker: collector echoed a credential"))
	}))
	defer server.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=private-marker")
	t.Setenv("OTEL_METRICS_EXPORTER", "none")
	t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "100")
	t.Setenv("OTEL_BSP_EXPORT_TIMEOUT", "100")
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	runtime, err := telemetry.Init(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	_, span := otel.Tracer("fixture").Start(context.Background(), "fixture.request")
	span.End()
	flushCtx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	started := time.Now()
	err = runtime.ForceFlush(flushCtx)
	cancel()
	if err == nil {
		t.Fatal("unavailable collector was reported as successfully flushed")
	}
	if time.Since(started) > time.Second {
		t.Fatal("flush exceeded its independent time budget")
	}
	shutdownCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	_ = runtime.Shutdown(shutdownCtx)
	if strings.Contains(output.String(), "private-marker") || strings.Contains(err.Error(), "private-marker") {
		t.Fatal("SDK failure exposed exporter credentials or response content")
	}
}
