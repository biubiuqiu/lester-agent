package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/biubiuqiu/lester-agent/backend/internal/toolcontext"
)

func TestOpenAICompatibleStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["model"] != "test-model" || payload["stream"] != true {
			t.Errorf("unexpected payload: %#v", payload)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	client, err := (OpenAICompatible{}).NewClient(ClientSpec{Endpoint: server.URL + "/v1", ModelID: "test-model", Credential: []byte("test-key")})
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.Stream(context.Background(), modelruntime.Request{Model: "test-model", Messages: []modelruntime.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	var content string
	for event := range events {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		content += event.Delta
	}
	if content != "hello" {
		t.Fatalf("content = %q", content)
	}
}

func TestStreamRetainsAllToolCallsAndDetectsInterruptedEOF(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"a\",\"function\":{\"name\":\"read\",\"arguments\":\"{}\"}},{\"index\":1,\"id\":\"b\",\"function\":{\"name\":\"read\",\"arguments\":\"{}\"}}]}}]}\n\n"
	client := &httpClient{protocol: "openai"}
	events := make(chan modelruntime.Event, 10)
	client.readSSE(context.Background(), io.NopCloser(strings.NewReader(body)), events)
	var calls []string
	var streamErr error
	for event := range events {
		if event.ToolCall != nil {
			calls = append(calls, event.ToolCall.ID)
		}
		if event.Err != nil {
			streamErr = event.Err
		}
	}
	if len(calls) != 2 || calls[0] != "a" || calls[1] != "b" || streamErr == nil {
		t.Fatalf("calls=%v error=%v", calls, streamErr)
	}
}

func TestRestoredToolMessagesConvertToProviderProtocols(t *testing.T) {
	request := modelruntime.Request{System: "system", Messages: []modelruntime.Message{
		{Role: "user", Content: "read file"},
		{Role: "assistant", Content: "reading", ToolCalls: []modelruntime.ToolCall{{ID: "read-1", Name: "read", Arguments: json.RawMessage(`{"file_path":"x"}`)}}},
		{Role: "tool", ToolCallID: "read-1", Content: `{"content":"     1\thello"}`},
	}}
	openAI := openAIPayload(request)["messages"].([]map[string]any)
	if openAI[3]["tool_call_id"] != "read-1" || openAI[3]["content"] != request.Messages[2].Content {
		t.Fatalf("OpenAI payload=%#v", openAI)
	}
	anthropic := anthropicPayload(request, "")["messages"].([]map[string]any)
	result := anthropic[2]["content"].([]any)[0].(map[string]any)
	if result["tool_use_id"] != "read-1" || result["content"] != request.Messages[2].Content {
		t.Fatalf("Anthropic payload=%#v", anthropic)
	}
}

func TestProjectedToolContextConvertsToProviderProtocols(t *testing.T) {
	history := []modelruntime.Message{{Role: "user", Content: "inspect"}}
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("read-%d", i)
		history = append(history,
			modelruntime.Message{Role: "assistant", RunID: "run", ToolCalls: []modelruntime.ToolCall{{ID: id, Name: "read", Arguments: json.RawMessage(`{"file_path":"x"}`)}}},
			modelruntime.Message{Role: "tool", ToolCallID: id, Content: `{"content":"original source"}`},
		)
	}
	projection, err := toolcontext.Build(history)
	if err != nil {
		t.Fatal(err)
	}
	request := modelruntime.Request{Messages: projection.Messages}
	for name, payload := range map[string]map[string]any{"openai": openAIPayload(request), "anthropic": anthropicPayload(request, ""), "vertex": anthropicPayload(request, "vertex")} {
		t.Run(name, func(t *testing.T) {
			calls, results, references := 0, 0, 0
			for _, message := range payload["messages"].([]map[string]any) {
				if _, ok := message["RunID"]; ok {
					t.Fatal("local provenance leaked as a provider field")
				}
				if text, ok := message["content"].(string); ok && strings.Contains(text, "Historical tool references") {
					references++
				}
				if name == "openai" {
					if message["role"] == "tool" {
						results++
					}
					if list, ok := message["tool_calls"].([]any); ok {
						calls += len(list)
					}
				} else if blocks, ok := message["content"].([]any); ok {
					for _, block := range blocks {
						switch block.(map[string]any)["type"] {
						case "tool_use":
							calls++
						case "tool_result":
							results++
						}
					}
				}
			}
			if calls != 10 || results != 10 || references != 2 {
				t.Fatalf("calls=%d results=%d references=%d", calls, results, references)
			}
		})
	}
}

func TestAnthropicStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "anthropic-key" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()

	client, err := (Anthropic{}).NewClient(ClientSpec{Endpoint: server.URL, Credential: []byte("anthropic-key")})
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.Stream(context.Background(), modelruntime.Request{Model: "claude-test", MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	first := <-events
	if first.Err != nil || first.Delta != "hello" {
		t.Fatalf("first event = %#v", first)
	}
}
