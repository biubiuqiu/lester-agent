package sandbox

import (
	"bytes"
	"testing"
	"time"
)

func TestSafeWorkDir(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "default", want: "/workspace"},
		{name: "workspace", input: "/workspace", want: "/workspace"},
		{name: "conversation", input: "/workspace/conversations/47557ef9-538f-4148-b7ad-e9db8620b3e2", want: "/workspace/conversations/47557ef9-538f-4148-b7ad-e9db8620b3e2"},
		{name: "nested", input: "/workspace/conversations/id/subdir", wantErr: true},
		{name: "escape", input: "/workspace/conversations/id/../../other", wantErr: true},
		{name: "wrong root", input: "/tmp", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := safeWorkDir(test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("safeWorkDir() error = %v, wantErr %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("safeWorkDir() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBoundedCaptureKeepsHeadAndTail(t *testing.T) {
	capture := newBoundedCapture(16)
	input := []byte("0123456789abcdefghijklmnop")
	written, err := capture.Write(input)
	if err != nil || written != len(input) {
		t.Fatalf("Write() = %d, %v", written, err)
	}
	if !capture.Truncated() || capture.OmittedBytes() != int64(len(input)-16) {
		t.Fatalf("truncation = %v, omitted = %d", capture.Truncated(), capture.OmittedBytes())
	}
	if got, want := []byte(capture.String()), append(append([]byte(nil), input[:12]...), input[len(input)-4:]...); !bytes.Equal(got, want) {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestScopedPath(t *testing.T) {
	workDir := "/workspace/conversations/47557ef9-538f-4148-b7ad-e9db8620b3e2"
	tests := []struct {
		name     string
		filePath string
		want     string
		wantErr  bool
	}{
		{name: "empty is root", want: workDir},
		{name: "dot is root", filePath: ".", want: workDir},
		{name: "legacy workspace root", filePath: "/workspace", want: workDir},
		{name: "relative file", filePath: "docs/readme.md", want: workDir + "/docs/readme.md"},
		{name: "legacy workspace file", filePath: "/workspace/docs/readme.md", want: workDir + "/docs/readme.md"},
		{name: "absolute current conversation file", filePath: workDir + "/docs/readme.md", want: workDir + "/docs/readme.md"},
		{name: "relative escape", filePath: "../../other/secret", wantErr: true},
		{name: "other conversation", filePath: "/workspace/conversations/another-id/secret", wantErr: true},
		{name: "other absolute root", filePath: "/tmp/secret", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, got, err := scopedPath(workDir, test.filePath)
			if (err != nil) != test.wantErr {
				t.Fatalf("scopedPath() error = %v, wantErr %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("scopedPath() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDecodeEntriesUsesPublicJSONShape(t *testing.T) {
	entries, err := decodeEntries([]byte(`[{"name":"docs","path":"docs","is_dir":true,"size":4096,"modified_at":"2026-08-31T10:30:00Z"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Name != "docs" || entry.Path != "docs" || !entry.IsDir || entry.Size != 4096 {
		t.Fatalf("decoded entry = %#v", entry)
	}
	if !entry.ModifiedAt.Equal(time.Date(2026, 8, 31, 10, 30, 0, 0, time.UTC)) {
		t.Fatalf("modified_at = %s", entry.ModifiedAt)
	}
}
