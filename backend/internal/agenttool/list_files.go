package agenttool

import (
	"context"
	"encoding/json"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
)

// ListFiles is retained only for compatibility with in-flight calls from
// older prompts. New model requests discover files through bash.
type ListFiles struct{}

type listFilesInput struct {
	Path string `json:"path,omitempty"`
}

func (ListFiles) Definition() model.Tool {
	return model.Tool{Name: "list_files", Description: "Compatibility-only file listing.", InputSchema: map[string]any{"type": "object"}}
}

func (ListFiles) Execute(ctx context.Context, environment Environment, raw json.RawMessage) (any, error) {
	var input listFilesInput
	if err := decodeArguments(raw, &input); err != nil {
		return nil, err
	}
	return environment.Sandboxes.ListFiles(ctx, environment.SandboxID, environment.WorkDir, input.Path)
}
