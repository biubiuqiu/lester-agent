package agenttool

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
)

type Write struct{}

type writeInput struct {
	FilePath string `json:"file_path"`
	Path     string `json:"path,omitempty"`
	Content  string `json:"content"`
}

func (Write) Definition() model.Tool {
	return model.Tool{Name: "write", Description: "Create a text file in the current conversation directory. Before overwriting an existing file, read all its current contents first. Writes reject a file changed since that observation; a partial read cannot authorize a complete overwrite.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{
		"file_path": pathProperty(),
		"content":   map[string]any{"type": "string", "description": "The complete file content."},
	}, "required": []string{"file_path", "content"}, "additionalProperties": false}}
}

func (Write) Execute(ctx context.Context, environment Environment, raw json.RawMessage) (any, error) {
	var input writeInput
	if err := decodeArguments(raw, &input); err != nil {
		return nil, err
	}
	if err := requiredStringField(raw, "content"); err != nil {
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
	expected, createOnly, err := environment.Files.ExpectedWrite(filePath)
	if err != nil {
		return nil, err
	}
	writer, supported := environment.Sandboxes.(sandbox.VersionedFileWriter)
	if !supported {
		return nil, errors.New("this Computer does not support protected file writes; update its runtime before writing")
	}
	result, err := writer.WriteFileVersioned(ctx, environment.SandboxID, environment.WorkDir, filePath, sandbox.FileWriteRequest{Content: []byte(input.Content), ExpectedSHA256: expected, CreateOnly: createOnly})
	if err != nil {
		return nil, err
	}
	if result == nil || !result.OK || !validSHA(result.SHA256) {
		return nil, errors.New("protected write did not confirm the resulting file version")
	}
	environment.Files.RecordWrite(filePath, input.Content, result.SHA256)
	if environment.Emit != nil {
		environment.Emit("FILE_UPDATED", map[string]any{"path": filePath, "operation": "write", "sha256": result.SHA256})
	}
	return result, nil
}
