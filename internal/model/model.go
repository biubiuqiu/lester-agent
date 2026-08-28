package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	Role, Content string
	ToolCallID    string     `json:"tool_call_id,omitempty"`
	ToolCalls     []ToolCall `json:"tool_calls,omitempty"`
}
type Tool struct {
	Name, Description string
	InputSchema       map[string]any
}
type ToolCall struct {
	ID, Name  string
	Arguments json.RawMessage
	Index     int `json:"-"`
}
type ModelRequest struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []Tool
	MaxTokens   int
	Temperature *float64
}
type ModelEvent struct {
	Type     string
	Delta    string
	ToolCall *ToolCall
	Usage    map[string]int
	Err      error
}
type ModelResponse struct {
	Content   string
	ToolCalls []ToolCall
	Usage     map[string]int
}
type ModelCapabilities struct{ Streaming, Tools, Vision, Reasoning, PromptCaching, StructuredOutput, TokenCounting bool }
type ModelClient interface {
	Generate(context.Context, ModelRequest) (*ModelResponse, error)
	Stream(context.Context, ModelRequest) (<-chan ModelEvent, error)
	Capabilities(context.Context, string) (ModelCapabilities, error)
}

type HTTPClient struct {
	Protocol, Endpoint, APIKey, Mode string
	Headers                          map[string]string
	Client                           *http.Client
}

func (c *HTTPClient) Capabilities(context.Context, string) (ModelCapabilities, error) {
	return ModelCapabilities{Streaming: true, Tools: true, Vision: true, StructuredOutput: c.Protocol == "openai", TokenCounting: true}, nil
}
func (c *HTTPClient) Generate(ctx context.Context, req ModelRequest) (*ModelResponse, error) {
	events, err := c.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	result := &ModelResponse{Usage: map[string]int{}}
	for event := range events {
		if event.Err != nil {
			return nil, event.Err
		}
		result.Content += event.Delta
		if event.ToolCall != nil {
			result.ToolCalls = append(result.ToolCalls, *event.ToolCall)
		}
		for k, v := range event.Usage {
			result.Usage[k] += v
		}
	}
	return result, nil
}
func (c *HTTPClient) Stream(ctx context.Context, req ModelRequest) (<-chan ModelEvent, error) {
	payload := c.openAIPayload(req)
	if c.Protocol == "anthropic" {
		payload = c.anthropicPayload(req)
	}
	body, _ := json.Marshal(payload)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if c.Protocol == "anthropic" {
		if c.APIKey != "" {
			request.Header.Set("x-api-key", c.APIKey)
		}
		request.Header.Set("anthropic-version", "2023-06-01")
	} else {
		request.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	for k, v := range c.Headers {
		request.Header.Set(k, v)
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 300 {
		defer response.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return nil, fmt.Errorf("model provider %s: %s", response.Status, string(data))
	}
	out := make(chan ModelEvent, 16)
	go c.readSSE(response.Body, out)
	return out, nil
}
func (c *HTTPClient) readSSE(body io.ReadCloser, out chan<- ModelEvent) {
	defer close(out)
	defer body.Close()
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 2<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			out <- ModelEvent{Type: "MODEL_COMPLETED"}
			return
		}
		var raw map[string]any
		if json.Unmarshal([]byte(data), &raw) != nil {
			continue
		}
		if c.Protocol == "anthropic" {
			out <- parseAnthropic(raw)
		} else {
			out <- parseOpenAI(raw)
		}
	}
	if err := scanner.Err(); err != nil {
		out <- ModelEvent{Type: "RUN_FAILED", Err: err}
	}
}

func (c *HTTPClient) openAIPayload(req ModelRequest) map[string]any {
	messages := make([]map[string]any, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, map[string]any{"role": "system", "content": req.System})
	}
	for _, m := range req.Messages {
		item := map[string]any{"role": m.Role, "content": m.Content}
		if m.ToolCallID != "" {
			item["tool_call_id"] = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			calls := make([]any, 0, len(m.ToolCalls))
			for _, call := range m.ToolCalls {
				calls = append(calls, map[string]any{"id": call.ID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": string(call.Arguments)}})
			}
			item["tool_calls"] = calls
		}
		messages = append(messages, item)
	}
	payload := map[string]any{"model": req.Model, "messages": messages, "stream": true, "stream_options": map[string]bool{"include_usage": true}}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		tools := make([]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.InputSchema}})
		}
		payload["tools"] = tools
	}
	return payload
}
func (c *HTTPClient) anthropicPayload(req ModelRequest) map[string]any {
	messages := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == "tool" {
			messages = append(messages, map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Content}}})
			continue
		}
		if len(m.ToolCalls) > 0 {
			content := make([]any, 0, len(m.ToolCalls)+1)
			if m.Content != "" {
				content = append(content, map[string]any{"type": "text", "text": m.Content})
			}
			for _, call := range m.ToolCalls {
				var input any = map[string]any{}
				_ = json.Unmarshal(call.Arguments, &input)
				content = append(content, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": input})
			}
			messages = append(messages, map[string]any{"role": "assistant", "content": content})
			continue
		}
		messages = append(messages, map[string]any{"role": m.Role, "content": m.Content})
	}
	payload := map[string]any{"model": req.Model, "system": req.System, "messages": messages, "max_tokens": max(req.MaxTokens, 4096), "stream": true}
	if c.Mode == "vertex" {
		delete(payload, "model")
		payload["anthropic_version"] = "vertex-2023-10-16"
	}
	if len(req.Tools) > 0 {
		tools := make([]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.InputSchema})
		}
		payload["tools"] = tools
	}
	return payload
}
func parseOpenAI(raw map[string]any) ModelEvent {
	event := ModelEvent{Type: "MODEL_DELTA"}
	choices, _ := raw["choices"].([]any)
	if len(choices) > 0 {
		choice, _ := choices[0].(map[string]any)
		delta, _ := choice["delta"].(map[string]any)
		event.Delta, _ = delta["content"].(string)
		if calls, _ := delta["tool_calls"].([]any); len(calls) > 0 {
			call, _ := calls[0].(map[string]any)
			fn, _ := call["function"].(map[string]any)
			args := asString(fn["arguments"])
			event.ToolCall = &ToolCall{ID: asString(call["id"]), Name: asString(fn["name"]), Arguments: json.RawMessage(args), Index: asInt(call["index"])}
		}
	}
	return event
}
func parseAnthropic(raw map[string]any) ModelEvent {
	event := ModelEvent{Type: "MODEL_DELTA"}
	delta, _ := raw["delta"].(map[string]any)
	event.Delta = asString(delta["text"])
	index := asInt(raw["index"])
	if raw["type"] == "content_block_start" {
		block, _ := raw["content_block"].(map[string]any)
		if block["type"] == "tool_use" {
			args, _ := json.Marshal(block["input"])
			event.ToolCall = &ToolCall{ID: asString(block["id"]), Name: asString(block["name"]), Arguments: args, Index: index}
		}
	}
	if raw["type"] == "content_block_delta" && delta["type"] == "input_json_delta" {
		event.ToolCall = &ToolCall{Arguments: json.RawMessage(asString(delta["partial_json"])), Index: index}
	}
	return event
}
func asString(value any) string { result, _ := value.(string); return result }
func asInt(value any) int       { number, _ := value.(float64); return int(number) }

var _ = errors.New
