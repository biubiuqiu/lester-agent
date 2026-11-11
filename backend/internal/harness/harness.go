// Package harness owns the model/tool execution loop shared by cloud and local
// runners. Persistence, context policy, permissions and tools belong to its host.
package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
)

// State retains the complete transcript. A host may project a separate request
// in PrepareTurn without pruning this canonical history.
type State struct {
	Request modelruntime.Request
	Turn    int
}

type Turn struct {
	Number  int
	Request modelruntime.Request
}

type Result struct {
	Turn         Turn
	Text         string
	Calls        []modelruntime.ToolCall
	CallsByIndex map[int]*modelruntime.ToolCall
	Usage        *modelruntime.NormalizedUsage
}

// Hooks execute synchronously and in transcript order. A tool failure becomes a
// model-visible tool result; a persistence failure stops the loop. No request or
// tool is retried unless RecoverModel explicitly accepts the model error.
type Hooks struct {
	PrepareTurn     func(context.Context, *State) (modelruntime.Request, error)
	ModelStarted    func(context.Context, Turn) error
	Delta           func(context.Context, string)
	ModelFinished   func(context.Context, Result, error) error
	RecoverModel    func(context.Context, Result, error) (bool, error)
	Partial         func(context.Context, Result)
	Assistant       func(context.Context, Result, modelruntime.Message) (modelruntime.Message, error)
	ToolStarted     func(context.Context, modelruntime.ToolCall)
	ExecuteTool     func(context.Context, modelruntime.ToolCall) (any, error)
	ToolResult      func(context.Context, modelruntime.ToolCall, any, error) (modelruntime.Message, error)
	ToolInterrupted func(context.Context, modelruntime.ToolCall, any)
	// Complete can request another turn when a host atomically discovers new
	// accepted input at the final-response boundary.
	Complete func(context.Context, *State) (bool, error)
}

func Run(ctx context.Context, client modelruntime.Client, request modelruntime.Request, hooks Hooks) (State, error) {
	state := State{Request: request}
	if client == nil {
		return state, errors.New("model client is unavailable")
	}
	for {
		if ctx.Err() != nil {
			return state, cancellationError(ctx)
		}
		state.Turn++
		prepared := state.Request
		var err error
		if hooks.PrepareTurn != nil {
			prepared, err = hooks.PrepareTurn(ctx, &state)
			if err != nil {
				return state, err
			}
		}
		if ctx.Err() != nil {
			return state, cancellationError(ctx)
		}
		turn := Turn{Number: state.Turn, Request: prepared}
		if hooks.ModelStarted != nil {
			if err = hooks.ModelStarted(ctx, turn); err != nil {
				return state, err
			}
		}
		stream, streamErr := client.Stream(ctx, prepared)
		if streamErr != nil || stream == nil {
			if streamErr == nil {
				streamErr = &modelruntime.ProviderError{Kind: modelruntime.ErrorProtocol}
			}
			result := missingUsage(Result{Turn: turn})
			if finishErr := finishModel(ctx, hooks, result, streamErr); finishErr != nil {
				return state, errors.Join(streamErr, finishErr)
			}
			retry, recoveryErr := recoverModel(ctx, hooks, result, streamErr)
			if recoveryErr != nil {
				return state, recoveryErr
			}
			if retry {
				continue
			}
			return state, streamErr
		}

		result, streamErr, flush := consumeStream(ctx, turn, stream, hooks.Delta)
		result = missingUsage(result)
		if streamErr != nil || ctx.Err() != nil {
			if streamErr == nil {
				streamErr = ctx.Err()
			}
			finishErr := finishModel(ctx, hooks, result, streamErr)
			if finishErr == nil {
				retry, recoveryErr := recoverModel(ctx, hooks, result, streamErr)
				if recoveryErr != nil {
					return state, recoveryErr
				}
				if retry {
					continue
				}
			}
			flushPartial(ctx, flush)
			if hooks.Partial != nil {
				hooks.Partial(ctx, result)
			}
			if ctx.Err() != nil {
				return state, cancellationError(ctx)
			}
			return state, errors.Join(streamErr, finishErr)
		}
		flush(ctx)
		result.Calls, err = AssembleToolCalls(result.CallsByIndex)
		if err != nil {
			finishErr := finishModel(ctx, hooks, result, err)
			if hooks.Partial != nil {
				hooks.Partial(ctx, result)
			}
			return state, errors.Join(err, finishErr)
		}
		if len(result.Calls) == 0 && strings.TrimSpace(result.Text) == "" {
			err = &modelruntime.ProviderError{Kind: modelruntime.ErrorProtocol}
			return state, errors.Join(err, finishModel(ctx, hooks, result, err))
		}
		if err = finishModel(ctx, hooks, result, nil); err != nil {
			return state, err
		}
		assistant := modelruntime.Message{Role: "assistant", Content: result.Text, ToolCalls: result.Calls}
		if hooks.Assistant != nil {
			assistant, err = hooks.Assistant(ctx, result, assistant)
			if err != nil {
				return state, err
			}
		}
		state.Request.Messages = append(state.Request.Messages, assistant)
		if len(result.Calls) == 0 {
			if hooks.Complete != nil {
				more, completionErr := hooks.Complete(ctx, &state)
				if completionErr != nil {
					return state, completionErr
				}
				if more {
					continue
				}
			}
			return state, nil
		}
		for _, call := range result.Calls {
			if ctx.Err() != nil {
				return state, cancellationError(ctx)
			}
			if hooks.ToolStarted != nil {
				hooks.ToolStarted(ctx, call)
			}
			if hooks.ExecuteTool == nil {
				return state, errors.New("tool execution is unavailable")
			}
			value, toolErr := hooks.ExecuteTool(ctx, call)
			if ctx.Err() != nil {
				if hooks.ToolInterrupted != nil {
					hooks.ToolInterrupted(ctx, call, value)
				}
				return state, cancellationError(ctx)
			}
			toolMessage := modelruntime.Message{}
			if hooks.ToolResult != nil {
				toolMessage, err = hooks.ToolResult(ctx, call, value, toolErr)
			} else {
				toolMessage, err = ToolResultMessage(call, value, toolErr)
			}
			if err != nil {
				return state, err
			}
			state.Request.Messages = append(state.Request.Messages, toolMessage)
		}
	}
}

func cancellationError(ctx context.Context) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return ctx.Err()
}

func missingUsage(result Result) Result {
	if result.Usage == nil {
		result.Usage = &modelruntime.NormalizedUsage{Source: modelruntime.UsageMissing}
	}
	return result
}

func finishModel(ctx context.Context, hooks Hooks, result Result, requestErr error) error {
	if hooks.ModelFinished != nil {
		return hooks.ModelFinished(ctx, result, requestErr)
	}
	return nil
}

func recoverModel(ctx context.Context, hooks Hooks, result Result, err error) (bool, error) {
	if hooks.RecoverModel == nil || ctx.Err() != nil || result.Text != "" || len(result.CallsByIndex) > 0 {
		return false, nil
	}
	return hooks.RecoverModel(ctx, result, err)
}

func flushPartial(ctx context.Context, flush func(context.Context)) {
	if ctx.Err() == nil {
		flush(ctx)
		return
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	flush(cleanup)
}

func consumeStream(ctx context.Context, turn Turn, stream <-chan modelruntime.Event, delta func(context.Context, string)) (Result, error, func(context.Context)) {
	result := Result{Turn: turn, CallsByIndex: map[int]*modelruntime.ToolCall{}}
	var text, pending strings.Builder
	flush := func(eventCtx context.Context) {
		if pending.Len() == 0 {
			return
		}
		if delta != nil {
			delta(eventCtx, pending.String())
		}
		pending.Reset()
	}
	ticker := time.NewTicker(75 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			result.Text = text.String()
			return result, ctx.Err(), flush
		}
		var event modelruntime.Event
		var open bool
		select {
		case event, open = <-stream:
			if !open {
				result.Text = text.String()
				return result, nil, flush
			}
		case <-ticker.C:
			flush(ctx)
			continue
		case <-ctx.Done():
			result.Text = text.String()
			return result, ctx.Err(), flush
		}
		if event.NormalizedUsage != nil {
			result.Usage = event.NormalizedUsage.Clone()
		}
		if event.Err != nil {
			result.Text = text.String()
			return result, event.Err, flush
		}
		if event.Delta != "" {
			text.WriteString(event.Delta)
			pending.WriteString(event.Delta)
			if pending.Len() >= 2048 {
				flush(ctx)
			}
		}
		for _, fragment := range event.ToolCallDeltas() {
			if fragment == nil {
				continue
			}
			call := result.CallsByIndex[fragment.Index]
			if call == nil {
				call = &modelruntime.ToolCall{Index: fragment.Index}
				result.CallsByIndex[fragment.Index] = call
			}
			if fragment.ID != "" {
				call.ID = fragment.ID
			}
			if fragment.Name != "" {
				call.Name = fragment.Name
			}
			if len(fragment.Arguments) > 0 && string(fragment.Arguments) != "{}" {
				call.Arguments = append(call.Arguments, fragment.Arguments...)
			}
		}
	}
}

// AssembleToolCalls validates the entire batch before executing its first tool.
func AssembleToolCalls(fragments map[int]*modelruntime.ToolCall) ([]modelruntime.ToolCall, error) {
	indices := make([]int, 0, len(fragments))
	for index := range fragments {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	calls := make([]modelruntime.ToolCall, 0, len(indices))
	seen := map[string]bool{}
	for _, index := range indices {
		fragment := fragments[index]
		if fragment == nil || fragment.ID == "" || fragment.Name == "" || seen[fragment.ID] {
			return nil, errors.New("incomplete or duplicate model tool call")
		}
		call := *fragment
		call.Arguments = append(json.RawMessage(nil), fragment.Arguments...)
		if len(call.Arguments) == 0 {
			call.Arguments = json.RawMessage(`{}`)
		}
		if !json.Valid(call.Arguments) {
			return nil, errors.New("invalid model tool arguments")
		}
		seen[call.ID] = true
		call.Index = 0
		calls = append(calls, call)
	}
	return calls, nil
}

func ToolResultMessage(call modelruntime.ToolCall, result any, toolErr error) (modelruntime.Message, error) {
	if toolErr != nil {
		if retained, ok := result.(map[string]any); ok && retained != nil {
			retained["error"] = toolErr.Error()
		} else {
			result = map[string]any{"error": toolErr.Error(), "observed_result": result}
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return modelruntime.Message{}, fmt.Errorf("encode tool result: %w", err)
	}
	return modelruntime.Message{Role: "tool", Content: string(raw), ToolCallID: call.ID}, nil
}
