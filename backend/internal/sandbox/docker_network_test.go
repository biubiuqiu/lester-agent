package sandbox

import "testing"

func TestDockerNetworkIsExplicitDeploymentConfiguration(t *testing.T) {
	for _, input := range []string{"", "none", "bridge", "lester-dependencies", "tenant_network.1"} {
		t.Run(input, func(t *testing.T) {
			t.Setenv("SANDBOX_PROVIDER", "docker")
			t.Setenv("SANDBOX_DOCKER_NETWORK", input)
			p, err := NewProviderFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			value := input
			if value == "" {
				value = "none"
			}
			if p.(*DockerProvider).Network != value {
				t.Fatal("incorrect configured network")
			}
		})
	}
	for _, input := range []string{"host", "HOST", "container:other", "--network=host", "name with space", "name/other"} {
		t.Run(input, func(t *testing.T) {
			t.Setenv("SANDBOX_PROVIDER", "docker")
			t.Setenv("SANDBOX_DOCKER_NETWORK", input)
			if _, err := NewProviderFromEnv(); err == nil {
				t.Fatal("accepted unsafe network selector")
			}
		})
	}
	// Constructing a provider directly must keep the old default, independently
	// of deployment environment. Per-task CreateOptions never exposes a network.
	t.Setenv("SANDBOX_DOCKER_NETWORK", "bridge")
	if NewDockerProvider("").Network != "none" {
		t.Fatal("task provider unexpectedly inherited egress")
	}
}
