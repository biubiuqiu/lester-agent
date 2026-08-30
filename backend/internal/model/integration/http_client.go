package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
)

type httpClient struct {
	protocol, endpoint, apiKey, mode string
	headers                          map[string]string
	client                           *http.Client
}

func (c *httpClient) Capabilities(context.Context, string) (modelruntime.Capabilities, error) {
	return modelruntime.Capabilities{Streaming: true, Tools: true, Vision: true, StructuredOutput: c.protocol == "openai", TokenCounting: true}, nil
}

func (c *httpClient) Generate(ctx context.Context, request modelruntime.Request) (*modelruntime.Response, error) {
	events, err := c.Stream(ctx, request)
	if err != nil {
		return nil, err
	}
	result := &modelruntime.Response{Usage: map[string]int{}}
	for event := range events {
		if event.Err != nil {
			return nil, event.Err
		}
		result.Content += event.Delta
		if event.ToolCall != nil {
			result.ToolCalls = append(result.ToolCalls, *event.ToolCall)
		}
		for key, value := range event.Usage {
			result.Usage[key] += value
		}
	}
	return result, nil
}

func (c *httpClient) Stream(ctx context.Context, request modelruntime.Request) (<-chan modelruntime.Event, error) {
	payload := openAIPayload(request)
	if c.protocol == "anthropic" {
		payload = anthropicPayload(request, c.mode)
	}
	body, _ := json.Marshal(payload)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if c.protocol == "anthropic" {
		if c.apiKey != "" {
			httpRequest.Header.Set("x-api-key", c.apiKey)
		}
		httpRequest.Header.Set("anthropic-version", "2023-06-01")
	} else {
		httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for key, value := range c.headers {
		httpRequest.Header.Set(key, value)
	}
	client := c.client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 300 {
		defer response.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return nil, fmt.Errorf("model provider %s: %s", response.Status, string(data))
	}
	events := make(chan modelruntime.Event, 16)
	go c.readSSE(response.Body, events)
	return events, nil
}

func (c *httpClient) readSSE(body io.ReadCloser, output chan<- modelruntime.Event) {
	defer close(output)
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
			output <- modelruntime.Event{Type: "MODEL_COMPLETED"}
			return
		}
		var raw map[string]any
		if json.Unmarshal([]byte(data), &raw) != nil {
			continue
		}
		if c.protocol == "anthropic" {
			output <- parseAnthropic(raw)
		} else {
			output <- parseOpenAI(raw)
		}
	}
	if err := scanner.Err(); err != nil {
		output <- modelruntime.Event{Type: "RUN_FAILED", Err: err}
	}
}

func openAIPayload(request modelruntime.Request) map[string]any {
	messages := make([]map[string]any, 0, len(request.Messages)+1)
	if request.System != "" {
		messages = append(messages, map[string]any{"role": "system", "content": request.System})
	}
	for _, message := range request.Messages {
		item := map[string]any{"role": message.Role, "content": message.Content}
		if message.ToolCallID != "" {
			item["tool_call_id"] = message.ToolCallID
		}
		if len(message.ToolCalls) > 0 {
			calls := make([]any, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				calls = append(calls, map[string]any{"id": call.ID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": string(call.Arguments)}})
			}
			item["tool_calls"] = calls
		}
		messages = append(messages, item)
	}
	payload := map[string]any{"model": request.Model, "messages": messages, "stream": true, "stream_options": map[string]bool{"include_usage": true}}
	if request.MaxTokens > 0 {
		payload["max_tokens"] = request.MaxTokens
	}
	if len(request.Tools) > 0 {
		tools := make([]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": tool.Name, "description": tool.Description, "parameters": tool.InputSchema}})
		}
		payload["tools"] = tools
	}
	return payload
}

func anthropicPayload(request modelruntime.Request, mode string) map[string]any {
	messages := make([]map[string]any, 0, len(request.Messages))
	for _, message := range request.Messages {
		if message.Role == "tool" {
			messages = append(messages, map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": message.ToolCallID, "content": message.Content}}})
			continue
		}
		if len(message.ToolCalls) > 0 {
			content := make([]any, 0, len(message.ToolCalls)+1)
			if message.Content != "" {
				content = append(content, map[string]any{"type": "text", "text": message.Content})
			}
			for _, call := range message.ToolCalls {
				var input any = map[string]any{}
				_ = json.Unmarshal(call.Arguments, &input)
				content = append(content, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": input})
			}
			messages = append(messages, map[string]any{"role": "assistant", "content": content})
			continue
		}
		messages = append(messages, map[string]any{"role": message.Role, "content": message.Content})
	}
	payload := map[string]any{"model": request.Model, "system": request.System, "messages": messages, "max_tokens": max(request.MaxTokens, 4096), "stream": true}
	if mode == "vertex" {
		delete(payload, "model")
		payload["anthropic_version"] = "vertex-2023-10-16"
	}
	if len(request.Tools) > 0 {
		tools := make([]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			tools = append(tools, map[string]any{"name": tool.Name, "description": tool.Description, "input_schema": tool.InputSchema})
		}
		payload["tools"] = tools
	}
	return payload
}

func parseOpenAI(raw map[string]any) modelruntime.Event {
	event := modelruntime.Event{Type: "MODEL_DELTA"}
	choices, _ := raw["choices"].([]any)
	if len(choices) == 0 {
		return event
	}
	choice, _ := choices[0].(map[string]any)
	delta, _ := choice["delta"].(map[string]any)
	event.Delta, _ = delta["content"].(string)
	if calls, _ := delta["tool_calls"].([]any); len(calls) > 0 {
		call, _ := calls[0].(map[string]any)
		function, _ := call["function"].(map[string]any)
		event.ToolCall = &modelruntime.ToolCall{ID: valueString(call["id"]), Name: valueString(function["name"]), Arguments: json.RawMessage(valueString(function["arguments"])), Index: valueInt(call["index"])}
	}
	return event
}

func parseAnthropic(raw map[string]any) modelruntime.Event {
	event := modelruntime.Event{Type: "MODEL_DELTA"}
	delta, _ := raw["delta"].(map[string]any)
	event.Delta = valueString(delta["text"])
	index := valueInt(raw["index"])
	if raw["type"] == "content_block_start" {
		block, _ := raw["content_block"].(map[string]any)
		if block["type"] == "tool_use" {
			arguments, _ := json.Marshal(block["input"])
			event.ToolCall = &modelruntime.ToolCall{ID: valueString(block["id"]), Name: valueString(block["name"]), Arguments: arguments, Index: index}
		}
	}
	if raw["type"] == "content_block_delta" && delta["type"] == "input_json_delta" {
		event.ToolCall = &modelruntime.ToolCall{Arguments: json.RawMessage(valueString(delta["partial_json"])), Index: index}
	}
	return event
}

func valueString(value any) string { result, _ := value.(string); return result }
func valueInt(value any) int       { number, _ := value.(float64); return int(number) }
