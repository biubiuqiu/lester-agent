package agenttool

import (
	"context"
	"encoding/json"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
)

// Normal execution discovers files through bash. Read-only Plan requests expose
// this existing handler because arbitrary shell commands cannot be read-only.
type ListFiles struct{}

type listFilesInput struct {
	Path string `json:"path,omitempty"`
}

func (ListFiles) Definition() model.Tool {
	return model.Tool{Name: "list_files", Description: "List files within a conversation-relative directory without executing shell commands. Narrow the path if the result is truncated.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"path": pathProperty()}, "additionalProperties": false}}
}

func (ListFiles) Execute(ctx context.Context, environment Environment, raw json.RawMessage) (any, error) {
	var input listFilesInput
	if err := decodeArguments(raw, &input); err != nil {
		return nil, err
	}
	files, err := environment.Sandboxes.ListFiles(ctx, environment.SandboxID, environment.WorkDir, input.Path)
	if err != nil {
		return nil, err
	}
	if len(files) > 400 {
		return map[string]any{"files": files[:400], "truncated": true, "notice": "Listing limited to 400 entries. Choose a narrower directory path to inspect omitted entries; this is not a complete inventory."}, nil
	}
	return files, nil
}
