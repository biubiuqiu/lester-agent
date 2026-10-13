package agenttool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

const MaxReviewFileBytes = 512 << 10

// Private capture reads do not update model-visible FileState permissions.
type FileChange struct {
	Path              string
	Operation         string
	Status            string
	BeforeExists      bool
	BeforeSHA256      string
	AfterSHA256       string
	BeforeContent     []byte
	AfterContent      []byte
	UnavailableReason string
}

func contentSHA(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func reviewable(data []byte) bool {
	return len(data) <= MaxReviewFileBytes && utf8.Valid(data) && !strings.ContainsRune(string(data), 0)
}
func captureFileChange(ctx context.Context, env Environment, filePath, operation, expected string, create bool, content, old string, all bool) *FileChange {
	change := &FileChange{Path: filePath, Operation: operation, Status: "pending", BeforeExists: !create, BeforeSHA256: expected}
	if create {
		change.BeforeContent = []byte{}
	} else {
		before, err := env.Sandboxes.ReadFile(ctx, env.SandboxID, env.WorkDir, filePath)
		if err != nil {
			change.UnavailableReason = "original_unavailable"
			return change
		}
		if contentSHA(before) != expected {
			change.UnavailableReason = "original_version_changed"
			return change
		}
		if !reviewable(before) {
			change.UnavailableReason = "binary_or_large"
			return change
		}
		change.BeforeContent = append([]byte{}, before...)
	}
	after := []byte(content)
	if operation == "edit" {
		count := 1
		if all {
			count = -1
		}
		after = []byte(strings.Replace(string(change.BeforeContent), old, content, count))
	}
	change.AfterSHA256 = contentSHA(after)
	if !reviewable(after) {
		change.BeforeContent = nil
		change.UnavailableReason = "binary_or_large"
		return change
	}
	change.AfterContent = after
	return change
}
func finishFileChange(ctx context.Context, env Environment, change *FileChange, sha string, confirmed bool) bool {
	if change == nil || env.RecordFileChange == nil {
		return true
	}
	change.Status = "unknown"
	if confirmed {
		change.Status = "confirmed"
		change.AfterSHA256 = sha
	}
	return env.RecordFileChange(ctx, change) == nil
}
