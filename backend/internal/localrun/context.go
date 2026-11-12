package localrun

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/harness"
	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/biubiuqiu/lester-agent/backend/internal/toolcontext"
)

func (s *Store) ContextCheckpoint(ctx context.Context, id string) (*toolcontext.Checkpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadContext(ctx, id)
	return state.Checkpoint, err
}

// Persistence stays in the host's synchronous harness hooks, just as it does
// for PostgreSQL cloud conversations. Compaction projects a working set; it
// never rewrites the complete JSONL transcript or grants tool permissions.
func (r *Runner) prepareContext(ctx context.Context, run Run, client modelruntime.Client) (func(context.Context, *harness.State) (modelruntime.Request, error), error) {
	checkpoint, err := r.Store.ContextCheckpoint(ctx, run.ConversationID)
	if err != nil {
		return nil, err
	}
	var estimator toolcontext.Estimator
	disabled := false
	return func(ctx context.Context, state *harness.State) (modelruntime.Request, error) {
		request := localCheckpointRequest(state.Request, checkpoint)
		before, err := estimator.Evaluate(request, toolcontext.BudgetConfig{})
		if err != nil {
			return request, err
		}
		if before.ShouldCompact && !disabled {
			next, compactErr := r.compactContext(ctx, run, client, state.Request, checkpoint, &estimator, before)
			if ctx.Err() != nil {
				return request, ctx.Err()
			}
			if compactErr != nil {
				disabled = true
				if event, e := r.Store.AppendEvent(ctx, run.ConversationID, run.ID, "CONTEXT_COMPACTION_FAILED", map[string]any{"prior_checkpoint_preserved": true}); e == nil {
					r.emit(event)
				}
			} else {
				checkpoint = next
				request = localCheckpointRequest(state.Request, checkpoint)
				before, err = estimator.Evaluate(request, toolcontext.BudgetConfig{})
				if err != nil {
					return request, err
				}
			}
		}
		if before.Exceeded {
			return request, errors.New("local context exceeds its input budget; the complete history has been preserved")
		}
		return request, nil
	}, nil
}

func localCheckpointRequest(request modelruntime.Request, checkpoint *toolcontext.Checkpoint) modelruntime.Request {
	request.Messages = toolcontext.ApplyCheckpoint(request.Messages, checkpoint)
	if checkpoint != nil {
		request.Messages[0].Content = strings.Replace(request.Messages[0].Content, "Original results remain available through read_tool_result.", "Original results remain in the local transcript.", 1)
	}
	return request
}

func (r *Runner) compactContext(ctx context.Context, run Run, client modelruntime.Client, request modelruntime.Request, previous *toolcontext.Checkpoint, estimator *toolcontext.Estimator, before toolcontext.Budget) (*toolcontext.Checkpoint, error) {
	maxTokens := min(2048, before.OutputReserve)
	plan, err := toolcontext.PlanCompaction(request.Messages, previous, request.Model, estimator, toolcontext.BudgetConfig{OutputReserve: maxTokens}, maxTokens)
	if err != nil {
		return nil, err
	}
	summaryRequest := toolcontext.CompactionRequest(request.Model, plan, previous, maxTokens)
	summaryCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	response, err := client.Generate(summaryCtx, summaryRequest)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if response == nil || len(response.ToolCalls) > 0 {
		return nil, errors.New("local compaction returned no summary or attempted a tool call")
	}
	summary, err := toolcontext.ValidateSummary(response.Content)
	if err != nil {
		return nil, err
	}
	candidate := &toolcontext.Checkpoint{ThroughSeq: plan.ThroughSeq, RetainedIDs: plan.RetainedIDs, Summary: summary}
	after, err := estimator.Evaluate(localCheckpointRequest(request, candidate), toolcontext.BudgetConfig{})
	if err != nil {
		return nil, err
	}
	if after.EstimatedInput >= before.EstimatedInput-before.EstimatedInput/20 {
		return nil, errors.New("local compaction did not reduce context sufficiently")
	}
	checkpoint, err := r.Store.SaveCheckpoint(ctx, run.ConversationID, run.ID, previous, plan, summary, request.Model)
	if err != nil {
		return nil, err
	}
	event, err := r.Store.AppendEvent(ctx, run.ConversationID, run.ID, "CONTEXT_COMPACTED", map[string]any{"checkpoint_id": checkpoint.ID, "through_seq": checkpoint.ThroughSeq, "source_count": checkpoint.SourceCount, "estimated_before": before.EstimatedInput, "estimated_after": after.EstimatedInput, "source": "estimated"})
	if err == nil {
		r.emit(event)
	}
	return checkpoint, err
}
