package integration

import (
	"fmt"
	"net/url"
	"strings"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
)

type OpenAI struct{}

func (OpenAI) Name() string     { return "openai" }
func (OpenAI) Protocol() string { return "openai" }
func (OpenAI) DefaultEndpoint(map[string]any) string {
	return "https://api.openai.com/v1/chat/completions"
}
func (OpenAI) NewClient(spec ClientSpec) (modelruntime.Client, error) {
	return newOpenAIHTTPClient(spec, spec.Endpoint, nil), nil
}

type AzureOpenAI struct{}

func (AzureOpenAI) Name() string     { return "azure_openai" }
func (AzureOpenAI) Protocol() string { return "openai" }
func (AzureOpenAI) DefaultEndpoint(config map[string]any) string {
	return fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s", strings.TrimRight(configString(config, "resource_endpoint"), "/"), configString(config, "deployment"), configString(config, "api_version"))
}
func (AzureOpenAI) NewClient(spec ClientSpec) (modelruntime.Client, error) {
	return newOpenAIHTTPClient(spec, spec.Endpoint, map[string]string{"api-key": string(spec.Credential)}), nil
}

type OpenAICompatible struct{}

func (OpenAICompatible) Name() string     { return "openai_compatible" }
func (OpenAICompatible) Protocol() string { return "openai" }
func (OpenAICompatible) DefaultEndpoint(config map[string]any) string {
	return configString(config, "endpoint")
}
func (OpenAICompatible) NewClient(spec ClientSpec) (modelruntime.Client, error) {
	return newOpenAIHTTPClient(spec, EnsureEndpointPath(spec.Endpoint, "/chat/completions"), nil), nil
}

func newOpenAIHTTPClient(spec ClientSpec, endpoint string, headers map[string]string) modelruntime.Client {
	apiKey := string(spec.Credential)
	if headers != nil {
		apiKey = ""
	}
	return &httpClient{protocol: "openai", endpoint: endpoint, apiKey: apiKey, headers: headers}
}

func EnsureEndpointPath(endpoint, suffix string) string {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return endpoint
	}
	cleanPath := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(cleanPath, suffix) {
		parsed.Path = cleanPath + suffix
	}
	return parsed.String()
}
