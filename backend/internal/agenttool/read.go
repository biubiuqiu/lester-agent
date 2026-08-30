package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
)

type Read struct{}

type readInput struct {
	FilePath string `json:"file_path"`
	Path     string `json:"path,omitempty"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

func (Read) Definition() model.Tool {
	return model.Tool{Name: "read", Description: "Read a text file from the current conversation directory, optionally by line range. Results are limited to 30000 characters; continue with offset when truncated.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{
		"file_path": pathProperty(),
		"offset":    map[string]any{"type": "integer", "minimum": 1, "description": "One-based first line to return. Defaults to 1."},
		"limit":     map[string]any{"type": "integer", "minimum": 1, "maximum": 2000, "description": "Maximum number of lines to return. Defaults to 2000."},
	}, "required": []string{"file_path"}, "additionalProperties": false}}
}

func (Read) Execute(ctx context.Context, environment Environment, raw json.RawMessage) (any, error) {
	var input readInput
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
	offset, limit := input.Offset, input.Limit
	if offset == 0 {
		offset = 1
	}
	if limit == 0 {
		limit = 2000
	}
	if offset < 1 || limit < 1 || limit > 2000 {
		return nil, errors.New("offset must be at least 1 and limit must be between 1 and 2000")
	}
	data, err := environment.Sandboxes.ReadFile(ctx, environment.SandboxID, environment.WorkDir, filePath)
	if err != nil {
		return nil, err
	}
	content, totalLines, returnedLines := sliceLines(string(data), offset, limit)
	limited, truncated, omitted := truncateText(content, outputCharacterLimit)
	result := map[string]any{"content": limited, "start_line": offset, "line_count": returnedLines, "total_lines": totalLines, "truncated": truncated}
	if truncated {
		result["omitted_characters"] = omitted
		result["notice"] = "Output was truncated. Use read with a later offset/smaller limit or a narrower bash command to continue."
	}
	return result, nil
}

func sliceLines(content string, offset, limit int) (string, int, int) {
	lines := strings.Split(content, "\n")
	if content == "" {
		lines = nil
	}
	total := len(lines)
	start := min(offset-1, total)
	end := min(start+limit, total)
	return strings.Join(lines[start:end], "\n"), total, end - start
}
