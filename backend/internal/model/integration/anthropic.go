package integration

import (
	"errors"
	"fmt"
	"strings"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
)

type Anthropic struct{}

func (Anthropic) Name() string     { return "anthropic" }
func (Anthropic) Protocol() string { return "anthropic" }
func (Anthropic) DefaultEndpoint(map[string]any) string {
	return "https://api.anthropic.com/v1/messages"
}
func (Anthropic) NewClient(spec ClientSpec) (modelruntime.Client, error) {
	return newAnthropicHTTPClient(spec, spec.Endpoint, "", nil), nil
}

type AnthropicCompatible struct{}

func (AnthropicCompatible) Name() string     { return "anthropic_compatible" }
func (AnthropicCompatible) Protocol() string { return "anthropic" }
func (AnthropicCompatible) DefaultEndpoint(config map[string]any) string {
	return configString(config, "endpoint")
}
func (AnthropicCompatible) NewClient(spec ClientSpec) (modelruntime.Client, error) {
	return newAnthropicHTTPClient(spec, EnsureEndpointPath(spec.Endpoint, "/v1/messages"), "", nil), nil
}

type VertexAnthropic struct{}

func (VertexAnthropic) Name() string     { return "vertex" }
func (VertexAnthropic) Protocol() string { return "anthropic" }
func (VertexAnthropic) DefaultEndpoint(config map[string]any) string {
	return configString(config, "endpoint")
}
func (VertexAnthropic) NewClient(spec ClientSpec) (modelruntime.Client, error) {
	region, project := configString(spec.Config, "region"), configString(spec.Config, "project")
	if region == "" || project == "" {
		return nil, errors.New("Vertex requires project and region")
	}
	endpoint := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/anthropic/models/%s:streamRawPredict", region, project, region, spec.ModelID)
	return newAnthropicHTTPClient(spec, endpoint, "vertex", map[string]string{"Authorization": "Bearer " + string(spec.Credential)}), nil
}

type FoundryAnthropic struct{}

func (FoundryAnthropic) Name() string     { return "foundry" }
func (FoundryAnthropic) Protocol() string { return "anthropic" }
func (FoundryAnthropic) DefaultEndpoint(config map[string]any) string {
	return strings.TrimRight(configString(config, "resource_endpoint"), "/") + "/anthropic/v1/messages"
}
func (FoundryAnthropic) NewClient(spec ClientSpec) (modelruntime.Client, error) {
	return newAnthropicHTTPClient(spec, spec.Endpoint, "", map[string]string{"api-key": string(spec.Credential)}), nil
}

func newAnthropicHTTPClient(spec ClientSpec, endpoint, mode string, headers map[string]string) modelruntime.Client {
	apiKey := string(spec.Credential)
	if headers != nil {
		apiKey = ""
	}
	return &httpClient{protocol: "anthropic", endpoint: endpoint, apiKey: apiKey, headers: headers, mode: mode}
}
