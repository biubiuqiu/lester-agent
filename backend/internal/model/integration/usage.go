package integration

import (
	"encoding/json"
	"math"
	"strconv"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
)

type usageAccumulator struct {
	provider string
	values   map[string]int
}

// Both OpenAI usage frames and Anthropic message_delta counters are cumulative
// snapshots. Merge only present numeric fields; never add repeated frames.
func (a *usageAccumulator) observe(value any) bool {
	raw, ok := value.(map[string]any)
	if !ok {
		return false
	}
	changed := false
	put := func(key string, value any) {
		if tokens, valid := usageInteger(value); valid {
			if a.values == nil {
				a.values = make(map[string]int)
			}
			a.values[key] = tokens
			changed = true
		}
	}
	for _, key := range []string{"input_tokens", "output_tokens", "prompt_tokens", "completion_tokens", "total_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"} {
		if value, present := raw[key]; present {
			put(key, value)
		}
	}
	for parent, keys := range map[string][]string{
		"prompt_tokens_details":     {"cached_tokens", "audio_tokens"},
		"completion_tokens_details": {"reasoning_tokens", "audio_tokens", "accepted_prediction_tokens", "rejected_prediction_tokens"},
		"cache_creation":            {"ephemeral_5m_input_tokens", "ephemeral_1h_input_tokens"},
	} {
		if details, ok := raw[parent].(map[string]any); ok {
			for _, key := range keys {
				if value, present := details[key]; present {
					put(parent+"."+key, value)
				}
			}
		}
	}
	return changed
}

func (a *usageAccumulator) snapshot() *modelruntime.NormalizedUsage {
	usage := &modelruntime.NormalizedUsage{Provider: a.provider, Source: modelruntime.UsageMissing, CachedIncluded: a.provider == "openai"}
	if len(a.values) == 0 {
		return usage
	}
	usage.Source = modelruntime.UsageActual
	usage.Raw = make(map[string]int, len(a.values))
	for key, value := range a.values {
		usage.Raw[key] = value
	}
	if a.provider == "openai" {
		usage.InputTokens, usage.InputReported = a.values["prompt_tokens"]
		if !usage.InputReported {
			usage.InputTokens, usage.InputReported = a.values["input_tokens"]
		}
		usage.OutputTokens, usage.OutputReported = a.values["completion_tokens"]
		if !usage.OutputReported {
			usage.OutputTokens, usage.OutputReported = a.values["output_tokens"]
		}
		usage.CacheReadInputTokens = a.values["prompt_tokens_details.cached_tokens"]
	} else {
		usage.InputTokens, usage.InputReported = a.values["input_tokens"]
		usage.OutputTokens, usage.OutputReported = a.values["output_tokens"]
		usage.CacheReadInputTokens = a.values["cache_read_input_tokens"]
		usage.CacheCreationInputTokens = a.values["cache_creation_input_tokens"]
		if _, reported := a.values["cache_creation_input_tokens"]; !reported {
			usage.CacheCreationInputTokens = a.values["cache_creation.ephemeral_5m_input_tokens"] + a.values["cache_creation.ephemeral_1h_input_tokens"]
		}
	}
	return usage
}

func usageInteger(value any) (int, bool) {
	var number float64
	switch value := value.(type) {
	case float64:
		number = value
	case int:
		if value >= 0 {
			return value, true
		}
		return 0, false
	case json.Number:
		parsed, err := strconv.ParseFloat(string(value), 64)
		if err != nil {
			return 0, false
		}
		number = parsed
	default:
		return 0, false
	}
	// Decode only exactly representable, nonnegative integer token counters.
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > 1<<53 || math.Trunc(number) != number || number >= float64(int(^uint(0)>>1)) {
		return 0, false
	}
	return int(number), true
}
