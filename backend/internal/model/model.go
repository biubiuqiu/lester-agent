package model

import modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"

// Public aliases keep the conversation runtime independent from the concrete
// provider integrations while preserving the existing model package API.
type Message = modelruntime.Message
type Tool = modelruntime.Tool
type ToolCall = modelruntime.ToolCall
type ModelRequest = modelruntime.Request
type ModelEvent = modelruntime.Event
type ModelResponse = modelruntime.Response
type ModelCapabilities = modelruntime.Capabilities
type ModelClient = modelruntime.Client
