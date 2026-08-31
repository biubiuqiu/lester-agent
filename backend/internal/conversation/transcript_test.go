package conversation

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
)

func TestHistoryRoundTripAndChatProjection(t *testing.T) {
	calls := []model.ToolCall{{ID: "read-1", Name: "read", Arguments: json.RawMessage(`{"file_path":"config.yaml"}`)}}
	encoded, err := json.Marshal(calls)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []model.ToolCall
	if err = json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, calls) {
		t.Fatal("tool calls changed after JSONB round trip")
	}
	messages := []Message{
		{Role: "user", Content: "check configuration", Seq: 1},
		{Role: "assistant", Content: "Reading configuration", ToolCalls: decoded, Seq: 2},
		{Role: "tool", Content: `{"content":"     1\tport: 8080"}`, ToolCallID: "read-1", Seq: 3},
		{Role: "assistant", Content: "Port is 8080", Seq: 4},
		{Role: "assistant", Content: "interrupted text", Metadata: map[string]any{"incomplete": true}, Seq: 5},
	}
	history := modelHistory(messages)
	if len(history) != 4 || history[1].ToolCalls[0].ID != history[2].ToolCallID || history[2].Content != messages[2].Content {
		t.Fatalf("history = %#v", history)
	}
	visible := visibleMessages(messages)
	if len(visible) != 2 || visible[0].Role != "user" || visible[1].Content != "Port is 8080" {
		t.Fatalf("visible = %#v", visible)
	}
}

func TestAssembleSparseToolIndices(t *testing.T) {
	calls, err := assembledToolCalls(map[int]*model.ToolCall{
		4: {ID: "b", Name: "edit", Arguments: json.RawMessage(`{"file_path":"b"}`)},
		1: {ID: "a", Name: "read", Arguments: json.RawMessage(`{"file_path":"a"}`)},
	})
	if err != nil || len(calls) != 2 || calls[0].ID != "a" || calls[1].ID != "b" {
		t.Fatalf("calls=%#v err=%v", calls, err)
	}
	for _, fragment := range []*model.ToolCall{
		{ID: "a", Name: "read", Arguments: json.RawMessage(`{"file_path":`)},
		{ID: "", Name: "read", Arguments: json.RawMessage(`{}`)},
	} {
		if _, err = assembledToolCalls(map[int]*model.ToolCall{1: fragment}); err == nil {
			t.Fatal("incomplete call accepted")
		}
	}
	if _, err = assembledToolCalls(map[int]*model.ToolCall{0: {ID: "a", Name: "read"}, 1: {ID: "a", Name: "read"}}); err == nil {
		t.Fatal("duplicate call ID accepted")
	}
}
