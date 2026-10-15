package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EventEmitter func(eventType string, payload map[string]any)

type Sandbox interface {
	Exec(context.Context, string, sandbox.Command) (*sandbox.CommandResult, error)
	ReadFile(context.Context, string, string, string) ([]byte, error)
	ReadFileLines(context.Context, string, string, string, int, int) (*sandbox.FileLines, error)
	WriteFile(context.Context, string, string, string, []byte) error
	EditFile(context.Context, string, string, string, sandbox.FileEditRequest) (*sandbox.FileEditResult, error)
	ListFiles(context.Context, string, string, string) ([]sandbox.FileEntry, error)
}

type Environment struct {
	RunID, ConversationID uuid.UUID
	SandboxID, WorkDir    string
	Sandboxes             Sandbox
	Files                 *FileState
	// RecordTask persists trusted launch identity before starting a command and
	// its actual ownership handle afterward. Never expose that handle to models.
	RecordFileChange func(context.Context, *FileChange) error
	RecordTask       func(context.Context, *sandbox.CommandTaskSnapshot) error
	TaskAccess       func(context.Context, uuid.UUID, sandbox.CommandTaskReadOptions, bool) (map[string]any, error)
	EnterPlan        func(context.Context, string) (map[string]any, error)
	Interact         func(context.Context, InteractionRequest) (map[string]any, error)
	AuthorizeTool    func(context.Context, string, json.RawMessage) error
	Emit             EventEmitter
}

type Handler interface {
	Definition() model.Tool
	Execute(context.Context, Environment, json.RawMessage) (any, error)
}

type Registry struct {
	definitions []model.Tool
	handlers    map[string]Handler
}

func NewRegistry() *Registry {
	return &Registry{handlers: map[string]Handler{}}
}

func NewDefaultRegistry(db *pgxpool.Pool) *Registry {
	registry := NewRegistry()
	registry.Register(Bash{}, "computer_exec")
	registry.Register(Read{}, "computer_read_file")
	registry.Register(Write{}, "computer_write_file")
	registry.Register(Edit{})
	registry.Register(LoadSkill{DB: db})
	registry.Register(TaskOutput{})
	registry.Register(StopTask{})
	registry.Register(ReadConversation{DB: db})
	registry.Register(ToolResult{DB: db})
	registry.Register(AskUserQuestion{})
	registry.Register(ProposePlan{})
	registry.Register(EnterPlan{})
	registry.RegisterHidden(ListFiles{}, "computer_list_files")
	return registry
}

func (r *Registry) Register(handler Handler, aliases ...string) {
	r.register(handler, true, aliases...)
}

func (r *Registry) RegisterHidden(handler Handler, aliases ...string) {
	r.register(handler, false, aliases...)
}

func (r *Registry) register(handler Handler, visible bool, aliases ...string) {
	definition := handler.Definition()
	if definition.Name == "" {
		panic("agent tool name cannot be empty")
	}
	if _, exists := r.handlers[definition.Name]; exists {
		panic("duplicate agent tool: " + definition.Name)
	}
	r.handlers[definition.Name] = handler
	for _, alias := range aliases {
		if _, exists := r.handlers[alias]; exists {
			panic("duplicate agent tool alias: " + alias)
		}
		r.handlers[alias] = handler
	}
	if visible {
		r.definitions = append(r.definitions, definition)
	}
}

func (r *Registry) Definitions() []model.Tool {
	return append([]model.Tool(nil), r.definitions...)
}

func (r *Registry) Execute(ctx context.Context, name string, arguments json.RawMessage, environment Environment) (any, error) {
	handler, exists := r.handlers[name]
	if !exists {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	if environment.Sandboxes == nil {
		return nil, errors.New("agent tool environment is incomplete")
	}
	if environment.AuthorizeTool != nil {
		if err := environment.AuthorizeTool(ctx, handler.Definition().Name, arguments); err != nil {
			return nil, err
		}
	}
	return handler.Execute(ctx, environment, arguments)
}

func pathProperty() map[string]any {
	return map[string]any{"type": "string", "description": "Path relative to the current conversation directory. Do not prefix it with /workspace."}
}
