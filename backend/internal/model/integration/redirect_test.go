package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
)

func TestProviderRedirectDoesNotForwardCredentialsOrConversation(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		for _, protocol := range []string{"openai", "anthropic", "azure"} {
			t.Run(fmt.Sprintf("%s/%d", protocol, status), func(t *testing.T) {
				var requests atomic.Int32
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					_, _ = io.Copy(io.Discard, r.Body)
					_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
				}))
				defer target.Close()
				provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodPost {
						t.Error("expected provider POST")
					}
					// A different hostname exercises Go's cross-host header handling.
					w.Header().Set("Location", strings.Replace(target.URL, "127.0.0.1", "localhost", 1))
					w.WriteHeader(status)
				}))
				defer provider.Close()
				client := &httpClient{protocol: protocol, endpoint: provider.URL, apiKey: "fixture-secret"}
				if protocol == "azure" {
					client.protocol = "openai"
					client.headers = map[string]string{"api-key": "fixture-azure-secret"}
					client.client = provider.Client()
				}
				events, err := client.Stream(context.Background(), modelruntime.Request{Model: "fixture", Messages: []modelruntime.Message{{Role: "user", Content: "private conversation"}}})
				if events != nil {
					for range events {
					}
				}
				if err == nil || !strings.Contains(err.Error(), fmt.Sprint(status)) {
					t.Errorf("expected a provider redirect error, got %v", err)
				}
				if n := requests.Load(); n != 0 {
					t.Errorf("redirect target received %d requests; credentials/conversation must stay at the configured endpoint", n)
				}
			})
		}
	}
}

func TestBedrockRedirectDoesNotForwardSignedRequest(t *testing.T) {
	var requests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = fmt.Fprint(w, `{ "content": [] }`)
	}))
	defer target.Close()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" || r.Header.Get("X-Amz-Security-Token") != "fixture-session-token" {
			t.Error("expected a signed provider request")
		}
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer provider.Close()
	endpoint, _ := url.Parse(provider.URL)
	client := &bedrockClient{region: "fixture", credentials: bedrockCredentials{AccessKeyID: "fixture-id", SecretAccessKey: "fixture-secret", SessionToken: "fixture-session-token"}, http: &http.Client{Transport: redirectTestTransport(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		if strings.HasPrefix(clone.URL.Host, "bedrock-runtime.") {
			clone.URL.Scheme, clone.URL.Host = endpoint.Scheme, endpoint.Host
		}
		return http.DefaultTransport.RoundTrip(clone)
	})}}
	_, err := client.Generate(context.Background(), modelruntime.Request{Model: "fixture", Messages: []modelruntime.Message{{Role: "user", Content: "private conversation"}}})
	if err == nil || !strings.Contains(err.Error(), "307") {
		t.Fatalf("expected a redirect error, got %v", err)
	}
	if requests.Load() != 0 {
		t.Fatal("signed request reached the redirect target")
	}
}

type redirectTestTransport func(*http.Request) (*http.Response, error)

func (f redirectTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
