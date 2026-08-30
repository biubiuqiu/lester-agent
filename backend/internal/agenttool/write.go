package agenttool

import (
	"context"
	"encoding/json"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
)

type Write struct{}

type writeInput struct {
	FilePath string `json:"file_path"`
	Path     string `json:"path,omitempty"`
	Content  string `json:"content"`
}

func (Write) Definition() model.Tool {
	return model.Tool{Name: "write", Description: "Create or completely overwrite a text file in the current conversation directory.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{
		"file_path": pathProperty(),
		"content":   map[string]any{"type": "string", "description": "The complete file content."},
	}, "required": []string{"file_path", "content"}, "additionalProperties": false}}
}

func (Write) Execute(ctx context.Context, environment Environment, raw json.RawMessage) (any, error) {
	var input writeInput
	if err := decodeArguments(raw, &input); err != nil {
		return nil, err
	}
	filePath := input.FilePath
	if filePath == "" {
		filePath = input.Path
	}
	filePath, err := required(filePath, "file_path")
	if err != nil {
		return nil, err
	}
	err = environment.Sandboxes.WriteFile(ctx, environment.SandboxID, environment.WorkDir, filePath, []byte(input.Content))
	if err == nil {
		environment.Emit("FILE_UPDATED", map[string]any{"path": filePath, "operation": "write"})
	}
	return map[string]bool{"ok": err == nil}, err
}
