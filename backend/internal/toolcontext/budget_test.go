package toolcontext

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
)

func TestBudgetCountsWholeRequestAndNeverInventsModelWindow(t *testing.T) {
	request := modelruntime.Request{Model: "test", Messages: []modelruntime.Message{{Role: "user", Content: "请完成任务"}}}
	var estimator Estimator
	base, err := estimator.Evaluate(request, BudgetConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if base.ModelWindowKnown || base.ModelWindow != 0 || base.PolicyWindow != 64000 || base.Source != "estimated" || base.EstimatedInput < 1 {
		t.Fatalf("unknown policy: %+v", base)
	}
	request.System = strings.Repeat("system", 10000)
	request.Tools = []modelruntime.Tool{{Name: "large_schema", InputSchema: map[string]any{"description": strings.Repeat("schema", 10000)}}}
	request.Messages = append(request.Messages, modelruntime.Message{Role: "assistant", ToolCalls: []modelruntime.ToolCall{{ID: "call", Name: "write", Arguments: json.RawMessage(`{"content":"` + strings.Repeat("argument", 10000) + `"}`)}}})
	full, err := estimator.Evaluate(request, BudgetConfig{ContextWindow: 32000})
	if err != nil {
		t.Fatal(err)
	}
	if !full.ModelWindowKnown || !full.Exceeded || full.EstimatedInput <= base.EstimatedInput+30000 {
		t.Fatalf("system/schema/calls were not counted: %+v", full)
	}
	if request.MaxTokens != 0 {
		t.Fatal("budget modified API output limit")
	}
}

func TestVisionCountsImagesWithoutTreatingBase64AsText(t *testing.T) {
	envelope := func(data string) string {
		raw, _ := json.Marshal(map[string]any{"content": "read image", "images": []map[string]string{{"media_type": "image/png", "encoding": "base64", "data": data}}})
		return string(raw)
	}
	a := modelruntime.Request{Messages: []modelruntime.Message{{Role: "tool", Content: envelope(base64.StdEncoding.EncodeToString([]byte("small")))}}}
	b := a
	b.Messages = append([]modelruntime.Message(nil), a.Messages...)
	b.Messages[0].Content = envelope(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("large", 50000))))
	if EstimateInput(a) != EstimateInput(b) {
		t.Fatal("base64 was charged as ordinary text")
	}
	b.Messages[0].Role = "user"
	if EstimateInput(b) <= EstimateInput(a) {
		t.Fatal("user-authored text was incorrectly treated as a native image")
	}
}

func TestUsageCalibrationIsPerRequestAndMissingUsageIsNotZero(t *testing.T) {
	var e Estimator
	request := modelruntime.Request{System: strings.Repeat("instructions", 100)}
	before := e.Estimate(request)
	e.Observe(before, &modelruntime.NormalizedUsage{Source: modelruntime.UsageActual, InputReported: true})
	if e.Estimate(request) != before {
		t.Fatal("zero usage removed request estimate")
	}
	e.Observe(before, &modelruntime.NormalizedUsage{Source: modelruntime.UsageActual, InputReported: true, InputTokens: before, CacheReadInputTokens: before, CachedIncluded: false})
	if e.Estimate(request) < before*2 {
		t.Fatal("Anthropic cached input was not counted exactly once")
	}
	if _, err := e.Evaluate(request, BudgetConfig{ContextWindow: 1000, OutputReserve: 1000}); err == nil {
		t.Fatal("invalid input budget accepted")
	}
}
