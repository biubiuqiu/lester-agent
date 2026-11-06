package integration

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const modelInstrumentation = "github.com/biubiuqiu/lester-agent/model"

// observedModelClient only sees bounded operational metadata. In particular,
// requests, responses, endpoint URLs and error messages are never attributes.
type observedModelClient struct {
	client   modelruntime.Client
	provider string
	tracer   trace.Tracer
	requests metric.Int64Counter
	duration metric.Float64Histogram
	tokens   metric.Int64Histogram
}

func observeModelClient(client modelruntime.Client, provider string) modelruntime.Client {
	return newObservedModelClient(client, provider, otel.GetTracerProvider(), otel.GetMeterProvider())
}

func newObservedModelClient(client modelruntime.Client, provider string, traces trace.TracerProvider, metrics metric.MeterProvider) *observedModelClient {
	meter := metrics.Meter(modelInstrumentation)
	requests, _ := meter.Int64Counter("lester.model.requests", metric.WithUnit("{request}"), metric.WithDescription("Completed model operations, including failed and cancelled operations."))
	duration, _ := meter.Float64Histogram("gen_ai.client.operation.duration", metric.WithUnit("s"), metric.WithDescription("Model operation duration through stream delivery and closure."), metric.WithExplicitBucketBoundaries(.01, .05, .1, .25, .5, 1, 2, 5, 10, 30, 60, 120, 300, 600))
	tokens, _ := meter.Int64Histogram("gen_ai.client.token.usage", metric.WithUnit("{token}"), metric.WithDescription("Provider-reported token usage per operation; cumulative stream snapshots are recorded only once."), metric.WithExplicitBucketBoundaries(1, 16, 64, 256, 1024, 4096, 16384, 65536, 262144, 1048576))
	return &observedModelClient{client: client, provider: modelProviderName(provider), tracer: traces.Tracer(modelInstrumentation), requests: requests, duration: duration, tokens: tokens}
}

func modelProviderName(provider string) string {
	switch provider {
	case "openai", "azure_openai", "openai_compatible", "anthropic", "anthropic_compatible", "vertex", "foundry", "bedrock":
		return provider
	default:
		return "other"
	}
}

func (c *observedModelClient) Capabilities(ctx context.Context, model string) (modelruntime.Capabilities, error) {
	return c.client.Capabilities(ctx, model)
}

func (c *observedModelClient) Generate(ctx context.Context, request modelruntime.Request) (*modelruntime.Response, error) {
	ctx, operation := c.start(ctx, false)
	// End the span on a provider panic without intercepting the original panic.
	defer operation.finish("provider", nil)
	response, err := c.client.Generate(ctx, request)
	kind := modelErrorKind(err)
	if err == nil && response == nil {
		err = &modelruntime.ProviderError{Kind: modelruntime.ErrorProtocol}
		kind = "protocol"
	} else if err == nil && ctx.Err() != nil {
		kind = modelErrorKind(ctx.Err())
	}
	var usage *modelruntime.NormalizedUsage
	if response != nil {
		usage = response.NormalizedUsage
	}
	operation.finish(kind, usage)
	return response, err
}

func (c *observedModelClient) Stream(ctx context.Context, request modelruntime.Request) (<-chan modelruntime.Event, error) {
	ctx, operation := c.start(ctx, true)
	started := false
	defer func() {
		if !started {
			operation.finish("provider", nil)
		}
	}()
	stream, err := c.client.Stream(ctx, request)
	if err != nil {
		operation.finish(modelErrorKind(err), nil)
		return stream, err
	}
	if stream == nil {
		// A nil channel without an error is an invalid provider result. Fail
		// safely instead of leaving the caller blocked forever on that channel.
		operation.finish("protocol", nil)
		return nil, &modelruntime.ProviderError{Kind: modelruntime.ErrorProtocol}
	}
	started = true
	output := make(chan modelruntime.Event, 16)
	go func() {
		defer close(output)
		kind := ""
		seen := false
		completed := false
		var usage *modelruntime.NormalizedUsage
		defer func() { operation.finish(kind, usage) }()
		for {
			// Prefer a ready event to cancellation. Providers may emit their
			// terminal error/completion while cancelling their request context.
			var event modelruntime.Event
			var open bool
			select {
			case event, open = <-stream:
			default:
				select {
				case event, open = <-stream:
				case <-ctx.Done():
					if kind == "" && !completed {
						kind = modelErrorKind(ctx.Err())
					}
					return
				}
			}
			if !open {
				if kind == "" {
					if ctx.Err() != nil && !completed {
						kind = modelErrorKind(ctx.Err())
					} else if !seen {
						kind = "protocol"
					}
				}
				return
			}
			seen = true
			if event.Type == "MODEL_COMPLETED" {
				completed = true
			}
			if event.NormalizedUsage != nil && event.NormalizedUsage.Source == modelruntime.UsageActual {
				snapshot := *event.NormalizedUsage
				snapshot.Raw = nil
				usage = &snapshot
			}
			if event.Err != nil && kind == "" {
				kind = modelErrorKind(event.Err)
			}
			// Preserve an already-received event when space is available, even
			// after cancellation. Never block indefinitely after a reader leaves.
			select {
			case output <- event:
			default:
				select {
				case output <- event:
				case <-ctx.Done():
					if kind == "" && !completed {
						kind = modelErrorKind(ctx.Err())
					}
					return
				}
			}
			// A misbehaving provider can remain continuously ready after
			// cancellation; do not let fast readers keep the span alive forever.
			if ctx.Err() != nil {
				if kind == "" && !completed {
					kind = modelErrorKind(ctx.Err())
				}
				return
			}
		}
	}()
	return output, nil
}

type modelOperation struct {
	client *observedModelClient
	ctx    context.Context
	span   trace.Span
	start  time.Time
	attrs  []attribute.KeyValue
	once   sync.Once
}

func (c *observedModelClient) start(ctx context.Context, streaming bool) (context.Context, *modelOperation) {
	attrs := []attribute.KeyValue{attribute.String("gen_ai.operation.name", "chat"), attribute.String("gen_ai.provider.name", c.provider), attribute.Bool("lester.model.streaming", streaming)}
	ctx, span := c.tracer.Start(ctx, "gen_ai.chat", trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...))
	return ctx, &modelOperation{client: c, ctx: ctx, span: span, start: time.Now(), attrs: attrs}
}

func (o *modelOperation) finish(kind string, usage *modelruntime.NormalizedUsage) {
	o.once.Do(func() {
		defer o.span.End()
		outcome := "success"
		attrs := append([]attribute.KeyValue(nil), o.attrs...)
		if kind != "" {
			outcome = "error"
			if kind == "cancelled" {
				outcome = "cancelled"
			}
			o.span.SetAttributes(attribute.String("error.type", kind))
			o.span.SetStatus(codes.Error, kind)
			attrs = append(attrs, attribute.String("error.type", kind))
		}
		o.span.SetAttributes(attribute.String("lester.outcome", outcome))
		attrs = append(attrs, attribute.String("lester.outcome", outcome))
		options := metric.WithAttributes(attrs...)
		if o.client.requests != nil {
			o.client.requests.Add(o.ctx, 1, options)
		}
		if o.client.duration != nil {
			o.client.duration.Record(o.ctx, time.Since(o.start).Seconds(), options)
		}
		if usage == nil || usage.Source != modelruntime.UsageActual {
			return
		}
		if tokens, reported := reportedModelInput(usage); reported {
			o.recordTokens(attrs, "input", tokens)
		}
		if usage.OutputReported && usage.OutputTokens >= 0 {
			o.recordTokens(attrs, "output", int64(usage.OutputTokens))
		}
	})
}

func (o *modelOperation) recordTokens(attrs []attribute.KeyValue, kind string, value int64) {
	o.span.SetAttributes(attribute.Int64("gen_ai.usage."+kind+"_tokens", value))
	if o.client.tokens != nil {
		labels := append(append([]attribute.KeyValue(nil), attrs...), attribute.String("gen_ai.token.type", kind))
		o.client.tokens.Record(o.ctx, value, metric.WithAttributes(labels...))
	}
}

func modelErrorKind(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	switch modelruntime.ErrorKindOf(err) {
	case modelruntime.ErrorContextExceeded:
		return "context_length_exceeded"
	case modelruntime.ErrorAuthentication:
		return "authentication"
	case modelruntime.ErrorRateLimit:
		return "rate_limit"
	case modelruntime.ErrorCancelled:
		return "cancelled"
	case modelruntime.ErrorTransport:
		return "transport"
	case modelruntime.ErrorProtocol:
		return "protocol"
	default:
		return "provider"
	}
}

// Malformed provider counters must not overflow into a negative token sample.
func reportedModelInput(usage *modelruntime.NormalizedUsage) (int64, bool) {
	if !usage.InputReported || usage.InputTokens < 0 {
		return 0, false
	}
	tokens := int64(usage.InputTokens)
	if !usage.CachedIncluded {
		for _, cached := range []int{usage.CacheReadInputTokens, usage.CacheCreationInputTokens} {
			if cached < 0 || int64(cached) > math.MaxInt64-tokens {
				return 0, false
			}
			tokens += int64(cached)
		}
	}
	return tokens, true
}
