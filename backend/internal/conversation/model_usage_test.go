package conversation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/biubiuqiu/lester-agent/backend/internal/toolcontext"
	"github.com/google/uuid"
)

func TestModelUsageStoresLatestSnapshotRatherThanCumulativeSum(t *testing.T) {
	f := newTranscriptFixture(t, false)
	runID := f.startRun(t, "finish")
	client := &scriptedModel{respond: func(_ int, _ model.ModelRequest) []model.ModelEvent {
		return []model.ModelEvent{
			{NormalizedUsage: &modelruntime.NormalizedUsage{Source: modelruntime.UsageActual, InputReported: true, InputTokens: 10, OutputTokens: 1, CacheReadInputTokens: 3}},
			{Delta: "done", NormalizedUsage: &modelruntime.NormalizedUsage{Source: modelruntime.UsageActual, InputReported: true, InputTokens: 25, OutputTokens: 2, CacheReadInputTokens: 5}},
		}
	}}
	f.service.executor.executeTurns(context.Background(), f.conversationID, runID, nil, client, model.ModelRequest{Model: "fixture", System: "instructions", Messages: modelHistory(f.messages(t))})
	var raw []byte
	var status string
	var estimated int
	if err := f.service.db.QueryRow(context.Background(), `SELECT usage,status,estimated_input_tokens FROM model_request_usage WHERE run_id=$1 AND request_index=1`, runID).Scan(&raw, &status, &estimated); err != nil {
		t.Fatal(err)
	}
	var usage modelruntime.NormalizedUsage
	if err := json.Unmarshal(raw, &usage); err != nil {
		t.Fatal(err)
	}
	input, known := usage.RequestInputTokens()
	if status != "completed" || !known || input != 30 || usage.OutputTokens != 2 || estimated < 1 {
		t.Fatalf("usage was summed or lost: %+v %s", usage, status)
	}
}

func TestEmptyModelResponseCannotCompleteAUserTask(t *testing.T) {
	f := newTranscriptFixture(t, false)
	runID := f.startRun(t, "create a deliverable")
	client := &scriptedModel{respond: func(_ int, _ model.ModelRequest) []model.ModelEvent { return []model.ModelEvent{{Delta: "   \n"}} }}
	f.service.executor.executeTurns(context.Background(), f.conversationID, runID, nil, client, model.ModelRequest{Messages: modelHistory(f.messages(t))})
	var status, requestStatus, kind string
	if err := f.service.db.QueryRow(context.Background(), `SELECT r.status,u.status,u.error_kind FROM runs r JOIN model_request_usage u ON u.run_id=r.id WHERE r.id=$1`, runID).Scan(&status, &requestStatus, &kind); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || requestStatus != "failed" || kind != string(modelruntime.ErrorProtocol) {
		t.Fatal("empty response claimed task completion")
	}
}

func TestConfiguredBudgetRejectsOversizedRequestBeforeProviderCall(t *testing.T) {
	f := newTranscriptFixture(t, false)
	input := strings.Repeat("不能丢弃的用户输入", 1000)
	runID := f.startRun(t, input)
	called := false
	client := &scriptedModel{respond: func(_ int, _ model.ModelRequest) []model.ModelEvent { called = true; return nil }}
	f.service.executor.executeTurns(context.Background(), f.conversationID, runID, nil, client, model.ModelRequest{System: "instructions", Messages: modelHistory(f.messages(t))}, toolcontext.BudgetConfig{ContextWindow: 1000})
	if called {
		t.Fatal("over-budget request reached provider")
	}
	if f.messages(t)[0].Content != input {
		t.Fatal("protected input was truncated")
	}
	var status string
	if err := f.service.db.QueryRow(context.Background(), `SELECT status FROM runs WHERE id=$1`, runID).Scan(&status); err != nil || status != "failed" {
		t.Fatalf("budget rejection state: %s %v", status, err)
	}
}

func TestCrashRecoveryClosesUnfinishedUsageWithoutInventingCounts(t *testing.T) {
	f := newTranscriptFixture(t, false)
	runID := f.startRun(t, "interrupted")
	if _, err := f.service.startModelRequest(context.Background(), runID, "execution", toolcontext.Budget{Source: "estimated", EstimatedInput: 123, PolicyWindow: 64000}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.recoverInterruptedRuns(context.Background(), f.conversationID); err != nil {
		t.Fatal(err)
	}
	var status string
	var raw []byte
	var estimated int
	if err := f.service.db.QueryRow(context.Background(), `SELECT status,usage,estimated_input_tokens FROM model_request_usage WHERE run_id=$1`, runID).Scan(&status, &raw, &estimated); err != nil {
		t.Fatal(err)
	}
	var usage modelruntime.NormalizedUsage
	_ = json.Unmarshal(raw, &usage)
	if status != "failed" || usage.Source != modelruntime.UsageMissing || estimated != 123 {
		t.Fatal("recovery invented usage or left request active")
	}
}

func TestStoredToolResultPaginationNeverReadsLiveFilesOrForeignConversations(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	old := f.startRun(t, "old")
	call := model.ToolCall{ID: "call-original", Name: "read", Arguments: json.RawMessage(`{"file_path":"changed.txt"}`)}
	if err := f.service.appendMessage(ctx, f.conversationID, old, model.Message{Role: "assistant", ToolCalls: []model.ToolCall{call}}, "", nil); err != nil {
		t.Fatal(err)
	}
	content := "旧结果🙂：文件后来发生了变化"
	if err := f.service.appendMessage(ctx, f.conversationID, old, model.Message{Role: "tool", ToolCallID: call.ID, Content: content}, "read", nil); err != nil {
		t.Fatal(err)
	}
	if err := f.service.finishCompletedRun(ctx, old, f.conversationID); err != nil {
		t.Fatal(err)
	}
	current := f.startRun(t, "read original output")
	reader := agenttool.ToolResult{DB: f.service.db}
	env := agenttool.Environment{RunID: current, ConversationID: f.conversationID}
	raw, _ := json.Marshal(map[string]any{"tool_execution_id": old.String() + ":" + call.ID, "offset": 1, "limit": 4})
	result, err := reader.Execute(ctx, env, raw)
	if err != nil {
		t.Fatal(err)
	}
	view := result.(map[string]any)
	if view["content"] != string([]rune(content)[1:5]) || view["next_offset"] != 5 || view["current_file_observation"] != false || view["historical"] != true {
		t.Fatalf("wrong stored page: %#v", view)
	}
	env.ConversationID = uuid.New()
	if _, err = reader.Execute(ctx, env, raw); err == nil {
		t.Fatal("foreign conversation gained stored-result access")
	}
}
