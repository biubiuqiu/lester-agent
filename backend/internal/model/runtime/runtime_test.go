package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestRequestInputTokensCountsCacheExactlyOnceAndRejectsMissingZero(t *testing.T) {
	for _, test := range []struct {
		name  string
		usage NormalizedUsage
		want  int
		known bool
	}{
		{"OpenAI cached subset", NormalizedUsage{Source: UsageActual, InputTokens: 100, CacheReadInputTokens: 60, CachedIncluded: true, InputReported: true}, 100, true},
		{"Anthropic separate cache", NormalizedUsage{Source: UsageActual, InputTokens: 10, CacheReadInputTokens: 60, CacheCreationInputTokens: 30, InputReported: true}, 100, true},
		{"Entire Anthropic input cached", NormalizedUsage{Source: UsageActual, InputTokens: 0, CacheReadInputTokens: 100, InputReported: true}, 100, true},
		{"Missing usage", NormalizedUsage{Source: UsageMissing}, 0, false},
		{"Zero actual", NormalizedUsage{Source: UsageActual, InputReported: true}, 0, false},
		{"Only output measured", NormalizedUsage{Source: UsageActual, OutputTokens: 20, OutputReported: true}, 0, false},
		{"Estimate remains labeled", NormalizedUsage{Source: UsageEstimated, InputTokens: 100}, 100, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, known := test.usage.RequestInputTokens()
			if got != test.want || known != test.known {
				t.Fatalf("input=%d known=%v", got, known)
			}
		})
	}
}

func TestToolCallAssemblerKeepsAllCallsWithoutCompatibilityDuplicates(t *testing.T) {
	first := &ToolCall{Index: 1, ID: "second", Name: "read", Arguments: []byte(`{"path":`)}
	var assembler ToolCallAssembler
	assembler.AddEvent(Event{ToolCall: first, ToolCalls: []*ToolCall{first, {Index: 0, ID: "first", Name: "search", Arguments: []byte("{}")}}})
	assembler.AddEvent(Event{ToolCalls: []*ToolCall{{Index: 1, Arguments: []byte(`"x"}`)}, {Index: 0, Arguments: []byte(`{"query":"hi"}`)}}})
	calls := assembler.Calls()
	if len(calls) != 2 || calls[0].ID != "first" || calls[1].ID != "second" || string(calls[0].Arguments) != `{"query":"hi"}` || string(calls[1].Arguments) != `{"path":"x"}` {
		t.Fatalf("calls=%+v", calls)
	}
	calls[1].Arguments[0] = '!'
	if string(assembler.Calls()[1].Arguments) != `{"path":"x"}` {
		t.Fatal("caller mutated the assembler's argument storage")
	}
}

func TestProviderErrorIsSafeAndPreservesCancellationIdentity(t *testing.T) {
	private := "credential-and-private-prompt"
	err := &ProviderError{Kind: ErrorTransport, Cause: &url.Error{Op: "Post", URL: "https://provider.invalid/?token=" + private, Err: errors.New(private)}}
	data, marshalErr := json.Marshal(err)
	if marshalErr != nil || strings.Contains(err.Error(), private) || strings.Contains(string(data), private) {
		t.Fatalf("error unexpectedly exposed transport contents: %s", data)
	}
	cancel := &ProviderError{Kind: ErrorCancelled, Cause: context.Canceled}
	if !errors.Is(cancel, context.Canceled) || ErrorKindOf(cancel) != ErrorCancelled {
		t.Fatal("cancellation identity lost")
	}
}
