//go:build !windows

package localrun

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/google/uuid"
)

func waitForRun(t *testing.T, store *Store, requestID, status string) Run {
	t.Helper()
	// Long tool-loop tests now sync both the receipt transaction and JSONL for
	// every message. Allow bounded disk/race-detector overhead, while still
	// asserting the exact terminal status and number of executed tools.
	deadline := time.Now().Add(15 * time.Second)
	for {
		run, err := store.Receipt(context.Background(), requestID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status != "running" {
			if run.Status != status {
				t.Fatalf("run status=%s code=%s want=%s", run.Status, run.ErrorCode, status)
			}
			return run
		}
		if time.Now().After(deadline) {
			t.Fatal("run did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRunnerHTTPModelToolLoopRealFilesCommandApprovalAndReceipt(t *testing.T) {
	store, state, project, c := openTestStore(t)
	secret := "private-fixture-api-key-NEVER-PERSIST"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("credential was not supplied privately")
		}
		var body struct {
			Tools []struct {
				Function struct {
					Parameters map[string]json.RawMessage `json:"parameters"`
				} `json:"function"`
			} `json:"tools"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid provider request")
		}
		for _, tool := range body.Tools {
			if required, present := tool.Function.Parameters["required"]; present && (string(required) == "null" || len(required) == 0 || required[0] != '[') {
				// Strict compatible providers reject the entire request, even when
				// the user only greets the agent and no tool will be called.
				http.Error(w, `{"error":{"type":"invalid_request_error"}}`, http.StatusBadRequest)
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		switch calls.Add(1) {
		case 1:
			chunk := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "write-1", "type": "function", "function": map[string]string{"name": "write", "arguments": `{"path":"hello.txt","content":"你好 local\n","create_only":true}`}}}}}}}
			raw, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", raw)
		case 2:
			if body.Messages[len(body.Messages)-1].Role != "tool" {
				t.Error("write result did not return to model")
			}
			chunk := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "bash-1", "type": "function", "function": map[string]string{"name": "bash", "arguments": `{"command":"cat hello.txt; printf once >> executed.txt"}`}}}}}}}
			raw, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", raw)
		case 3:
			if !strings.Contains(body.Messages[len(body.Messages)-1].Content, "你好 local") {
				t.Error("real command output did not return to model")
			}
			io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"已完成 ✓\"}}]}\n\n")
		default:
			t.Error("unexpected model replay")
		}
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	permissions := make(chan Permission, 2)
	runner := NewRunner(store, nil, func(method string, value any) {
		if method == "event" {
			event := value.(Event)
			if event.Type == "PERMISSION_REQUESTED" {
				permissions <- event.Payload.(Permission)
			}
		}
	})
	defer runner.Close()
	if _, err := runner.ConfigureModels([]ModelConfig{{ID: "fixture", Source: "local", Provider: "openai_compatible", Endpoint: server.URL, ModelID: "fixture-model", Credential: secret}}); err != nil {
		t.Fatal(err)
	}
	requestID := uuid.NewString()
	run, accepted, err := runner.Send(context.Background(), c.ID, requestID, "写文件并读取")
	if err != nil || !accepted {
		t.Fatalf("send: %v %v", accepted, err)
	}
	select {
	case p := <-permissions:
		if _, err := os.Stat(filepath.Join(project.Path, "executed.txt")); !os.IsNotExist(err) {
			t.Fatal("command ran before approval")
		}
		if err := runner.RespondPermission(context.Background(), c.ID, run.ID, p.ID, "allow"); err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("command approval missing")
	}
	waitForRun(t, store, requestID, "completed")
	receipt, accepted, err := runner.Send(context.Background(), c.ID, requestID, "写文件并读取")
	if err != nil || accepted || receipt.ID != run.ID {
		t.Fatalf("duplicate send: %v %v", accepted, err)
	}
	content, err := os.ReadFile(filepath.Join(project.Path, "executed.txt"))
	if err != nil || string(content) != "once" {
		t.Fatalf("command execution count: %q %v", content, err)
	}
	snapshot, err := store.Snapshot(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Messages) != 6 || snapshot.Messages[5].Content != "已完成 ✓" || calls.Load() != 3 {
		t.Fatalf("incomplete tool loop: %d messages/%d calls", len(snapshot.Messages), calls.Load())
	}
	assertNoPrivateBytes(t, state, secret)
}

type fixtureClient struct {
	stream   func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error)
	generate func(context.Context, modelruntime.Request) (*modelruntime.Response, error)
}

func (f *fixtureClient) Stream(ctx context.Context, p modelruntime.Request) (<-chan modelruntime.Event, error) {
	return f.stream(ctx, p)
}
func (f *fixtureClient) Generate(ctx context.Context, p modelruntime.Request) (*modelruntime.Response, error) {
	if f.generate != nil {
		return f.generate(ctx, p)
	}
	return &modelruntime.Response{Content: `{"decision":"ask","reason":"needs review"}`}, nil
}
func (*fixtureClient) Capabilities(context.Context, string) (modelruntime.Capabilities, error) {
	return modelruntime.Capabilities{Streaming: true, Tools: true, StructuredOutput: true}, nil
}
func fixtureEvents(events ...modelruntime.Event) <-chan modelruntime.Event {
	ch := make(chan modelruntime.Event, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch
}

func TestRunnerCancelPendingPermissionAndRejectLateApproval(t *testing.T) {
	store, _, project, c := openTestStore(t)
	requests := make(chan Permission, 1)
	client := &fixtureClient{stream: func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error) {
		return fixtureEvents(modelruntime.Event{ToolCall: &modelruntime.ToolCall{ID: "command", Name: "bash", Arguments: []byte(`{"command":"touch never"}`)}}), nil
	}}
	runner := NewRunner(store, func(context.Context, ModelConfig) (modelruntime.Client, error) { return client, nil }, func(method string, value any) {
		if method == "event" {
			e := value.(Event)
			if e.Type == "PERMISSION_REQUESTED" {
				requests <- e.Payload.(Permission)
			}
		}
	})
	defer runner.Close()
	_, _ = runner.ConfigureModels([]ModelConfig{{ID: "fixture", Source: "local"}})
	requestID := uuid.NewString()
	run, _, err := runner.Send(context.Background(), c.ID, requestID, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	var permission Permission
	select {
	case permission = <-requests:
	case <-time.After(time.Second):
		t.Fatal("missing approval")
	}
	if stopped, err := runner.Stop(context.Background(), c.ID, run.ID); !stopped || err != nil {
		t.Fatalf("stop: %v %v", stopped, err)
	}
	waitForRun(t, store, requestID, "cancelled")
	if err := runner.RespondPermission(context.Background(), c.ID, run.ID, permission.ID, "allow"); err == nil {
		t.Fatal("late permission approved")
	}
	if _, err := os.Stat(filepath.Join(project.Path, "never")); !os.IsNotExist(err) {
		t.Fatal("cancelled command ran")
	}
	messages, err := store.Messages(context.Background(), c.ID)
	if err != nil || len(messages) != 3 || !strings.Contains(messages[2].Content, `"status":"unknown"`) {
		t.Fatalf("cancel outcome missing: %+v %v", messages, err)
	}
}

func TestAutoReviewRequiresIndependentValidatedStructuredResponse(t *testing.T) {
	for _, test := range []struct{ output, want string }{
		{`{"decision":"allow","reason":"routine"}`, "allow"}, {`{"decision":"deny","reason":"unsafe"}`, "deny"}, {`{"decision":"ask","reason":"uncertain"}`, "ask"},
		{`{"decision":"allow"}`, "ask"}, {`{"decision":"allow","reason":"ok","extra":"bad"}`, "ask"}, {`{"decision":true,"reason":"bad"}`, "ask"}, {`{bad}`, "ask"},
	} {
		t.Run(test.output, func(t *testing.T) {
			client := &fixtureClient{generate: func(_ context.Context, request modelruntime.Request) (*modelruntime.Response, error) {
				if request.ResponseSchema == nil || request.ResponseSchema.AllowJSONMode || len(request.Tools) != 0 || len(request.Messages) != 1 {
					t.Fatal("review lacked strict independent schema")
				}
				return &modelruntime.Response{Content: test.output}, nil
			}}
			decision := reviewLocalCommand(context.Background(), client, "fixture", "printf ok")
			if decision != test.want {
				t.Fatalf("review decision=%v", decision)
			}
		})
	}
}

func TestCommandModesPreserveAutoDenyAskAllowAndExplicitFull(t *testing.T) {
	for _, test := range []struct {
		mode, review string
		ask, exec    bool
	}{
		{"auto", `{"decision":"deny","reason":"unsafe"}`, false, false},
		{"auto", `{"decision":"allow","reason":"routine"}`, false, true},
		{"auto", `{"decision":"ask","reason":"uncertain"}`, true, true},
		{"auto", `{"decision":"allow"}`, true, true},
		{"full", "", false, true},
	} {
		t.Run(test.mode+test.review, func(t *testing.T) {
			store, _, project, _ := openTestStore(t)
			c, err := store.CreateConversation(context.Background(), project.ID, "fixture", "mode", test.mode)
			if err != nil {
				t.Fatal(err)
			}
			var turns, reviews atomic.Int32
			permissions := make(chan Permission, 2)
			client := &fixtureClient{stream: func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error) {
				if turns.Add(1) == 1 {
					return fixtureEvents(modelruntime.Event{ToolCall: &modelruntime.ToolCall{ID: "command", Name: "bash", Arguments: []byte(`{"command":"printf ok > mode-result"}`)}}), nil
				}
				return fixtureEvents(modelruntime.Event{Delta: "finished"}), nil
			}, generate: func(context.Context, modelruntime.Request) (*modelruntime.Response, error) {
				reviews.Add(1)
				return &modelruntime.Response{Content: test.review}, nil
			}}
			runner := NewRunner(store, func(context.Context, ModelConfig) (modelruntime.Client, error) { return client, nil }, func(method string, value any) {
				if method == "event" {
					e := value.(Event)
					if e.Type == "PERMISSION_REQUESTED" {
						permissions <- e.Payload.(Permission)
					}
				}
			})
			defer runner.Close()
			_, _ = runner.ConfigureModels([]ModelConfig{{ID: "fixture", Source: "local"}})
			requestID := uuid.NewString()
			run, _, err := runner.Send(context.Background(), c.ID, requestID, "write mode result")
			if err != nil {
				t.Fatal(err)
			}
			if test.ask {
				select {
				case permission := <-permissions:
					if err := runner.RespondPermission(context.Background(), c.ID, run.ID, permission.ID, "allow"); err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("ask did not request local approval")
				}
			}
			waitForRun(t, store, requestID, "completed")
			select {
			case <-permissions:
				t.Fatal("unexpected local approval; hard deny was changed to ask")
			default:
			}
			_, err = os.Stat(filepath.Join(project.Path, "mode-result"))
			if (err == nil) != test.exec {
				t.Fatalf("command execution=%v want=%v", err == nil, test.exec)
			}
			if (test.mode == "full" && reviews.Load() != 0) || (test.mode == "auto" && reviews.Load() != 1) {
				t.Fatalf("review calls=%d", reviews.Load())
			}
		})
	}
}

func TestCancellationBetweenPersistedToolBatchAndExecutionKeepsContinuationValid(t *testing.T) {
	store, _, project, c := openTestStore(t)
	var turns atomic.Int32
	client := &fixtureClient{stream: func(_ context.Context, request modelruntime.Request) (<-chan modelruntime.Event, error) {
		if turns.Add(1) == 1 {
			return fixtureEvents(modelruntime.Event{ToolCalls: []*modelruntime.ToolCall{{ID: "never-1", Index: 0, Name: "write", Arguments: []byte(`{"path":"never-one","content":"x"}`)}, {ID: "never-2", Index: 1, Name: "write", Arguments: []byte(`{"path":"never-two","content":"x"}`)}}}), nil
		}
		if len(request.Messages) != 5 || request.Messages[2].Role != "tool" || request.Messages[3].Role != "tool" {
			t.Error("continuation has unmatched tool calls")
		}
		return fixtureEvents(modelruntime.Event{Delta: "continued without replay"}), nil
	}}
	var runner *Runner
	runner = NewRunner(store, func(context.Context, ModelConfig) (modelruntime.Client, error) { return client, nil }, func(method string, value any) {
		if method != "event" {
			return
		}
		event := value.(Event)
		if event.Type != "MESSAGE_SAVED" {
			return
		}
		message := event.Payload.(Message)
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			_, _ = runner.Stop(context.Background(), c.ID, message.RunID)
		}
	})
	defer runner.Close()
	_, _ = runner.ConfigureModels([]ModelConfig{{ID: "fixture", Source: "local"}})
	requestID := uuid.NewString()
	if _, _, err := runner.Send(context.Background(), c.ID, requestID, "cancel before executing batch"); err != nil {
		t.Fatal(err)
	}
	waitForRun(t, store, requestID, "cancelled")
	for _, name := range []string{"never-one", "never-two"} {
		if _, err := os.Stat(filepath.Join(project.Path, name)); !os.IsNotExist(err) {
			t.Fatal("cancelled batch ran")
		}
	}
	messages, err := store.Messages(context.Background(), c.ID)
	if err != nil || len(messages) != 4 {
		t.Fatalf("cancelled batch transcript: %d %v", len(messages), err)
	}
	nextID := uuid.NewString()
	if _, _, err := runner.Send(context.Background(), c.ID, nextID, "continue"); err != nil {
		t.Fatal(err)
	}
	waitForRun(t, store, nextID, "completed")
}

func TestRunnerContinuesPast64ToolTurnsAndStopsOnProviderContextFailure(t *testing.T) {
	const toolTurns = 66
	for _, contextFailure := range []bool{false, true} {
		t.Run(fmt.Sprintf("context_failure=%t", contextFailure), func(t *testing.T) {
			store, _, project, c := openTestStore(t)
			if err := os.WriteFile(filepath.Join(project.Path, "progress.txt"), []byte("observed local progress"), 0600); err != nil {
				t.Fatal(err)
			}
			var turns atomic.Int32
			client := &fixtureClient{stream: func(_ context.Context, request modelruntime.Request) (<-chan modelruntime.Event, error) {
				turn := turns.Add(1)
				if request.MaxTokens != 0 {
					t.Error("task output limit should remain provider-owned when unset")
				}
				if turn > 1 {
					last := request.Messages[len(request.Messages)-1]
					if last.Role != "tool" || last.ToolCallID != fmt.Sprintf("read-%d", turn-1) || !strings.Contains(last.Content, "observed local progress") {
						t.Error("next turn did not observe the preceding real file read")
					}
				}
				if turn > toolTurns {
					if contextFailure {
						return nil, &modelruntime.ProviderError{Kind: modelruntime.ErrorContextExceeded}
					}
					return fixtureEvents(modelruntime.Event{Delta: "finished after all tool turns"}), nil
				}
				return fixtureEvents(modelruntime.Event{ToolCall: &modelruntime.ToolCall{ID: fmt.Sprintf("read-%d", turn), Name: "read", Arguments: []byte(`{"path":"progress.txt"}`)}}), nil
			}}
			runner := NewRunner(store, func(context.Context, ModelConfig) (modelruntime.Client, error) { return client, nil }, nil)
			defer runner.Close()
			if _, err := runner.ConfigureModels([]ModelConfig{{ID: "fixture", Source: "local"}}); err != nil {
				t.Fatal(err)
			}
			requestID := uuid.NewString()
			if _, _, err := runner.Send(context.Background(), c.ID, requestID, "continue the requested reads"); err != nil {
				t.Fatal(err)
			}
			wantStatus := "completed"
			if contextFailure {
				wantStatus = "failed"
			}
			run := waitForRun(t, store, requestID, wantStatus)
			if turns.Load() != toolTurns+1 {
				t.Fatalf("model calls=%d, want %d without replay", turns.Load(), toolTurns+1)
			}
			if contextFailure && run.ErrorCode != string(modelruntime.ErrorContextExceeded) {
				t.Fatalf("context failure lost its terminal receipt: %+v", run)
			}
			messages, err := store.Messages(context.Background(), c.ID)
			if err != nil {
				t.Fatal(err)
			}
			for index := 0; index < toolTurns; index++ {
				assistant, result := messages[1+2*index], messages[2+2*index]
				if assistant.Role != "assistant" || len(assistant.ToolCalls) != 1 || result.Role != "tool" || result.ToolCallID != assistant.ToolCalls[0].ID {
					t.Fatalf("tool turn %d was not durably paired", index+1)
				}
			}
		})
	}
}

func TestRunnerKeepsPartialAuditOutputOutOfContinuationContext(t *testing.T) {
	store, _, _, c := openTestStore(t)
	var turns atomic.Int32
	client := &fixtureClient{stream: func(_ context.Context, request modelruntime.Request) (<-chan modelruntime.Event, error) {
		if turns.Add(1) == 1 {
			return fixtureEvents(modelruntime.Event{Delta: "incomplete provider output"}, modelruntime.Event{Err: &modelruntime.ProviderError{Kind: modelruntime.ErrorProtocol}}), nil
		}
		if len(request.Messages) != 2 || request.Messages[0].Role != "user" || request.Messages[1].Role != "user" {
			t.Error("partial audit output reached continuation context")
		}
		return fixtureEvents(modelruntime.Event{Delta: "complete reply"}), nil
	}}
	runner := NewRunner(store, func(context.Context, ModelConfig) (modelruntime.Client, error) { return client, nil }, nil)
	defer runner.Close()
	if _, err := runner.ConfigureModels([]ModelConfig{{ID: "fixture", Source: "local"}}); err != nil {
		t.Fatal(err)
	}
	firstID := uuid.NewString()
	if _, _, err := runner.Send(context.Background(), c.ID, firstID, "first input"); err != nil {
		t.Fatal(err)
	}
	waitForRun(t, store, firstID, "failed")
	secondID := uuid.NewString()
	if _, _, err := runner.Send(context.Background(), c.ID, secondID, "continue after provider failure"); err != nil {
		t.Fatal(err)
	}
	waitForRun(t, store, secondID, "completed")
	messages, err := store.Messages(context.Background(), c.ID)
	if err != nil || len(messages) != 4 || !messages[1].Partial || messages[1].Content != "incomplete provider output" {
		t.Fatalf("partial audit record was lost: %+v %v", messages, err)
	}
}
