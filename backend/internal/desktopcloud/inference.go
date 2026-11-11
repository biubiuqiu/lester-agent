package desktopcloud

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/google/uuid"
)

const (
	maxRequestBytes  = 2 << 20
	maxResponseBytes = 8 << 20
	maxInferenceTime = 5 * time.Minute
	maxPeerCalls     = 8
)

func (s *Service) acquire() (func(), error) {
	select {
	case s.gateway <- struct{}{}:
		return func() { <-s.gateway }, nil
	default:
		return nil, &modelruntime.ProviderError{Kind: modelruntime.ErrorRateLimit}
	}
}
func validateRequest(req modelruntime.Request) error {
	if len(req.Messages) > 512 || len(req.Tools) > 64 || req.MaxTokens < 0 || int64(req.MaxTokens) > 2147483647 || (req.Temperature != nil && (math.IsNaN(*req.Temperature) || math.IsInf(*req.Temperature, 0) || *req.Temperature < 0 || *req.Temperature > 2)) {
		return errors.New("model request exceeds desktop limits")
	}
	for _, t := range req.Tools {
		if len(t.Name) < 1 || len(t.Name) > 256 || len(t.Description) > 65536 {
			return errors.New("invalid model tool schema")
		}
	}
	for _, m := range req.Messages {
		switch m.Role {
		case "user", "assistant", "tool", "system":
		default:
			return errors.New("invalid model message role")
		}
		if len(m.ToolCalls) > 64 {
			return errors.New("too many model tool calls")
		}
	}
	raw, err := json.Marshal(req)
	if err != nil || len(raw) > maxRequestBytes {
		return errors.New("model request exceeds desktop size limit")
	}
	return nil
}
func safeKind(kind modelruntime.ErrorKind) modelruntime.ErrorKind {
	switch kind {
	case modelruntime.ErrorContextExceeded, modelruntime.ErrorAuthentication, modelruntime.ErrorRateLimit, modelruntime.ErrorCancelled, modelruntime.ErrorTransport, modelruntime.ErrorProtocol, modelruntime.ErrorProvider:
		return kind
	}
	return modelruntime.ErrorProvider
}
func toWireCall(call *modelruntime.ToolCall) *WireToolCall {
	if call == nil {
		return nil
	}
	return &WireToolCall{ID: call.ID, Name: call.Name, Arguments: string(call.Arguments), Index: call.Index}
}
func fromWireCall(call *WireToolCall) *modelruntime.ToolCall {
	if call == nil {
		return nil
	}
	return &modelruntime.ToolCall{ID: call.ID, Name: call.Name, Arguments: json.RawMessage(call.Arguments), Index: call.Index}
}
func toWireEvent(ev modelruntime.Event) WireEvent {
	result := WireEvent{Type: ev.Type, Delta: ev.Delta, ToolCall: toWireCall(ev.ToolCall), Usage: ev.Usage, NormalizedUsage: ev.NormalizedUsage}
	for _, call := range ev.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, toWireCall(call))
	}
	if ev.Err != nil {
		result.Type = "RUN_FAILED"
		result.ErrorKind = safeKind(modelruntime.ErrorKindOf(ev.Err))
	}
	return result
}
func fromWireEvent(ev WireEvent) modelruntime.Event {
	result := modelruntime.Event{Type: ev.Type, Delta: ev.Delta, ToolCall: fromWireCall(ev.ToolCall), Usage: ev.Usage, NormalizedUsage: ev.NormalizedUsage}
	for _, call := range ev.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, fromWireCall(call))
	}
	if ev.ErrorKind != "" {
		result.Err = &modelruntime.ProviderError{Kind: safeKind(ev.ErrorKind)}
	} else if ev.Type == "RUN_FAILED" || ev.Type == "error" {
		result.Err = &modelruntime.ProviderError{Kind: modelruntime.ErrorProtocol}
	}

	return result
}
func toWireResponse(r *modelruntime.Response) *WireResponse {
	if r == nil {
		return nil
	}
	return &WireResponse{Content: r.Content, ToolCalls: r.ToolCalls, Usage: r.Usage, NormalizedUsage: r.NormalizedUsage}
}
func validUsage(usage map[string]int, u *modelruntime.NormalizedUsage) bool {
	if len(usage) > 64 {
		return false
	}
	for k, v := range usage {
		if len(k) > 100 || v < 0 {
			return false
		}
	}
	if u != nil {
		if len(u.Provider) > 64 || len(u.Raw) > 64 || u.InputTokens < 0 || u.OutputTokens < 0 || u.CacheReadInputTokens < 0 || u.CacheCreationInputTokens < 0 {
			return false
		}
		if u.Source != modelruntime.UsageActual && u.Source != modelruntime.UsageEstimated && u.Source != modelruntime.UsageMissing {
			return false
		}
		return validUsage(u.Raw, nil)
	}
	return true
}
func validEvent(ev WireEvent) bool {
	switch ev.Type {
	case "MODEL_DELTA", "MODEL_COMPLETED", "RUN_FAILED", "text_delta", "tool_call_delta", "tool_call", "tool_calls", "usage", "done", "error", "content_delta", "delta":
	default:
		return false
	}
	if len(ev.Delta) > maxRequestBytes || len(ev.ToolCalls) > 64 || !validUsage(ev.Usage, ev.NormalizedUsage) {
		return false
	}
	calls := append([]*WireToolCall{}, ev.ToolCalls...)
	if ev.ToolCall != nil {
		calls = append(calls, ev.ToolCall)
	}
	for _, call := range calls {
		if call == nil || len(call.ID) > 256 || len(call.Name) > 256 || len(call.Arguments) > maxRequestBytes || call.Index < 0 || call.Index >= 64 {
			return false
		}
	}
	return ev.ErrorKind == "" || safeKind(ev.ErrorKind) == ev.ErrorKind
}
func validResponse(r *modelruntime.Response) bool {
	if r == nil || len(r.Content) > maxResponseBytes || len(r.ToolCalls) > 64 || !validUsage(r.Usage, r.NormalizedUsage) {
		return false
	}
	for _, call := range r.ToolCalls {
		if len(call.ID) > 256 || len(call.Name) > 256 || len(call.Arguments) > maxRequestBytes || !json.Valid(call.Arguments) {
			return false
		}
	}
	raw, err := json.Marshal(toWireResponse(r))
	return err == nil && len(raw) <= maxResponseBytes
}

type desktopClient struct {
	s                        *Service
	workspace, owner, device uuid.UUID
	key                      string
}

func (s *Service) DesktopClient(ctx context.Context, workspace, owner, device uuid.UUID, key string) (model.ModelClient, error) {
	actor, ok := modelruntime.Actor(ctx)
	if !ok || actor != owner || !validKey(key) {
		return nil, errDenied
	}
	return &desktopClient{s: s, workspace: workspace, owner: owner, device: device, key: key}, nil
}
func (c *desktopClient) Capabilities(context.Context, string) (modelruntime.Capabilities, error) {
	return modelruntime.Capabilities{Streaming: true, Tools: true, Vision: true}, nil
}
func (c *desktopClient) start(ctx context.Context, req modelruntime.Request, stream bool) (*peer, string, <-chan Frame, context.Context, context.CancelFunc, error) {
	actor, ok := modelruntime.Actor(ctx)
	if !ok || actor != c.owner {
		return nil, "", nil, nil, nil, errDenied
	}
	if err := validateRequest(req); err != nil {
		return nil, "", nil, nil, nil, err
	}
	release, err := c.s.acquire()
	if err != nil {
		return nil, "", nil, nil, nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, maxInferenceTime)
	fail := func(err error) (*peer, string, <-chan Frame, context.Context, context.CancelFunc, error) {
		cancel()
		release()
		return nil, "", nil, nil, nil, err
	}
	var generation int64
	err = c.s.DB.QueryRow(bounded, `SELECT d.generation FROM desktop_devices d JOIN sessions s ON s.id=d.session_id JOIN users u ON u.id=d.user_id JOIN desktop_device_models dm ON dm.device_id=d.id JOIN model_deployments md ON md.id=dm.deployment_id WHERE dm.model_key=$4 AND md.enabled AND d.id=$1 AND d.workspace_id=$2 AND d.user_id=$3 AND d.revoked_at IS NULL AND d.token_expires_at>now() AND s.expires_at>now() AND NOT u.disabled AND (NOT u.email_verification_required OR u.email_verified) AND EXISTS(SELECT 1 FROM workspace_members WHERE workspace_id=d.workspace_id AND user_id=d.user_id)`, c.device, c.workspace, c.owner, c.key).Scan(&generation)
	if err != nil {
		return fail(ErrUnavailable)
	}
	c.s.mu.Lock()
	p := c.s.peers[c.device]
	c.s.mu.Unlock()
	if p == nil || p.device.Generation != generation {
		return fail(ErrUnavailable)
	}
	id := uuid.NewString()
	frames := make(chan Frame, 64)
	p.mu.Lock()
	if len(p.calls) >= maxPeerCalls {
		p.mu.Unlock()
		return fail(&modelruntime.ProviderError{Kind: modelruntime.ErrorRateLimit})
	}
	p.calls[id] = &pendingCall{frames: frames}
	p.mu.Unlock()
	finish := func() { cancel(); release(); p.mu.Lock(); delete(p.calls, id); p.mu.Unlock() }
	// The registered call belongs to this exact peer. Replacement peers receive
	// no in-flight work and completion frames never migrate between sockets.
	req.Model = c.key
	if err = p.send(Frame{Type: "model.request", ID: id, ModelKey: c.key, Stream: stream, Request: &req}); err != nil {
		finish()
		return nil, "", nil, nil, nil, ErrUnavailable
	}
	return p, id, frames, bounded, finish, nil
}
func (c *desktopClient) Generate(ctx context.Context, req modelruntime.Request) (*modelruntime.Response, error) {
	p, id, frames, bounded, finish, err := c.start(ctx, req, false)
	if err != nil {
		return nil, err
	}
	defer finish()
	for {
		select {
		case <-bounded.Done():
			_ = p.send(Frame{Type: "model.cancel", ID: id})
			return nil, bounded.Err()
		case <-p.closed:
			return nil, ErrUnavailable
		case frame := <-frames:
			if frame.Type != "model.done" {
				return nil, &modelruntime.ProviderError{Kind: modelruntime.ErrorProtocol}
			}
			if frame.ErrorKind != "" {
				return nil, &modelruntime.ProviderError{Kind: safeKind(frame.ErrorKind)}
			}
			if frame.Response == nil {
				return nil, &modelruntime.ProviderError{Kind: modelruntime.ErrorProtocol}
			}
			r := &modelruntime.Response{Content: frame.Response.Content, ToolCalls: frame.Response.ToolCalls, Usage: frame.Response.Usage, NormalizedUsage: frame.Response.NormalizedUsage}
			if !validResponse(r) {
				return nil, &modelruntime.ProviderError{Kind: modelruntime.ErrorProtocol}
			}
			return r, nil
		}
	}
}
func (c *desktopClient) Stream(ctx context.Context, req modelruntime.Request) (<-chan modelruntime.Event, error) {
	p, id, frames, bounded, finish, err := c.start(ctx, req, true)
	if err != nil {
		return nil, err
	}
	events := make(chan modelruntime.Event, 8)
	go func() {
		defer close(events)
		defer finish()
		bytes := 0
		completed := false
		emit := func(ev modelruntime.Event) bool {
			select {
			case events <- ev:
				return true
			case <-bounded.Done():
				return false
			case <-p.closed:
				return false
			}
		}
		terminal := func(err error) {
			select {
			case events <- modelruntime.Event{Type: "RUN_FAILED", Err: err}:
			case <-ctx.Done():
			}
		}
		for {
			select {
			case <-bounded.Done():
				_ = p.send(Frame{Type: "model.cancel", ID: id})
				terminal(bounded.Err())
				return
			case <-p.closed:
				terminal(ErrUnavailable)
				return
			case frame := <-frames:
				if frame.Type == "model.done" {
					if frame.ErrorKind != "" {
						terminal(&modelruntime.ProviderError{Kind: safeKind(frame.ErrorKind)})
					} else if !completed {
						event := modelruntime.Event{Type: "MODEL_COMPLETED"}
						if frame.Response != nil {
							event.Usage = frame.Response.Usage
							event.NormalizedUsage = frame.Response.NormalizedUsage
						}
						select {
						case events <- event:
						case <-ctx.Done():
						}
					}
					return
				}
				if frame.Event == nil || !validEvent(*frame.Event) {
					terminal(&modelruntime.ProviderError{Kind: modelruntime.ErrorProtocol})
					_ = p.send(Frame{Type: "model.cancel", ID: id})
					return
				}
				raw, _ := json.Marshal(frame.Event)
				bytes += len(raw)
				if bytes > maxResponseBytes {
					terminal(&modelruntime.ProviderError{Kind: modelruntime.ErrorProtocol})
					_ = p.send(Frame{Type: "model.cancel", ID: id})
					return
				}
				if !emit(fromWireEvent(*frame.Event)) {
					_ = p.send(Frame{Type: "model.cancel", ID: id})
					if bounded.Err() != nil {
						terminal(bounded.Err())
					} else {
						terminal(ErrUnavailable)
					}
					return
				}
				if frame.Event.Type == "MODEL_COMPLETED" {
					completed = true
				}
				if frame.Event.ErrorKind != "" || frame.Event.Type == "RUN_FAILED" || frame.Event.Type == "error" {
					return
				}
			}
		}
	}()
	return events, nil
}
