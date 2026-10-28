package speech

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestNativeSpeechConfigurationDefaultsAndFixedEndpoints(t *testing.T) {
	for _, input := range []struct{ provider, endpoint, model string }{
		{"elevenlabs", "https://api.elevenlabs.io/v1", "scribe_v2"},
		{"deepgram", "https://api.deepgram.com/v1", "nova-3"},
		{"mistral", "https://api.mistral.ai/v1", "voxtral-mini-latest"},
	} {
		t.Run(input.provider, func(t *testing.T) {
			cfg, err := normalizeConfig(ConfigChange{Provider: input.provider})
			if err != nil || cfg.Endpoint != input.endpoint || cfg.ModelID != input.model {
				t.Fatalf("incorrect native defaults: %+v %v", cfg, err)
			}
			for _, endpoint := range []string{
				"https://foreign.example/v1", input.endpoint + "/other", input.endpoint + "?token=private",
				strings.Replace(input.endpoint, "https:", "http:", 1), "http://localhost:8000/v1",
			} {
				if _, err := normalizeConfig(ConfigChange{Provider: input.provider, Endpoint: endpoint}); !errors.Is(err, ErrConfig) {
					t.Errorf("native endpoint could redirect credentials to %q: %v", endpoint, err)
				}
			}
			cfg, err = normalizeConfig(ConfigChange{Provider: input.provider, Endpoint: input.endpoint + "/", ModelID: "administrator-selected-model"})
			if err != nil || cfg.Endpoint != input.endpoint || cfg.ModelID != "administrator-selected-model" {
				t.Fatalf("native model configuration rejected: %+v %v", cfg, err)
			}
		})
	}
	if _, err := normalizeConfig(ConfigChange{Provider: "unimplemented-provider"}); !errors.Is(err, ErrConfig) {
		t.Fatal("unsupported provider accepted", err)
	}
	groq, err := normalizeConfig(ConfigChange{Provider: "openai", Endpoint: "https://api.groq.com/openai/v1", ModelID: "whisper-large-v3-turbo"})
	if err != nil || groq.Provider != "openai" || groq.ModelID != "whisper-large-v3-turbo" {
		t.Fatal("verified compatible service must reuse OpenAI protocol", groq, err)
	}
}

type speechFixtureTransport func(*http.Request) (*http.Response, error)

func (f speechFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// Keep the saved official endpoint and its credential scope intact. The test
// transport redirects network I/O into a local protocol fixture, not production.
func useNativeProtocolFixture(t *testing.T, s *Service, provider string, server *httptest.Server) {
	t.Helper()
	endpoint, _, _ := providerDefaults(provider)
	official, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	local, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := server.Client().Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	s.client = &http.Client{Transport: speechFixtureTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Scheme != official.Scheme || request.URL.Host != official.Host {
			t.Error("saved native endpoint changed before network I/O")
			return nil, errors.New("unexpected fixture destination")
		}
		clone := request.Clone(request.Context())
		target := *request.URL
		target.Scheme, target.Host = local.Scheme, local.Host
		clone.URL = &target
		return transport.RoundTrip(clone)
	})}
}

func TestNativeSpeechConfigurationReceiptsAndUnknownResponsesUseOneProviderCall(t *testing.T) {
	for _, provider := range []string{"deepgram", "mistral"} {
		t.Run(provider, func(t *testing.T) {
			f := newServiceFixture(t)
			ctx := context.Background()
			cfg, err := f.s.Save(ctx, f.admin, ConfigChange{Provider: provider, Enabled: true, Credential: "native-private-token"})
			if err != nil || !cfg.HasCredential {
				t.Fatal("native configuration failed", cfg, err)
			}
			capability, err := f.s.Capabilities(ctx, f.member)
			if err != nil || !capability.Enabled || capability.Provider != provider {
				t.Fatal("configured native service unavailable", capability, err)
			}
			var before, after []byte
			if err = f.s.DB.QueryRow(ctx, `SELECT credential_ciphertext FROM speech_settings`).Scan(&before); err != nil || bytes.Contains(before, []byte("native-private-token")) {
				t.Fatal("native credential not encrypted", err)
			}
			cfg, err = f.s.Save(ctx, f.admin, ConfigChange{Revision: cfg.Revision, Provider: provider, Enabled: true, ModelID: cfg.ModelID})
			if err != nil || !cfg.HasCredential {
				t.Fatal("blank native key did not preserve credential", cfg, err)
			}
			if err = f.s.DB.QueryRow(ctx, `SELECT credential_ciphertext FROM speech_settings`).Scan(&after); err != nil || !bytes.Equal(before, after) {
				t.Fatal("blank key replaced encrypted credential", err)
			}
			var calls atomic.Int32
			status := http.StatusOK
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				var processing int
				if err := f.s.DB.QueryRow(ctx, `SELECT count(*) FROM speech_transcriptions WHERE status='processing' AND settings_revision=$1`, cfg.Revision).Scan(&processing); err != nil || processing != 1 {
					t.Error("native provider cost preceded durable original receipt", processing, err)
				}
				if status != http.StatusOK {
					w.WriteHeader(status)
					_, _ = io.WriteString(w, `{"error":"native-private-token and private audio"}`)
					return
				}
				if provider == "deepgram" {
					_, _ = io.WriteString(w, `{"results":{"channels":[{"alternatives":[{"transcript":"Fixture transcription, not a live accuracy result."}]}]}}`)
				} else {
					_, _ = io.WriteString(w, `{"text":"Fixture transcription, not a live accuracy result."}`)
				}
			}))
			defer server.Close()
			useNativeProtocolFixture(t, f.s, provider, server)
			for _, want := range []struct {
				code        int
				state, text string
				failure     error
			}{
				{http.StatusOK, "completed", "Fixture transcription, not a live accuracy result.", nil},
				{http.StatusServiceUnavailable, "unknown", "", ErrProvider},
				{http.StatusUnauthorized, "failed", "", ErrProviderAuth},
			} {
				status = want.code
				id := uuid.New()
				upload := Upload{Data: tinyWAV(), ConfigurationRevision: cfg.Revision}
				beforeCalls := calls.Load()
				value, err := f.s.Transcribe(ctx, f.admin, id, upload)
				if err != nil || value.ID != id || value.Status != want.state || value.Text != want.text {
					t.Fatal("unexpected native receipt", value, err)
				}
				if want.failure != nil && value.Error != want.failure.Error() {
					t.Fatal("unsafe provider error persisted", value)
				}
				recovered, err := f.s.Receipt(ctx, f.admin, id)
				if err != nil || recovered != value {
					t.Fatal("original native receipt not recoverable", recovered, err)
				}
				repeated, err := f.s.Transcribe(ctx, f.admin, id, upload)
				if err != nil || repeated != value || calls.Load() != beforeCalls+1 {
					t.Fatal("native receipt caused a second provider call", repeated, err, calls.Load())
				}
			}
			if _, err = f.s.Save(ctx, f.admin, ConfigChange{Revision: cfg.Revision, Provider: "openai", Enabled: true}); !errors.Is(err, ErrConfig) {
				t.Fatal("native key transferred to another provider", err)
			}
		})
	}
}

func TestSpeechProviderMigrationRollbackRequiresExplicitCompatibleSelection(t *testing.T) {
	for _, provider := range []string{"deepgram", "mistral"} {
		t.Run(provider, func(t *testing.T) {
			f := newServiceFixture(t)
			ctx := context.Background()
			cfg, err := f.s.Save(ctx, f.admin, ConfigChange{Provider: provider, Enabled: true, Credential: "native-private-token"})
			if err != nil {
				t.Fatal(err)
			}
			read := func(name string) string {
				raw, err := os.ReadFile("../../migrations/" + name)
				if err != nil {
					t.Fatal(err)
				}
				return string(raw)
			}
			down := read("000035_speech_providers.down.sql")
			var before, after []byte
			if err = f.s.DB.QueryRow(ctx, `SELECT credential_ciphertext FROM speech_settings`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			tx, err := f.s.DB.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(ctx, down)
			_ = tx.Rollback(ctx)
			if err == nil || !strings.Contains(err.Error(), "Select OpenAI-compatible or ElevenLabs") {
				t.Fatal("rollback discarded a native configuration", err)
			}
			preserved, err := f.s.Config(ctx, f.admin)
			if err != nil || preserved.Provider != provider || preserved.Revision != cfg.Revision || !preserved.HasCredential {
				t.Fatal("refused rollback changed native configuration", preserved, err)
			}
			if err = f.s.DB.QueryRow(ctx, `SELECT credential_ciphertext FROM speech_settings`).Scan(&after); err != nil || !bytes.Equal(before, after) {
				t.Fatal("refused rollback altered native credential", err)
			}
			compatible, err := f.s.Save(ctx, f.admin, ConfigChange{Revision: cfg.Revision, Provider: "openai", Enabled: true, Credential: "explicit-compatible-token"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.s.DB.Exec(ctx, down); err != nil {
				t.Fatal("explicit compatible selection did not allow rollback", err)
			}
			if _, err = f.s.DB.Exec(ctx, `UPDATE speech_settings SET provider=$1`, provider); err == nil {
				t.Fatal("rollback failed to restore the older provider constraint")
			}
			if _, err = f.s.DB.Exec(ctx, read("000035_speech_providers.up.sql")); err != nil {
				t.Fatal("provider expansion could not be reapplied", err)
			}
			restored, err := f.s.Config(ctx, f.admin)
			if err != nil || restored.Provider != compatible.Provider || restored.Revision != compatible.Revision || !restored.HasCredential {
				t.Fatal("migration changed compatible configuration", restored, err)
			}
		})
	}
}
