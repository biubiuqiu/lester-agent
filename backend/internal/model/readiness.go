package model

import (
	"context"
	"errors"
	"strings"
	"time"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/google/uuid"
)

type DeploymentTestResult struct {
	OK              bool                          `json:"ok"`
	Verified        bool                          `json:"verified"`
	Status          string                        `json:"status"`
	CapabilityScope string                        `json:"capability_scope"`
	DeploymentID    uuid.UUID                     `json:"deployment_id"`
	LatencyMS       int64                         `json:"latency_ms"`
	Usage           *modelruntime.NormalizedUsage `json:"usage,omitempty"`
	ErrorKind       modelruntime.ErrorKind        `json:"error_kind,omitempty"`
	Error           string                        `json:"error,omitempty"`
	Retryable       bool                          `json:"retryable,omitempty"`
}

// TestDeployment is an explicit bounded text-only provider call. It does not
// verify tools, vision, the model's context limit, or a complete Agent task.
// Shared models can be called by members without returning their connection
// configuration, provider response text, or decrypted credentials.
func (s *Store) TestDeployment(ctx context.Context, workspace, id uuid.UUID) (DeploymentTestResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result := DeploymentTestResult{Status: "failed", CapabilityScope: "text_call_only", DeploymentID: id}
	started := time.Now()
	client, deployment, err := s.Client(ctx, workspace, id)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			result.ErrorKind = modelruntime.ErrorTransport
		} else {
			result.ErrorKind = modelruntime.ErrorProvider
		}
		result.Error = "could not initialize the configured model"
		result.LatencyMS = time.Since(started).Milliseconds()
		return result, err
	}
	response, err := client.Generate(ctx, ModelRequest{Model: deployment.ModelID, Messages: []Message{{Role: "user", Content: "Reply with OK."}}, MaxTokens: 32})
	result.LatencyMS = time.Since(started).Milliseconds()
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			result.ErrorKind = modelruntime.ErrorCancelled
		} else {
			result.ErrorKind = modelruntime.ErrorTransport
		}
		result.Error = "model provider text check was cancelled or timed out"
		return result, nil
	}
	if err != nil {
		result.ErrorKind = modelruntime.ErrorKindOf(err)
		var provider *modelruntime.ProviderError
		if errors.As(err, &provider) {
			result.Error = provider.Error()
			result.Retryable = provider.Retryable
		} else {
			result.Error = "model provider text check failed"
		}
		return result, nil
	}
	if response == nil || strings.TrimSpace(response.Content) == "" || len(response.ToolCalls) > 0 {
		result.ErrorKind = modelruntime.ErrorProtocol
		result.Error = "model provider did not return text for the text-only check"
		return result, nil
	}
	result.OK, result.Verified, result.Status = true, true, "verified_text_call"
	result.Usage = response.NormalizedUsage
	if result.Usage == nil {
		result.Usage = &modelruntime.NormalizedUsage{Source: modelruntime.UsageMissing}
	}
	return result, nil
}
