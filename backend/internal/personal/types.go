// Package personal owns enduring, user-private dialogue and explicit memory.
// It deliberately has no sandbox, file system or Agent tool registry dependency.
package personal

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrMemoryFull   = errors.New("记忆空间已满，请先忘记不再需要的记忆")
	ErrInvalid      = errors.New("个人助理请求无效")
	ErrConflict     = errors.New("内容已更新，请检查最新状态后重试")
	ErrBusy         = errors.New("Lester 正在回复，请稍后发送")
	ErrUnauthorized = errors.New("登录或工作区权限已失效")
	ErrNotFound     = errors.New("记录不存在或无法访问")
	ErrModel        = errors.New("个人助理暂时无法回复，本次未执行任务")
)

type Request struct {
	ID        uuid.UUID  `json:"id"`
	Content   string     `json:"content"`
	ModelID   uuid.UUID  `json:"model_id"`
	ProjectID *uuid.UUID `json:"project_id"`
}
type Turn struct {
	ID           uuid.UUID   `json:"id"`
	Seq          int64       `json:"seq"`
	Content      string      `json:"content"`
	Reply        string      `json:"reply"`
	MemoryNotice string      `json:"memory_notice,omitempty"`
	Status       string      `json:"status"`
	CreatedAt    time.Time   `json:"created_at"`
	Changes      []Change    `json:"changes"`
	Task         *Delegation `json:"task,omitempty"`
}
type Memory struct {
	ID           uuid.UUID  `json:"id"`
	ProjectID    *uuid.UUID `json:"project_id"`
	Kind         string     `json:"kind"`
	Topic        string     `json:"topic"`
	Content      string     `json:"content"`
	Evidence     string     `json:"evidence"`
	SourceTurnID *uuid.UUID `json:"source_turn_id"`
	Automatic    bool       `json:"automatic"`
	Deleted      bool       `json:"deleted"`
	Revision     int64      `json:"revision"`
	UpdatedAt    time.Time  `json:"updated_at"`
}
type Change struct {
	ID        uuid.UUID `json:"id"`
	MemoryID  uuid.UUID `json:"memory_id"`
	Topic     string    `json:"topic"`
	Deleted   bool      `json:"deleted"`
	Automatic bool      `json:"automatic"`
	Undone    bool      `json:"undone"`
}
type Delegation struct {
	ConversationID uuid.UUID  `json:"conversation_id"`
	ProjectID      uuid.UUID  `json:"project_id"`
	Title          string     `json:"title"`
	Status         string     `json:"status"`
	RunID          *uuid.UUID `json:"run_id"`
	InputID        *uuid.UUID `json:"input_id"`
}
type Task struct {
	ID          uuid.UUID  `json:"id"`
	ProjectID   uuid.UUID  `json:"project_id"`
	ProjectName string     `json:"project_name"`
	Title       string     `json:"title"`
	Status      string     `json:"status"`
	RunID       *uuid.UUID `json:"run_id"`
	Reply       string     `json:"reply"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
type projectItem struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Default bool      `json:"is_default"`
}
type memoryAction struct {
	Operation string `json:"operation"`
	ID        string `json:"id"`
	Scope     string `json:"scope"`
	Kind      string `json:"kind"`
	Topic     string `json:"topic"`
	Content   string `json:"content"`
	Evidence  string `json:"evidence"`
	Automatic bool   `json:"automatic"`
}
type taskAction struct {
	Action         string `json:"action"`
	ProjectID      string `json:"project_id"`
	ConversationID string `json:"conversation_id"`
	Title          string `json:"title"`
	Instructions   string `json:"instructions"`
	Evidence       string `json:"evidence"`
}
type modelReply struct {
	Reply   string         `json:"reply"`
	Changes []memoryAction `json:"memory_changes"`
	Task    taskAction     `json:"task"`
}

func bounded(s string, min, max int) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	return n >= min && n <= max
}
func decodeReply(raw string) (modelReply, error) {
	var v modelReply
	if len(raw) > 24000 {
		return v, ErrModel
	}
	d := json.NewDecoder(bytes.NewBufferString(raw))
	d.DisallowUnknownFields()
	if d.Decode(&v) != nil || d.Decode(new(any)) != io.EOF || !bounded(v.Reply, 1, 4000) || len(v.Changes) > 4 {
		return v, ErrModel
	}
	// Independently require the whole schema, even when a provider ignores it.
	var shape map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &shape) != nil || len(shape) != 3 {
		return v, ErrModel
	}
	var task map[string]json.RawMessage
	if json.Unmarshal(shape["task"], &task) != nil || len(task) != 6 {
		return v, ErrModel
	}
	for _, key := range []string{"action", "project_id", "conversation_id", "title", "instructions", "evidence"} {
		if len(task[key]) == 0 || task[key][0] != '"' {
			return v, ErrModel
		}
	}
	var changes []map[string]json.RawMessage
	if len(shape["memory_changes"]) == 0 || shape["memory_changes"][0] != '[' || json.Unmarshal(shape["memory_changes"], &changes) != nil {
		return v, ErrModel
	}
	for _, change := range changes {
		if len(change) != 8 {
			return v, ErrModel
		}
		for _, key := range []string{"operation", "id", "scope", "kind", "topic", "content", "evidence"} {
			if len(change[key]) == 0 || change[key][0] != '"' {
				return v, ErrModel
			}
		}
		if string(change["automatic"]) != "true" && string(change["automatic"]) != "false" {
			return v, ErrModel
		}
	}
	if v.Task.Action != "none" && v.Task.Action != "create" && v.Task.Action != "continue" && v.Task.Action != "inspect" && v.Task.Action != "search" {
		return v, ErrModel
	}
	if (v.Task.Action == "inspect" || v.Task.Action == "search") && len(v.Changes) > 0 {
		return v, ErrModel
	}
	return v, nil
}

func responseSchema() map[string]any {
	str := map[string]any{"type": "string"}
	object := func(props map[string]any) map[string]any {
		required := make([]string, 0, len(props))
		for k := range props {
			required = append(required, k)
		}
		sort.Strings(required)
		return map[string]any{"type": "object", "additionalProperties": false, "properties": props, "required": required}
	}
	return object(map[string]any{
		"reply":          str,
		"memory_changes": map[string]any{"type": "array", "items": object(map[string]any{"operation": map[string]any{"type": "string", "enum": []string{"remember", "forget"}}, "id": str, "scope": map[string]any{"type": "string", "enum": []string{"personal", "project"}}, "kind": map[string]any{"type": "string", "enum": []string{"preference", "project", "work"}}, "topic": str, "content": str, "evidence": str, "automatic": map[string]any{"type": "boolean"}})},
		"task":           object(map[string]any{"action": map[string]any{"type": "string", "enum": []string{"none", "inspect", "search", "create", "continue"}}, "project_id": str, "conversation_id": str, "title": str, "instructions": str, "evidence": str}),
	})
}
