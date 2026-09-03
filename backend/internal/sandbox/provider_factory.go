package sandbox

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type providerBuilder func() (Provider, error)

var providerBuilders = map[string]providerBuilder{
	"docker": func() (Provider, error) { return NewDockerProvider(os.Getenv("SANDBOX_IMAGE")), nil },
	"acs":    newACSProviderFromEnv,
}

// NewProviderFromEnv is the deployment composition root. Adding a cloud
// provider requires an adapter and one registry entry, not changes in
// conversation, file, command, or terminal handlers.
func NewProviderFromEnv() (Provider, error) {
	name := strings.ToLower(strings.TrimSpace(os.Getenv("SANDBOX_PROVIDER")))
	if name == "" {
		name = "docker"
	}
	builder, ok := providerBuilders[name]
	if !ok {
		return nil, fmt.Errorf("unsupported SANDBOX_PROVIDER %q (supported: docker, acs)", name)
	}
	return builder()
}

func newACSProviderFromEnv() (Provider, error) {
	timeoutSeconds, err := parsePositiveInt(os.Getenv("ACS_SANDBOX_TIMEOUT_SECONDS"), acsDefaultTimeoutSeconds)
	if err != nil {
		return nil, fmt.Errorf("ACS_SANDBOX_TIMEOUT_SECONDS: %w", err)
	}
	requestTimeoutSeconds, err := parsePositiveInt(os.Getenv("ACS_SANDBOX_REQUEST_TIMEOUT_SECONDS"), 60)
	if err != nil {
		return nil, fmt.Errorf("ACS_SANDBOX_REQUEST_TIMEOUT_SECONDS: %w", err)
	}
	runtimePort, err := parsePositiveInt(os.Getenv("ACS_SANDBOX_RUNTIME_PORT"), 49983)
	if err != nil {
		return nil, fmt.Errorf("ACS_SANDBOX_RUNTIME_PORT: %w", err)
	}
	secure, err := envBool("ACS_SANDBOX_SECURE", true)
	if err != nil {
		return nil, err
	}
	autoPause, err := envBool("ACS_SANDBOX_AUTO_PAUSE", true)
	if err != nil {
		return nil, err
	}
	return NewACSProvider(ACSConfig{
		Domain: os.Getenv("ACS_SANDBOX_DOMAIN"), Scheme: os.Getenv("ACS_SANDBOX_SCHEME"), Protocol: os.Getenv("ACS_SANDBOX_PROTOCOL"),
		APIURL: os.Getenv("ACS_SANDBOX_API_URL"), SandboxBaseURL: os.Getenv("ACS_SANDBOX_BASE_URL"), APIKey: os.Getenv("ACS_SANDBOX_API_KEY"),
		Template: os.Getenv("ACS_SANDBOX_TEMPLATE"), TimeoutSeconds: timeoutSeconds, RequestTimeout: time.Duration(requestTimeoutSeconds) * time.Second,
		RuntimePort: runtimePort, Secure: secure, AutoPause: autoPause,
	})
}

func envBool(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return parsed, nil
}
