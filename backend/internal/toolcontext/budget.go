package toolcontext

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"unicode"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
)

const DefaultPolicyWindow = 64000

type BudgetConfig struct{ ContextWindow, OutputReserve, PolicyWindow int }
type Budget struct {
	Source           string `json:"source"`
	EstimatedInput   int    `json:"estimated_input_tokens"`
	ModelWindow      int    `json:"model_window"`
	ModelWindowKnown bool   `json:"model_window_known"`
	PolicyWindow     int    `json:"policy_window"`
	OutputReserve    int    `json:"output_reserve"`
	InputLimit       int    `json:"input_limit"`
	ShouldCompact    bool   `json:"should_compact"`
	Exceeded         bool   `json:"exceeded"`
}

// Estimator holds per-model calibration for the next request. Measurements of
// a previous request calibrate an estimate; they never become exact counts of
// a changed request or a lifetime conversation budget.
type Estimator struct{ ratio float64 }

func (e *Estimator) Observe(estimate int, usage *modelruntime.NormalizedUsage) {
	if usage == nil || estimate < 1 || usage.Source != modelruntime.UsageActual {
		return
	}
	actual, ok := usage.RequestInputTokens()
	if !ok {
		return
	}
	e.ratio = max(1, float64(actual)/float64(estimate))
}
func (e *Estimator) Estimate(request modelruntime.Request) int {
	raw := EstimateInput(request)
	return int(math.Ceil(float64(raw) * max(1, e.ratio)))
}
func (e *Estimator) Evaluate(request modelruntime.Request, config BudgetConfig) (Budget, error) {
	if config.ContextWindow < 0 || config.OutputReserve < 0 || config.PolicyWindow < 0 {
		return Budget{}, errors.New("invalid model context budget")
	}
	policy := config.PolicyWindow
	if policy == 0 {
		policy = DefaultPolicyWindow
	}
	window := policy
	if config.ContextWindow > 0 {
		window = min(window, config.ContextWindow)
	}
	reserve := config.OutputReserve
	if reserve == 0 {
		reserve = min(8192, max(1, window/8))
	}
	if reserve >= window {
		return Budget{}, errors.New("output reserve must leave room for model input")
	}
	input := e.Estimate(request)
	source := "estimated"
	if e.ratio > 1 {
		source = "estimated_calibrated"
	}
	limit := window - reserve
	return Budget{Source: source, EstimatedInput: input, ModelWindow: config.ContextWindow, ModelWindowKnown: config.ContextWindow > 0, PolicyWindow: policy, OutputReserve: reserve, InputLimit: limit, ShouldCompact: input > limit*4/5, Exceeded: input > limit}, nil
}

// EstimateInput includes the complete request: system instructions, tool
// schemas, all prose, call arguments and native vision parts. It is deliberately
// a conservative heuristic, not a tokenizer or a claimed provider limit.
func EstimateInput(request modelruntime.Request) int {
	tokens := 24 + textTokens(request.System) + textTokens(request.Model)
	if request.ResponseSchema != nil {
		schema, _ := json.Marshal(request.ResponseSchema.Schema)
		tokens += 12 + textTokens(request.ResponseSchema.Name) + textTokens(string(schema))
	}
	for _, tool := range request.Tools {
		schema, _ := json.Marshal(tool.InputSchema)
		tokens += 12 + textTokens(tool.Name) + textTokens(tool.Description) + textTokens(string(schema))
	}
	for _, message := range request.Messages {
		tokens += 12 + textTokens(message.Role) + textTokens(message.ToolCallID)
		content, images := visionContent(message)
		tokens += textTokens(content) + images*4096
		for _, call := range message.ToolCalls {
			tokens += 12 + textTokens(call.ID) + textTokens(call.Name) + textTokens(string(call.Arguments))
		}
	}
	return int(math.Ceil(float64(tokens) * 1.1))
}

func textTokens(value string) int {
	ascii, other := 0, 0
	for _, r := range value {
		if r < 128 {
			ascii++
		} else if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			other += 2
		} else {
			other += 3
		}
	}
	return (ascii+3)/4 + other
}

func visionContent(message modelruntime.Message) (string, int) {
	if message.Role != "tool" {
		return message.Content, 0
	}
	var envelope struct {
		Content string `json:"content"`
		Images  []struct {
			MediaType string `json:"media_type"`
			Encoding  string `json:"encoding"`
			Data      string `json:"data"`
		} `json:"images"`
	}
	if json.Unmarshal([]byte(message.Content), &envelope) != nil {
		return message.Content, 0
	}
	images := 0
	for _, item := range envelope.Images {
		if item.Encoding != "base64" || !strings.HasPrefix(item.MediaType, "image/") || item.Data == "" {
			continue
		}
		if _, err := base64.StdEncoding.DecodeString(item.Data); err == nil {
			images++
		}
	}
	if images == 0 {
		return message.Content, 0
	}
	return envelope.Content, images
}
