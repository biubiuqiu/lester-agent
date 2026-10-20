package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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
	return modelruntime.Capabilities{Streaming: true, Tools: true, Vision: true}, nil
}

func (c *httpClient) Generate(ctx context.Context, request modelruntime.Request) (*modelruntime.Response, error) {
	events, err := c.Stream(ctx, request)
	if err != nil {
		return nil, err
	}
	result := &modelruntime.Response{NormalizedUsage: (&usageAccumulator{provider: c.protocol}).snapshot()}
	var calls modelruntime.ToolCallAssembler
	for event := range events {
		if event.Err != nil {
			return nil, event.Err
		}
		result.Content += event.Delta
		calls.AddEvent(event)
		if event.NormalizedUsage != nil {
			result.NormalizedUsage = event.NormalizedUsage.Clone()
			result.Usage = result.NormalizedUsage.Raw
		}
	}
	if ctx.Err() != nil {
		return nil, providerTransportError(ctx, ctx.Err())
	}
	result.ToolCalls = calls.Calls()
	return result, nil
}

func (c *httpClient) Stream(ctx context.Context, request modelruntime.Request) (<-chan modelruntime.Event, error) {
	return c.stream(ctx, request, false)
}

func (c *httpClient) stream(ctx context.Context, request modelruntime.Request, jsonMode bool) (<-chan modelruntime.Event, error) {
	if jsonMode {
		schema, _ := json.Marshal(request.ResponseSchema.Schema)
		request.System += "\nReturn a JSON object matching this schema exactly, with every required field and no other fields:\n" + string(schema)
	}
	payload := openAIPayload(request)
	if jsonMode {
		payload["response_format"] = map[string]any{"type": "json_object"}
	}
	if c.protocol == "anthropic" {
		payload = anthropicPayload(request, c.mode)
	}
	body, _ := json.Marshal(payload)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, providerProtocolError(err)
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
	client := providerHTTPClient(c.client)
	response, err := client.Do(httpRequest)
	if err != nil {
		return nil, providerTransportError(ctx, err)
	}
	if response.StatusCode >= 300 {
		defer response.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if !jsonMode && c.protocol == "openai" && len(request.Tools) == 0 && request.ResponseSchema != nil && request.ResponseSchema.AllowJSONMode && unsupportedJSONSchema(response.StatusCode, data) {
			// The provider explicitly rejected the request before generation. This
			// is format negotiation, never replay of a generated action or a tool.
			_ = response.Body.Close()
			return c.stream(ctx, request, true)
		}
		return nil, providerResponseError(response.StatusCode, data)
	}
	events := make(chan modelruntime.Event, 16)
	go c.readSSE(ctx, response.Body, events)
	return events, nil
}

func unsupportedJSONSchema(status int, body []byte) bool {
	if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
		return false
	}
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Param   string `json:"param"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return false
	}
	message := strings.ToLower(envelope.Error.Message)
	if strings.Contains(message, "invalid schema") || strings.Contains(message, "invalid json schema") || strings.Contains(message, "invalid json_schema") || strings.Contains(message, "schema validation") {
		return false
	}
	format := strings.Contains(message, "json_schema") || envelope.Error.Param == "response_format"
	unsupported := strings.Contains(message, "not supported") || strings.Contains(message, "unsupported") || strings.Contains(message, "does not support") || strings.Contains(message, "supported values")
	return format && unsupported
}

// Provider requests carry credentials and private conversation content. Never
// replay them at a redirect target, including redirects on the same host.
// Copy injected clients so this policy does not mutate a shared client.
func providerHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &copy
}

func (c *httpClient) readSSE(ctx context.Context, body io.ReadCloser, output chan<- modelruntime.Event) {
	defer close(output)
	defer body.Close()
	stopClose := context.AfterFunc(ctx, func() { _ = body.Close() })
	defer stopClose()
	emit := func(event modelruntime.Event) bool {
		if ctx.Err() != nil {
			return false
		}
		select {
		case output <- event:
			return true
		case <-ctx.Done():
			return false
		}
	}
	usage := usageAccumulator{provider: c.protocol}
	completed := func() bool {
		snapshot := usage.snapshot()
		return emit(modelruntime.Event{Type: "MODEL_COMPLETED", Usage: snapshot.Raw, NormalizedUsage: snapshot})
	}
	finished := false
	ended := false
	dispatch := func(data string) bool {
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			ended = true
			completed()
			return false
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(data), &raw); err != nil || raw == nil {
			emit(modelruntime.Event{Type: "RUN_FAILED", Err: providerProtocolError(err)})
			return false
		}
		if c.protocol == "anthropic" {
			if raw["type"] == "message_stop" {
				ended = true
				completed()
				return false
			}
			if raw["type"] == "error" {
				emit(modelruntime.Event{Type: "RUN_FAILED", Err: providerError(0, raw)})
				return false
			}
			event := parseAnthropic(raw)
			if raw["type"] == "message_start" {
				message, _ := raw["message"].(map[string]any)
				if usage.observe(message["usage"]) {
					event.NormalizedUsage = usage.snapshot()
					event.Usage = event.NormalizedUsage.Raw
				}
			} else if usage.observe(raw["usage"]) {
				event.NormalizedUsage = usage.snapshot()
				event.Usage = event.NormalizedUsage.Raw
			}
			return emit(event)
		}
		if _, ok := raw["error"]; ok {
			emit(modelruntime.Event{Type: "RUN_FAILED", Err: providerError(0, raw)})
			return false
		}
		choices, _ := raw["choices"].([]any)
		if len(choices) > 0 {
			choice, _ := choices[0].(map[string]any)
			if valueString(choice["finish_reason"]) != "" {
				finished = true
			}
		}
		event := parseOpenAI(raw)
		if usage.observe(raw["usage"]) {
			event.NormalizedUsage = usage.snapshot()
			event.Usage = event.NormalizedUsage.Raw
		}
		return emit(event)
	}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 2<<20)
	var dataLines []string
	dataBytes := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(dataLines) > 0 && !dispatch(strings.Join(dataLines, "\n")) {
				return
			}
			dataLines, dataBytes = nil, 0
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")
			dataBytes += len(data) + 1
			if dataBytes > 2<<20 {
				emit(modelruntime.Event{Type: "RUN_FAILED", Err: providerProtocolError(nil)})
				return
			}
			dataLines = append(dataLines, data)
		}
	}
	if ctx.Err() != nil {
		// A cancelled consumer may no longer drain; never block or report a
		// successful completion merely because a finish_reason arrived earlier.
		select {
		case output <- modelruntime.Event{Type: "RUN_FAILED", Err: providerTransportError(ctx, ctx.Err())}:
		default:
		}
		return
	}
	if err := scanner.Err(); err != nil {
		emit(modelruntime.Event{Type: "RUN_FAILED", Err: providerTransportError(ctx, err)})
		return
	}
	if len(dataLines) > 0 && !dispatch(strings.Join(dataLines, "\n")) {
		return
	}
	if finished && !ended {
		// Some compatible providers close after finish_reason without [DONE].
		completed()
	} else if !ended {
		emit(modelruntime.Event{Type: "RUN_FAILED", Err: providerTransportError(ctx, io.ErrUnexpectedEOF)})
	}
}

func openAIPayload(request modelruntime.Request) map[string]any {
	messages := make([]map[string]any, 0, len(request.Messages)+1)
	var pendingImages []toolImage
	flushImages := func() {
		if len(pendingImages) == 0 {
			return
		}
		parts := make([]any, 0, len(pendingImages)+1)
		parts = append(parts, map[string]any{"type": "text", "text": "The read tool returned the following image attachment(s)."})
		for _, image := range pendingImages {
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageDataURL(image)}})
		}
		messages = append(messages, map[string]any{"role": "user", "content": parts})
		pendingImages = nil
	}
	if request.System != "" {
		messages = append(messages, map[string]any{"role": "system", "content": request.System})
	}
	for _, message := range request.Messages {
		if message.Role != "tool" {
			flushImages()
		}
		item := map[string]any{"role": message.Role, "content": message.Content}
		if message.ToolCallID != "" {
			item["tool_call_id"] = message.ToolCallID
		}
		if message.Role == "tool" {
			text, images := decodeToolResult(message.Content)
			if len(images) > 0 {
				item["content"] = text
				pendingImages = append(pendingImages, images...)
			}
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
	flushImages()
	payload := map[string]any{"model": request.Model, "messages": messages, "stream": true, "stream_options": map[string]bool{"include_usage": true}}
	if request.ResponseSchema != nil {
		payload["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": request.ResponseSchema.Name, "strict": true, "schema": request.ResponseSchema.Schema}}
	}
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
			messages = append(messages, map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": message.ToolCallID, "content": anthropicToolResultContent(message.Content)}}})
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
	payload := map[string]any{"model": request.Model, "system": request.System, "messages": messages, "max_tokens": anthropicMaxTokens(request.MaxTokens), "stream": true}
	if request.ResponseSchema != nil {
		payload["output_config"] = map[string]any{"format": map[string]any{"type": "json_schema", "schema": request.ResponseSchema.Schema}}
	}
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
	usage := usageAccumulator{provider: "openai"}
	if usage.observe(raw["usage"]) {
		event.NormalizedUsage = usage.snapshot()
		event.Usage = event.NormalizedUsage.Raw
	}
	choices, _ := raw["choices"].([]any)
	if len(choices) == 0 {
		return event
	}
	choice, _ := choices[0].(map[string]any)
	delta, _ := choice["delta"].(map[string]any)
	event.Delta, _ = delta["content"].(string)
	if calls, _ := delta["tool_calls"].([]any); len(calls) > 0 {
		for _, rawCall := range calls {
			call, _ := rawCall.(map[string]any)
			function, _ := call["function"].(map[string]any)
			event.ToolCalls = append(event.ToolCalls, &modelruntime.ToolCall{ID: valueString(call["id"]), Name: valueString(function["name"]), Arguments: json.RawMessage(valueString(function["arguments"])), Index: valueInt(call["index"])})
		}
		event.ToolCall = event.ToolCalls[0]
	}
	return event
}

func parseAnthropic(raw map[string]any) modelruntime.Event {
	event := modelruntime.Event{Type: "MODEL_DELTA"}
	usage := usageAccumulator{provider: "anthropic"}
	value := raw["usage"]
	if raw["type"] == "message_start" {
		message, _ := raw["message"].(map[string]any)
		value = message["usage"]
	}
	if usage.observe(value) {
		event.NormalizedUsage = usage.snapshot()
		event.Usage = event.NormalizedUsage.Raw
	}
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

func anthropicMaxTokens(explicit int) int {
	if explicit > 0 {
		return explicit
	}
	// This protocol requires max_tokens. Other adapters omit an unset limit.
	return 4096
}

func valueString(value any) string { result, _ := value.(string); return result }
func valueInt(value any) int       { number, _ := value.(float64); return int(number) }
