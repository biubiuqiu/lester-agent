package toolcontext

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
)

func addExchange(history *[]modelruntime.Message, name, args, result string) {
	id := fmt.Sprintf("call-%d", len(*history))
	*history = append(*history,
		modelruntime.Message{Role: "assistant", RunID: "run-1", ToolCalls: []modelruntime.ToolCall{{ID: id, Name: name, Arguments: json.RawMessage(args)}}},
		modelruntime.Message{Role: "tool", ToolCallID: id, Content: result},
	)
}

func addReads(history *[]modelruntime.Message, count int) {
	for i := 0; i < count; i++ {
		result, _ := json.Marshal(map[string]any{"content": strings.Repeat("code line\n", 400), "start_line": 7, "line_count": 400, "next_offset": 407, "truncated": true})
		addExchange(history, "read", `{"file_path":"service.go","offset":7,"limit":1000}`, string(result))
	}
}

func build(t *testing.T, history []modelruntime.Message) Projection {
	t.Helper()
	projection, err := Build(history)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Exchanges(projection.Messages); err != nil {
		t.Fatalf("projection broke call/result protocol: %v", err)
	}
	return projection
}

func decisions(t *testing.T, history []modelruntime.Message) []ToolContextItem {
	t.Helper()
	exchanges, err := Exchanges(history)
	if err != nil {
		t.Fatal(err)
	}
	return (DefaultPolicy{}).Resolve(exchanges)
}

func TestRecentWindowAndReferencesDoNotMutateHistory(t *testing.T) {
	history := []modelruntime.Message{{Role: "user", Content: "Inspect the project"}}
	addReads(&history, 30)
	history = append(history, modelruntime.Message{Role: "assistant", Content: "Inspection complete"})
	before, _ := json.Marshal(history)
	projection := build(t, history)
	if projection.Stats.Full != 10 || projection.Stats.Reference != 20 || projection.Stats.Evicted != 0 {
		t.Fatalf("stats=%+v", projection.Stats)
	}
	if projection.Stats.AfterCharacters >= projection.Stats.BeforeCharacters/2 {
		t.Fatalf("expected >50%% reduction for this synthetic fixture: %+v", projection.Stats)
	}
	ref := projection.Messages[1].Content
	for _, expected := range []string{`"file":"service.go"`, `"start_line":7`, `"end_line":406`, `"next_offset":407`, `"tool_execution_id":"run-1:call-1"`} {
		if !strings.Contains(ref, expected) {
			t.Fatalf("missing %s in %s", expected, ref)
		}
	}
	if projection.Messages[0].Content != history[0].Content || projection.Messages[len(projection.Messages)-1].Content != "Inspection complete" {
		t.Fatal("ordinary dialogue was pruned")
	}
	if again := build(t, history); !reflect.DeepEqual(projection, again) {
		t.Fatal("projection is not deterministic")
	}
	// Even a consumer mutating its request must not change durable/full history.
	for i := range projection.Messages {
		if len(projection.Messages[i].ToolCalls) > 0 {
			projection.Messages[i].ToolCalls[0].Arguments[0] = '!'
			projection.Messages[i].ToolCalls[0].Name = "changed"
			break
		}
	}
	after, _ := json.Marshal(history)
	if string(before) != string(after) {
		t.Fatal("projection mutated source history")
	}
	t.Logf("fixture characters: %d -> %d (not tokenizer counts)", projection.Stats.BeforeCharacters, projection.Stats.AfterCharacters)
}

func TestLowValueResultsMustBeObservedBeforeEarlyEviction(t *testing.T) {
	for _, command := range []string{"pwd", "ls", "ls -la", "git status --short"} {
		t.Run(command, func(t *testing.T) {
			var history []modelruntime.Message
			addExchange(&history, "bash", fmt.Sprintf(`{"command":%q}`, command), `{"exit_code":0,"stdout":"useful current result"}`)
			if item := decisions(t, history)[0]; item.Mode != ToolContextFull || item.Reason != "unobserved_result" {
				t.Fatalf("fresh result hidden: %+v", item)
			}
			history = append(history, modelruntime.Message{Role: "user", Content: "continue"})
			if item := decisions(t, history)[0]; item.Mode != ToolContextFull {
				t.Fatal("user message is not proof that model consumed result")
			}
			history = append(history, modelruntime.Message{Role: "assistant", Content: "Observed the result"})
			projection := build(t, history)
			if projection.Stats.Evicted != 1 || len(projection.Messages) != 2 {
				t.Fatalf("low-value pair not evicted: %+v", projection)
			}
		})
	}
	for _, command := range []string{"pwd && go test ./...", "ls > report.txt", "ls important/path", "rg TODO .", "go test ./..."} {
		var history []modelruntime.Message
		addExchange(&history, "bash", fmt.Sprintf(`{"command":%q}`, command), `{"exit_code":0,"stdout":"important"}`)
		history = append(history, modelruntime.Message{Role: "assistant", Content: "consumed"})
		if item := decisions(t, history)[0]; item.Mode != ToolContextFull {
			t.Fatalf("command %q was falsely classified low-value", command)
		}
	}
}

func TestUnresolvedFailurePinnedUntilSameOperationSucceeds(t *testing.T) {
	var history []modelruntime.Message
	addExchange(&history, "bash", `{"command":"go test ./...","timeout":1000}`, `{"exit_code":1,"stderr":"critical failing assertion"}`)
	addReads(&history, 12)
	if item := decisions(t, history)[0]; !item.Pinned || item.Mode != ToolContextFull {
		t.Fatalf("nonzero exit code was not pinned: %+v", item)
	}
	addExchange(&history, "bash", `{"command":"go test ./other"}`, `{"exit_code":0}`)
	addExchange(&history, "bash", `{"command":"go test ./...","run_in_background":true}`, `{"status":"running","task_id":"bg-1"}`)
	if item := decisions(t, history)[0]; !item.Pinned {
		t.Fatal("different command or background launch incorrectly resolved failure")
	}
	addExchange(&history, "computer_exec", `{"description":"retry","command":"go test ./...","timeout":600000}`, `{"exit_code":0,"stdout":"PASS"}`)
	item := decisions(t, history)[0]
	if item.Pinned || item.Mode != ToolContextReference || !strings.Contains(item.Reference, `"resolved_by_later_success":true`) {
		t.Fatalf("successful retry did not unpin old failure: %+v", item)
	}
	build(t, history)
}

func TestErrorSignalsAndConservativeResolution(t *testing.T) {
	for _, result := range []string{`{"error":"denied"}`, `{"error":{"message":"denied"}}`, `{"is_error":true}`, `{"ok":false}`, `{"status":"interrupted"}`} {
		var history []modelruntime.Message
		addExchange(&history, "read", `{"file_path":"a.go"}`, result)
		addReads(&history, 11)
		if item := decisions(t, history)[0]; !item.Pinned {
			t.Fatalf("error not pinned: %s", result)
		}
		addExchange(&history, "computer_read_file", `{"path":"a.go","offset":100}`, `{"content":"now readable"}`)
		if item := decisions(t, history)[0]; item.Pinned {
			t.Fatalf("same-file read retry did not resolve error: %s", result)
		}
	}
	var history []modelruntime.Message
	addExchange(&history, "edit", `{"file_path":"x","old_string":"a","new_string":"b"}`, `{"error":"not found"}`)
	addReads(&history, 11)
	addExchange(&history, "edit", `{"file_path":"x","old_string":"c","new_string":"d"}`, `{"ok":true}`)
	if !decisions(t, history)[0].Pinned {
		t.Fatal("an unrelated edit resolved an error")
	}
	addExchange(&history, "edit", `{"new_string":"b", "old_string":"a", "file_path":"x"}`, `{"ok":true}`)
	if decisions(t, history)[0].Pinned {
		t.Fatal("equivalent JSON retry did not resolve error")
	}
}

func TestMixedBatchPairingAndRunScopedIDs(t *testing.T) {
	history := []modelruntime.Message{
		{Role: "assistant", RunID: "run-a", Content: "Original explanation", ToolCalls: []modelruntime.ToolCall{
			{ID: "same", Name: "read", Arguments: json.RawMessage(`{"file_path":"a"}`)},
			{ID: "list", Name: "bash", Arguments: json.RawMessage(`{"command":"pwd"}`)},
			{ID: "failed", Name: "bash", Arguments: json.RawMessage(`{"command":"go test"}`)},
		}},
		// Results may arrive out of call order, but remain one contiguous batch.
		{Role: "tool", ToolCallID: "failed", Content: `{"exit_code":1}`},
		{Role: "tool", ToolCallID: "list", Content: `{"exit_code":0}`},
		{Role: "tool", ToolCallID: "same", Content: `{"content":"old source"}`},
	}
	addReads(&history, 10)
	history = append(history,
		modelruntime.Message{Role: "assistant", RunID: "run-b", ToolCalls: []modelruntime.ToolCall{{ID: "same", Name: "read", Arguments: json.RawMessage(`{"file_path":"b"}`)}}},
		modelruntime.Message{Role: "tool", ToolCallID: "same", Content: `{"content":"new source"}`},
	)
	projection := build(t, history)
	if projection.Stats.Full != 11 || projection.Stats.Evicted != 1 || projection.Stats.Reference != 2 {
		t.Fatalf("stats=%+v", projection.Stats)
	}
	first := projection.Messages[0]
	if !strings.HasPrefix(first.Content, "Original explanation") || !strings.Contains(first.Content, "run-a:same") || len(first.ToolCalls) != 1 || first.ToolCalls[0].ID != "failed" {
		t.Fatalf("mixed batch projection=%+v", first)
	}
	if projection.Messages[1].ToolCallID != "failed" {
		t.Fatal("retained result lost its pair")
	}
}

func TestLatestBatchIsProtectedAndSiblingSuccessDoesNotResolveFailure(t *testing.T) {
	var calls []modelruntime.ToolCall
	history := []modelruntime.Message{{Role: "assistant"}}
	for i := 0; i < 12; i++ {
		id := fmt.Sprint(i)
		calls = append(calls, modelruntime.ToolCall{ID: id, Name: "bash", Arguments: json.RawMessage(`{"command":"go test"}`)})
		result := `{"exit_code":0}`
		if i == 0 {
			result = `{"exit_code":1}`
		}
		history = append(history, modelruntime.Message{Role: "tool", ToolCallID: id, Content: result})
	}
	history[0].ToolCalls = calls
	if projection := build(t, history); projection.Stats.Full != 12 || projection.Stats.Pinned != 1 {
		t.Fatalf("latest batch should be completely visible: %+v", projection.Stats)
	}
	history = append(history, modelruntime.Message{Role: "assistant", Content: "consumed"})
	if items := decisions(t, history); !items[0].Pinned || items[1].Mode != ToolContextReference || items[2].Mode != ToolContextFull {
		t.Fatalf("window must count individual calls: %+v", items)
	}
}

func TestReferencesDropLargeWriteEditArgumentsAndPreserveBackgroundHandles(t *testing.T) {
	var history []modelruntime.Message
	large := strings.Repeat("private old file body", 1000)
	writeArgs, _ := json.Marshal(map[string]any{"file_path": "a.go", "content": large})
	editArgs, _ := json.Marshal(map[string]any{"file_path": "b.go", "old_string": large, "new_string": large})
	addExchange(&history, "write", string(writeArgs), `{"ok":true}`)
	addExchange(&history, "edit", string(editArgs), `{"ok":true,"replacements":2}`)
	addExchange(&history, "bash", `{"command":"long build","run_in_background":true}`, `{"status":"running","task_id":"task-1","log_path":".lester/tasks/task-1.log","pid":"42"}`)
	addReads(&history, 10)
	projection := build(t, history)
	encoded, _ := json.Marshal(projection.Messages)
	if strings.Contains(string(encoded), "private old file body") || projection.Stats.Reference != 3 {
		t.Fatal("large call arguments remained in reference context")
	}
	if !strings.Contains(projection.Messages[1].Content, `"replacements":2`) || strings.Contains(projection.Messages[1].Content, "start_line") {
		t.Fatal("edit reference must use real metadata, not invented line ranges")
	}
	for _, expected := range []string{"task-1", ".lester/tasks/task-1.log", "running"} {
		if !strings.Contains(projection.Messages[2].Content, expected) {
			t.Fatalf("lost background handle %s", expected)
		}
	}
}

func TestSkillsUnknownToolsAndMalformedResultsStayFull(t *testing.T) {
	var history []modelruntime.Message
	addExchange(&history, "load_skill", `{"slug":"coding"}`, `{"content":"Important skill instructions"}`)
	addExchange(&history, "custom_tool", `{}`, `{"ok":true,"important":"unknown semantics"}`)
	addExchange(&history, "read", `{"file_path":"legacy"}`, `legacy unstructured content`)
	addExchange(&history, "list_files", `{}`, `[]`)
	addReads(&history, 10)
	projection := build(t, history)
	if projection.Stats.Full != 13 || projection.Stats.Evicted != 1 {
		t.Fatalf("conservative policy violated: %+v", projection.Stats)
	}
}

func TestInvalidTranscriptsFailBeforeProviderCall(t *testing.T) {
	call := modelruntime.ToolCall{ID: "a", Name: "read", Arguments: json.RawMessage(`{}`)}
	for name, history := range map[string][]modelruntime.Message{
		"orphan":             {{Role: "tool", ToolCallID: "a"}},
		"missing":            {{Role: "assistant", ToolCalls: []modelruntime.ToolCall{call}}},
		"wrong id":           {{Role: "assistant", ToolCalls: []modelruntime.ToolCall{call}}, {Role: "tool", ToolCallID: "b"}},
		"interleaved user":   {{Role: "assistant", ToolCalls: []modelruntime.ToolCall{call}}, {Role: "user", Content: "hi"}, {Role: "tool", ToolCallID: "a"}},
		"duplicate call":     {{Role: "assistant", ToolCalls: []modelruntime.ToolCall{call, call}}},
		"non-assistant call": {{Role: "user", ToolCalls: []modelruntime.ToolCall{call}}},
		"extra result":       {{Role: "assistant", ToolCalls: []modelruntime.ToolCall{call}}, {Role: "tool", ToolCallID: "a"}, {Role: "tool", ToolCallID: "a"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Build(history); err == nil {
				t.Fatal("invalid transcript accepted")
			}
		})
	}
}
