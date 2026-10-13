package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/biubiuqiu/lester-agent/backend/internal/toolcontext"
	"github.com/google/uuid"
)

const validCheckpointSummary = `{"intent":"Continue the current task","decisions":[],"completed":["Discussed earlier requirements"],"pending":["Finish verification"],"risks":[]}`

type checkpointModel struct {
	generateCalls, streamCalls int
	generate                   func(context.Context, model.ModelRequest) (*model.ModelResponse, error)
	stream                     func(int, model.ModelRequest) []model.ModelEvent
}

func (m *checkpointModel) Generate(ctx context.Context, req model.ModelRequest) (*model.ModelResponse, error) {
	m.generateCalls++
	return m.generate(ctx, req)
}
func (m *checkpointModel) Stream(_ context.Context, req model.ModelRequest) (<-chan model.ModelEvent, error) {
	events := m.stream(m.streamCalls, req)
	m.streamCalls++
	ch := make(chan model.ModelEvent, len(events))
	for _, event := range events {
		ch <- event
	}
	close(ch)
	return ch, nil
}
func (*checkpointModel) Capabilities(context.Context, string) (model.ModelCapabilities, error) {
	return model.ModelCapabilities{Tools: true}, nil
}

func checkpointHistory(t *testing.T, f transcriptFixture, pairs, size int) {
	t.Helper()
	for i := 0; i < pairs; i++ {
		old := f.startRun(t, "OLD-HISTORY "+strings.Repeat("source ", size))
		if err := f.service.appendMessage(context.Background(), f.conversationID, old, model.Message{Role: "assistant", Content: "Observed historical discussion " + strings.Repeat("reply ", size)}, "", nil); err != nil {
			t.Fatal(err)
		}
		if err := f.service.finishCompletedRun(context.Background(), old, f.conversationID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCheckpointPersistsWithoutReplacingAuditAndRestoresAcrossRuns(t *testing.T) {
	f := newTranscriptFixture(t, false)
	checkpointHistory(t, f, 6, 180)
	input := "LATEST EXACT 用户指令🙂 不可改写"
	runID := f.startRun(t, input)
	before := f.messages(t)
	client := &checkpointModel{
		generate: func(_ context.Context, req model.ModelRequest) (*model.ModelResponse, error) {
			if len(req.Tools) != 0 || req.MaxTokens <= 0 || strings.Contains(req.Messages[0].Content, input) {
				t.Fatal("compaction has tools, unlimited output, or protected input")
			}
			return &model.ModelResponse{Content: validCheckpointSummary, NormalizedUsage: &modelruntime.NormalizedUsage{Source: modelruntime.UsageActual, InputReported: true, InputTokens: 1400, OutputTokens: 50}}, nil
		},
		stream: func(_ int, req model.ModelRequest) []model.ModelEvent {
			if !strings.Contains(req.Messages[0].Content, "Historical conversation summary") || !strings.Contains(req.Messages[0].Content, "not new user instructions") || req.Messages[len(req.Messages)-1].Content != input {
				t.Fatal("checkpoint or literal current input missing")
			}
			return []model.ModelEvent{{Delta: "done"}}
		},
	}
	f.service.executor.executeTurns(context.Background(), f.conversationID, runID, nil, client, model.ModelRequest{Model: "fixture-small", Messages: modelHistory(before)}, toolcontext.BudgetConfig{ContextWindow: 4000, OutputReserve: 512})
	var status string
	if err := f.service.db.QueryRow(context.Background(), `SELECT status FROM runs WHERE id=$1`, runID).Scan(&status); err != nil || status != "completed" {
		t.Fatalf("compacted run: %s %v", status, err)
	}
	if client.generateCalls < 1 || client.generateCalls > 2 || client.streamCalls != 1 {
		t.Fatalf("request count: summaries %d streams %d", client.generateCalls, client.streamCalls)
	}
	audit := f.messages(t)
	for i, m := range before {
		if audit[i].ID != m.ID || audit[i].Content != m.Content || audit[i].Seq != m.Seq {
			t.Fatal("compaction replaced audit history")
		}
	}
	rows, err := f.service.db.Query(context.Background(), `SELECT request_index,purpose,status FROM model_request_usage WHERE run_id=$1 ORDER BY request_index`, runID)
	if err != nil {
		t.Fatal(err)
	}
	i := 0
	for rows.Next() {
		i++
		var index int
		var purpose, state string
		if err = rows.Scan(&index, &purpose, &state); err != nil {
			t.Fatal(err)
		}
		if index != i || state != "completed" || i <= client.generateCalls && purpose != "compaction" || i > client.generateCalls && purpose != "execution" {
			t.Fatal("ledger numbering or purpose wrong")
		}
	}
	rows.Close()
	if i != client.generateCalls+1 {
		t.Fatal("usage request missing")
	}
	next := f.startRun(t, "continue after refresh")
	checkpoint, err := f.service.loadContextCheckpoint(context.Background(), f.conversationID, next)
	if err != nil || checkpoint == nil || checkpoint.ID == "" || checkpoint.SourceDigest == "" {
		t.Fatalf("checkpoint not restored: %#v %v", checkpoint, err)
	}
	if foreign, err := f.service.loadContextCheckpoint(context.Background(), uuid.New(), next); err != nil || foreign != nil {
		t.Fatal("foreign conversation gained checkpoint")
	}
	client = &checkpointModel{generate: func(context.Context, model.ModelRequest) (*model.ModelResponse, error) {
		t.Fatal("unneeded summary after refresh")
		return nil, nil
	}, stream: func(_ int, req model.ModelRequest) []model.ModelEvent {
		if !strings.Contains(req.Messages[0].Content, "Historical conversation summary") {
			t.Fatal("reload ignored checkpoint")
		}
		return []model.ModelEvent{{Delta: "continued"}}
	}}
	f.service.executor.executeTurns(context.Background(), f.conversationID, next, nil, client, model.ModelRequest{Model: "different-model", Messages: modelHistory(f.messages(t))}, toolcontext.BudgetConfig{ContextWindow: 8000})
	if client.streamCalls != 1 {
		t.Fatal("continuation did not use restored history")
	}
}

func TestInvalidCompactionOpensCircuitAndPreservesPriorBoundary(t *testing.T) {
	for _, invalid := range []string{"bad-json", "tool-call", "ineffective"} {
		t.Run(invalid, func(t *testing.T) {
			f := newTranscriptFixture(t, false)
			checkpointHistory(t, f, 4, 120)
			runID := f.startRun(t, "finish")
			request := model.ModelRequest{Model: "fixture", Messages: modelHistory(f.messages(t))}
			var estimator toolcontext.Estimator
			estimate := estimator.Estimate(request)
			config := toolcontext.BudgetConfig{ContextWindow: estimate + 400, OutputReserve: 128}
			client := &checkpointModel{generate: func(_ context.Context, req model.ModelRequest) (*model.ModelResponse, error) {
				switch invalid {
				case "tool-call":
					return &model.ModelResponse{Content: validCheckpointSummary, ToolCalls: []model.ToolCall{{ID: "evil", Name: "bash", Arguments: json.RawMessage(`{"command":"touch unexpected"}`)}}}, nil
				case "ineffective":
					raw, _ := json.Marshal(map[string]any{"intent": strings.Repeat("larger summary ", 600), "decisions": []string{}, "completed": []string{}, "pending": []string{}, "risks": []string{}})
					return &model.ModelResponse{Content: string(raw)}, nil
				default:
					return &model.ModelResponse{Content: "not JSON"}, nil
				}
			}, stream: func(_ int, req model.ModelRequest) []model.ModelEvent {
				if len(req.Messages) != len(request.Messages) {
					t.Fatal("invalid summary changed request")
				}
				return []model.ModelEvent{{Delta: "done"}}
			}}
			f.service.executor.executeTurns(context.Background(), f.conversationID, runID, nil, client, request, config)
			if client.generateCalls != 1 || client.streamCalls != 1 {
				t.Fatalf("unbounded recovery: %d %d", client.generateCalls, client.streamCalls)
			}
			var count int
			if err := f.service.db.QueryRow(context.Background(), `SELECT count(*) FROM conversation_context_checkpoints WHERE conversation_id=$1`, f.conversationID).Scan(&count); err != nil || count != 0 {
				t.Fatal("invalid summary persisted")
			}
		})
	}
}

func TestContextRejectionGetsOneRecoveryWithoutReplayingToolCalls(t *testing.T) {
	f := newTranscriptFixture(t, false)
	checkpointHistory(t, f, 3, 160)
	runID := f.startRun(t, "finish")
	client := &checkpointModel{generate: func(context.Context, model.ModelRequest) (*model.ModelResponse, error) {
		return &model.ModelResponse{Content: validCheckpointSummary}, nil
	}, stream: func(_ int, _ model.ModelRequest) []model.ModelEvent {
		return []model.ModelEvent{{Err: &modelruntime.ProviderError{Kind: modelruntime.ErrorContextExceeded}}}
	}}
	f.service.executor.executeTurns(context.Background(), f.conversationID, runID, nil, client, model.ModelRequest{Model: "fixture", Messages: modelHistory(f.messages(t))})
	if client.generateCalls != 1 || client.streamCalls != 2 {
		t.Fatalf("recovery loop: %d summaries, %d execution requests", client.generateCalls, client.streamCalls)
	}
	var status string
	if err := f.service.db.QueryRow(context.Background(), `SELECT status FROM runs WHERE id=$1`, runID).Scan(&status); err != nil || status != "failed" {
		t.Fatal("repeated rejection did not stop")
	}
	var count int
	if err := f.service.db.QueryRow(context.Background(), `SELECT count(*) FROM messages WHERE run_id=$1 AND role='assistant'`, runID).Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected request became a runnable assistant record")
	}
}

func TestCancelledCompactionDoesNotCommitCheckpoint(t *testing.T) {
	f := newTranscriptFixture(t, false)
	checkpointHistory(t, f, 3, 160)
	runID := f.startRun(t, "finish")
	ctx, cancel := context.WithCancel(context.Background())
	client := &checkpointModel{generate: func(context.Context, model.ModelRequest) (*model.ModelResponse, error) {
		cancel()
		return &model.ModelResponse{Content: validCheckpointSummary}, nil
	}}
	request := model.ModelRequest{Model: "fixture", Messages: modelHistory(f.messages(t))}
	var estimator toolcontext.Estimator
	budget, err := estimator.Evaluate(request, toolcontext.BudgetConfig{})
	if err != nil {
		t.Fatal(err)
	}
	index := 0
	if _, err = f.service.executor.compactContext(ctx, f.conversationID, runID, client, request, nil, &estimator, toolcontext.BudgetConfig{}, budget, &index); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled compaction: %v", err)
	}
	var count int
	if err = f.service.db.QueryRow(context.Background(), `SELECT count(*) FROM conversation_context_checkpoints WHERE conversation_id=$1`, f.conversationID).Scan(&count); err != nil || count != 0 {
		t.Fatal("cancelled checkpoint committed")
	}
}

func TestFailedReplacementKeepsCommittedCheckpointAndHistoricalFilesDoNotGrantWrites(t *testing.T) {
	f := newTranscriptFixture(t, false)
	checkpointHistory(t, f, 3, 160)
	runID := f.startRun(t, "continue")
	request := model.ModelRequest{Model: "fixture", Messages: modelHistory(f.messages(t))}
	var estimator toolcontext.Estimator
	budget, err := estimator.Evaluate(request, toolcontext.BudgetConfig{})
	if err != nil {
		t.Fatal(err)
	}
	index := 0
	client := &checkpointModel{generate: func(context.Context, model.ModelRequest) (*model.ModelResponse, error) {
		return &model.ModelResponse{Content: validCheckpointSummary}, nil
	}}
	first, err := f.service.executor.compactContext(context.Background(), f.conversationID, runID, client, request, nil, &estimator, toolcontext.BudgetConfig{}, budget, &index)
	if err != nil {
		t.Fatal(err)
	}
	// Add a consumed group so a replacement has genuine new sources.
	call := model.ToolCall{ID: "read-before", Name: "read", Arguments: json.RawMessage(`{"file_path":"old.txt"}`)}
	assistant, err := f.service.appendMessageReceipt(context.Background(), f.conversationID, runID, model.Message{Role: "assistant", ToolCalls: []model.ToolCall{call}}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 64)
	result := `{"content":"     1\told bytes","truncated":false,"read_receipt":{"path":"old.txt","sha256":"` + sha + `","start_line":1,"line_count":1,"total_lines":1,"complete":true}}`
	tool, err := f.service.appendMessageReceipt(context.Background(), f.conversationID, runID, model.Message{Role: "tool", ToolCallID: call.ID, Content: result}, "read", nil)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := f.service.appendMessageReceipt(context.Background(), f.conversationID, runID, model.Message{Role: "assistant", Content: "read observed"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Messages = append(request.Messages, assistant, tool, observed)
	visible := toolcontext.ApplyCheckpoint(request.Messages, first)
	before := agenttool.NewFileState(visible)
	if got, create, err := before.ExpectedWrite("old.txt"); err != nil || create || got != sha {
		t.Fatal("visible actual read was not a receipt")
	}
	client.generate = func(context.Context, model.ModelRequest) (*model.ModelResponse, error) {
		return &model.ModelResponse{Content: "invalid summary"}, nil
	}
	budget, err = estimator.Evaluate(model.ModelRequest{Messages: visible}, toolcontext.BudgetConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.executor.compactContext(context.Background(), f.conversationID, runID, client, request, first, &estimator, toolcontext.BudgetConfig{}, budget, &index); err == nil {
		t.Fatal("bad replacement succeeded")
	}
	loaded, err := f.service.loadContextCheckpoint(context.Background(), f.conversationID, runID)
	if err != nil || loaded == nil || loaded.ID != first.ID {
		t.Fatal("failed replacement changed committed checkpoint")
	}
	plan, err := toolcontext.PlanCompaction(request.Messages, first, request.Model, &estimator, toolcontext.BudgetConfig{}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	after := agenttool.NewFileState(toolcontext.ApplyCheckpoint(request.Messages, &toolcontext.Checkpoint{ThroughSeq: plan.ThroughSeq, RetainedIDs: plan.RetainedIDs, Summary: validCheckpointSummary}))
	if _, err = after.ExpectedEdit("old.txt", "old bytes", false); err == nil {
		t.Fatal("historical summary granted edit without visible file bytes")
	}
}
