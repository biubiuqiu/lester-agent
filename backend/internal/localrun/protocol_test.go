//go:build !windows

package localrun

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
)

type protocolCapture struct {
	frames chan map[string]json.RawMessage
	mu     sync.Mutex
}

func newProtocolCapture() *protocolCapture {
	return &protocolCapture{frames: make(chan map[string]json.RawMessage, 256)}
}
func (w *protocolCapture) Write(raw []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var frame map[string]json.RawMessage
	if err := json.Unmarshal(raw, &frame); err != nil {
		return 0, err
	}
	w.frames <- frame
	return len(raw), nil
}
func nextFrame(t *testing.T, w *protocolCapture) map[string]json.RawMessage {
	t.Helper()
	select {
	case frame := <-w.frames:
		return frame
	case <-time.After(3 * time.Second):
		t.Fatal("RPC frame missing")
		return nil
	}
}
func frameText(frame map[string]json.RawMessage, key string) string {
	var result string
	_ = json.Unmarshal(frame[key], &result)
	return result
}
func writeFrame(t *testing.T, w io.Writer, frame any) {
	t.Helper()
	raw, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
}
func rpcRequest(t *testing.T, input io.Writer, output *protocolCapture, id, method string, params any) map[string]json.RawMessage {
	t.Helper()
	writeFrame(t, input, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	frame := nextFrame(t, output)
	if frameText(frame, "id") != id {
		t.Fatalf("response identity: %s", frame["id"])
	}
	return frame
}

func TestStdioUnicodeBoundedFramesAndGenericErrors(t *testing.T) {
	output := newProtocolCapture()
	server := NewServer(output, nil)
	input, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Serve(input) }()
	state := t.TempDir()
	frame := rpcRequest(t, writer, output, "init", "initialize", map[string]string{"state_dir": state, "profile_id": "用户 ✓", "device_id": "device"})
	if len(frame["error"]) != 0 {
		t.Fatalf("initialize: %s", frame["error"])
	}
	// Deliberately split every UTF-8 code point across input reads.
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "unicode", "method": "project.open", "params": map[string]string{"name": "中文 🌎 café", "path": t.TempDir()}})
	for _, b := range append(raw, '\n') {
		if _, err := writer.Write([]byte{b}); err != nil {
			t.Fatal(err)
		}
	}
	frame = nextFrame(t, output)
	var project Project
	if json.Unmarshal(frame["result"], &project) != nil || project.Name != "中文 🌎 café" {
		t.Fatalf("Unicode corruption: %s", frame["result"])
	}
	_, _ = writer.Write([]byte("{\"jsonrpc\":\"2.0\",\"id\":\"bad\",\"method\":\"x\",\"params\":\"\xff\"}\n"))
	frame = nextFrame(t, output)
	if len(frame["error"]) == 0 {
		t.Fatal("invalid UTF-8 accepted")
	}
	frame = rpcRequest(t, writer, output, "secret", "configureModels", map[string]any{"models": []ModelConfig{{ID: "invalid", Source: "BAD", Credential: "DO-NOT-ECHO-PRIVATE-KEY"}}})
	encoded, _ := json.Marshal(frame)
	if len(frame["error"]) == 0 || strings.Contains(string(encoded), "DO-NOT-ECHO") {
		t.Fatalf("unsafe error: %s", encoded)
	}
	_ = writer.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stdio EOF did not close runner")
	}
	assertNoPrivateBytes(t, state, "DO-NOT-ECHO-PRIVATE-KEY")
	if _, err := readRPCLine(bufio.NewReader(strings.NewReader(strings.Repeat("x", MaxRPCFrame) + "\n"))); err == nil {
		t.Fatal("oversized frame accepted")
	}
	if _, err := readRPCLine(bufio.NewReader(strings.NewReader(`{"jsonrpc":"2.0"}`))); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("unterminated frame accepted: %v", err)
	}
}

func TestCloudModelBridgePreservesInterleavedToolFragmentsAndCancellation(t *testing.T) {
	output := newProtocolCapture()
	server := NewServer(output, nil)
	defer server.Close()
	client := &cloudClient{server: server, modelID: "cloud:123e4567-e89b-42d3-a456-426614174000"}
	stream, err := client.Stream(context.Background(), modelruntime.Request{Model: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	request := nextFrame(t, output)
	if frameText(request, "method") != "cloud.model.stream" {
		t.Fatalf("unexpected reverse method: %s", request["method"])
	}
	id := frameText(request, "id")
	var params struct {
		ModelID string `json:"model_id"`
	}
	_ = json.Unmarshal(request["params"], &params)
	if strings.HasPrefix(params.ModelID, "cloud:") {
		t.Fatal("cloud virtual key reached gateway")
	}
	events := []WireEvent{
		{Type: "TEXT_DELTA", Delta: "你好 ✓"},
		{Type: "TOOL_CALL_DELTA", ToolCalls: []*WireToolCall{{ID: "first", Name: "read", Index: 0, Arguments: `{"path":`}, {ID: "second", Name: "read", Index: 1, Arguments: `{"path":`}}},
		{Type: "TOOL_CALL_DELTA", ToolCalls: []*WireToolCall{{Index: 1, Arguments: `"two"}`}, {Index: 0, Arguments: `"one"}`}}},
	}
	for _, event := range events {
		raw, err := json.Marshal(map[string]any{"request_id": id, "event": event})
		if err != nil {
			t.Fatal(err)
		}
		server.receiveCloudEvent(raw)
	}
	idRaw, _ := json.Marshal(id)
	server.receiveModelResponse(rpcFrame{ID: idRaw, Result: []byte(`{"done":true}`)})
	var assembler modelruntime.ToolCallAssembler
	var text string
	for event := range stream {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		text += event.Delta
		assembler.AddEvent(event)
	}
	calls := assembler.Calls()
	if text != "你好 ✓" || len(calls) != 2 || string(calls[0].Arguments) != `{"path":"one"}` || string(calls[1].Arguments) != `{"path":"two"}` {
		t.Fatalf("wire lost fragments: %q %+v", text, calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stream, err = client.Stream(ctx, modelruntime.Request{})
	if err != nil {
		t.Fatal(err)
	}
	request = nextFrame(t, output)
	cancel()
	for range stream {
	}
	cancelFrame := nextFrame(t, output)
	if frameText(cancelFrame, "method") != "cloud.model.cancel" {
		t.Fatalf("missing reverse cancellation: %s", cancelFrame["method"])
	}
}

func TestLocalModelBridgeOnlyInfersAndStreamsSnakeCaseWire(t *testing.T) {
	output := newProtocolCapture()
	calls := 0
	client := &fixtureClient{stream: func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error) {
		calls++
		return fixtureEvents(modelruntime.Event{Type: "TOOL_CALL_DELTA", ToolCall: &modelruntime.ToolCall{ID: "local-call", Index: 3, Name: "bash", Arguments: []byte(`{"command":`)}}, modelruntime.Event{Type: "TOOL_CALL_DELTA", ToolCall: &modelruntime.ToolCall{Index: 3, Arguments: []byte(`"touch should-not-execute"}`)}}), nil
	}, generate: func(context.Context, modelruntime.Request) (*modelruntime.Response, error) {
		return &modelruntime.Response{Content: "answer", ToolCalls: []modelruntime.ToolCall{{ID: "tool", Name: "bash", Arguments: []byte(`{"command":"touch never"}`)}}}, nil
	}}
	server := NewServer(output, func(context.Context, ModelConfig) (modelruntime.Client, error) { return client, nil })
	input, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Serve(input) }()
	defer func() { _ = writer.Close(); <-done }()
	frame := rpcRequest(t, writer, output, "init", "initialize", map[string]string{"state_dir": t.TempDir(), "profile_id": "profile", "device_id": "device"})
	if len(frame["error"]) > 0 {
		t.Fatal(string(frame["error"]))
	}
	frame = rpcRequest(t, writer, output, "models", "configureModels", map[string]any{"models": []ModelConfig{{ID: "local", Source: "local", ModelID: "provider-model"}, {ID: "cloud:123e4567-e89b-42d3-a456-426614174000", Source: "cloud"}}})
	if len(frame["error"]) > 0 {
		t.Fatal(string(frame["error"]))
	}
	writeFrame(t, writer, map[string]any{"jsonrpc": "2.0", "id": "stream-call", "method": "models.stream", "params": map[string]any{"id": "inference-1", "model_id": "local", "request": modelruntime.Request{Model: "untrusted-override"}}})
	for index := 0; index < 2; index++ {
		frame = nextFrame(t, output)
		if frameText(frame, "method") != "model.event" {
			t.Fatalf("event method: %s", frame["method"])
		}
		var p struct {
			ID    string    `json:"id"`
			Event WireEvent `json:"event"`
		}
		if json.Unmarshal(frame["params"], &p) != nil || p.ID != "inference-1" || p.Event.ToolCall.Index != 3 || p.Event.ToolCall.Arguments == "" {
			t.Fatalf("invalid wire event: %s", frame["params"])
		}
	}
	frame = nextFrame(t, output)
	if frameText(frame, "id") != "stream-call" || len(frame["result"]) == 0 || calls != 1 {
		t.Fatal("stream response missing")
	}
	frame = rpcRequest(t, writer, output, "gen", "models.generate", map[string]any{"model_id": "local", "request": modelruntime.Request{}})
	var response WireResponse
	if json.Unmarshal(frame["result"], &response) != nil || len(response.ToolCalls) != 1 || response.Content != "answer" {
		t.Fatalf("generate response: %s", frame["result"])
	}
	frame = rpcRequest(t, writer, output, "no-loop", "models.generate", map[string]any{"model_id": "cloud:123e4567-e89b-42d3-a456-426614174000", "request": modelruntime.Request{}})
	if len(frame["error"]) == 0 {
		t.Fatal("cloud model exposed through local reverse inference")
	}
	server.mu.Lock()
	r := server.runner
	server.mu.Unlock()
	projects, err := r.Store.Projects(context.Background())
	if err != nil || len(projects) != 0 {
		t.Fatal("pure inference created task/project")
	}
}

func TestStdioModelCancellationStopsProviderAndAllowsFurtherReads(t *testing.T) {
	output := newProtocolCapture()
	started := make(chan struct{})
	stopped := make(chan struct{})
	client := &fixtureClient{stream: func(ctx context.Context, _ modelruntime.Request) (<-chan modelruntime.Event, error) {
		close(started)
		events := make(chan modelruntime.Event)
		go func() { <-ctx.Done(); close(stopped); close(events) }()
		return events, nil
	}}
	server := NewServer(output, func(context.Context, ModelConfig) (modelruntime.Client, error) { return client, nil })
	input, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- server.Serve(input) }()
	defer func() { _ = writer.Close(); <-done }()
	frame := rpcRequest(t, writer, output, "init", "initialize", map[string]string{"state_dir": t.TempDir(), "profile_id": "profile", "device_id": "device"})
	if len(frame["error"]) > 0 {
		t.Fatal(string(frame["error"]))
	}
	frame = rpcRequest(t, writer, output, "models", "configureModels", map[string]any{"models": []ModelConfig{{ID: "local", Source: "local"}}})
	if len(frame["error"]) > 0 {
		t.Fatal(string(frame["error"]))
	}
	writeFrame(t, writer, map[string]any{"jsonrpc": "2.0", "id": "long-inference", "method": "models.stream", "params": map[string]any{"id": "cancel-this", "model_id": "local", "request": modelruntime.Request{}}})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("inference did not start")
	}
	writeFrame(t, writer, map[string]any{"jsonrpc": "2.0", "method": "models.cancel", "params": map[string]string{"id": "cancel-this"}})
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("provider did not receive cancellation")
	}
	frame = nextFrame(t, output)
	if frameText(frame, "id") != "long-inference" || len(frame["error"]) == 0 {
		t.Fatalf("cancelled inference response: %v", frame)
	}
	frame = rpcRequest(t, writer, output, "still-readable", "project.list", map[string]any{})
	if string(frame["result"]) != "[]" {
		t.Fatalf("stdio blocked after model cancellation: %s", frame["result"])
	}
}
