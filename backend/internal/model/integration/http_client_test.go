package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
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
