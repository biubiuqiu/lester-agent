package conversation

import "testing"

func TestToolStringArgument(t *testing.T) {
	if got, err := toolStringArgument(map[string]any{}, "path", false); err != nil || got != "" {
		t.Fatalf("optional argument = %q, %v", got, err)
	}
	if got, err := toolStringArgument(map[string]any{"content": ""}, "content", true); err != nil || got != "" {
		t.Fatalf("empty file content = %q, %v", got, err)
	}
	if _, err := toolStringArgument(map[string]any{}, "content", true); err == nil {
		t.Fatal("missing required argument was accepted")
	}
	if _, err := toolStringArgument(map[string]any{"path": 42}, "path", true); err == nil {
		t.Fatal("non-string argument was accepted")
	}
	if _, err := requiredToolStringArgument(map[string]any{"path": "  "}, "path"); err == nil {
		t.Fatal("blank required argument was accepted")
	}
}

func TestAgentTools(t *testing.T) {
	tools := agentTools()
	want := []string{"bash", "read", "write", "edit"}
	if len(tools) != len(want) {
		t.Fatalf("agentTools() returned %d tools, want %d", len(tools), len(want))
	}
	for index, name := range want {
		if tools[index].Name != name {
			t.Fatalf("agentTools()[%d].Name = %q, want %q", index, tools[index].Name, name)
		}
	}
}

func TestSliceFileLines(t *testing.T) {
	content, total, count := sliceFileLines("one\ntwo\nthree\nfour", 2, 2)
	if content != "two\nthree" || total != 4 || count != 2 {
		t.Fatalf("sliceFileLines() = %q, %d, %d", content, total, count)
	}
	content, total, count = sliceFileLines("", 1, 20)
	if content != "" || total != 0 || count != 0 {
		t.Fatalf("sliceFileLines(empty) = %q, %d, %d", content, total, count)
	}
}

func TestReplaceExactString(t *testing.T) {
	updated, replacements, err := replaceExactString("color=red\n", "red", "green", false)
	if err != nil || updated != "color=green\n" || replacements != 1 {
		t.Fatalf("replaceExactString() = %q, %d, %v", updated, replacements, err)
	}
	if _, _, err = replaceExactString("x x", "x", "y", false); err == nil {
		t.Fatal("ambiguous replacement was accepted")
	}
	updated, replacements, err = replaceExactString("x x", "x", "y", true)
	if err != nil || updated != "y y" || replacements != 2 {
		t.Fatalf("replace all = %q, %d, %v", updated, replacements, err)
	}
	if _, _, err = replaceExactString("abc", "missing", "x", false); err == nil {
		t.Fatal("missing old_string was accepted")
	}
}

func TestShellQuoteArgument(t *testing.T) {
	got := shellQuoteArgument("printf '%s' hello")
	want := `'printf '"'"'%s'"'"' hello'`
	if got != want {
		t.Fatalf("shellQuoteArgument() = %q, want %q", got, want)
	}
}
