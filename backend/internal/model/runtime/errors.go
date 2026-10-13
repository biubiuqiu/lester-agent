package runtime

import (
	"context"
	"errors"
	"fmt"
)

type ErrorKind string

const (
	ErrorContextExceeded ErrorKind = "context_length_exceeded"
	ErrorAuthentication  ErrorKind = "authentication"
	ErrorRateLimit       ErrorKind = "rate_limit"
	ErrorCancelled       ErrorKind = "cancelled"
	ErrorTransport       ErrorKind = "transport"
	ErrorProtocol        ErrorKind = "protocol"
	ErrorProvider        ErrorKind = "provider"
)

// ProviderError exposes stable recovery information without serializing a
// provider response, URL, credential or echoed conversation. Cause is available
// for errors.Is (not for UI/log output); Error deliberately never formats it.
type ProviderError struct {
	Kind       ErrorKind `json:"kind"`
	StatusCode int       `json:"status_code,omitempty"`
	Retryable  bool      `json:"retryable"`
	Cause      error     `json:"-"`
}

func (e *ProviderError) Error() string {
	message := "model provider request failed"
	switch e.Kind {
	case ErrorContextExceeded:
		message = "model request exceeds the provider context window"
	case ErrorAuthentication:
		message = "model provider authentication or access was denied"
	case ErrorRateLimit:
		message = "model provider rate limit reached"
	case ErrorCancelled:
		message = "model request cancelled"
	case ErrorTransport:
		message = "model provider connection failed or ended before completion"
	case ErrorProtocol:
		message = "model provider returned an invalid response"
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("%s (HTTP %d)", message, e.StatusCode)
	}
	return message
}

func (e *ProviderError) Unwrap() error { return e.Cause }

func ErrorKindOf(err error) ErrorKind {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return ErrorCancelled
	}
	var provider *ProviderError
	if errors.As(err, &provider) {
		return provider.Kind
	}
	return ErrorProvider
}
