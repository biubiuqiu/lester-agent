package model

import "testing"

func TestEnsureEndpointPath(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		suffix   string
		want     string
	}{
		{name: "base URL", endpoint: "https://api.deepseek.com", suffix: "/chat/completions", want: "https://api.deepseek.com/chat/completions"},
		{name: "versioned base URL", endpoint: "https://example.com/v1/", suffix: "/chat/completions", want: "https://example.com/v1/chat/completions"},
		{name: "complete URL", endpoint: "https://example.com/v1/chat/completions", suffix: "/chat/completions", want: "https://example.com/v1/chat/completions"},
		{name: "preserves query", endpoint: "https://example.com/gateway?api-version=1", suffix: "/chat/completions", want: "https://example.com/gateway/chat/completions?api-version=1"},
		{name: "anthropic base URL", endpoint: "https://example.com/anthropic", suffix: "/v1/messages", want: "https://example.com/anthropic/v1/messages"},
		{name: "invalid URL", endpoint: "not a URL", suffix: "/chat/completions", want: "not a URL"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ensureEndpointPath(test.endpoint, test.suffix); got != test.want {
				t.Fatalf("ensureEndpointPath(%q, %q) = %q, want %q", test.endpoint, test.suffix, got, test.want)
			}
		})
	}
}
