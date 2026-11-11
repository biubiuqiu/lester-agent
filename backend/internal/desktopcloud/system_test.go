package desktopcloud

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/google/uuid"
)

// TestHybridDesktopSystem crosses the production Go HTTP/WebSocket gateway,
// Electron main-process bridge, private stdio RPC, shared Harness and provider
// adapter. Opt in because ordinary Go CI does not install desktop's Node deps.
func TestHybridDesktopSystem(t *testing.T) {
	if os.Getenv("LESTER_TEST_HYBRID_SYSTEM") != "1" {
		t.Skip("set LESTER_TEST_HYBRID_SYSTEM=1, LESTER_TEST_DATABASE_URL and install desktop Node dependencies")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("hybrid desktop system test requires Node.js")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
	defer cancel()
	f := newFixture(t)
	f.service.WebOrigin = f.server.URL
	owner := f.identity(t, uuid.Nil)
	device, token := f.register(t, owner)
	const cloudSecret = "hybrid-cloud-fixture-secret"
	const localSecret = "hybrid-local-fixture-secret"
	const reverseArgs = `{"path":"reverse-must-not-exist.txt","content":"must not execute"}`
	var cloudCalls, localCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/chat/completions") || !request.Stream {
			t.Errorf("unexpected provider protocol: %s %s stream=%v", r.Method, r.URL.Path, request.Stream)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := func(delta any) {
			raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
		}
		tool := func(id, arguments string) map[string]any {
			return map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": "write", "arguments": arguments}}}}
		}
		switch request.Model {
		case "cloud-fixture":
			if r.Header.Get("Authorization") != "Bearer "+cloudSecret {
				t.Error("cloud provider credential was not resolved on the server")
			}
			switch cloudCalls.Add(1) {
			case 1:
				chunk(tool("cloud-write", `{"path":"hybrid.txt","content":"Created by local tools through cloud inference.\n","create_only":true}`))
			case 2:
				if len(request.Messages) == 0 || request.Messages[len(request.Messages)-1].Role != "tool" {
					t.Error("local tool receipt did not return across the cloud gateway")
				}
				chunk(map[string]any{"content": "Cloud inference and local tools completed."})
			default:
				t.Error("cloud task or receipt was replayed")
			}
		case "local-fixture":
			localCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer "+localSecret {
				t.Error("local provider credential did not stay on the device")
			}
			if len(request.Tools) != 1 || request.Tools[0].Function.Name != "write" {
				t.Error("cloud model request lost its real tool schema")
			}
			// Deliberately split invalid JSON fragments: both Generate assembly
			// and Stream must preserve the tool arguments through both bridges.
			chunk(tool("reverse-write", `{"path":"reverse-must-not-exist.txt",`))
			chunk(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": `"content":"must not execute"}`}}}})
		default:
			t.Errorf("caller-chosen model escaped resolution: %q", request.Model)
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":17,\"completion_tokens\":9}}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()
	connection, err := f.service.Models.CreateConnection(ctx, owner.p.WorkspaceID, "Hybrid cloud fixture", "openai", provider.URL+"/v1/chat/completions", nil, cloudSecret)
	if err != nil {
		t.Fatal(err)
	}
	cloud, err := f.service.Models.CreateDeployment(ctx, owner.p.WorkspaceID, connection.ID, "Hybrid cloud fixture", "cloud-fixture", false)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	temp := t.TempDir()
	runner := filepath.Join(temp, "desktop-runner")
	build := exec.CommandContext(ctx, "go", "build", "-o", runner, "./cmd/desktop-runner")
	build.Dir = filepath.Join(root, "backend")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build actual desktop runner: %v\n%s", err, output)
	}
	command := exec.CommandContext(ctx, node, filepath.Join(root, "desktop/tests/hybrid-system.mjs"))
	command.Dir = root
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	// Always reap the Node helper (which closes the real sidecar's stdin).
	exited := make(chan struct{})
	var commandErr error
	lines := make(chan []byte, 4)
	go func() {
		scan := bufio.NewScanner(stdout)
		for scan.Scan() {
			lines <- bytes.Clone(scan.Bytes())
		}
		commandErr = command.Wait()
		close(exited)
		close(lines)
	}()
	t.Cleanup(func() {
		_ = stdin.Close()
		cancel()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			t.Error("hybrid helper did not exit after cancellation")
		}
	})
	config := map[string]any{"origin": f.server.URL, "account_id": owner.p.UserID, "workspace_id": owner.p.WorkspaceID,
		"device_id": device.ID, "device_token": token, "session_cookie": owner.cookie.String(), "cloud_model_id": cloud.ID,
		"runner_path": runner, "state_dir": filepath.Join(temp, "state"), "project_dir": filepath.Join(temp, "project"),
		"provider_url": provider.URL + "/v1", "local_credential": localSecret}
	if err = json.NewEncoder(stdin).Encode(config); err != nil {
		t.Fatal(err)
	}
	read := func(want string) map[string]json.RawMessage {
		t.Helper()
		select {
		case raw, ok := <-lines:
			if !ok {
				<-exited
				t.Fatalf("hybrid helper exited before %s:\n%s", want, stderr.String())
			}
			var value map[string]json.RawMessage
			if json.Unmarshal(raw, &value) != nil || string(value["type"]) != fmt.Sprintf("%q", want) {
				t.Fatalf("unexpected hybrid helper frame: %s", raw)
			}
			return value
		case <-ctx.Done():
			t.Fatalf("hybrid system timed out waiting for %s", want)
			return nil
		}
	}
	ready := read("ready")
	var localKey string
	if err = json.Unmarshal(ready["local_model_key"], &localKey); err != nil {
		t.Fatal(err)
	}
	var localID uuid.UUID
	if err = f.db.QueryRow(ctx, `SELECT dm.deployment_id FROM desktop_device_models dm JOIN model_deployments d ON d.id=dm.deployment_id WHERE dm.device_id=$1 AND dm.model_key=$2 AND d.enabled`, device.ID, localKey).Scan(&localID); err != nil {
		t.Fatal("real bridge did not sync shared model:", err)
	}
	client, _, err := f.service.Models.Client(modelruntime.WithActor(ctx, owner.p.UserID), owner.p.WorkspaceID, localID)
	if err != nil {
		t.Fatal(err)
	}
	request := modelruntime.Request{Model: "must-not-override-local-model", Messages: []modelruntime.Message{{Role: "user", Content: "Return a write tool call as inference only."}},
		Tools: []modelruntime.Tool{{Name: "write", Description: "Write a local file", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}}}}}
	response, err := client.Generate(modelruntime.WithActor(ctx, owner.p.UserID), request)
	if err != nil {
		t.Fatal("cloud-to-local Generate through real desktop bridge:", err)
	}
	assertCalls := func(calls []modelruntime.ToolCall) {
		t.Helper()
		if len(calls) != 1 || calls[0].Name != "write" || calls[0].ID != "reverse-write" || string(calls[0].Arguments) != reverseArgs {
			t.Fatalf("reverse inference tool fragments corrupted: %+v", calls)
		}
	}
	assertCalls(response.ToolCalls)
	if response.Usage["prompt_tokens"] != 17 || response.Usage["completion_tokens"] != 9 {
		t.Fatalf("reverse inference usage lost: %+v", response.Usage)
	}
	stream, err := client.Stream(modelruntime.WithActor(ctx, owner.p.UserID), request)
	if err != nil {
		t.Fatal("cloud-to-local Stream through real desktop bridge:", err)
	}
	var assembled modelruntime.ToolCallAssembler
	completed := false
	for event := range stream {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		assembled.AddEvent(event)
		completed = completed || event.Type == "MODEL_COMPLETED"
	}
	assertCalls(assembled.Calls())
	if !completed {
		t.Fatal("reverse stream ended without an explicit completion")
	}
	if err = json.NewEncoder(stdin).Encode(map[string]string{"type": "verify"}); err != nil {
		t.Fatal(err)
	}
	read("verified")
	if cloudCalls.Load() != 2 || localCalls.Load() != 2 {
		t.Fatalf("unexpected provider calls cloud=%d local=%d", cloudCalls.Load(), localCalls.Load())
	}
	_ = stdin.Close()
	select {
	case <-exited:
		if commandErr != nil {
			t.Fatalf("hybrid helper failed: %v\n%s", commandErr, stderr.String())
		}
	case <-ctx.Done():
		t.Fatal("hybrid helper did not close its real sidecar")
	}
}
