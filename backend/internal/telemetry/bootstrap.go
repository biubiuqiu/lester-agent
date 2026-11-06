package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type Runtime struct {
	traces  *sdktrace.TracerProvider
	metrics *sdkmetric.MeterProvider
}

// Init installs exporters only after both signals have initialized successfully.
// With no collector configuration the existing application remains a no-op.
func Init(ctx context.Context, serviceName string) (*Runtime, error) {
	cfg, err := loadSettings()
	if err != nil {
		return nil, err
	}
	runtime := &Runtime{}
	if !cfg.enabled {
		return runtime, nil
	}
	// Export failures may contain a collector address or authentication material.
	// Report a bounded generic warning rather than logging SDK error strings.
	var warningMu sync.Mutex
	var lastWarning time.Time
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {
		warningMu.Lock()
		defer warningMu.Unlock()
		if time.Since(lastWarning) >= time.Minute {
			lastWarning = time.Now()
			slog.Warn("OpenTelemetry export failed; check collector availability and configuration")
		}
	}))
	res, err := resource.New(ctx, resource.WithAttributes(
		attribute.String("service.name", serviceName),
		attribute.String("service.instance.id", uuid.NewString()),
	), resource.WithTelemetrySDK(), resource.WithFromEnv())
	if err != nil {
		return nil, errors.New("invalid OpenTelemetry resource configuration")
	}
	if cfg.traces {
		var exporter sdktrace.SpanExporter
		if cfg.traceProtocol == "grpc" {
			exporter, err = otlptracegrpc.New(ctx)
		} else {
			exporter, err = otlptracehttp.New(ctx)
		}
		if err != nil {
			return nil, errors.New("could not initialize OpenTelemetry trace exporter")
		}
		runtime.traces = sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithSampler(cfg.sampler), sdktrace.WithBatcher(exporter))
	}
	if cfg.metrics {
		var exporter sdkmetric.Exporter
		if cfg.metricProtocol == "grpc" {
			exporter, err = otlpmetricgrpc.New(ctx)
		} else {
			exporter, err = otlpmetrichttp.New(ctx)
		}
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = runtime.Shutdown(cleanup)
			return nil, errors.New("could not initialize OpenTelemetry metric exporter")
		}
		runtime.metrics = sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)))
	}
	if runtime.traces != nil {
		otel.SetTracerProvider(runtime.traces)
	}
	if runtime.metrics != nil {
		otel.SetMeterProvider(runtime.metrics)
	}
	otel.SetTextMapPropagator(TraceparentPropagator())
	return runtime, nil
}

func (r *Runtime) Enabled() bool { return r != nil && (r.traces != nil || r.metrics != nil) }

func (r *Runtime) ForceFlush(ctx context.Context) error {
	return r.finish(ctx, false)
}

// Shutdown uses the caller's independent shutdown budget, not a cancelled request.
func (r *Runtime) Shutdown(ctx context.Context) error {
	return r.finish(ctx, true)
}

func (r *Runtime) finish(ctx context.Context, shutdown bool) error {
	if r == nil {
		return nil
	}
	results := make(chan error, 2)
	count := 0
	if r.traces != nil {
		count++
		go func() {
			if shutdown {
				results <- r.traces.Shutdown(ctx)
			} else {
				results <- r.traces.ForceFlush(ctx)
			}
		}()
	}
	if r.metrics != nil {
		count++
		go func() {
			if shutdown {
				results <- r.metrics.Shutdown(ctx)
			} else {
				results <- r.metrics.ForceFlush(ctx)
			}
		}()
	}
	failed := false
	for range count {
		if err := <-results; err != nil {
			failed = true
		}
	}
	if failed {
		return errors.New("OpenTelemetry export did not complete within the shutdown or flush budget")
	}
	return nil
}
