package sandbox

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestServiceRequiresTokenExceptHealth(t *testing.T) {
	handler := NewServiceHandler(NewDockerProvider(""), "0123456789abcdef0123456789abcdef").Router()

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d", health.Code)
	}

	protected := httptest.NewRecorder()
	handler.ServeHTTP(protected, httptest.NewRequest(http.MethodGet, "/v1/sandboxes/test", nil))
	if protected.Code != http.StatusUnauthorized {
		t.Fatalf("protected status = %d, want %d", protected.Code, http.StatusUnauthorized)
	}
}

func TestTerminalRejectsOversizedMessage(t *testing.T) {
	terminal := &boundedTestTerminal{closed: make(chan struct{}), inputs: make(chan string, 2)}
	server := httptest.NewServer(NewServiceHandler(&terminalTestProvider{terminal: terminal}, "fixture-service-token").Router())
	defer server.Close()
	headers := http.Header{"Authorization": []string{"Bearer fixture-service-token"}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/sandboxes/test/terminal", headers)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(terminalMessage{Type: "input", Data: "echo hello\n"}); err != nil {
		t.Fatal(err)
	}
	select {
	case input := <-terminal.inputs:
		if input != "echo hello\n" {
			t.Fatalf("input = %q", input)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("normal terminal input was not delivered")
	}
	if err := conn.WriteJSON(terminalMessage{Type: "input", Data: strings.Repeat("x", 1<<20)}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = conn.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
		t.Fatalf("oversized message must close with 1009, got %v", err)
	}
	select {
	case input := <-terminal.inputs:
		t.Fatalf("oversized input reached the terminal (%d bytes)", len(input))
	case <-terminal.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal was not closed")
	}
}

type terminalTestProvider struct {
	Provider
	terminal TerminalSession
}

func (p *terminalTestProvider) OpenTerminal(context.Context, string, string) (TerminalSession, error) {
	return p.terminal, nil
}

type boundedTestTerminal struct {
	closed chan struct{}
	inputs chan string
	once   sync.Once
}

func (t *boundedTestTerminal) Read([]byte) (int, error) { <-t.closed; return 0, io.EOF }
func (t *boundedTestTerminal) Write(data []byte) (int, error) {
	t.inputs <- string(data)
	return len(data), nil
}
func (t *boundedTestTerminal) Resize(context.Context, int, int) error { return nil }
func (t *boundedTestTerminal) Close() error                           { t.once.Do(func() { close(t.closed) }); return nil }

func TestTerminalPreservesSplitUTF8AndClosesAtEOF(t *testing.T) {
	terminal := &chunkedTerminal{chunks: [][]byte{{0xe4}, {0xb8, 0xad, 0xf0, 0x9f}, {0x98, 0x80, '\n'}}}
	server := httptest.NewServer(NewServiceHandler(&terminalTestProvider{terminal: terminal}, "fixture-service-token").Router())
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/sandboxes/test/terminal", http.Header{"Authorization": {"Bearer fixture-service-token"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var output string
	for {
		var message terminalMessage
		err = conn.ReadJSON(&message)
		if err != nil {
			break
		}
		if message.Type == "output" {
			output += message.Data
		}
	}
	if output != "中😀\n" {
		t.Fatalf("UTF-8 output = %q", output)
	}
	if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		t.Fatalf("shell EOF must close WebSocket normally: %v", err)
	}
}

type chunkedTerminal struct{ chunks [][]byte }

func (t *chunkedTerminal) Read(data []byte) (int, error) {
	if len(t.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(data, t.chunks[0])
	t.chunks = t.chunks[1:]
	return n, nil
}
func (*chunkedTerminal) Write(data []byte) (int, error)         { return len(data), nil }
func (*chunkedTerminal) Resize(context.Context, int, int) error { return nil }
func (*chunkedTerminal) Close() error                           { return nil }

func TestServiceEditFileUsesProvider(t *testing.T) {
	provider := &editFileProvider{result: &FileEditResult{OK: true, Replacements: 2, SHA256: "abc123"}}
	handler := NewServiceHandler(provider, "0123456789abcdef0123456789abcdef").Router()
	request := httptest.NewRequest(http.MethodPatch, "/v1/sandboxes/test/files/content?work_dir=%2Fworkspace%2Fconversations%2Ftest&path=notes.txt", strings.NewReader(`{"old_string":"red","new_string":"green","replace_all":true}`))
	request.Header.Set("Authorization", "Bearer 0123456789abcdef0123456789abcdef")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if provider.id != "test" || provider.workDir != "/workspace/conversations/test" || provider.path != "notes.txt" || !provider.request.ReplaceAll {
		t.Fatalf("provider call = %#v", provider)
	}
	var result FileEditResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Replacements != 2 || result.SHA256 != "abc123" {
		t.Fatalf("result = %#v, %v", result, err)
	}
}

type editFileProvider struct {
	Provider
	id, workDir, path string
	request           FileEditRequest
	result            *FileEditResult
}

func (p *editFileProvider) EditFile(_ context.Context, id, workDir, path string, request FileEditRequest) (*FileEditResult, error) {
	p.id, p.workDir, p.path, p.request = id, workDir, path, request
	return p.result, nil
}
