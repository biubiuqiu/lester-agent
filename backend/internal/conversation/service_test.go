package conversation

import (
	"strings"
	"testing"
)

func TestMessageContentForModelAddsOnlyAttachmentMetadata(t *testing.T) {
	message := Message{Role: "user", Content: "请检查附件", Metadata: map[string]any{"attachments": []any{map[string]any{
		"stored_path": ".agent/upload/example.txt", "original_name": "example.txt", "content_type": "text/plain", "size_bytes": float64(42),
	}}}}
	got := messageContentForModel(message)
	if !strings.Contains(got, ".agent/upload/example.txt") || !strings.Contains(got, "contents are not included") {
		t.Fatalf("attachment notice missing metadata: %q", got)
	}
	if strings.Contains(got, "example file body") {
		t.Fatalf("attachment body leaked into model content: %q", got)
	}
}
