package integration

import (
	"errors"
	"fmt"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
)

type ClientSpec struct {
	Provider, Protocol, Endpoint, ModelID string
	Config                                map[string]any
	Credential                            []byte
}

// Provider is the extension point for a model integration. Adding a provider
// only requires metadata, a default endpoint, and a runtime client builder.
type Provider interface {
	Name() string
	Protocol() string
	DefaultEndpoint(map[string]any) string
	NewClient(ClientSpec) (modelruntime.Client, error)
}

type Registry struct {
	providers map[string]Provider
}

func NewRegistry(providers ...Provider) *Registry {
	registry := &Registry{providers: map[string]Provider{}}
	for _, provider := range providers {
		registry.Register(provider)
	}
	return registry
}

func NewDefaultRegistry() *Registry {
	return NewRegistry(
		OpenAI{}, AzureOpenAI{}, OpenAICompatible{},
		Anthropic{}, AnthropicCompatible{}, VertexAnthropic{}, FoundryAnthropic{},
		Bedrock{},
	)
}

func (r *Registry) Register(provider Provider) {
	if provider.Name() == "" {
		panic("model provider name cannot be empty")
	}
	if _, exists := r.providers[provider.Name()]; exists {
		panic("duplicate model provider: " + provider.Name())
	}
	r.providers[provider.Name()] = provider
}

func (r *Registry) Resolve(name string) (Provider, error) {
	provider, exists := r.providers[name]
	if !exists {
		return nil, errors.New("unsupported provider")
	}
	return provider, nil
}

func (r *Registry) NewClient(spec ClientSpec) (modelruntime.Client, error) {
	provider, err := r.Resolve(spec.Provider)
	if err != nil {
		return nil, err
	}
	client, err := provider.NewClient(spec)
	if err != nil {
		return nil, fmt.Errorf("initialize %s provider: %w", spec.Provider, err)
	}
	return client, nil
}

func configString(config map[string]any, key string) string {
	value, _ := config[key].(string)
	return value
}
