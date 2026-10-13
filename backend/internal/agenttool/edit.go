package agenttool

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
)

type Edit struct{}

type editInput struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

func (Edit) Definition() model.Tool {
	return model.Tool{Name: "edit", Description: "Edit a text file by exact string replacement after reading its current matching portion. The observed file version is checked before replacement. replace_all requires a complete, untruncated read. This is not a regular expression or AST edit.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{
		"file_path":   pathProperty(),
		"old_string":  map[string]any{"type": "string", "description": "The exact text to replace. Include enough context to make a single match."},
		"new_string":  map[string]any{"type": "string", "description": "The replacement text. It may be empty."},
		"replace_all": map[string]any{"type": "boolean", "description": "Replace every exact match. Defaults to false; otherwise multiple matches are rejected as ambiguous."},
	}, "required": []string{"file_path", "old_string", "new_string"}, "additionalProperties": false}}
}

func (Edit) Execute(ctx context.Context, environment Environment, raw json.RawMessage) (any, error) {
	var input editInput
	if err := decodeArguments(raw, &input); err != nil {
		return nil, err
	}
	if err := requiredStringField(raw, "new_string"); err != nil {
		return nil, err
	}
	filePath, err := required(input.FilePath, "file_path")
	if err != nil {
		return nil, err
	}
	if input.OldString == "" {
		return nil, errors.New("old_string cannot be empty")
	}
	expected, err := environment.Files.ExpectedEdit(filePath, input.OldString, input.ReplaceAll)
	if err != nil {
		return nil, err
	}
	var change *FileChange
	if environment.RecordFileChange != nil {
		change = captureFileChange(ctx, environment, filePath, "edit", expected, false, input.NewString, input.OldString, input.ReplaceAll)
		if err = environment.RecordFileChange(ctx, change); err != nil {
			return nil, errors.New("file review intent could not be saved; edit was not started")
		}
	}
	result, err := environment.Sandboxes.EditFile(ctx, environment.SandboxID, environment.WorkDir, filePath, sandbox.FileEditRequest{
		OldString: input.OldString, NewString: input.NewString, ReplaceAll: input.ReplaceAll, ExpectedSHA256: expected,
	})
	if err != nil {
		finishFileChange(ctx, environment, change, "", false)
		return nil, err
	}
	if result == nil || !result.OK || !validSHA(result.SHA256) || change != nil && change.AfterSHA256 != "" && change.AfterSHA256 != result.SHA256 {
		finishFileChange(ctx, environment, change, "", false)
		return nil, errors.New("edit did not confirm the resulting file version")
	}
	environment.Files.RecordEdit(filePath, input.OldString, input.NewString, input.ReplaceAll, result.SHA256)
	if environment.Emit != nil {
		environment.Emit("FILE_UPDATED", map[string]any{"path": filePath, "operation": "edit", "replacements": result.Replacements, "sha256": result.SHA256})
	}
	if !finishFileChange(ctx, environment, change, result.SHA256, true) {
		return map[string]any{"ok": true, "sha256": result.SHA256, "review_saved": false, "notice": "File edit succeeded, but its review receipt could not be saved. Do not repeat the edit to recover the receipt."}, nil
	}
	return map[string]any{"ok": true, "replacements": result.Replacements, "sha256": result.SHA256}, nil
}
