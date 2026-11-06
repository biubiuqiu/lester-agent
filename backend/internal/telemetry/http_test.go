package telemetry_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/biubiuqiu/lester-agent/backend/internal/telemetry"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func captureHTTP(t *testing.T) (*tracetest.InMemoryExporter, *sdkmetric.ManualReader) {
	t.Helper()
	previousTrace, previousMetric := otel.GetTracerProvider(), otel.GetMeterProvider()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetTracerProvider(provider)
	otel.SetMeterProvider(meter)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousTrace)
		otel.SetMeterProvider(previousMetric)
		_ = provider.Shutdown(context.Background())
		_ = meter.Shutdown(context.Background())
	})
	return exporter, reader
}

func TestHTTPInternalPropagationTemplatesAndPrivateData(t *testing.T) {
	exporter, reader := captureHTTP(t)
	downstream := chi.NewRouter()
	downstream.Use(telemetry.HTTPMiddleware("sandbox-service"))
	downstream.Get("/v1/sandboxes/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-credential" {
			t.Error("internal authentication changed")
		}
		if r.Header.Get("traceparent") == "" {
			t.Error("internal traceparent missing")
		}
		if r.Header.Get("baggage") != "" || r.Header.Get("tracestate") != "" {
			t.Error("unexpected context forwarding")
		}
		httpapi.JSON(w, 200, map[string]string{"id": "private-sandbox", "status": "running"})
	})
	server := httptest.NewServer(downstream)
	defer server.Close()
	client := sandbox.NewClient(server.URL, "private-credential")
	router := chi.NewRouter()
	router.Use(telemetry.HTTPMiddleware("api"))
	router.Post("/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		if _, err := client.Inspect(r.Context(), "private-sandbox"); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusAccepted)
	})
	request := httptest.NewRequest("POST", "/runs/private-id?code=private-code", strings.NewReader("private-prompt"))
	request.Header.Set("traceparent", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
	request.Header.Set("tracestate", "vendor=private-state")
	request.Header.Set("baggage", "user=private-user")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 202 {
		t.Fatalf("response changed: %d", response.Code)
	}
	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("got %d spans", len(spans))
	}
	child, parent := spans[0], spans[1]
	if parent.Name != "POST /runs/{id}" || child.Name != "GET /v1/sandboxes/{id}" {
		t.Fatalf("non-template names: %q %q", parent.Name, child.Name)
	}
	if child.Parent.SpanID() != parent.SpanContext.SpanID() || child.SpanContext.TraceID() != parent.SpanContext.TraceID() {
		t.Fatal("internal request lost parent")
	}
	if parent.Parent.SpanID().String() != "0123456789abcdef" {
		t.Fatal("incoming parent lost")
	}
	raw, _ := json.Marshal(spans)
	for _, value := range []string{"private-", "authorization", "url.full", "url.path"} {
		if strings.Contains(strings.ToLower(string(raw)), value) {
			t.Fatalf("private telemetry: %s", value)
		}
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	observations := 0
	for _, scope := range metrics.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			switch data := measurement.Data.(type) {
			case metricdata.Histogram[float64]:
				if measurement.Name == "http.server.request.duration" {
					observations += len(data.DataPoints)
					for _, point := range data.DataPoints {
						if point.Count != 1 {
							t.Fatal("request counted twice")
						}
					}
				}
			case metricdata.Sum[int64]:
				if measurement.Name == "http.server.active_requests" {
					for _, point := range data.DataPoints {
						if point.Value != 0 {
							t.Fatal("unbalanced active requests")
						}
					}
				}
			}
		}
	}
	if observations != 2 {
		t.Fatalf("got %d duration points", observations)
	}
}

func TestHTTPUnmatchedPanicAndArtifactNamesAreBounded(t *testing.T) {
	exporter, _ := captureHTTP(t)
	router := chi.NewRouter()
	router.Use(telemetry.HTTPMiddleware("api"), httpapi.Recover(slog.New(slog.NewTextHandler(io.Discard, nil))))
	router.Get("/panic/{id}", func(http.ResponseWriter, *http.Request) { panic("private-panic") })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("PRIVATE-METHOD", "/private-path?private-query=1", nil))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/panic/private-id", nil))
	if response.Code != 500 {
		t.Fatal("recovery changed")
	}
	telemetry.HTTPMiddleware("artifact-host")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/s/private-id/private-file.html", nil))
	spans := exporter.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("got %d spans", len(spans))
	}
	if spans[0].Name != "_OTHER unmatched" || spans[2].Name != "GET static artifact" {
		t.Fatalf("unbounded names: %q %q", spans[0].Name, spans[2].Name)
	}
	if spans[0].Status.Code == codes.Error || spans[2].Status.Code == codes.Error {
		t.Fatal("4xx marked server error")
	}
	if spans[1].Status.Code != codes.Error {
		t.Fatal("panic not marked failed")
	}
	panicKind := false
	for _, a := range spans[1].Attributes {
		if string(a.Key) == "error.type" && a.Value.AsString() == "panic" {
			panicKind = true
		}
	}
	if !panicKind {
		t.Fatal("recovered panic kind missing")
	}
	raw, _ := json.Marshal(spans)
	if strings.Contains(string(raw), "private-") {
		t.Fatal("private unmatched URL or panic data exported")
	}
}

func TestHTTPStreamingAndUpgradeKeepProtocolBehavior(t *testing.T) {
	exporter, _ := captureHTTP(t)
	recorder := httptest.NewRecorder()
	telemetry.HTTPMiddleware("api")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Hijacker); ok {
			t.Error("introduced unsupported Hijacker")
		}
		_, _ = w.Write([]byte("data: fixture\n\n"))
		w.(http.Flusher).Flush()
	})).ServeHTTP(recorder, httptest.NewRequest("GET", "/events", nil))
	if !recorder.Flushed || recorder.Body.String() != "data: fixture\n\n" {
		t.Fatal("SSE changed")
	}
	router := chi.NewRouter()
	router.Use(telemetry.HTTPMiddleware("sandbox-service"))
	done := make(chan struct{})
	router.Get("/terminal/{id}", func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		kind, data, err := conn.ReadMessage()
		if err != nil {
			t.Error(err)
			return
		}
		if err = conn.WriteMessage(kind, data); err != nil {
			t.Error(err)
		}
	})
	server := httptest.NewServer(router)
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/terminal/private-id", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err = conn.WriteMessage(websocket.TextMessage, []byte("private-terminal-output")); err != nil {
		t.Fatal(err)
	}
	_, echo, err := conn.ReadMessage()
	if err != nil || string(echo) != "private-terminal-output" {
		t.Fatalf("upgrade echo changed: %q %v", echo, err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("upgraded handler did not finish")
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for len(exporter.GetSpans()) < 2 {
		select {
		case <-deadline.C:
			t.Fatal("upgraded span did not end")
		case <-time.After(time.Millisecond):
		}
	}
	raw, _ := json.Marshal(exporter.GetSpans())
	if bytes.Contains(raw, []byte("private-")) {
		t.Fatal("websocket content or IDs exported")
	}
}

func TestHTTPReaderFromAndHTTP2PushRemainConditional(t *testing.T) {
	captureHTTP(t)
	fancy := &httpFancyFixture{ResponseRecorder: httptest.NewRecorder()}
	telemetry.HTTPMiddleware("api")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Hijacker); !ok {
			t.Error("lost HTTP/1 Hijacker")
		}
		if _, ok := w.(io.ReaderFrom); !ok {
			t.Error("lost ReaderFrom")
		}
		_, _ = io.Copy(w, struct{ io.Reader }{strings.NewReader("fixture-file")})
	})).ServeHTTP(fancy, httptest.NewRequest("GET", "/file", nil))
	if !fancy.readFrom || fancy.Body.String() != "fixture-file" {
		t.Fatal("ReaderFrom changed")
	}
	h2 := &http2Fixture{ResponseRecorder: httptest.NewRecorder()}
	request := httptest.NewRequest("GET", "/http2", nil)
	request.ProtoMajor = 2
	telemetry.HTTPMiddleware("api")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Hijacker); ok {
			t.Error("introduced HTTP/2 Hijacker")
		}
		_ = w.(http.Pusher).Push("/fixture", nil)
		w.(http.Flusher).Flush()
	})).ServeHTTP(h2, request)
	if !h2.pushed || !h2.Flushed {
		t.Fatal("HTTP/2 capability changed")
	}
}

type httpFancyFixture struct {
	*httptest.ResponseRecorder
	readFrom bool
}

func (w *httpFancyFixture) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, http.ErrNotSupported
}
func (w *httpFancyFixture) ReadFrom(r io.Reader) (int64, error) {
	w.readFrom = true
	return io.Copy(w.Body, r)
}

type http2Fixture struct {
	*httptest.ResponseRecorder
	pushed bool
}

func (w *http2Fixture) Push(string, *http.PushOptions) error {
	w.pushed = true
	return http.ErrNotSupported
}

func TestHTTPUncaughtPanicDoesNotInventAResponseStatus(t *testing.T) {
	for _, status := range []int{0, http.StatusOK} {
		name := "before headers"
		if status != 0 {
			name = "after headers"
		}
		t.Run(name, func(t *testing.T) {
			exporter, _ := captureHTTP(t)
			handler := telemetry.HTTPMiddleware("artifact-host")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if status != 0 {
					w.WriteHeader(status)
				}
				panic("private-panic")
			}))
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/private-path", nil))
			}()
			if recovered != "private-panic" {
				t.Fatal("middleware changed panic propagation")
			}
			spans := exporter.GetSpans()
			if len(spans) != 1 {
				t.Fatalf("got %d spans", len(spans))
			}
			if spans[0].Status.Code != codes.Error {
				t.Fatal("panic span not marked failed")
			}
			foundStatus := false
			foundPanic := false
			for _, a := range spans[0].Attributes {
				if string(a.Key) == "http.response.status_code" {
					foundStatus = true
					if status == 0 || a.Value.AsInt64() != int64(status) {
						t.Fatalf("invented response status: %v", a.Value)
					}
				}
				if string(a.Key) == "error.type" && a.Value.AsString() == "panic" {
					foundPanic = true
				}
			}
			if status != 0 && !foundStatus {
				t.Fatal("discarded observed response status")
			}
			if !foundPanic {
				t.Fatal("panic classification missing")
			}
		})
	}
}
