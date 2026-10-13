package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
)

func providerResponseError(status int, data []byte) error {
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	return providerError(status, raw)
}

func providerError(status int, raw map[string]any) error {
	value := raw
	if nested, ok := raw["error"].(map[string]any); ok {
		value = nested
	}
	code := strings.ToLower(valueString(value["code"]))
	kind := strings.ToLower(valueString(value["type"]))
	if kind == "" {
		kind = strings.ToLower(valueString(value["__type"]))
		if separator := strings.LastIndexByte(kind, '#'); separator >= 0 {
			kind = kind[separator+1:]
		}
	}
	message := strings.ToLower(strings.TrimSpace(valueString(value["message"])))
	classification := modelruntime.ErrorProvider
	retryable := status == 408 || status == 429 || status >= 500
	switch {
	case status == 401 || status == 403 || oneOf(code, "invalid_api_key", "authentication_error", "accessdeniedexception", "unrecognizedclientexception") || oneOf(kind, "authentication_error", "permission_error", "accessdeniedexception", "unrecognizedclientexception"):
		classification = modelruntime.ErrorAuthentication
		retryable = false
	case status == 429 || oneOf(code, "rate_limit_exceeded", "rate_limit_error", "throttlingexception", "toomanyrequestsexception") || oneOf(kind, "rate_limit_error", "throttlingexception", "toomanyrequestsexception"):
		classification = modelruntime.ErrorRateLimit
		retryable = true
	case oneOf(code, "context_length_exceeded", "context_window_exceeded", "max_context_length", "prompt_too_long", "input_too_long", "too_many_tokens") || oneOf(kind, "context_length_exceeded", "context_window_exceeded", "prompt_too_long"):
		classification = modelruntime.ErrorContextExceeded
		retryable = false
	case (status == 400 || kind == "invalid_request_error" || kind == "validationexception") && contextOverflowMessage(message):
		classification = modelruntime.ErrorContextExceeded
		retryable = false
	case kind == "overloaded_error" || kind == "api_error":
		retryable = true
	}
	return &modelruntime.ProviderError{Kind: classification, StatusCode: status, Retryable: retryable}
}

// Anthropic and Bedrock sometimes use a generic invalid-request type with a
// specific leading message. Match the provider's leading diagnosis, not an
// arbitrary echoed prompt that happens to contain "context length".
func contextOverflowMessage(message string) bool {
	for _, prefix := range []string{
		"prompt is too long", "input is too long", "input length exceeds", "input tokens exceed", "prompt tokens exceed",
		"maximum context length", "context length exceeded", "context window exceeded", "this model's maximum context length",
	} {
		if strings.HasPrefix(message, prefix) {
			return true
		}
	}
	return false
}

func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

func providerTransportError(ctx context.Context, cause error) error {
	if errors.Is(cause, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return &modelruntime.ProviderError{Kind: modelruntime.ErrorCancelled, Cause: context.Canceled}
	}
	return &modelruntime.ProviderError{Kind: modelruntime.ErrorTransport, Retryable: ctx.Err() == nil, Cause: cause}
}

func providerProtocolError(cause error) error {
	return &modelruntime.ProviderError{Kind: modelruntime.ErrorProtocol, Cause: cause}
}
