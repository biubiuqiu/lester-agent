package speech

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const maxProviderResponseBytes = 2 << 20

// validateAudio checks the container rather than trusting a browser-supplied
// filename or MIME type. It is a bounded format gate, not an audio decoder or a
// claim about the recording's actual duration or whether speech is present.
func validateAudio(data []byte) (string, string, error) {
	if len(data) == 0 || len(data) > MaxAudioBytes {
		return "", "", ErrLimit
	}
	header := data[:min(len(data), 4096)]
	switch {
	case len(data) >= 44 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WAVE")):
		return "audio/wav", "wav", nil
	case len(data) >= 16 && bytes.Equal(data[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}) && bytes.Contains(header, []byte("webm")):
		return "audio/webm", "webm", nil
	case len(data) >= 24 && bytes.Equal(data[4:8], []byte("ftyp")):
		size := binary.BigEndian.Uint32(data[:4])
		// Exclude other ISO-BMFF containers, e.g. AVIF/HEIC images, which share
		// the ftyp signature but are not microphone audio.
		brands := map[string]bool{"isom": true, "iso2": true, "iso5": true, "iso6": true, "mp41": true, "mp42": true, "M4A ": true, "M4B ": true, "M4V ": true, "qt  ": true, "dash": true, "MSNV": true}
		if size >= 16 && int64(size) <= int64(len(data)) && brands[string(data[8:12])] {
			return "audio/mp4", "m4a", nil
		}
	case len(data) >= 28 && bytes.Equal(data[:4], []byte("OggS")) && data[4] == 0:
		segments := int(data[26])
		if segments > 0 && 27+segments < len(data) && (bytes.Contains(header, []byte("OpusHead")) || bytes.Contains(header, []byte("\x01vorbis")) || bytes.Contains(header, []byte("fLaC"))) {
			return "audio/ogg", "ogg", nil
		}
	case len(data) >= 42 && bytes.Equal(data[:4], []byte("fLaC")) && data[4]&0x7f == 0 && data[5] == 0 && data[6] == 0 && data[7] == 34:
		return "audio/flac", "flac", nil
	}
	if mp3Audio(data) {
		return "audio/mpeg", "mp3", nil
	}
	return "", "", ErrUnsupported
}

func mp3Audio(data []byte) bool {
	position := 0
	if len(data) >= 10 && bytes.Equal(data[:3], []byte("ID3")) {
		if data[3] < 2 || data[3] > 4 || data[6]&0x80 != 0 || data[7]&0x80 != 0 || data[8]&0x80 != 0 || data[9]&0x80 != 0 {
			return false
		}
		size := int(data[6])<<21 | int(data[7])<<14 | int(data[8])<<7 | int(data[9])
		position = 10 + size
		if data[3] == 4 && data[5]&0x10 != 0 {
			position += 10 // ID3v2.4 optional footer.
		}
	}
	if position+4 > len(data) {
		return false
	}
	header := data[position : position+4]
	return header[0] == 0xff && header[1]&0xe0 == 0xe0 && header[1]&0x18 != 0x08 && header[1]&0x06 != 0 && header[2]&0xf0 != 0 && header[2]&0xf0 != 0xf0 && header[2]&0x0c != 0x0c
}

// providerCall sends one completed recording. There is no fallback provider,
// automatic retry, webhook, translation or generative rewrite of the result.
// The caller owns the durable request identity before any network I/O.
func providerCall(ctx context.Context, configured *http.Client, cfg Config, credential string, upload Upload) (string, error) {
	mimeType, extension, err := validateAudio(upload.Data)
	if err != nil {
		return "", err
	}
	if upload.Language != "" {
		if len(upload.Language) < 2 || len(upload.Language) > 3 {
			return "", ErrUnsupported
		}
		for _, letter := range upload.Language {
			if letter < 'a' || letter > 'z' {
				return "", ErrUnsupported
			}
		}
	}
	if credential == "" || strings.ContainsAny(credential, "\r\n\x00") {
		return "", ErrProviderAuth
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	model := cfg.ModelID
	target := strings.TrimRight(cfg.Endpoint, "/")
	switch cfg.Provider {
	case "openai":
		if model == "" {
			model = "gpt-4o-transcribe"
		}
		_ = writer.WriteField("model", model)
		_ = writer.WriteField("response_format", "json")
		if upload.Language != "" {
			_ = writer.WriteField("language", upload.Language)
		}
		target += "/audio/transcriptions"
	case "elevenlabs":
		if model == "" {
			model = "scribe_v2"
		}
		_ = writer.WriteField("model_id", model)
		_ = writer.WriteField("tag_audio_events", "false")
		_ = writer.WriteField("diarize", "false")
		_ = writer.WriteField("webhook", "false")
		if upload.Language != "" {
			_ = writer.WriteField("language_code", upload.Language)
		}
		target += "/speech-to-text"
	default:
		return "", ErrUnsupported
	}
	fileHeader := textproto.MIMEHeader{}
	fileHeader.Set("Content-Disposition", `form-data; name="file"; filename="recording.`+extension+`"`)
	fileHeader.Set("Content-Type", mimeType)
	file, err := writer.CreatePart(fileHeader)
	if err != nil {
		return "", ErrProvider
	}
	if _, err = file.Write(upload.Data); err != nil || writer.Close() != nil {
		return "", ErrProvider
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", ErrProvider
	}
	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(callCtx, http.MethodPost, target, &body)
	if err != nil {
		return "", ErrProvider
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Lester-Agent/1.0")
	if cfg.Provider == "openai" {
		request.Header.Set("Authorization", "Bearer "+credential)
	} else {
		request.Header.Set("xi-api-key", credential)
	}
	client := http.Client{}
	if configured != nil {
		client = *configured
	}
	// Never forward provider credentials to a Location selected by an upstream.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if client.Timeout <= 0 || client.Timeout > 60*time.Second {
		client.Timeout = 60 * time.Second
	}
	response, err := client.Do(request)
	if err != nil {
		return "", ErrProvider
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return "", ErrProviderAuth
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", ErrProvider
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes+1))
	if err != nil || len(raw) > maxProviderResponseBytes || !utf8.Valid(raw) {
		return "", ErrProvider
	}
	var result struct {
		Text *string `json:"text"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Text == nil || strings.ContainsRune(*result.Text, 0) {
		return "", ErrProvider
	}
	text := strings.TrimSpace(*result.Text)
	if text == "" {
		return "", ErrNoSpeech
	}
	if utf8.RuneCountInString(text) > MaxTextRunes {
		return "", ErrLimit
	}
	return text, nil
}
