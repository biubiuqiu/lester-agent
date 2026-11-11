package localrun

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/google/uuid"
)

// Streaming arguments are strings because an individual JSON fragment is not
// itself valid JSON. Index must survive the bridge for interleaved tool calls.
type WireToolCall struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Index     int    `json:"index"`
}
type WireEvent struct {
	Type            string                        `json:"type"`
	Delta           string                        `json:"delta,omitempty"`
	ToolCall        *WireToolCall                 `json:"tool_call,omitempty"`
	ToolCalls       []*WireToolCall               `json:"tool_calls,omitempty"`
	Usage           map[string]int                `json:"usage,omitempty"`
	NormalizedUsage *modelruntime.NormalizedUsage `json:"normalized_usage,omitempty"`
	ErrorKind       modelruntime.ErrorKind        `json:"error_kind,omitempty"`
}
type WireResponse struct {
	Content         string                        `json:"content"`
	ToolCalls       []modelruntime.ToolCall       `json:"tool_calls,omitempty"`
	Usage           map[string]int                `json:"usage,omitempty"`
	NormalizedUsage *modelruntime.NormalizedUsage `json:"normalized_usage,omitempty"`
}

func wireCall(c *modelruntime.ToolCall) *WireToolCall {
	if c == nil {
		return nil
	}
	return &WireToolCall{ID: c.ID, Name: c.Name, Arguments: string(c.Arguments), Index: c.Index}
}
func (w *WireToolCall) runtime() *modelruntime.ToolCall {
	if w == nil {
		return nil
	}
	return &modelruntime.ToolCall{ID: w.ID, Name: w.Name, Arguments: json.RawMessage(w.Arguments), Index: w.Index}
}
func eventWire(e modelruntime.Event) WireEvent {
	w := WireEvent{Type: e.Type, Delta: e.Delta, ToolCall: wireCall(e.ToolCall), Usage: e.Usage, NormalizedUsage: e.NormalizedUsage, ErrorKind: modelruntime.ErrorKindOf(e.Err)}
	for _, call := range e.ToolCalls {
		w.ToolCalls = append(w.ToolCalls, wireCall(call))
	}
	return w
}
func (w WireEvent) runtime() modelruntime.Event {
	e := modelruntime.Event{Type: w.Type, Delta: w.Delta, ToolCall: w.ToolCall.runtime(), Usage: w.Usage, NormalizedUsage: w.NormalizedUsage}
	for _, call := range w.ToolCalls {
		e.ToolCalls = append(e.ToolCalls, call.runtime())
	}
	if w.ErrorKind != "" {
		e.Err = &modelruntime.ProviderError{Kind: w.ErrorKind}
	}
	return e
}

type remoteCall struct {
	events chan modelruntime.Event
	done   chan error
	once   sync.Once
}

func (c *remoteCall) finish(err error) { c.once.Do(func() { c.done <- err }) }

type cloudClient struct {
	server  *Server
	modelID string
}

func (c *cloudClient) Capabilities(context.Context, string) (modelruntime.Capabilities, error) {
	return modelruntime.Capabilities{Streaming: true, Tools: true, StructuredOutput: true}, nil
}
func (c *cloudClient) Generate(ctx context.Context, request modelruntime.Request) (*modelruntime.Response, error) {
	stream, err := c.Stream(ctx, request)
	if err != nil {
		return nil, err
	}
	response := &modelruntime.Response{}
	var calls modelruntime.ToolCallAssembler
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case event, ok := <-stream:
			if !ok {
				response.ToolCalls = calls.Calls()
				return response, nil
			}
			if event.Err != nil {
				return nil, event.Err
			}
			response.Content += event.Delta
			calls.AddEvent(event)
			if event.Usage != nil {
				response.Usage = event.Usage
			}
			if event.NormalizedUsage != nil {
				response.NormalizedUsage = event.NormalizedUsage
			}
		}
	}
}
func (c *cloudClient) Stream(ctx context.Context, request modelruntime.Request) (<-chan modelruntime.Event, error) {
	id := "runner:model:" + uuid.NewString()
	pending := &remoteCall{events: make(chan modelruntime.Event, 128), done: make(chan error, 1)}
	s := c.server
	s.mu.Lock()
	if len(s.remote) >= 64 || s.closed {
		s.mu.Unlock()
		return nil, errors.New("model bridge is busy")
	}
	s.remote[id] = pending
	s.mu.Unlock()
	if err := s.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": "cloud.model.stream", "params": map[string]any{"model_id": strings.TrimPrefix(c.modelID, "cloud:"), "request": request}}); err != nil {
		s.mu.Lock()
		delete(s.remote, id)
		s.mu.Unlock()
		return nil, err
	}
	out := make(chan modelruntime.Event, 16)
	go func() {
		defer close(out)
		defer func() { s.mu.Lock(); delete(s.remote, id); s.mu.Unlock() }()
		forward := func(event modelruntime.Event) bool {
			select {
			case out <- event:
				return true
			case <-ctx.Done():
				return false
			case <-s.ctx.Done():
				return false
			}
		}
		for {
			select {
			case event := <-pending.events:
				if !forward(event) {
					s.notify("cloud.model.cancel", map[string]string{"request_id": id})
					return
				}
			case err := <-pending.done:
				// Notifications precede the final response on stdio. Drain every frame.
				for {
					select {
					case event := <-pending.events:
						if !forward(event) {
							return
						}
					default:
						if err != nil {
							_ = forward(modelruntime.Event{Type: "error", Err: err})
						}
						return
					}
				}
			case <-ctx.Done():
				s.notify("cloud.model.cancel", map[string]string{"request_id": id})
				return
			case <-s.ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (s *Server) receiveCloudEvent(raw json.RawMessage) {
	var p struct {
		RequestID string    `json:"request_id"`
		Event     WireEvent `json:"event"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return
	}
	s.mu.Lock()
	call := s.remote[p.RequestID]
	s.mu.Unlock()
	if call == nil {
		return
	}
	select {
	case call.events <- p.Event.runtime():
	default:
		call.finish(&modelruntime.ProviderError{Kind: modelruntime.ErrorProtocol})
	}
}

func (s *Server) receiveModelResponse(frame rpcFrame) {
	var id string
	if json.Unmarshal(frame.ID, &id) != nil {
		return
	}
	s.mu.Lock()
	call := s.remote[id]
	s.mu.Unlock()
	if call == nil {
		return
	}
	if len(frame.Error) > 0 && string(frame.Error) != "null" {
		call.finish(&modelruntime.ProviderError{Kind: modelruntime.ErrorProvider})
		return
	}
	if len(frame.Result) == 0 {
		call.finish(&modelruntime.ProviderError{Kind: modelruntime.ErrorProtocol})
		return
	}
	call.finish(nil)
}

func (s *Server) inferLocal(ctx context.Context, runner *Runner, stream bool, raw json.RawMessage) (any, error) {
	var p struct {
		ID      string               `json:"id"`
		ModelID string               `json:"model_id"`
		Request modelruntime.Request `json:"request"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return nil, errors.New("invalid model request")
	}
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if len(p.ID) > 160 {
		return nil, errors.New("invalid model request identity")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.mu.Lock()
	if _, exists := s.inference[p.ID]; exists || len(s.inference) >= 32 {
		s.mu.Unlock()
		return nil, errors.New("model inference is busy")
	}
	s.inference[p.ID] = cancel
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.inference, p.ID); s.mu.Unlock() }()
	client, model, err := runner.Model(ctx, p.ModelID, true)
	if err != nil {
		return nil, err
	}
	p.Request.Model = model.ModelID
	// This path performs inference only. It never invokes the harness or tools.
	if !stream {
		response, err := client.Generate(ctx, p.Request)
		if err != nil || response == nil {
			return nil, errors.New("local model inference failed")
		}
		return WireResponse{Content: response.Content, ToolCalls: response.ToolCalls, Usage: response.Usage, NormalizedUsage: response.NormalizedUsage}, nil
	}
	events, err := client.Stream(ctx, p.Request)
	if err != nil || events == nil {
		return nil, errors.New("local model inference failed")
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case event, ok := <-events:
			if !ok {
				return map[string]any{"done": true, "id": p.ID}, nil
			}
			if err = s.write(map[string]any{"jsonrpc": "2.0", "method": "model.event", "params": map[string]any{"id": p.ID, "event": eventWire(event)}}); err != nil {
				return nil, err
			}
			if event.Err != nil {
				return nil, errors.New("local model inference failed")
			}
		}
	}
}
