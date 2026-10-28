package speech

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
)

// Provider defaults select wire protocols, not an arbitrary model or API host
// supplied by a recording. OpenAI-compatible endpoints remain administrator-owned.
func providerDefaults(provider string) (endpoint, model string, supported bool) {
	switch provider {
	case "openai":
		return "https://api.openai.com/v1", "gpt-4o-transcribe", true
	case "elevenlabs":
		return "https://api.elevenlabs.io/v1", "scribe_v2", true
	case "deepgram":
		return "https://api.deepgram.com/v1", "nova-3", true
	case "mistral":
		return "https://api.mistral.ai/v1", "voxtral-mini-latest", true
	default:
		return "", "", false
	}
}

func transcriptionRequest(ctx context.Context, cfg Config, credential string, upload Upload, mimeType, extension string) (*http.Request, error) {
	_, model, supported := providerDefaults(cfg.Provider)
	if !supported {
		return nil, ErrUnsupported
	}
	if cfg.ModelID != "" {
		model = cfg.ModelID
	}
	target := strings.TrimRight(cfg.Endpoint, "/")
	var body bytes.Buffer
	contentType := mimeType
	if cfg.Provider == "deepgram" {
		// The prerecorded API accepts the original encoded audio. Supplying
		// encoding/sample_rate would incorrectly treat WebM/MP4 as raw PCM.
		query := url.Values{"model": {model}}
		if upload.Language == "" {
			query.Set("detect_language", "true")
		} else {
			query.Set("language", upload.Language)
		}
		target += "/listen?" + query.Encode()
		_, _ = body.Write(upload.Data)
	} else {
		writer := multipart.NewWriter(&body)
		switch cfg.Provider {
		case "openai", "mistral":
			_ = writer.WriteField("model", model)
			if cfg.Provider == "openai" {
				_ = writer.WriteField("response_format", "json")
			} else {
				// Mistral's typed transcription request has its own non-streaming
				// schema; it does not accept OpenAI's response_format field.
				_ = writer.WriteField("stream", "false")
				_ = writer.WriteField("diarize", "false")
			}
			if upload.Language != "" {
				_ = writer.WriteField("language", upload.Language)
			}
			target += "/audio/transcriptions"
		case "elevenlabs":
			_ = writer.WriteField("model_id", model)
			_ = writer.WriteField("tag_audio_events", "false")
			_ = writer.WriteField("diarize", "false")
			_ = writer.WriteField("webhook", "false")
			if upload.Language != "" {
				_ = writer.WriteField("language_code", upload.Language)
			}
			target += "/speech-to-text"
		}
		fileHeader := textproto.MIMEHeader{}
		fileHeader.Set("Content-Disposition", `form-data; name="file"; filename="recording.`+extension+`"`)
		fileHeader.Set("Content-Type", mimeType)
		file, err := writer.CreatePart(fileHeader)
		if err != nil {
			return nil, ErrProvider
		}
		if _, err = file.Write(upload.Data); err != nil || writer.Close() != nil {
			return nil, ErrProvider
		}
		contentType = writer.FormDataContentType()
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, ErrProvider
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, &body)
	if err != nil {
		return nil, ErrProvider
	}
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Lester-Agent/1.0")
	switch cfg.Provider {
	case "elevenlabs":
		request.Header.Set("xi-api-key", credential)
	case "deepgram":
		request.Header.Set("Authorization", "Token "+credential)
	default:
		request.Header.Set("Authorization", "Bearer "+credential)
	}
	return request, nil
}

func transcriptionText(provider string, raw []byte) (string, error) {
	var text *string
	if provider == "deepgram" {
		var result struct {
			Results struct {
				Channels []struct {
					Alternatives []struct {
						Transcript *string `json:"transcript"`
					} `json:"alternatives"`
				} `json:"channels"`
			} `json:"results"`
		}
		if json.Unmarshal(raw, &result) != nil || len(result.Results.Channels) == 0 || len(result.Results.Channels[0].Alternatives) == 0 {
			return "", ErrProvider
		}
		text = result.Results.Channels[0].Alternatives[0].Transcript
	} else {
		var result struct {
			Text *string `json:"text"`
		}
		if json.Unmarshal(raw, &result) != nil {
			return "", ErrProvider
		}
		text = result.Text
	}
	if text == nil || strings.ContainsRune(*text, 0) {
		return "", ErrProvider
	}
	return *text, nil
}
