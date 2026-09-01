package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
)

type Read struct{}

type readInput struct {
	FilePath string `json:"file_path"`
	Path     string `json:"path,omitempty"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

func (Read) Definition() model.Tool {
	return model.Tool{Name: "read", Description: "Read a text file from the current conversation directory by line range. Each content line is formatted as a right-aligned line number, a TAB, then the original text (cat -n style). Numbers are 1-based. The number and separator are display-only: never include them in edit/write input; preserve any indentation after the separator. Returns at most 2000 lines and 30000 characters per page. Use next_offset to continue. Individual lines longer than 2000 characters are explicitly truncated; inspect those with a narrower bash command.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{
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
	data, err := environment.Sandboxes.ReadFileLines(ctx, environment.SandboxID, environment.WorkDir, filePath, offset, limit)
	if err != nil {
		return nil, err
	}
	return numberedLinesResult(data), nil
}

func sliceLines(content string, offset, limit int) (string, int, int) {
	lines := fileLines(content)
	total := len(lines)
	start := min(offset-1, total)
	end := min(start+limit, total)
	return strings.Join(lines[start:end], "\n"), total, end - start
}

func fileLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	// A terminating newline does not introduce another physical line, as in cat -n.
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func numberedReadResult(content string, offset, limit int) map[string]any {
	lines := fileLines(content)
	start := min(offset-1, len(lines))
	end := min(start+limit, len(lines))
	chunk := &sandbox.FileLines{StartLine: offset, TotalLines: len(lines)}
	for _, line := range lines[start:end] {
		chunk.Lines = append(chunk.Lines, sandbox.FileLine{Text: line})
	}
	return numberedLinesResult(chunk)
}

func numberedLinesResult(chunk *sandbox.FileLines) map[string]any {
	var output strings.Builder
	count, characters, omitted := 0, 0, 0
	longLines := []int{}
	for index, item := range chunk.Lines {
		line := item.Text
		runes := []rune(line)
		lineOmitted := item.OmittedCharacters
		if len(runes) > 2000 {
			lineOmitted += len(runes) - 2000
			line = string(runes[:2000]) + fmt.Sprintf(" [line truncated: %d characters omitted]", lineOmitted)
		} else if lineOmitted > 0 {
			line += fmt.Sprintf(" [line truncated: %d characters omitted]", lineOmitted)
		}
		lineNumber := chunk.StartLine + index
		formatted := fmt.Sprintf("%6d\t%s", lineNumber, line)
		needed := utf8.RuneCountInString(formatted)
		if count > 0 {
			needed++
		}
		if characters+needed > outputCharacterLimit {
			break
		}
		if count > 0 {
			output.WriteByte('\n')
		}
		output.WriteString(formatted)
		characters += needed
		count++
		omitted += lineOmitted
		if lineOmitted > 0 {
			longLines = append(longLines, lineNumber)
		}
	}
	hasMore := chunk.StartLine-1+count < chunk.TotalLines
	result := map[string]any{"content": output.String(), "start_line": chunk.StartLine, "line_count": count, "total_lines": chunk.TotalLines, "truncated": hasMore || len(longLines) > 0}
	if hasMore {
		result["next_offset"] = chunk.StartLine + count
		result["notice"] = "Partial file view. Continue with next_offset; line numbers and the separating TAB are not file content."
	}
	if len(longLines) > 0 {
		result["truncated_lines"] = longLines
		result["omitted_characters"] = omitted
		notice, _ := result["notice"].(string)
		result["notice"] = strings.TrimSpace(notice + " Lines listed in truncated_lines exceed 2000 characters; use a narrower bash command to inspect their remaining content.")
	}
	return result
}
