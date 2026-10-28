package speech

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func testWAV() []byte {
	data := make([]byte, 48)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 16000)
	binary.LittleEndian.PutUint32(data[28:], 32000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], 4)
	return data
}

func TestAudioContainerGateUsesBytesRatherThanFilename(t *testing.T) {
	mp4 := make([]byte, 32)
	binary.BigEndian.PutUint32(mp4, 24)
	copy(mp4[4:], "ftypM4A ")
	webm := append([]byte{0x1a, 0x45, 0xdf, 0xa3}, []byte("header-webm-more-audio")...)
	ogg := make([]byte, 48)
	copy(ogg, "OggS")
	ogg[26], ogg[27] = 1, 20
	copy(ogg[28:], "OpusHead")
	flac := make([]byte, 48)
	copy(flac, "fLaC")
	flac[7] = 34
	mp3 := []byte{0xff, 0xfb, 0x90, 0x44, 0x01, 0x02}
	id3MP3 := append([]byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0}, mp3...)
	for _, input := range []struct {
		name string
		data []byte
		mime string
	}{
		{"wav", testWAV(), "audio/wav"}, {"webm", webm, "audio/webm"}, {"mp4", mp4, "audio/mp4"},
		{"ogg", ogg, "audio/ogg"}, {"flac", flac, "audio/flac"}, {"mp3", mp3, "audio/mpeg"}, {"id3", id3MP3, "audio/mpeg"},
	} {
		t.Run(input.name, func(t *testing.T) {
			mime, ext, err := validateAudio(input.data)
			if err != nil || mime != input.mime || ext == "" {
				t.Fatalf("audio container rejected: %s %s %v", mime, ext, err)
			}
		})
	}
	avif := bytes.Clone(mp4)
	copy(avif[8:], "avif")
	for _, data := range [][]byte{nil, []byte("<html>pretend this is recording.webm</html>"), []byte("RIFF"), avif, []byte("ID3\x04\x00\x00\x00\x00\x00\x00"), bytes.Repeat([]byte("x"), MaxAudioBytes+1)} {
		if _, _, err := validateAudio(data); err == nil {
			t.Fatal("invalid or oversized audio accepted")
		}
	}
}

func TestProviderProtocolUsesSeparateMultipartAndAuthentication(t *testing.T) {
	for _, provider := range []string{"openai", "elevenlabs"} {
		t.Run(provider, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost {
					t.Error("not a POST")
				}
				if err := r.ParseMultipartForm(MaxAudioBytes + 2048); err != nil {
					t.Error(err)
				}
				defer r.MultipartForm.RemoveAll()
				file, header, err := r.FormFile("file")
				if err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				defer file.Close()
				data, _ := io.ReadAll(file)
				if !bytes.Equal(data, testWAV()) || header.Filename != "recording.wav" || header.Header.Get("Content-Type") != "audio/wav" {
					t.Error("audio bytes or canonical file metadata changed")
				}
				if provider == "openai" {
					if r.URL.Path != "/v1/audio/transcriptions" || r.Header.Get("Authorization") != "Bearer fixture-secret" || r.Header.Get("xi-api-key") != "" || r.FormValue("model") != "gpt-4o-transcribe" || r.FormValue("response_format") != "json" || r.FormValue("language") != "zh" || r.FormValue("model_id") != "" {
						t.Error("incorrect OpenAI transcriptions wire format")
					}
				} else {
					if r.URL.Path != "/v1/speech-to-text" || r.Header.Get("xi-api-key") != "fixture-secret" || r.Header.Get("Authorization") != "" || r.FormValue("model_id") != "scribe_v2" || r.FormValue("language_code") != "zh" || r.FormValue("diarize") != "false" || r.FormValue("tag_audio_events") != "false" || r.FormValue("webhook") != "false" || r.URL.RawQuery != "" {
						t.Error("incorrect ElevenLabs Scribe wire format")
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"text": "  修复 Lester 的 GitHub API，然后运行 tests。  ", "words": []any{}})
			}))
			defer server.Close()
			text, err := providerCall(context.Background(), server.Client(), Config{Provider: provider, Endpoint: server.URL + "/v1"}, "fixture-secret", Upload{Data: testWAV(), Filename: "../../spoof.html", MIME: "text/html", Language: "zh"})
			if err != nil || text != "修复 Lester 的 GitHub API，然后运行 tests。" || calls.Load() != 1 {
				t.Fatalf("failed transcription: %q %v calls=%d", text, err, calls.Load())
			}
		})
	}
}

func TestProviderDoesNotFollowRedirectOrLeakUpstreamErrors(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		destinationCalls.Add(1)
		_, _ = io.WriteString(w, `{"text":"should not arrive"}`)
	}))
	defer destination.Close()
	for _, status := range []int{301, 302, 307, 308, 400, 401, 403, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", destination.URL)
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"fixture-secret microphone private data"}`)
			}))
			defer server.Close()
			text, err := providerCall(context.Background(), server.Client(), Config{Provider: "openai", Endpoint: server.URL}, "fixture-secret", Upload{Data: testWAV()})
			if err == nil || text != "" || strings.Contains(err.Error(), "fixture-secret") || strings.Contains(err.Error(), "private data") {
				t.Fatal("upstream failure leaked or became success", err)
			}
			if (status == 401 || status == 403) && !errors.Is(err, ErrProviderAuth) {
				t.Fatal("authorization failure classification changed", err)
			}
		})
	}
	if destinationCalls.Load() != 0 {
		t.Fatal("provider credentials followed a redirect")
	}
}

func TestProviderRejectsInvalidOrUnboundedResultsAndDoesNotRewrite(t *testing.T) {
	for _, input := range []struct {
		name string
		body string
		want error
	}{
		{"silence", `{"text":" \n\t "}`, ErrNoSpeech},
		{"missing", `{"words":[]}`, ErrProvider},
		{"null", `{"text":null}`, ErrProvider},
		{"wrong type", `{"text":12}`, ErrProvider},
		{"nul", `{"text":"hello\u0000world"}`, ErrProvider},
		{"trailing", `{"text":"first"}{"text":"second"}`, ErrProvider},
		{"invalid unicode", "{\"text\":\"\xff\"}", ErrProvider},
		{"too much text", `{"text":"` + strings.Repeat("中", MaxTextRunes+1) + `"}`, ErrLimit},
		{"too much body", `{"text":"` + strings.Repeat("a", maxProviderResponseBytes) + `"}`, ErrProvider},
	} {
		t.Run(input.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, input.body) }))
			defer server.Close()
			text, err := providerCall(context.Background(), server.Client(), Config{Provider: "openai", Endpoint: server.URL}, "fixture-secret", Upload{Data: testWAV()})
			if text != "" || !errors.Is(err, input.want) {
				t.Fatalf("unexpected provider result %q %v want %v", text, err, input.want)
			}
		})
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1) }))
	defer server.Close()
	for _, input := range []Upload{{Data: []byte("not audio")}, {Data: testWAV(), Language: "auto"}, {Data: testWAV(), Language: "EN"}} {
		if _, err := providerCall(context.Background(), server.Client(), Config{Provider: "openai", Endpoint: server.URL}, "fixture-secret", input); err == nil {
			t.Fatal("invalid upload reached provider")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid upload made a provider request")
	}
}
