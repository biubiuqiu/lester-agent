package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/toolcontext"
	"github.com/biubiuqiu/lester-agent/backend/prompts"
	"github.com/google/uuid"
)

// runExecutor owns in-process execution and shutdown, while Service owns the
// conversation API and durable state. It is not a queue or remote worker.
type runExecutor struct {
	service  *Service
	mu       sync.Mutex
	stopping bool
	tasks    sync.WaitGroup
	active   sync.Map
}

func (e *runExecutor) start(guard *runGuard, workspaceID, conversationID, runID uuid.UUID) bool {
	return e.launch(guard, conversationID, runID, func(ctx context.Context) { e.execute(ctx, workspaceID, conversationID, runID) })
}

func (e *runExecutor) launch(guard *runGuard, conversationID, runID uuid.UUID, run func(context.Context)) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopping {
		return false
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	execution := &activeExecution{cancel: cancel, done: make(chan struct{})}
	e.active.Store(runID, execution)
	e.tasks.Add(1)
	go func() {
		defer e.tasks.Done()
		defer func() { cancel(context.Canceled); guard.Close(); e.active.Delete(runID); close(execution.done) }()
		defer func() {
			if recover() != nil {
				slog.Error("run executor panic", "run_id", runID)
				e.service.finishExecutionError(ctx, runID, conversationID, errors.New("run executor stopped unexpectedly"))
			}
		}()
		stop := guard.Watch(ctx, runID, cancel)
		defer stop()
		run(ctx)
	}()
	return true
}

func (s *Service) Shutdown(ctx context.Context) error {
	e := s.executor
	e.mu.Lock()
	e.stopping = true
	e.active.Range(func(_, value any) bool {
		value.(*activeExecution).cancel(errors.New("API shutting down; execution interrupted, tools were not replayed"))
		return true
	})
	e.mu.Unlock()
	done := make(chan struct{})
	go func() { e.tasks.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *runExecutor) execute(ctx context.Context, workspaceID, conversationID, runID uuid.UUID) {
	s := e.service
	if ctx.Err() != nil {
		s.finishContext(ctx, runID, conversationID)
		return
	}
	conversation, messages, err := s.Get(ctx, workspaceID, conversationID)
	if err != nil {
		s.finishExecutionError(ctx, runID, conversationID, err)
		return
	}
	if conversation.ModelDeploymentID == uuid.Nil {
		s.finishExecutionError(ctx, runID, conversationID, errors.New("configure a default model deployment first"))
		return
	}
	client, deployment, err := s.models.Client(ctx, workspaceID, conversation.ModelDeploymentID)
	if err != nil {
		s.finishExecutionError(ctx, runID, conversationID, err)
		return
	}
	computer, err := s.ensureComputer(ctx, conversation)
	if err != nil {
		s.finishExecutionError(ctx, runID, conversationID, err)
		return
	}
	resources, err := s.installAgentFiles(ctx, workspaceID, conversation, computer)
	if err != nil {
		s.finishExecutionError(ctx, runID, conversationID, err)
		return
	}
	if !conversation.AgentSkillsInstalled {
		if s.installAgentSkill == nil {
			s.finishExecutionError(ctx, runID, conversationID, errors.New("agent skill installer unavailable"))
			return
		}
		for _, slug := range conversation.AgentSkillSlugs {
			var installed bool
			if err = s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_skills cs JOIN skills sk ON sk.id=cs.skill_id WHERE cs.conversation_id=$1 AND sk.slug=$2)`, conversationID, slug).Scan(&installed); err != nil {
				s.finishExecutionError(ctx, runID, conversationID, err)
				return
			}
			if !installed {
				if err = s.installAgentSkill(ctx, workspaceID, conversation.CreatedBy, conversationID, computer.SandboxID, computer.WorkDir, slug); err != nil {
					s.finishExecutionError(ctx, runID, conversationID, err)
					return
				}
			}
		}
		if _, err = s.db.Exec(ctx, `UPDATE conversations SET agent_skills_installed=true WHERE id=$1 AND workspace_id=$2`, conversationID, workspaceID); err != nil {
			s.finishExecutionError(ctx, runID, conversationID, err)
			return
		}
	}
	skills, err := s.installedSkills(ctx, workspaceID, conversationID)
	if err != nil {
		s.finishExecutionError(ctx, runID, conversationID, err)
		return
	}
	promptSkills := make([]prompts.Skill, 0, len(skills))
	for _, item := range skills {
		promptSkills = append(promptSkills, prompts.Skill{Slug: item.Slug, Name: item.Name, Description: item.Description})
	}
	var system string
	if conversation.AgentInstructions != "" {
		system, err = prompts.ComposeCustom(conversation.AgentInstructions, conversationID.String(), workspaceID.String(), deployment.Name, computer.Status, promptSkills)
	} else {
		system, err = prompts.Compose(conversation.AgentSlug, conversationID.String(), workspaceID.String(), deployment.Name, computer.Status, promptSkills)
	}
	if err != nil {
		s.finishExecutionError(ctx, runID, conversationID, err)
		return
	}
	if conversation.AgentSlug == "agent-designer" {
		catalog, catalogErr := s.designerSkillCatalog(ctx)
		if catalogErr != nil {
			s.finishExecutionError(ctx, runID, conversationID, catalogErr)
			return
		}
		system += catalog
	}
	if len(resources) > 0 {
		var files strings.Builder
		files.WriteString("\n\n<agent_resources>\nThis conversation was initialized with these files. They may since have changed; inspect them when relevant. Their contents are not injected into this prompt.\n")
		for _, name := range resources {
			fmt.Fprintf(&files, "- agent-resources/%s\n", name)
		}
		files.WriteString("</agent_resources>")
		system += files.String()
	}
	history := modelHistory(messages)
	toolDefinitions := s.tools.Definitions()
	if conversation.AgentSlug != "agent-designer" {
		toolDefinitions = slices.DeleteFunc(toolDefinitions, func(tool model.Tool) bool { return tool.Name == "save_agent" })
	}
	request := model.ModelRequest{Model: deployment.ModelID, System: system, Messages: history, Tools: toolDefinitions}
	if err = s.saveRunContext(ctx, runID, messages, request); err != nil {
		s.finishExecutionError(ctx, runID, conversationID, err)
		return
	}
	e.executeTurns(ctx, conversationID, runID, computer, client, request)
}

func (e *runExecutor) executeTurns(ctx context.Context, conversationID, runID uuid.UUID, computer *Computer, client model.ModelClient, request model.ModelRequest) {
	s := e.service
	for turn := 0; ; turn++ {
		if ctx.Err() != nil {
			s.finishContext(ctx, runID, conversationID)
			return
		}
		// request retains the complete transcript; only this iteration's copy is
		// projected. Never append new messages to a previously pruned history.
		projection, err := toolcontext.Build(request.Messages)
		if err != nil {
			s.finishExecutionError(ctx, runID, conversationID, fmt.Errorf("build tool context: %w", err))
			return
		}
		modelRequest := request
		modelRequest.Messages = projection.Messages
		s.event(ctx, runID, conversationID, "MODEL_STARTED", map[string]any{"turn": turn + 1, "tool_context": projection.Stats})
		stream, err := client.Stream(ctx, modelRequest)
		if err != nil {
			s.finishExecutionError(ctx, runID, conversationID, err)
			return
		}
		var text, pendingDelta strings.Builder
		flushDeltaWithContext := func(eventCtx context.Context) {
			if pendingDelta.Len() == 0 {
				return
			}
			s.event(eventCtx, runID, conversationID, "MODEL_DELTA", map[string]any{"delta": pendingDelta.String()})
			pendingDelta.Reset()
		}
		flushDelta := func() { flushDeltaWithContext(ctx) }
		deltaTicker := time.NewTicker(75 * time.Millisecond)
		callsByIndex := map[int]*model.ToolCall{}
		streamOpen := true
		for streamOpen {
			var event model.ModelEvent
			var ok bool
			select {
			case event, ok = <-stream:
				if !ok {
					streamOpen = false
					continue
				}
			case <-deltaTicker.C:
				flushDelta()
				continue
			case <-ctx.Done():
				streamOpen = false
				continue
			}
			if event.Err != nil {
				deltaTicker.Stop()
				if ctx.Err() != nil {
					cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					flushDeltaWithContext(cleanupCtx)
					cancel()
					s.finishContext(ctx, runID, conversationID)
					return
				}
				flushDelta()
				s.savePartialResponse(runID, conversationID, text.String(), callsByIndex)
				s.finishExecutionError(ctx, runID, conversationID, event.Err)
				return
			}
			if event.Delta != "" {
				text.WriteString(event.Delta)
				pendingDelta.WriteString(event.Delta)
				if pendingDelta.Len() >= 2048 {
					flushDelta()
				}
			}
			if event.ToolCall != nil {
				call := callsByIndex[event.ToolCall.Index]
				if call == nil {
					call = &model.ToolCall{Index: event.ToolCall.Index}
					callsByIndex[event.ToolCall.Index] = call
				}
				if event.ToolCall.ID != "" {
					call.ID = event.ToolCall.ID
				}
				if event.ToolCall.Name != "" {
					call.Name = event.ToolCall.Name
				}
				if fragment := event.ToolCall.Arguments; len(fragment) > 0 && string(fragment) != "{}" {
					call.Arguments = append(call.Arguments, fragment...)
				}
			}
		}
		deltaTicker.Stop()
		if ctx.Err() != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			flushDeltaWithContext(cleanupCtx)
			cancel()
			s.finishContext(ctx, runID, conversationID)
			return
		}
		flushDelta()
		calls, err := assembledToolCalls(callsByIndex)
		if err != nil {
			s.savePartialResponse(runID, conversationID, text.String(), callsByIndex)
			s.finishExecutionError(ctx, runID, conversationID, err)
			return
		}
		assistant := model.Message{Role: "assistant", Content: text.String(), ToolCalls: calls, RunID: runID.String()}
		if err = s.appendMessage(ctx, conversationID, runID, assistant, "", nil); err != nil {
			s.finishExecutionError(ctx, runID, conversationID, err)
			return
		}
		s.event(ctx, runID, conversationID, "MODEL_COMPLETED", map[string]any{})
		if len(calls) == 0 {
			if err = s.finishCompletedRun(ctx, runID, conversationID); err != nil {
				s.finishExecutionError(ctx, runID, conversationID, err)
			}
			return
		}
		request.Messages = append(request.Messages, assistant)
		for _, call := range calls {
			if ctx.Err() != nil {
				s.finishContext(ctx, runID, conversationID)
				return
			}
			s.event(ctx, runID, conversationID, "TOOL_STARTED", map[string]any{"tool": call.Name, "tool_call_id": call.ID, "arguments": string(call.Arguments)})
			result, toolErr := e.runTool(ctx, runID, conversationID, computer, call)
			if ctx.Err() != nil {
				s.finishContext(ctx, runID, conversationID)
				return
			}
			eventType := "TOOL_COMPLETED"
			metadata := map[string]any{}
			if toolErr != nil {
				result = map[string]any{"error": toolErr.Error()}
				metadata["is_error"] = true
				eventType = "TOOL_FAILED"
			}
			raw, err := json.Marshal(result)
			if err != nil {
				s.finishExecutionError(ctx, runID, conversationID, fmt.Errorf("encode tool result: %w", err))
				return
			}
			toolMessage := model.Message{Role: "tool", Content: string(raw), ToolCallID: call.ID}
			if err = s.appendMessage(ctx, conversationID, runID, toolMessage, call.Name, metadata); err != nil {
				s.finishExecutionError(ctx, runID, conversationID, err)
				return
			}
			payload := map[string]any{"tool": call.Name, "tool_call_id": call.ID, "result": json.RawMessage(raw)}
			if toolErr != nil {
				payload["error"] = toolErr.Error()
			}
			s.event(ctx, runID, conversationID, eventType, payload)
			request.Messages = append(request.Messages, toolMessage)
		}
	}
}

func (e *runExecutor) runTool(ctx context.Context, runID, conversationID uuid.UUID, computer *Computer, call model.ToolCall) (any, error) {
	s := e.service
	_, _ = s.db.Exec(ctx, `UPDATE sandboxes SET last_active_at=now() WHERE provider_ref=$1`, computer.SandboxID)
	environment := agenttool.Environment{
		RunID: runID, ConversationID: conversationID,
		SandboxID: computer.SandboxID, WorkDir: computer.WorkDir,
		Sandboxes: s.sandboxes,
		Emit: func(eventType string, payload map[string]any) {
			payload["tool_call_id"] = call.ID
			payload["tool"] = call.Name
			s.event(ctx, runID, conversationID, eventType, payload)
		},
	}
	return s.tools.Execute(ctx, call.Name, call.Arguments, environment)
}
