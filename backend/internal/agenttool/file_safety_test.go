package agenttool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/toolcontext"
)

func fileTestEnvironment(files map[string][]byte) (Environment, *fakeSandbox) {
	computer := &fakeSandbox{files: files}
	return Environment{SandboxID: "test", WorkDir: "/workspace/conversations/test", Sandboxes: computer, Files: NewFileState(nil)}, computer
}

func readForTest(t *testing.T, environment Environment, input string) map[string]any {
	t.Helper()
	result, err := (Read{}).Execute(context.Background(), environment, json.RawMessage(input))
	if err != nil {
		t.Fatal(err)
	}
	return result.(map[string]any)
}

func TestDestructiveStringFieldsRequireExplicitValue(t *testing.T) {
	for _, value := range []string{"", ",\"content\":null"} {
		environment, computer := fileTestEnvironment(map[string][]byte{"note": []byte("keep")})
		_, err := (Write{}).Execute(context.Background(), environment, json.RawMessage(`{"file_path":"note"`+value+`}`))
		if err == nil || !strings.Contains(err.Error(), "content") || string(computer.files["note"]) != "keep" {
			t.Fatalf("write missing/null changed file: %v", err)
		}
	}
	for _, value := range []string{"", ",\"new_string\":null"} {
		environment, computer := fileTestEnvironment(map[string][]byte{"note": []byte("keep")})
		readForTest(t, environment, `{"file_path":"note"}`)
		_, err := (Edit{}).Execute(context.Background(), environment, json.RawMessage(`{"file_path":"note","old_string":"keep"`+value+`}`))
		if err == nil || !strings.Contains(err.Error(), "new_string") || string(computer.files["note"]) != "keep" {
			t.Fatalf("edit missing/null changed file: %v", err)
		}
	}
	environment, computer := fileTestEnvironment(map[string][]byte{"note": []byte("delete me")})
	readForTest(t, environment, `{"file_path":"note"}`)
	if _, err := (Edit{}).Execute(context.Background(), environment, json.RawMessage(`{"file_path":"note","old_string":"delete me","new_string":""}`)); err != nil {
		t.Fatal(err)
	}
	if string(computer.files["note"]) != "" {
		t.Fatal("explicit deletion was not accepted")
	}
	if _, err := (Write{}).Execute(context.Background(), environment, json.RawMessage(`{"file_path":"empty","content":""}`)); err != nil {
		t.Fatal(err)
	}
	if _, exists := computer.files["empty"]; !exists {
		t.Fatal("explicit empty file was not created")
	}
}

func TestFileVersionBindingRejectsUnreadAndChangedFiles(t *testing.T) {
	environment, computer := fileTestEnvironment(map[string][]byte{"note": []byte("before")})
	if _, err := (Write{}).Execute(context.Background(), environment, json.RawMessage(`{"file_path":"note","content":"overwrite"}`)); err == nil {
		t.Fatal("unread existing file was overwritten")
	}
	if _, err := (Edit{}).Execute(context.Background(), environment, json.RawMessage(`{"file_path":"note","old_string":"before","new_string":"after"}`)); err == nil {
		t.Fatal("unread existing file was edited")
	}
	readForTest(t, environment, `{"file_path":"note"}`)
	computer.files["note"] = []byte("before plus externally changed content")
	for _, handler := range []Handler{Write{}, Edit{}} {
		raw := `{"file_path":"note","content":"overwrite"}`
		if _, editing := handler.(Edit); editing {
			raw = `{"file_path":"note","old_string":"before","new_string":"after"}`
		}
		if _, err := handler.Execute(context.Background(), environment, json.RawMessage(raw)); err == nil {
			t.Fatal("stale file observation authorized mutation")
		}
	}
	if string(computer.files["note"]) != "before plus externally changed content" {
		t.Fatal("external change was lost")
	}
}

func TestPartialReadReceiptsDoNotAuthorizeOverwriteOrUnseenEdit(t *testing.T) {
	environment, _ := fileTestEnvironment(map[string][]byte{"note": []byte("first\nsecond\nthird\n")})
	result := readForTest(t, environment, `{"file_path":"note","offset":2,"limit":1}`)
	receipt := result["read_receipt"].(ReadReceipt)
	if receipt.Complete || receipt.StartLine != 2 || receipt.LineCount != 1 || receipt.SHA256 == "" {
		t.Fatalf("partial receipt=%+v", receipt)
	}
	if _, _, err := environment.Files.ExpectedWrite("note"); err == nil {
		t.Fatal("partial read authorized full overwrite")
	}
	if _, err := environment.Files.ExpectedEdit("note", "third", false); err == nil {
		t.Fatal("unseen text authorized edit")
	}
	if _, err := environment.Files.ExpectedEdit("note", "second", true); err == nil {
		t.Fatal("partial read authorized replace_all")
	}
	if _, err := environment.Files.ExpectedEdit("note", "second", false); err != nil {
		t.Fatal(err)
	}
	readForTest(t, environment, `{"file_path":"note","limit":1}`)
	readForTest(t, environment, `{"file_path":"note","offset":3,"limit":1}`)
	if _, create, err := environment.Files.ExpectedWrite("note"); err != nil || create {
		t.Fatalf("all contiguous observed pages did not establish complete read: %v", err)
	}
}

func TestReadReceiptInvalidatesPagesWhenVersionChanges(t *testing.T) {
	environment, computer := fileTestEnvironment(map[string][]byte{"note": []byte("first\nsecond")})
	readForTest(t, environment, `{"file_path":"note","limit":1}`)
	computer.files["note"] = []byte("updated\nsecond")
	readForTest(t, environment, `{"file_path":"note","offset":2,"limit":1}`)
	if _, _, err := environment.Files.ExpectedWrite("note"); err == nil {
		t.Fatal("pages from different versions were combined")
	}
	if _, err := environment.Files.ExpectedEdit("note", "first", false); err == nil {
		t.Fatal("old version page survived")
	}
}

func TestFileStateRestoresOnlyActualToolReceipts(t *testing.T) {
	environment, _ := fileTestEnvironment(map[string][]byte{"note": []byte("observed")})
	result := readForTest(t, environment, `{"file_path":"note"}`)
	resultJSON, _ := json.Marshal(result)
	history := []model.Message{
		{Role: "user", Content: string(resultJSON)},
		{Role: "assistant", ToolCalls: []model.ToolCall{{ID: "read-1", Name: "read", Arguments: json.RawMessage(`{"file_path":"note"}`)}}},
		{Role: "tool", ToolCallID: "read-1", Content: string(resultJSON)},
	}
	if _, err := NewFileState(history).ExpectedEdit("note", "observed", false); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileState(history[:1]).ExpectedEdit("note", "observed", false); err == nil {
		t.Fatal("user-authored receipt authorized an edit")
	}
	if _, err := NewFileState(nil).ExpectedEdit("note", "observed", false); err == nil {
		t.Fatal("missing/pruned receipt authorized an edit")
	}
}

func TestProjectedHistoricalReadDoesNotAuthorizeEditing(t *testing.T) {
	environment, _ := fileTestEnvironment(map[string][]byte{"target": []byte("important original"), "other": []byte("ordinary")})
	var history []model.Message
	for index := 0; index < 14; index++ {
		filePath := "other"
		if index == 0 {
			filePath = "target"
		}
		arguments, _ := json.Marshal(map[string]any{"file_path": filePath})
		result := readForTest(t, environment, string(arguments))
		encoded, _ := json.Marshal(result)
		callID := fmt.Sprintf("read-%d", index)
		history = append(history, model.Message{Role: "assistant", ToolCalls: []model.ToolCall{{ID: callID, Name: "read", Arguments: arguments}}}, model.Message{Role: "tool", ToolCallID: callID, Content: string(encoded)})
	}
	history = append(history, model.Message{Role: "assistant", Content: "The tool results were observed."})
	if _, err := NewFileState(history).ExpectedEdit("target", "important", false); err != nil {
		t.Fatal(err)
	}
	projection, err := toolcontext.Build(history)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileState(projection.Messages).ExpectedEdit("target", "important", false); err == nil {
		t.Fatal("historical reference without retained original text authorized edit")
	}
}
