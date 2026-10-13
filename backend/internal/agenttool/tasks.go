package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
)

type TaskOutput struct{}

func (TaskOutput) Definition() model.Tool {
	return model.Tool{Name: "task_output", Description: "Inspect a managed command task and read its retained output by stable byte offset. Use the task_id returned by bash; never rerun a command to retrieve its output. Task launch is not task success.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"task_id": map[string]any{"type": "string"}, "offset": map[string]any{"type": "integer", "minimum": 0}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 30000}}, "required": []string{"task_id"}}}
}
func (TaskOutput) Execute(ctx context.Context, env Environment, raw json.RawMessage) (any, error) {
	var args struct {
		TaskID string `json:"task_id"`
		Offset int64  `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(args.TaskID)
	if err != nil || args.Offset < 0 || args.Limit < 0 || args.Limit > 30000 {
		return nil, errors.New("invalid task or log range")
	}
	if args.Limit == 0 {
		args.Limit = 24000
	}
	args.Limit = max(4, args.Limit)
	if env.TaskAccess == nil {
		return nil, errors.New("managed task output unavailable")
	}
	view, err := env.TaskAccess(ctx, id, sandbox.CommandTaskReadOptions{Offset: args.Offset, Limit: args.Limit}, false)
	if err != nil {
		return nil, err
	}
	if page, ok := view["page"].(sandbox.CommandTaskPage); ok {
		data := page.Data
		// Retry an incomplete trailing code point at the next byte offset instead
		// of permanently replacing it with U+FFFD in the model transcript.
		if len(data) > 0 {
			last := len(data) - 1
			for last > 0 && !utf8.RuneStart(data[last]) {
				last--
			}
			if !utf8.FullRune(data[last:]) && page.HasMore {
				data = data[:last]
			}
		}
		content := string(data)
		if !utf8.Valid(data) {
			content = "This byte page contains invalid UTF-8. Download the raw log or inspect the retained log file as binary; no command was rerun."
		}
		view["page"] = map[string]any{"content": content, "offset": page.Offset, "next_offset": page.Offset + int64(len(data)), "has_more": page.HasMore || len(data) < len(page.Data), "stored_bytes": page.StoredBytes, "omitted_bytes": page.OmittedBytes, "truncated": page.Truncated, "stream": page.Stream, "offset_unit": "bytes", "valid_utf8": utf8.Valid(data)}
	}
	return view, nil
}

type StopTask struct{}

func (StopTask) Definition() model.Tool {
	return model.Tool{Name: "stop_task", Description: "Stop a managed command task in this conversation using its platform task_id. Stopping the Agent alone does not stop previously launched background tasks. Inspect cleanup_confirmed; an unknown result is not proof that the process stopped.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"task_id": map[string]any{"type": "string"}}, "required": []string{"task_id"}}}
}
func (StopTask) Execute(ctx context.Context, env Environment, raw json.RawMessage) (any, error) {
	var args struct {
		TaskID string `json:"task_id"`
	}
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(args.TaskID)
	if err != nil {
		return nil, errors.New("invalid task")
	}
	if env.TaskAccess == nil {
		return nil, errors.New("managed task control unavailable")
	}
	return env.TaskAccess(ctx, id, sandbox.CommandTaskReadOptions{}, true)
}
