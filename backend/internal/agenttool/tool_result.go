package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ToolResult struct{ DB *pgxpool.Pool }

func (ToolResult) Definition() model.Tool {
	return model.Tool{Name: "read_tool_result", Description: "Read an original stored tool result in this conversation using a historical tool_execution_id (run UUID:tool call ID). This does not rerun the tool or read current files. Results originally shortened remain shortened; use task_output for a managed command's retained raw log. offset and limit are Unicode character offsets. Supply result_message_id if an old provider reused a call ID.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"tool_execution_id": map[string]any{"type": "string"}, "result_message_id": map[string]any{"type": "string"}, "offset": map[string]any{"type": "integer", "minimum": 0}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 30000}}, "required": []string{"tool_execution_id"}}}
}
func (h ToolResult) Execute(ctx context.Context, env Environment, raw json.RawMessage) (any, error) {
	var input struct {
		ExecutionID string `json:"tool_execution_id"`
		MessageID   string `json:"result_message_id"`
		Offset      int    `json:"offset"`
		Limit       int    `json:"limit"`
	}
	if err := decodeArguments(raw, &input); err != nil {
		return nil, err
	}
	parts := strings.SplitN(input.ExecutionID, ":", 2)
	if h.DB == nil || len(parts) != 2 || len(parts[1]) == 0 || len(parts[1]) > 512 || input.Offset < 0 || input.Limit < 0 || input.Limit > 30000 {
		return nil, errors.New("invalid stored-result reference")
	}
	runID, err := uuid.Parse(parts[0])
	if err != nil {
		return nil, errors.New("invalid stored-result run")
	}
	var messageID *uuid.UUID
	if input.MessageID != "" {
		id, e := uuid.Parse(input.MessageID)
		if e != nil {
			return nil, errors.New("invalid result message")
		}
		messageID = &id
	}
	rows, err := h.DB.Query(ctx, `SELECT m.id,m.seq,m.content,m.tool_name FROM messages m
	JOIN runs current_run ON current_run.id=$1 AND current_run.conversation_id=m.conversation_id AND current_run.status='running'
	WHERE m.conversation_id=$2 AND m.run_id=$3 AND m.role='tool' AND m.tool_call_id=$4 AND ($5::uuid IS NULL OR m.id=$5)
	ORDER BY m.seq LIMIT 2`, env.RunID, env.ConversationID, runID, parts[1], messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var id uuid.UUID
	var seq int64
	var content, name string
	count := 0
	for rows.Next() {
		count++
		if err = rows.Scan(&id, &seq, &content, &name); err != nil {
			return nil, err
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, errors.New("stored tool result not found in this conversation")
	}
	if count != 1 {
		return nil, errors.New("historical call ID is ambiguous; supply the original result_message_id")
	}
	provenance := map[string]any{"tool_execution_id": input.ExecutionID, "result_message_id": id.String(), "message_seq": seq, "tool": name, "historical": true, "current_file_observation": false}
	// Preserve native image envelopes instead of injecting their base64 bytes
	// into text pages. Historical images do not grant fresh file-write receipts.
	var envelope map[string]any
	if json.Unmarshal([]byte(content), &envelope) == nil && envelope["images"] != nil {
		if input.Offset != 0 {
			return nil, errors.New("historical image results are read as native image envelopes, not text pages")
		}
		envelope["historical_result"] = provenance
		return envelope, nil
	}
	if input.Limit == 0 {
		input.Limit = 20000
	}
	runes := []rune(content)
	start := min(input.Offset, len(runes))
	end := min(start+input.Limit, len(runes))
	provenance["content"] = string(runes[start:end])
	provenance["offset"] = start
	provenance["next_offset"] = end
	provenance["has_more"] = end < len(runes)
	provenance["total_characters"] = len(runes)
	provenance["offset_unit"] = "unicode_characters"
	return provenance, nil
}
