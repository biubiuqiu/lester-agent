package agenttool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
)

func TestDefaultRegistryDefinitions(t *testing.T) {
	definitions := NewDefaultRegistry(nil).Definitions()
	want := []string{"bash", "read", "write", "edit", "load_skill"}
	if len(definitions) != len(want) {
		t.Fatalf("definitions = %d, want %d", len(definitions), len(want))
	}
	for index, name := range want {
		if definitions[index].Name != name {
			t.Fatalf("definitions[%d] = %q, want %q", index, definitions[index].Name, name)
		}
	}
}

func TestDecodeArgumentsRejectsUnknownFields(t *testing.T) {
	var input bashInput
	if err := decodeArguments(json.RawMessage(`{"command":"pwd","unexpected":true}`), &input); err == nil {
		t.Fatal("unknown input field was accepted")
	}
}

func TestSliceLines(t *testing.T) {
	content, total, count := sliceLines("one\ntwo\nthree\nfour", 2, 2)
	if content != "two\nthree" || total != 4 || count != 2 {
		t.Fatalf("sliceLines() = %q, %d, %d", content, total, count)
	}
}

func TestTruncateText(t *testing.T) {
	value := "0123456789abcdefghijklmnop"
	got, truncated, omitted := truncateText(value, 12)
	if !truncated || omitted != len([]rune(value))-12 || !strings.Contains(got, "Output truncated") {
		t.Fatalf("truncateText() = %q, %v, %d", got, truncated, omitted)
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote("printf '%s' hello")
	want := `'printf '"'"'%s'"'"' hello'`
	if got != want {
		t.Fatalf("shellQuote() = %q, want %q", got, want)
	}
}

func TestFileHandlersComposeThroughRegistry(t *testing.T) {
	computer := &fakeSandbox{files: map[string][]byte{}}
	environment := Environment{SandboxID: "test", WorkDir: "/workspace/conversations/test", Sandboxes: computer, Emit: func(string, map[string]any) {}}
	registry := NewRegistry()
	registry.Register(Write{})
	registry.Register(Edit{})
	registry.Register(Read{})
	ctx := context.Background()
	if _, err := registry.Execute(ctx, "write", json.RawMessage(`{"file_path":"notes.txt","content":"red red"}`), environment); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(ctx, "edit", json.RawMessage(`{"file_path":"notes.txt","old_string":"red","new_string":"green","replace_all":true}`), environment); err != nil {
		t.Fatal(err)
	}
	result, err := registry.Execute(ctx, "read", json.RawMessage(`{"file_path":"notes.txt"}`), environment)
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["content"] != "     1\tgreen green" {
		t.Fatalf("read result = %#v", result)
	}
}

type fakeSandbox struct{ files map[string][]byte }

func (f *fakeSandbox) Exec(context.Context, string, sandbox.Command) (*sandbox.CommandResult, error) {
	return &sandbox.CommandResult{}, nil
}
func (f *fakeSandbox) ReadFile(_ context.Context, _, _, path string) ([]byte, error) {
	return append([]byte(nil), f.files[path]...), nil
}
func (f *fakeSandbox) ReadFileLines(_ context.Context, _, _, path string, offset, limit int) (*sandbox.FileLines, error) {
	content := string(f.files[path])
	lines := fileLines(content)
	start := min(offset-1, len(lines))
	end := min(start+limit, len(lines))
	result := &sandbox.FileLines{StartLine: offset, TotalLines: len(lines)}
	for _, line := range lines[start:end] {
		result.Lines = append(result.Lines, sandbox.FileLine{Text: line})
	}
	return result, nil
}
func (f *fakeSandbox) WriteFile(_ context.Context, _, _, path string, data []byte) error {
	f.files[path] = append([]byte(nil), data...)
	return nil
}
func (f *fakeSandbox) EditFile(_ context.Context, _, _, path string, input sandbox.FileEditRequest) (*sandbox.FileEditResult, error) {
	content := string(f.files[path])
	replacements := strings.Count(content, input.OldString)
	if input.ReplaceAll {
		content = strings.ReplaceAll(content, input.OldString, input.NewString)
	} else {
		content = strings.Replace(content, input.OldString, input.NewString, 1)
	}
	f.files[path] = []byte(content)
	return &sandbox.FileEditResult{OK: true, Replacements: replacements, SHA256: "test"}, nil
}
func (f *fakeSandbox) ListFiles(context.Context, string, string, string) ([]sandbox.FileEntry, error) {
	return nil, nil
}
