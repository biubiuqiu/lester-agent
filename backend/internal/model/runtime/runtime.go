package runtime

import (
	"context"
	"encoding/json"
)

type Message struct {
	Role, Content string
	ToolCallID    string     `json:"tool_call_id,omitempty"`
	ToolCalls     []ToolCall `json:"tool_calls,omitempty"`
	RunID         string     `json:"-"` // Local transcript provenance, never a provider wire field.
}

type Tool struct {
	Name, Description string
	InputSchema       map[string]any
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Index     int             `json:"-"`
}

type Request struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []Tool
	MaxTokens   int
	Temperature *float64
}

type Event struct {
	Type     string
	Delta    string
	ToolCall *ToolCall
	Usage    map[string]int
	Err      error
}

type Response struct {
	Content   string
	ToolCalls []ToolCall
	Usage     map[string]int
}

type Capabilities struct {
	Streaming, Tools, Vision, Reasoning, PromptCaching, StructuredOutput, TokenCounting bool
}

type Client interface {
	Generate(context.Context, Request) (*Response, error)
	Stream(context.Context, Request) (<-chan Event, error)
	Capabilities(context.Context, string) (Capabilities, error)
}
