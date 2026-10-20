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

func TestOpenAIPayloadOmitsUnsetMaxTokens(t *testing.T) {
	payload := openAIPayload(modelruntime.Request{Model: "test-model"})
	if _, ok := payload["max_tokens"]; ok {
		t.Fatalf("unset max_tokens must be omitted: %#v", payload)
	}

	payload = openAIPayload(modelruntime.Request{Model: "test-model", MaxTokens: 8192})
	if payload["max_tokens"] != 8192 {
		t.Fatalf("explicit max_tokens = %#v", payload["max_tokens"])
	}
}

func TestToolFreeSchemaNegotiatesJSONModeOnlyOnExplicitRejection(t *testing.T) {
	for _, scenario := range []struct {
		name                   string
		status                 int
		body                   string
		optIn, tools, fallback bool
	}{
		{"compatible provider", 400, `{"error":{"param":"response_format","message":"json_schema is not supported; use json_object"}}`, true, false, true},
		{"explicit format without param", 422, `{"error":{"message":"This model does not support json_schema"}}`, true, false, true},
		{"approval remains strict", 400, `{"error":{"param":"response_format","message":"json_schema is not supported"}}`, false, false, false},
		{"tools never downgrade", 400, `{"error":{"param":"response_format","message":"json_schema is not supported"}}`, true, true, false},
		{"invalid schema is not unsupported", 400, `{"error":{"param":"response_format","message":"invalid schema: required is missing"}}`, true, false, false},
		{"unsupported schema keyword is not a format rejection", 400, `{"error":{"param":"response_format","message":"invalid schema: minLength is unsupported"}}`, true, false, false},
		{"authentication", 401, `{"error":{"message":"json_schema is not supported"}}`, true, false, false},
		{"server failure", 500, `{"error":{"message":"json_schema is not supported"}}`, true, false, false},
		{"unrelated parameter", 400, `{"error":{"param":"temperature","message":"temperature not supported"}}`, true, false, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				format := payload["response_format"].(map[string]any)
				if calls == 1 {
					if format["type"] != "json_schema" {
						t.Error("first request must use the schema")
					}
					w.WriteHeader(scenario.status)
					_, _ = fmt.Fprint(w, scenario.body)
					return
				}
				if calls > 2 || !scenario.fallback {
					t.Error("unexpected negotiation")
				}
				if format["type"] != "json_object" || payload["tools"] != nil {
					t.Error("fallback must be JSON and tool-free")
				}
				encoded, _ := json.Marshal(payload["messages"])
				if !strings.Contains(string(encoded), `\"required\":[\"reply\"]`) {
					t.Error("fallback lost independent validation schema")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"reply\\\":\\\"hello\\\"}\"}}]}\n\ndata: [DONE]\n\n")
			}))
			defer server.Close()
			client, err := (OpenAICompatible{}).NewClient(ClientSpec{Endpoint: server.URL + "/v1", Credential: []byte("fixture")})
			if err != nil {
				t.Fatal(err)
			}
			request := modelruntime.Request{Model: "fixture", System: "Return JSON", Messages: []modelruntime.Message{{Role: "user", Content: "hello"}}, ResponseSchema: &modelruntime.JSONSchema{Name: "personal_assistant", Schema: map[string]any{"type": "object", "required": []string{"reply"}}, AllowJSONMode: scenario.optIn}}
			if scenario.tools {
				request.Tools = []modelruntime.Tool{{Name: "command", InputSchema: map[string]any{"type": "object"}}}
			}
			result, err := client.Generate(context.Background(), request)
			if scenario.fallback {
				if err != nil || result == nil || result.Content != `{"reply":"hello"}` || calls != 2 {
					t.Fatalf("negotiation failed: result=%v err=%v calls=%d", result, err, calls)
				}
			} else if err == nil || calls != 1 {
				t.Fatalf("must fail once: err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestStructuredReviewSchemaIsSentAndNeverSilentlyRetried(t *testing.T) {
	schema := &modelruntime.JSONSchema{Name: "review", Schema: map[string]any{"type": "object", "additionalProperties": false, "required": []string{"decision"}, "properties": map[string]any{"decision": map[string]any{"type": "string", "enum": []string{"allow", "deny", "ask_user"}}}}}
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if protocol == "openai" {
					format, _ := payload["response_format"].(map[string]any)
					detail, _ := format["json_schema"].(map[string]any)
					if format["type"] != "json_schema" || detail["strict"] != true || detail["name"] != "review" || detail["schema"] == nil {
						t.Error("OpenAI schema missing", payload)
					}
				} else {
					config, _ := payload["output_config"].(map[string]any)
					format, _ := config["format"].(map[string]any)
					if format["type"] != "json_schema" || format["schema"] == nil {
						t.Error("Anthropic schema missing", payload)
					}
				}
				if _, present := payload["tools"]; present {
					t.Error("review call exposed execution tools")
				}
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":{"message":"structured output unsupported"}}`))
			}))
			defer server.Close()
			client := &httpClient{protocol: protocol, endpoint: server.URL, apiKey: "fixture"}
			if _, err := client.Generate(context.Background(), modelruntime.Request{Model: "fixture", System: "review policy", Messages: []modelruntime.Message{{Role: "user", Content: "command data"}}, ResponseSchema: schema}); err == nil {
				t.Fatal("provider rejection ignored")
			}
			if calls != 1 {
				t.Fatal("structured review silently retried", calls)
			}
		})
	}
	for _, payload := range []map[string]any{openAIPayload(modelruntime.Request{}), anthropicPayload(modelruntime.Request{}, "")} {
		if payload["response_format"] != nil || payload["output_config"] != nil {
			t.Fatal("ordinary Agent request received a review schema")
		}
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
		for _, call := range event.ToolCallDeltas() {
			calls = append(calls, call.ID)
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

func TestImageToolResultUsesVisionContentParts(t *testing.T) {
	raw := `{"content":"Read image file \"photo.png\" (image/png, 3 bytes).","images":[{"path":"photo.png","media_type":"image/png","encoding":"base64","data":"iVBORw=="}]}`
	request := modelruntime.Request{Messages: []modelruntime.Message{
		{Role: "assistant", Content: "", ToolCalls: []modelruntime.ToolCall{{ID: "read-image", Name: "read", Arguments: json.RawMessage(`{"file_path":"photo.png"}`)}}},
		{Role: "tool", ToolCallID: "read-image", Content: raw},
	}}
	openAI := openAIPayload(request)["messages"].([]map[string]any)
	if len(openAI) != 3 || openAI[1]["role"] != "tool" || openAI[1]["content"] != `Read image file "photo.png" (image/png, 3 bytes).` {
		t.Fatalf("OpenAI image payload = %#v", openAI)
	}
	imageMessage := openAI[2]
	parts := imageMessage["content"].([]any)
	imagePart := parts[1].(map[string]any)
	imageURL := imagePart["image_url"].(map[string]any)["url"]
	if imageMessage["role"] != "user" || imageURL != "data:image/png;base64,iVBORw==" {
		t.Fatalf("OpenAI image part = %#v", imageMessage)
	}
	anthropic := anthropicPayload(request, "")["messages"].([]map[string]any)
	result := anthropic[1]["content"].([]any)[0].(map[string]any)
	blocks := result["content"].([]any)
	textBlock := blocks[0].(map[string]any)
	imageBlock := blocks[1].(map[string]any)
	if result["type"] != "tool_result" || textBlock["type"] != "text" || imageBlock["type"] != "image" {
		t.Fatalf("Anthropic image payload = %#v", result)
	}
	source := imageBlock["source"].(map[string]any)
	if source["type"] != "base64" || source["media_type"] != "image/png" || source["data"] != "iVBORw==" {
		t.Fatalf("Anthropic image source = %#v", source)
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
