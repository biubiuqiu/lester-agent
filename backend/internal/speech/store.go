package speech

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"net/url"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func authorize(ctx context.Context, tx pgx.Tx, a Actor, admin bool) error {
	if a.UserID == uuid.Nil || a.WorkspaceID == uuid.Nil || len(a.SessionHash) != 32 {
		return ErrUnauthorized
	}
	var role string
	if err := tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1 AND NOT disabled AND (NOT email_verification_required OR email_verified) FOR SHARE`, a.UserID).Scan(&role); err != nil {
		return ErrUnauthorized
	}
	if admin && role != "admin" {
		return ErrAdmin
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM sessions WHERE user_id=$1 AND token_hash=$2 AND expires_at>now() FOR SHARE`, a.UserID, a.SessionHash).Scan(&id); err != nil {
		return ErrUnauthorized
	}
	if err := tx.QueryRow(ctx, `SELECT user_id FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, a.WorkspaceID, a.UserID).Scan(&id); err != nil {
		return ErrUnauthorized
	}
	return nil
}

func normalizeConfig(in ConfigChange) (Config, error) {
	c := Config{Enabled: in.Enabled, Provider: strings.TrimSpace(in.Provider), Endpoint: strings.TrimRight(strings.TrimSpace(in.Endpoint), "/"), ModelID: strings.TrimSpace(in.ModelID)}
	if c.Provider == "" {
		c.Provider = "openai"
	}
	switch c.Provider {
	case "openai":
		if c.Endpoint == "" {
			c.Endpoint = "https://api.openai.com/v1"
		}
		if c.ModelID == "" {
			c.ModelID = "gpt-4o-transcribe"
		}
	case "elevenlabs":
		if c.Endpoint == "" {
			c.Endpoint = "https://api.elevenlabs.io/v1"
		}
		if c.ModelID == "" {
			c.ModelID = "scribe_v2"
		}
	default:
		return Config{}, ErrConfig
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || len(c.Endpoint) > 2048 || strings.ContainsAny(c.Endpoint, "\x00\r\n\\") {
		return Config{}, ErrConfig
	}
	if u.Path != "" && path.Clean(u.Path) != u.Path {
		return Config{}, ErrConfig
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || !(u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()) {
			return Config{}, ErrConfig
		}
	}
	if c.Provider == "elevenlabs" && c.Endpoint != "https://api.elevenlabs.io/v1" {
		return Config{}, ErrConfig
	}
	if c.ModelID == "" || len(c.ModelID) > 200 || strings.ContainsAny(c.ModelID, "\x00\r\n\t") || len(in.Credential) > 8192 || strings.ContainsAny(in.Credential, "\x00\r\n") || in.ClearCredential && strings.TrimSpace(in.Credential) != "" {
		return Config{}, ErrConfig
	}
	return c, nil
}

func credentialAAD(c Config) []byte {
	u, _ := url.Parse(c.Endpoint)
	return []byte("lester:speech:v1:" + c.Provider + ":" + strings.ToLower(u.Scheme+"://"+u.Host))
}
func sameCredentialScope(a, b Config) bool {
	return string(credentialAAD(a)) == string(credentialAAD(b))
}
func (s *Service) credential(c Config) (string, error) {
	if len(c.nonce) != s.vault.NonceSize() || len(c.ciphertext) == 0 {
		return "", ErrConfig
	}
	plain, err := s.vault.Open(nil, c.nonce, c.ciphertext, credentialAAD(c))
	if err != nil {
		return "", ErrConfig
	}
	return string(plain), nil
}
func readConfig(ctx context.Context, tx pgx.Tx, lock bool) (Config, error) {
	var c Config
	query := `SELECT enabled,provider,endpoint,model_id,credential_ciphertext,credential_nonce,revision FROM speech_settings WHERE singleton=true`
	if lock {
		query += " FOR UPDATE"
	}
	err := tx.QueryRow(ctx, query).Scan(&c.Enabled, &c.Provider, &c.Endpoint, &c.ModelID, &c.ciphertext, &c.nonce, &c.Revision)
	c.HasCredential = len(c.ciphertext) > 0
	return c, err
}
func (s *Service) Config(ctx context.Context, a Actor) (Config, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Config{}, ErrStorage
	}
	defer tx.Rollback(ctx)
	if err = authorize(ctx, tx, a, true); err != nil {
		return Config{}, err
	}
	c, err := readConfig(ctx, tx, false)
	if err != nil {
		return Config{}, ErrStorage
	}
	if err = tx.Commit(ctx); err != nil {
		return Config{}, ErrStorage
	}
	return c, nil
}
func (s *Service) Capabilities(ctx context.Context, a Actor) (Capability, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Capability{}, ErrStorage
	}
	defer tx.Rollback(ctx)
	if err = authorize(ctx, tx, a, false); err != nil {
		return Capability{}, err
	}
	c, err := readConfig(ctx, tx, false)
	if err != nil {
		return Capability{}, ErrStorage
	}
	enabled := c.Enabled && c.HasCredential
	if enabled {
		_, err = s.credential(c)
		enabled = err == nil
	}
	if err = tx.Commit(ctx); err != nil {
		return Capability{}, ErrStorage
	}
	return Capability{Enabled: enabled, Provider: c.Provider, MaxAudioBytes: MaxAudioBytes, MaxDurationSeconds: MaxDurationSeconds, Revision: c.Revision}, nil
}
func (s *Service) Save(ctx context.Context, a Actor, in ConfigChange) (Config, error) {
	next, err := normalizeConfig(in)
	if err != nil {
		return Config{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Config{}, ErrStorage
	}
	defer tx.Rollback(ctx)
	if err = authorize(ctx, tx, a, true); err != nil {
		return Config{}, err
	}
	current, err := readConfig(ctx, tx, true)
	if err != nil {
		return Config{}, ErrStorage
	}
	if current.Revision != in.Revision {
		return Config{}, ErrConflict
	}
	credential := strings.TrimSpace(in.Credential)
	if credential != "" {
		next.nonce = make([]byte, s.vault.NonceSize())
		if _, err = rand.Read(next.nonce); err != nil {
			return Config{}, ErrStorage
		}
		next.ciphertext = s.vault.Seal(nil, next.nonce, []byte(credential), credentialAAD(next))
	} else if !in.ClearCredential && sameCredentialScope(current, next) {
		next.ciphertext = current.ciphertext
		next.nonce = current.nonce
	}
	next.HasCredential = len(next.ciphertext) > 0
	if next.Enabled && !next.HasCredential {
		return Config{}, ErrConfig
	}
	err = tx.QueryRow(ctx, `UPDATE speech_settings SET enabled=$1,provider=$2,endpoint=$3,model_id=$4,credential_ciphertext=$5,credential_nonce=$6,revision=revision+1,updated_at=now() WHERE singleton=true RETURNING revision`, next.Enabled, next.Provider, next.Endpoint, next.ModelID, next.ciphertext, next.nonce).Scan(&next.Revision)
	if err != nil {
		return Config{}, ErrStorage
	}
	if err = tx.Commit(ctx); err != nil {
		return Config{}, ErrStorage
	}
	return next, nil
}

func errorStatus(err error) int {
	switch {
	case errors.Is(err, ErrUnauthorized):
		return 401
	case errors.Is(err, ErrAdmin):
		return 403
	case errors.Is(err, ErrDisabled):
		return 503
	case errors.Is(err, ErrConflict), errors.Is(err, ErrBusy):
		return 409
	case errors.Is(err, ErrRate):
		return 429
	case errors.Is(err, ErrLimit):
		return 413
	case errors.Is(err, ErrNotFound):
		return 404
	case errors.Is(err, ErrExpired):
		return 410
	case errors.Is(err, ErrProvider):
		return 502
	case errors.Is(err, ErrStorage):
		return 503
	default:
		return 400
	}
}
