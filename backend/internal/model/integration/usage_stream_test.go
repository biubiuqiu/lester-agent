package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
)

func streamFixture(t *testing.T, protocol, body string) (*modelruntime.NormalizedUsage, []modelruntime.Event) {
	t.Helper()
	events := make(chan modelruntime.Event, 32)
	(&httpClient{protocol: protocol}).readSSE(context.Background(), io.NopCloser(strings.NewReader(body)), events)
	var result []modelruntime.Event
	var final *modelruntime.NormalizedUsage
	for event := range events {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		result = append(result, event)
		if event.Type == "MODEL_COMPLETED" {
			if final != nil {
				t.Fatal("duplicate completion")
			}
			final = event.NormalizedUsage
		}
	}
	if final == nil {
		t.Fatal("missing normalized final usage")
	}
	return final, result
}

func TestOpenAIEmptyChoiceUsageTailIsCapturedWithoutDoubleCounting(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":12,\"total_tokens\":112,\"prompt_tokens_details\":{\"cached_tokens\":80}}}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":12,\"total_tokens\":112,\"prompt_tokens_details\":{\"cached_tokens\":80}}}\n\n" +
		"data: [DONE]\n\n"
	usage, events := streamFixture(t, "openai", body)
	input, known := usage.RequestInputTokens()
	if input != 100 || !known || usage.OutputTokens != 12 || usage.CacheReadInputTokens != 80 || !usage.CachedIncluded || usage.Raw["total_tokens"] != 112 {
		t.Fatalf("usage=%+v", usage)
	}
	if events[len(events)-1].Usage["prompt_tokens"] != 100 {
		t.Fatal("provider-compatible raw usage missing from completion")
	}
	// Generate must use snapshots rather than summing repeated streaming totals.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
	defer server.Close()
	response, err := (&httpClient{protocol: "openai", endpoint: server.URL}).Generate(context.Background(), modelruntime.Request{Model: "fixture"})
	if err != nil || response.Usage["prompt_tokens"] != 100 || response.NormalizedUsage.OutputTokens != 12 {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}

func TestAnthropicStartAndCumulativeDeltaUsagePreserveSeparateCache(t *testing.T) {
	body := "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":0,\"cache_read_input_tokens\":60,\"cache_creation_input_tokens\":30,\"cache_creation\":{\"ephemeral_5m_input_tokens\":10,\"ephemeral_1h_input_tokens\":20}}}}\n\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":8}}\n\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":12}}\n\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":12}}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	usage, events := streamFixture(t, "anthropic", body)
	input, known := usage.RequestInputTokens()
	if input != 100 || !known || usage.OutputTokens != 12 || usage.CacheCreationInputTokens != 30 || usage.CachedIncluded || usage.Source != modelruntime.UsageActual {
		t.Fatalf("usage=%+v", usage)
	}
	if events[0].NormalizedUsage.OutputTokens != 0 || events[1].NormalizedUsage.OutputTokens != 8 {
		t.Fatal("later frames mutated earlier cumulative snapshots")
	}
}

func TestMissingAndZeroUsageRequireAnEstimate(t *testing.T) {
	for _, suffix := range []string{"", "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":0}}\n\n"} {
		usage, _ := streamFixture(t, "openai", suffix+"data: [DONE]\n\n")
		if input, known := usage.RequestInputTokens(); known || input != 0 {
			t.Fatalf("zero/missing usage treated as an exact budget: %+v", usage)
		}
		if suffix == "" && usage.Source != modelruntime.UsageMissing || suffix != "" && usage.Source != modelruntime.UsageActual {
			t.Fatalf("source=%q", usage.Source)
		}
	}
}

func TestInvalidUsageCountersDoNotEraseEarlierMeasurement(t *testing.T) {
	usage, _ := streamFixture(t, "openai", "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":3}}\n\n"+
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":-1,\"completion_tokens\":\"private response\",\"total_tokens\":1.5}}\n\n"+
		"data: [DONE]\n\n")
	if usage.InputTokens != 10 || usage.OutputTokens != 3 || usage.Raw["total_tokens"] != 0 {
		t.Fatalf("usage=%+v", usage)
	}
	data, _ := json.Marshal(usage)
	if strings.Contains(string(data), "private response") {
		t.Fatal("non-numeric usage data leaked into normalized counters")
	}
}

func TestGenerateAssemblesEveryToolCallAndArgumentFragment(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"a\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\"}},{\"index\":1,\"id\":\"b\",\"function\":{\"name\":\"search\",\"arguments\":\"{\\\"query\\\":\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"x\\\"}\"}},{\"index\":1,\"function\":{\"arguments\":\"\\\"hi\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
	defer server.Close()
	response, err := (&httpClient{protocol: "openai", endpoint: server.URL}).Generate(context.Background(), modelruntime.Request{Model: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.ToolCalls) != 2 || response.ToolCalls[0].ID != "a" || response.ToolCalls[1].ID != "b" || string(response.ToolCalls[0].Arguments) != `{"path":"x"}` || string(response.ToolCalls[1].Arguments) != `{"query":"hi"}` {
		t.Fatalf("calls=%+v", response.ToolCalls)
	}
}

func TestStreamEndAndMalformedFrameHaveDifferentSafeFailures(t *testing.T) {
	for _, test := range []struct {
		body      string
		kind      modelruntime.ErrorKind
		completed bool
	}{
		{"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10}}\n\n", "", true},
		{"data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n", modelruntime.ErrorTransport, false},
		{"data: private-malformed-provider-response\n\n", modelruntime.ErrorProtocol, false},
	} {
		events := make(chan modelruntime.Event, 8)
		(&httpClient{protocol: "openai"}).readSSE(context.Background(), io.NopCloser(strings.NewReader(test.body)), events)
		completed, foundError := false, false
		for event := range events {
			completed = completed || event.Type == "MODEL_COMPLETED"
			if event.Err != nil {
				foundError = true
				if modelruntime.ErrorKindOf(event.Err) != test.kind || strings.Contains(event.Err.Error(), "private-malformed") {
					t.Fatalf("error=%v kind=%q", event.Err, modelruntime.ErrorKindOf(event.Err))
				}
				if test.kind == modelruntime.ErrorTransport && !errors.Is(event.Err, io.ErrUnexpectedEOF) {
					t.Fatal("interrupted EOF lost its error identity")
				}
			}
		}
		if completed != test.completed || foundError == test.completed {
			t.Fatalf("completed=%v error=%v", completed, foundError)
		}
	}
}

func TestStreamCancellationClosesBlockedBodyWithoutCompleting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	defer writer.Close()
	events := make(chan modelruntime.Event, 8)
	done := make(chan struct{})
	go func() {
		(&httpClient{protocol: "openai"}).readSSE(ctx, reader, events)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled stream kept the provider body open")
	}
	for event := range events {
		if event.Type == "MODEL_COMPLETED" {
			t.Fatal("cancelled stream emitted success")
		}
		if event.Err != nil && (!errors.Is(event.Err, context.Canceled) || modelruntime.ErrorKindOf(event.Err) != modelruntime.ErrorCancelled) {
			t.Fatalf("cancellation=%v", event.Err)
		}
	}
}

func TestSSEMultilineDataAndCommentsDoNotLoseUsage(t *testing.T) {
	usage, _ := streamFixture(t, "openai", ": heartbeat\n\ndata: {\"choices\":[],\ndata: \"usage\":{\"prompt_tokens\":10,\"completion_tokens\":3}}\n\ndata: [DONE]\n\n")
	if usage.InputTokens != 10 || usage.OutputTokens != 3 {
		t.Fatalf("usage=%+v", usage)
	}
}

func TestProviderFailuresAreClassifiedWithoutEchoingPrivateBody(t *testing.T) {
	for _, test := range []struct {
		status int
		body   string
		kind   modelruntime.ErrorKind
	}{
		{400, `{"error":{"code":"context_length_exceeded","message":"private-prompt"}}`, modelruntime.ErrorContextExceeded},
		{400, `{"error":{"type":"invalid_request_error","message":"prompt is too long: private-prompt"}}`, modelruntime.ErrorContextExceeded},
		{400, `{"error":{"type":"invalid_request_error","message":"invalid tool argument: private-prompt contains context length exceeded"}}`, modelruntime.ErrorProvider},
		{401, `{"error":{"message":"private-prompt secret-key"}}`, modelruntime.ErrorAuthentication},
		{403, `{"message":"private-prompt"}`, modelruntime.ErrorAuthentication},
		{429, `{"error":{"message":"private-prompt"}}`, modelruntime.ErrorRateLimit},
		{400, `{"__type":"com.amazon.bedrock#ThrottlingException","message":"private-prompt"}`, modelruntime.ErrorRateLimit},
		{503, `{"error":{"message":"private-prompt"}}`, modelruntime.ErrorProvider},
	} {
		t.Run(fmt.Sprint(test.status, test.kind), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			_, err := (&httpClient{protocol: "openai", endpoint: server.URL}).Stream(context.Background(), modelruntime.Request{Model: "fixture"})
			if err == nil || modelruntime.ErrorKindOf(err) != test.kind || strings.Contains(err.Error(), "private-prompt") || strings.Contains(err.Error(), "secret-key") {
				t.Fatalf("error=%v kind=%q", err, modelruntime.ErrorKindOf(err))
			}
		})
	}
	events := make(chan modelruntime.Event, 8)
	(&httpClient{protocol: "anthropic"}).readSSE(context.Background(), io.NopCloser(strings.NewReader("data: {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"prompt is too long: private-prompt\"}}\n\n")), events)
	for event := range events {
		if event.Err == nil || modelruntime.ErrorKindOf(event.Err) != modelruntime.ErrorContextExceeded || strings.Contains(event.Err.Error(), "private-prompt") {
			t.Fatalf("stream error=%v", event.Err)
		}
	}
}

func TestBedrockUsageAndExplicitOutputLimit(t *testing.T) {
	client := &bedrockClient{region: "fixture", credentials: bedrockCredentials{AccessKeyID: "fixture-id", SecretAccessKey: "fixture-secret"}, http: &http.Client{Transport: redirectTestTransport(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["max_tokens"] != float64(100) {
			t.Fatalf("explicit output limit was overridden: %v", payload["max_tokens"])
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":10,"output_tokens":7,"cache_read_input_tokens":20,"cache_creation_input_tokens":30}}`)), Header: make(http.Header)}, nil
	})}}
	events, err := client.Stream(context.Background(), modelruntime.Request{Model: "fixture", MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	for event := range events {
		if event.Type == "MODEL_COMPLETED" {
			input, known := event.NormalizedUsage.RequestInputTokens()
			if input != 60 || !known || event.Usage["output_tokens"] != 7 || event.NormalizedUsage.Provider != "bedrock" {
				t.Fatalf("usage=%+v", event.NormalizedUsage)
			}
		}
	}
	if anthropicPayload(modelruntime.Request{MaxTokens: 100}, "")["max_tokens"] != 100 {
		t.Fatal("Anthropic explicit limit overridden")
	}
	for _, client := range []modelruntime.Client{&httpClient{protocol: "openai"}, &httpClient{protocol: "anthropic"}, client} {
		caps, err := client.Capabilities(context.Background(), "fixture")
		if err != nil || caps.TokenCounting || caps.StructuredOutput {
			t.Fatal("claimed an API option that is not implemented")
		}
	}
}
