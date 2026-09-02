package toolboxfs

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteReadEditAndList(t *testing.T) {
	root := t.TempDir()
	writeResult, err := Write(root, "src/example.txt", strings.NewReader("red red\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	if !writeResult.OK || writeResult.BytesWritten != 8 || len(writeResult.SHA256) != 64 {
		t.Fatalf("write result = %#v", writeResult)
	}

	editResult, err := Edit(root, "src/example.txt", EditRequest{OldString: "red", NewString: "green", ReplaceAll: true})
	if err != nil {
		t.Fatal(err)
	}
	if !editResult.OK || editResult.Replacements != 2 || len(editResult.SHA256) != 64 {
		t.Fatalf("edit result = %#v", editResult)
	}

	var content bytes.Buffer
	if err = Read(root, "src/example.txt", &content); err != nil {
		t.Fatal(err)
	}
	if content.String() != "green green\n" {
		t.Fatalf("content = %q", content.String())
	}

	entries, err := List(root, "src")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "example.txt" || entries[0].Path != "src/example.txt" || entries[0].IsDir {
		t.Fatalf("entries = %#v", entries)
	}
	matches, err := filepath.Glob(filepath.Join(root, "src", ".lester-write-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files = %v, %v", matches, err)
	}
}

func TestEditRejectsAmbiguousAndStaleChanges(t *testing.T) {
	root := t.TempDir()
	result, err := Write(root, "notes.txt", strings.NewReader("x x"), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Edit(root, "notes.txt", EditRequest{OldString: "x", NewString: "y"}); err == nil || !strings.Contains(err.Error(), "matched 2 locations") {
		t.Fatalf("ambiguous edit error = %v", err)
	}
	if _, err = Edit(root, "notes.txt", EditRequest{OldString: "x", NewString: "y", ReplaceAll: true, ExpectedSHA256: strings.Repeat("0", 64)}); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("stale edit error = %v", err)
	}
	if result.SHA256 == "" {
		t.Fatal("write did not return a digest")
	}
}

func TestReadLinesStreamsPagesAndBoundsLongLines(t *testing.T) {
	root := t.TempDir()
	content := "one\n\n" + strings.Repeat("界", MaxCharactersInLine+3) + "\nlast\n"
	if _, err := Write(root, "long.txt", strings.NewReader(content), ""); err != nil {
		t.Fatal(err)
	}
	result, err := ReadLines(root, "long.txt", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if result.StartLine != 2 || result.TotalLines != 4 || len(result.Lines) != 2 {
		t.Fatalf("read lines result = %#v", result)
	}
	if result.Lines[0].Text != "" || result.Lines[1].OmittedCharacters != 3 {
		t.Fatalf("lines = %#v", result.Lines)
	}
	if len([]rune(result.Lines[1].Text)) != MaxCharactersInLine {
		t.Fatalf("kept characters = %d", len([]rune(result.Lines[1].Text)))
	}
}

func TestPathBoundaryRejectsTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if _, err := Write(root, "../escape.txt", strings.NewReader("no"), ""); err == nil {
		t.Fatal("relative traversal was accepted")
	}
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Write(root, "outside-link/escape.txt", strings.NewReader("no"), ""); err == nil {
		t.Fatal("symlink escape was accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("escape target exists or stat failed unexpectedly: %v", err)
	}
}

func TestRunUsesStructuredEditProtocol(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	if err := Run([]string{"write", "--root", root, "--path", "notes.txt"}, strings.NewReader("before"), &output); err != nil {
		t.Fatal(err)
	}
	var written WriteResult
	if err := json.Unmarshal(output.Bytes(), &written); err != nil || !written.OK {
		t.Fatalf("write output = %q, %v", output.String(), err)
	}

	output.Reset()
	request := `{"old_string":"before","new_string":"after"}`
	if err := Run([]string{"edit", "--root", root, "--path", "notes.txt"}, strings.NewReader(request), &output); err != nil {
		t.Fatal(err)
	}
	var edited EditResult
	if err := json.Unmarshal(output.Bytes(), &edited); err != nil || edited.Replacements != 1 {
		t.Fatalf("edit output = %q, %v", output.String(), err)
	}
	data, err := os.ReadFile(filepath.Join(root, "notes.txt"))
	if err != nil || string(data) != "after" {
		t.Fatalf("content = %q, %v", data, err)
	}
}
