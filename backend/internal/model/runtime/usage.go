package runtime

// UsageSource distinguishes provider measurements from estimates. An absent or
// zero measurement must never make a full request appear to cost zero tokens.
type UsageSource string

const (
	UsageActual    UsageSource = "actual"
	UsageEstimated UsageSource = "estimated"
	UsageMissing   UsageSource = "missing"
)

// NormalizedUsage describes one model request, not lifetime conversation usage.
// InputTokens retains the provider's input counter: OpenAI includes cache hits
// in it, whereas Anthropic reports uncached input separately. Raw contains only
// numeric provider counters, never provider messages or private request text.
// Stream snapshots are cumulative and replace earlier values; do not sum them.
type NormalizedUsage struct {
	Provider                 string         `json:"provider"`
	Source                   UsageSource    `json:"source"`
	InputTokens              int            `json:"input_tokens"`
	OutputTokens             int            `json:"output_tokens"`
	CacheReadInputTokens     int            `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int            `json:"cache_creation_input_tokens"`
	CachedIncluded           bool           `json:"cached_included"`
	InputReported            bool           `json:"input_reported"`
	OutputReported           bool           `json:"output_reported"`
	Raw                      map[string]int `json:"raw,omitempty"`
}

// RequestInputTokens includes cached tokens exactly once. False means that a
// caller needs a separately labeled estimate; a reported zero is not evidence
// that the system prompt, tool schemas, images and messages fit the window.
func (u NormalizedUsage) RequestInputTokens() (int, bool) {
	if u.Source == UsageMissing || u.Source == "" || (!u.InputReported && u.Source != UsageEstimated) {
		return 0, false
	}
	tokens := max(0, u.InputTokens)
	if !u.CachedIncluded {
		tokens += max(0, u.CacheReadInputTokens) + max(0, u.CacheCreationInputTokens)
	}
	return tokens, tokens > 0
}

func (u NormalizedUsage) Clone() *NormalizedUsage {
	copy := u
	if u.Raw != nil {
		copy.Raw = make(map[string]int, len(u.Raw))
		for key, value := range u.Raw {
			copy.Raw[key] = value
		}
	}
	return &copy
}
