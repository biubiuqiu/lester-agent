package auth

import (
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/biubiuqiu/lester-agent/backend/internal/blob"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"golang.org/x/oauth2"
)

// Provider endpoints are server-owned. Configuration never accepts a browser-
// supplied endpoint, email claim, access token, or identity subject.
type OAuthProvider struct {
	Config      oauth2.Config
	UserInfoURL string
	EmailsURL   string
}
type Options struct {
	WebOrigin           string
	RegistrationEnabled bool
	Providers           map[string]OAuthProvider
	Mailer              Mailer
	Avatars             blob.Store
	HTTPClient          *http.Client
}

func (s *Service) Configure(options Options) error {
	u, err := url.Parse(options.WebOrigin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("WEB_ORIGIN must be an absolute HTTP(S) origin without path, query, or credentials")
	}
	if len(options.Providers) > 0 && u.Scheme != "https" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return errors.New("OAuth requires HTTPS outside localhost")
	}
	if u.Scheme == "https" && !s.secure {
		return errors.New("HTTPS authentication requires SESSION_COOKIE_SECURE=true")
	}
	options.WebOrigin = strings.TrimRight(options.WebOrigin, "/")
	for name, provider := range options.Providers {
		if name != "google" && name != "github" {
			return errors.New("unsupported OAuth provider")
		}
		if provider.Config.ClientID == "" || provider.Config.ClientSecret == "" {
			return errors.New("OAuth client ID and secret must both be set")
		}
		provider.Config.RedirectURL = options.WebOrigin + "/api/v1/auth/oauth/" + name + "/callback"
		options.Providers[name] = provider
	}
	s.options = options
	return nil
}

func (s *Service) Capabilities(w http.ResponseWriter, r *http.Request) {
	providers := []string{}
	for _, name := range []string{"google", "github"} {
		if _, ok := s.options.Providers[name]; ok {
			providers = append(providers, name)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	httpapi.JSON(w, 200, map[string]any{"providers": providers, "registration_enabled": s.options.RegistrationEnabled, "email_verification_required": s.options.Mailer != nil, "password_reset_enabled": s.options.Mailer != nil})
}

func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > 254 || !strings.Contains(email, "@") {
		return "", errors.New("请填写有效的邮箱地址")
	}
	return email, nil
}
func validatePassword(password string) error {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 10 || len(password) > 1024 {
		return errors.New("密码需至少 10 个字符，且不超过 1024 字节")
	}
	return nil
}

// Reject browser cross-origin mutations, including same-site sibling hosts.
// Non-browser clients can omit Origin; browsers must match the deployment.
func MutationOrigin(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
				supplied := r.Header.Get("Origin")
				if (supplied != "" && supplied != strings.TrimRight(origin, "/")) || (supplied == "" && r.Header.Get("Sec-Fetch-Site") == "cross-site") {
					httpapi.Error(w, http.StatusForbidden, errors.New("请求来源不被允许"))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
