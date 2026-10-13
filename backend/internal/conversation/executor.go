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
	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
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
	var acceptedModel *uuid.UUID
	if err = s.db.QueryRow(ctx, `SELECT model_deployment_id FROM runs WHERE id=$1 AND conversation_id=$2`, runID, conversationID).Scan(&acceptedModel); err != nil {
		s.finishExecutionError(ctx, runID, conversationID, err)
		return
	}
	if acceptedModel != nil {
		conversation.ModelDeploymentID = *acceptedModel
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
	if err = s.installSelectedSkills(ctx, workspaceID, conversation, runID, messages, computer); err != nil {
		s.finishExecutionError(ctx, runID, conversationID, err)
		return
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
	e.executeTurns(ctx, conversationID, runID, computer, client, request, toolcontext.BudgetConfig{ContextWindow: deployment.ContextWindow, OutputReserve: deployment.OutputReserve, PolicyWindow: deployment.PolicyWindow})
}

func (e *runExecutor) executeTurns(ctx context.Context, conversationID, runID uuid.UUID, computer *Computer, client model.ModelClient, request model.ModelRequest, budgets ...toolcontext.BudgetConfig) {
	s := e.service
	var estimator toolcontext.Estimator
	var config toolcontext.BudgetConfig
	if len(budgets) > 0 {
		config = budgets[0]
	}
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
		budget, err := estimator.Evaluate(modelRequest, config)
		if err != nil {
			s.finishExecutionError(ctx, runID, conversationID, err)
			return
		}
		if budget.Exceeded {
			s.finishExecutionError(ctx, runID, conversationID, errors.New("上下文超过已配置的预算；请缩短输入或调整模型预算。"))
			return
		}
		requestIndex := turn + 1
		if err = s.startModelRequest(ctx, runID, requestIndex, "execution", budget); err != nil {
			s.finishExecutionError(ctx, runID, conversationID, err)
			return
		}
		var usage *modelruntime.NormalizedUsage
		finishedUsage := false
		finishUsage := func(requestErr error) error {
			if finishedUsage {
				return nil
			}
			finishedUsage = true
			if usage == nil {
				usage = &modelruntime.NormalizedUsage{Source: modelruntime.UsageMissing}
			}
			return s.finishModelRequest(ctx, runID, requestIndex, usage, requestErr)
		}
		// A historical read that is no longer visible to this model request does
		// not grant permission to overwrite that file. Keep the state shared only
		// within this visible turn's tool batch.
		files := agenttool.NewFileState(modelRequest.Messages)
		s.event(ctx, runID, conversationID, "MODEL_STARTED", map[string]any{"turn": turn + 1, "request_index": requestIndex, "tool_context": projection.Stats, "context_budget": budget})
		stream, err := client.Stream(ctx, modelRequest)
		if err != nil {
			_ = finishUsage(err)
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
			if event.NormalizedUsage != nil {
				usage = event.NormalizedUsage.Clone()
			}
			if event.Err != nil {
				_ = finishUsage(event.Err)
				deltaTicker.Stop()
				if ctx.Err() != nil {
					cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					flushDeltaWithContext(cleanupCtx)
					cancel()
					s.savePartialResponse(runID, conversationID, text.String(), callsByIndex)
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
			for _, fragment := range event.ToolCallDeltas() {
				if fragment == nil {
					continue
				}
				call := callsByIndex[fragment.Index]
				if call == nil {
					call = &model.ToolCall{Index: fragment.Index}
					callsByIndex[fragment.Index] = call
				}
				if fragment.ID != "" {
					call.ID = fragment.ID
				}
				if fragment.Name != "" {
					call.Name = fragment.Name
				}
				if arguments := fragment.Arguments; len(arguments) > 0 && string(arguments) != "{}" {
					call.Arguments = append(call.Arguments, arguments...)
				}
			}
		}
		deltaTicker.Stop()
		if ctx.Err() != nil {
			_ = finishUsage(ctx.Err())
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			flushDeltaWithContext(cleanupCtx)
			cancel()
			s.savePartialResponse(runID, conversationID, text.String(), callsByIndex)
			s.finishContext(ctx, runID, conversationID)
			return
		}
		flushDelta()
		calls, err := assembledToolCalls(callsByIndex)
		if err != nil {
			_ = finishUsage(err)
			s.savePartialResponse(runID, conversationID, text.String(), callsByIndex)
			s.finishExecutionError(ctx, runID, conversationID, err)
			return
		}
		if len(calls) == 0 && strings.TrimSpace(text.String()) == "" {
			empty := &modelruntime.ProviderError{Kind: modelruntime.ErrorProtocol}
			_ = finishUsage(empty)
			s.finishExecutionError(ctx, runID, conversationID, empty)
			return
		}
		if err = finishUsage(nil); err != nil {
			s.finishExecutionError(ctx, runID, conversationID, err)
			return
		}
		estimator.Observe(toolcontext.EstimateInput(modelRequest), usage)
		assistant := model.Message{Role: "assistant", Content: text.String(), ToolCalls: calls, RunID: runID.String()}
		if err = s.appendMessage(ctx, conversationID, runID, assistant, "", nil); err != nil {
			s.finishExecutionError(ctx, runID, conversationID, err)
			return
		}
		s.event(ctx, runID, conversationID, "MODEL_COMPLETED", map[string]any{"request_index": requestIndex, "usage": usage, "context_budget": budget})
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
			result, toolErr := e.runTool(ctx, runID, conversationID, computer, call, files)
			if ctx.Err() != nil {
				if errors.Is(context.Cause(ctx), ErrRunCancelled) && result != nil {
					observed, encodeErr := json.Marshal(result)
					if encodeErr == nil {
						var envelope map[string]any
						if json.Unmarshal(observed, &envelope) != nil || envelope == nil {
							envelope = map[string]any{"observed_result": json.RawMessage(observed)}
						}
						envelope["cancelled"] = true
						envelope["error"] = "The turn was cancelled. This result was observed; inspect its status and current state before continuing."
						if raw, marshalErr := json.Marshal(envelope); marshalErr == nil {
							cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							_ = s.appendMessage(cleanupCtx, conversationID, runID, model.Message{Role: "tool", Content: string(raw), ToolCallID: call.ID}, call.Name, map[string]any{"is_error": true, "cancelled": true})
							cancel()
						}
					}
				}
				s.finishContext(ctx, runID, conversationID)
				return
			}
			eventType := "TOOL_COMPLETED"
			metadata := map[string]any{}
			if commandResultFailed(call.Name, result) {
				metadata["is_error"] = true
				eventType = "TOOL_FAILED"
			}
			if toolErr != nil {
				if retained, ok := result.(map[string]any); ok && retained != nil {
					retained["error"] = toolErr.Error()
				} else {
					result = map[string]any{"error": toolErr.Error(), "observed_result": result}
				}
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

func (e *runExecutor) runTool(ctx context.Context, runID, conversationID uuid.UUID, computer *Computer, call model.ToolCall, fileStates ...*agenttool.FileState) (any, error) {
	s := e.service
	_, _ = s.db.Exec(ctx, `UPDATE sandboxes SET last_active_at=now() WHERE provider_ref=$1`, computer.SandboxID)
	environment := agenttool.Environment{
		RunID: runID, ConversationID: conversationID,
		SandboxID: computer.SandboxID, WorkDir: computer.WorkDir,
		Sandboxes: s.sandboxes,
		RecordTask: func(taskCtx context.Context, snapshot *sandbox.CommandTaskSnapshot) error {
			return s.recordCommandTask(taskCtx, runID, conversationID, computer, snapshot)
		},
		TaskAccess: func(taskCtx context.Context, taskID uuid.UUID, read sandbox.CommandTaskReadOptions, stop bool) (map[string]any, error) {
			var workspaceID uuid.UUID
			if err := s.db.QueryRow(taskCtx, `SELECT c.workspace_id FROM conversations c JOIN runs r ON r.conversation_id=c.id WHERE c.id=$1 AND r.id=$2 AND r.status='running'`, conversationID, runID).Scan(&workspaceID); err != nil {
				return nil, err
			}
			return s.commandTask(taskCtx, workspaceID, conversationID, taskID, read, stop)
		},
		Emit: func(eventType string, payload map[string]any) {
			payload["tool_call_id"] = call.ID
			payload["tool"] = call.Name
			s.event(ctx, runID, conversationID, eventType, payload)
		},
	}
	if len(fileStates) > 0 {
		environment.Files = fileStates[0]
	}
	return s.tools.Execute(ctx, call.Name, call.Arguments, environment)
}

// An executed command can fail without a transport/tool error. Keep its full
// result so the Agent can repair it; only its event and audit status change.
func commandResultFailed(tool string, result any) bool {
	if tool != "bash" && tool != "computer_exec" {
		return false
	}
	value, ok := result.(map[string]any)
	if !ok {
		return false
	}
	if status, _ := value["status"].(string); status == "timed_out" || status == "cancelled" || status == "unknown" {
		return true
	}
	if known, present := value["exit_code_known"].(bool); present && !known {
		return false
	}
	switch code := value["exit_code"].(type) {
	case int:
		return code != 0
	case float64:
		return code != 0
	default:
		return false
	}
}
