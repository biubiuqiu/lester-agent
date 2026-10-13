package runtime

import "sort"

// ToolCallDeltas prefers the multi-call frame and never replays its compatibility
// ToolCall a second time. Older clients may still populate only ToolCall.
func (e Event) ToolCallDeltas() []*ToolCall {
	if len(e.ToolCalls) > 0 {
		return e.ToolCalls
	}
	if e.ToolCall != nil {
		return []*ToolCall{e.ToolCall}
	}
	return nil
}

// ToolCallAssembler assembles streamed argument fragments per provider index.
// Generate and the conversation executor use the same frame semantics.
type ToolCallAssembler struct {
	calls map[int]*ToolCall
}

func (a *ToolCallAssembler) AddEvent(event Event) {
	for _, delta := range event.ToolCallDeltas() {
		if delta == nil {
			continue
		}
		if a.calls == nil {
			a.calls = make(map[int]*ToolCall)
		}
		call := a.calls[delta.Index]
		if call == nil {
			call = &ToolCall{Index: delta.Index}
			a.calls[delta.Index] = call
		}
		if delta.ID != "" {
			call.ID = delta.ID
		}
		if delta.Name != "" {
			call.Name = delta.Name
		}
		// Anthropic starts a tool_use block with {} before input_json_delta.
		if len(delta.Arguments) > 0 && string(delta.Arguments) != "{}" {
			call.Arguments = append(call.Arguments, delta.Arguments...)
		}
	}
}

func (a *ToolCallAssembler) Calls() []ToolCall {
	indices := make([]int, 0, len(a.calls))
	for index := range a.calls {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	result := make([]ToolCall, 0, len(indices))
	for _, index := range indices {
		call := *a.calls[index]
		call.Arguments = append([]byte(nil), call.Arguments...)
		if len(call.Arguments) == 0 {
			call.Arguments = []byte("{}")
		}
		result = append(result, call)
	}
	return result
}
