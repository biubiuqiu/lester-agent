package speech

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDeepgramUsesEncodedAudioAndExplicitLanguagePolicy(t *testing.T) {
	for _, language := range []string{"", "zh"} {
		t.Run(language, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "POST" || r.URL.Path != "/v1/listen" || r.Header.Get("Authorization") != "Token fixture-secret" || r.Header.Get("xi-api-key") != "" || r.Header.Get("Content-Type") != "audio/wav" {
					t.Error("Deepgram method, auth or raw content type changed")
				}
				data, err := io.ReadAll(r.Body)
				if err != nil || !bytes.Equal(data, testWAV()) {
					t.Error("Deepgram encoded audio was rewritten or wrapped in multipart")
				}
				query := r.URL.Query()
				if query.Get("model") != "nova-3" || query.Get("encoding") != "" || query.Get("sample_rate") != "" {
					t.Error("encoded audio must not be relabeled as PCM")
				}
				if language == "" && (query.Get("detect_language") != "true" || query.Get("language") != "") || language != "" && (query.Get("language") != language || query.Get("detect_language") != "") {
					t.Error("language detection did not follow requested mode")
				}
				_, _ = io.WriteString(w, `{"results":{"channels":[{"alternatives":[{"transcript":"修复 GitHub API，不要改写用户口述。"}]}]}}`)
			}))
			defer server.Close()
			text, err := providerCall(context.Background(), server.Client(), Config{Provider: "deepgram", Endpoint: server.URL + "/v1"}, "fixture-secret", Upload{Data: testWAV(), Language: language})
			if err != nil || text != "修复 GitHub API，不要改写用户口述。" || calls.Load() != 1 {
				t.Fatalf("invalid Deepgram transcript %q %v calls %d", text, err, calls.Load())
			}
		})
	}
}

func TestNativeProviderRejectsWrongOrUnboundedResponsesWithoutRetry(t *testing.T) {
	for _, provider := range []string{"deepgram", "mistral"} {
		bodies := []struct {
			name, body string
			want       error
		}{
			{"openai-shape-is-not-deepgram", `{"text":"must not be used as a fallback"}`, ErrProvider},
			{"missing", `{}`, ErrProvider},
			{"malformed", `{`, ErrProvider},
			{"trailing", `{} {}`, ErrProvider},
		}
		if provider == "mistral" {
			bodies = bodies[1:]
			bodies = append(bodies, struct {
				name, body string
				want       error
			}{"wrong-shape", `{"results":{"channels":[{"alternatives":[{"transcript":"wrong"}]}]}}`, ErrProvider})
		}
		encode := func(text any) string {
			var value any = map[string]any{"text": text}
			if provider == "deepgram" {
				value = map[string]any{"results": map[string]any{"channels": []any{map[string]any{"alternatives": []any{map[string]any{"transcript": text}}}}}}
			}
			raw, _ := json.Marshal(value)
			return string(raw)
		}
		for _, input := range []struct {
			name, body string
			want       error
		}{
			{"empty", encode(" \n\t "), ErrNoSpeech},
			{"null", encode(nil), ErrProvider},
			{"wrong-type", encode(12), ErrProvider},
			{"nul", encode("hello\x00world"), ErrProvider},
			{"too-many-runes", encode(strings.Repeat("文", MaxTextRunes+1)), ErrLimit},
			{"too-much-body", encode(strings.Repeat("x", maxProviderResponseBytes)), ErrProvider},
		} {
			bodies = append(bodies, input)
		}
		for _, input := range bodies {
			t.Run(provider+"/"+input.name, func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					calls.Add(1)
					_, _ = io.WriteString(w, input.body)
				}))
				defer server.Close()
				text, err := providerCall(context.Background(), server.Client(), Config{Provider: provider, Endpoint: server.URL}, "fixture-secret", Upload{Data: testWAV()})
				if text != "" || !errors.Is(err, input.want) || calls.Load() != 1 {
					t.Fatalf("invalid response handling %q %v want %v; calls %d", text, err, input.want, calls.Load())
				}
			})
		}
	}
}

func TestEveryNativeProviderRejectsRedirectsAndSanitizesFailures(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		destinationCalls.Add(1)
		_, _ = io.WriteString(w, `{"text":"unexpected"}`)
	}))
	defer destination.Close()
	for _, provider := range []string{"deepgram", "mistral"} {
		for _, status := range []int{301, 302, 307, 308, 400, 401, 403, 429, 500} {
			t.Run(provider+"/"+http.StatusText(status), func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					w.Header().Set("Location", destination.URL)
					w.WriteHeader(status)
					_, _ = io.WriteString(w, `{"error":"fixture-secret and private recording details"}`)
				}))
				defer server.Close()
				text, err := providerCall(context.Background(), server.Client(), Config{Provider: provider, Endpoint: server.URL}, "fixture-secret", Upload{Data: testWAV()})
				if err == nil || text != "" || strings.Contains(err.Error(), "fixture-secret") || strings.Contains(err.Error(), "recording details") || calls.Load() != 1 {
					t.Fatal("provider failure leaked or retried", text, err, calls.Load())
				}
				if (status == 401 || status == 403) && !errors.Is(err, ErrProviderAuth) {
					t.Fatal("upstream auth is not a Lester login failure", err)
				}
			})
		}
	}
	if destinationCalls.Load() != 0 {
		t.Fatal("native provider forwarded credentials to a redirect")
	}
}
