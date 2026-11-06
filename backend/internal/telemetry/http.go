package telemetry

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// HTTPRoute returns a registered template, never a request path or query.
func HTTPRoute(r *http.Request) string {
	if route := chi.RouteContext(r.Context()); route != nil {
		if pattern := route.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return "unmatched"
}

// HTTPMethod limits method attributes even for unknown methods on public routes.
func HTTPMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace, http.MethodPatch:
		return method
	default:
		return "_OTHER"
	}
}

// HTTPMiddleware records template-based server operations and preserves streaming
// and upgrade interfaces. Published artifact paths are intentionally never named.
func HTTPMiddleware(component string) func(http.Handler) http.Handler {
	tracer := otel.Tracer("lester/http")
	meter := otel.Meter("lester/http")
	duration, _ := meter.Float64Histogram("http.server.request.duration", metric.WithUnit("s"), metric.WithDescription("Duration of HTTP server requests, including streaming lifetimes"), metric.WithExplicitBucketBoundaries(.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 300, 900))
	active, _ := meter.Int64UpDownCounter("http.server.active_requests", metric.WithUnit("{request}"), metric.WithDescription("HTTP requests whose handlers have not returned"))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			parent := TraceparentPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			method := HTTPMethod(r.Method)
			common := []attribute.KeyValue{attribute.String("http.request.method", method), attribute.String("lester.component", component)}
			ctx, span := tracer.Start(parent, "HTTP request", trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(common...))
			request := r.WithContext(ctx)
			start := time.Now()
			active.Add(ctx, 1, metric.WithAttributes(common...))
			wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			writer, hijacked := observeHijack(wrapped)
			defer func() {
				panicValue := recover()
				route := HTTPRoute(request)
				if component == "artifact-host" {
					route = "static artifact"
				}
				attributes := append(append([]attribute.KeyValue{}, common...), attribute.String("http.route", route))
				status := wrapped.Status()
				if panicValue != nil {
					MarkHTTPPanic(ctx)
				}
				// A hijacked connection may have written its handshake directly. Its status
				// is not observable through ResponseWriter, so do not invent a 200 or 101.
				if status == 0 && !*hijacked && panicValue == nil {
					status = http.StatusOK
				}
				if status != 0 {
					attributes = append(attributes, attribute.Int("http.response.status_code", status))
				}
				if *hijacked {
					span.SetAttributes(attribute.Bool("lester.http.hijacked", true))
				}
				span.SetName(method + " " + route)
				span.SetAttributes(attributes...)
				if status >= 500 {
					span.SetStatus(codes.Error, "HTTP server error")
				}
				duration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attributes...))
				active.Add(ctx, -1, metric.WithAttributes(common...))
				span.End()
				if panicValue != nil {
					panic(panicValue)
				}
			}()
			next.ServeHTTP(writer, request)
		})
	}
}

// MarkHTTPPanic lets an inner recovery boundary mark the original server span
// without recording the panic value or stack in exported telemetry.
func MarkHTTPPanic(ctx context.Context) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("error.type", "panic"))
	span.SetStatus(codes.Error, "HTTP handler panic")
}

type hijackObserver struct {
	middleware.WrapResponseWriter
	hijacker http.Hijacker
	hijacked *bool
}

func (w *hijackObserver) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffer, err := w.hijacker.Hijack()
	if err == nil {
		*w.hijacked = true
	}
	return conn, buffer, err
}

// Keep optional interfaces conditional, just as chi's original wrapper does.
// Delegating ReaderFrom and Flush to it also keeps its status/byte accounting.
func observeHijack(w middleware.WrapResponseWriter) (http.ResponseWriter, *bool) {
	hijacked := new(bool)
	h, ok := w.(http.Hijacker)
	if !ok {
		return w, hijacked
	}
	observed := &hijackObserver{WrapResponseWriter: w, hijacker: h, hijacked: hijacked}
	f, flush := w.(http.Flusher)
	reader, readFrom := w.(io.ReaderFrom)
	switch {
	case flush && readFrom:
		return struct {
			*hijackObserver
			http.Flusher
			io.ReaderFrom
		}{observed, f, reader}, hijacked
	case flush:
		return struct {
			*hijackObserver
			http.Flusher
		}{observed, f}, hijacked
	case readFrom:
		return struct {
			*hijackObserver
			io.ReaderFrom
		}{observed, reader}, hijacked
	default:
		return observed, hijacked
	}
}
