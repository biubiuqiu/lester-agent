// Package localrun owns the desktop device's durable task state and execution.
// It is reached only over the desktop main process's private stdio channel.
package localrun

import (
	"context"
	"encoding/json"
	"time"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
)

type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Conversation struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"project_id"`
	ProjectName     string    `json:"project_name"`
	Title           string    `json:"title"`
	ModelID         string    `json:"model_id"`
	PermissionMode  string    `json:"permission_mode"`
	ExecutionTarget string    `json:"execution_target"`
	DeviceID        string    `json:"device_id"`
	Status          string    `json:"status"`
	ActiveRunID     string    `json:"active_run_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Message struct {
	ID             string                  `json:"id"`
	ConversationID string                  `json:"conversation_id"`
	RunID          string                  `json:"run_id"`
	Seq            int64                   `json:"seq"`
	Role           string                  `json:"role"`
	Content        string                  `json:"content"`
	ToolCalls      []modelruntime.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID     string                  `json:"tool_call_id,omitempty"`
	Partial        bool                    `json:"partial,omitempty"`
	CreatedAt      time.Time               `json:"created_at"`
}

func (m Message) Runtime() modelruntime.Message {
	return modelruntime.Message{Role: m.Role, Content: m.Content, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID, RunID: m.RunID, MessageID: m.ID, Seq: m.Seq}
}

type Run struct {
	ID             string     `json:"id"`
	ConversationID string     `json:"conversation_id"`
	RequestID      string     `json:"request_id"`
	Status         string     `json:"status"`
	ModelID        string     `json:"model_id"`
	PermissionMode string     `json:"permission_mode"`
	ErrorCode      string     `json:"error_code,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

type Event struct {
	ConversationID string    `json:"conversation_id"`
	RunID          string    `json:"run_id,omitempty"`
	Seq            int64     `json:"seq"`
	Type           string    `json:"type"`
	Payload        any       `json:"payload"`
	CreatedAt      time.Time `json:"created_at"`
}

type Permission struct {
	ID             string          `json:"id"`
	ConversationID string          `json:"conversation_id"`
	RunID          string          `json:"run_id"`
	Tool           string          `json:"tool"`
	Arguments      json.RawMessage `json:"arguments"`
	Status         string          `json:"status"`
	CreatedAt      time.Time       `json:"created_at"`
}

// ModelConfig is accepted only from the main process. Credential is never
// serialized back to a renderer, an event, SQLite, or an error message.
type ModelConfig struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Source     string         `json:"source"`
	Provider   string         `json:"provider"`
	Protocol   string         `json:"protocol"`
	Endpoint   string         `json:"endpoint"`
	ModelID    string         `json:"model_id"`
	Config     map[string]any `json:"config"`
	Credential string         `json:"credential"`
}

type ModelInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Source   string `json:"source"`
	Provider string `json:"provider"`
	ModelID  string `json:"model_id"`
}

type ModelFactory func(context.Context, ModelConfig) (modelruntime.Client, error)

type Snapshot struct {
	Conversation Conversation `json:"conversation"`
	Messages     []Message    `json:"messages"`
	Permissions  []Permission `json:"permissions"`
	Runs         []Run        `json:"runs"`
	Events       []Event      `json:"events"`
}
