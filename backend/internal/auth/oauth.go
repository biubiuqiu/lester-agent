package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/oauth2"
)

var errEmailConflict = errors.New("email_conflict")
var errIdentityConflict = errors.New("identity_conflict")
var errRegistrationClosed = errors.New("registration_closed")
var errAccountUnavailable = errors.New("account_unavailable")

type providerIdentity struct{ Subject, Email, Name, AvatarURL string }
type oauthFlow struct {
	Provider, Purpose, Verifier string
	UserID                      *uuid.UUID
	SessionHash                 []byte
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func tokenDigest(value string) []byte { sum := sha256.Sum256([]byte(value)); return sum[:] }
func sessionDigest(r *http.Request) []byte {
	cookie, err := r.Cookie("lester_session")
	if err != nil {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil || len(raw) != 32 {
		return nil
	}
	sum := sha256.Sum256(raw)
	return sum[:]
}
func flowCookieName(provider string) string { return "lester_oauth_" + provider }
func (s *Service) flowCookie(w http.ResponseWriter, provider, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: flowCookieName(provider), Value: value, Path: "/api/v1/auth/oauth/" + provider, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}

func (s *Service) OAuthStart(w http.ResponseWriter, r *http.Request) { s.beginOAuth(w, r, "login") }
func (s *Service) OAuthLink(w http.ResponseWriter, r *http.Request)  { s.beginOAuth(w, r, "link") }
func (s *Service) beginOAuth(w http.ResponseWriter, r *http.Request, purpose string) {
	name := chi.URLParam(r, "provider")
	provider, ok := s.options.Providers[name]
	if !ok {
		httpapi.Error(w, 404, errors.New("此登录方式尚未配置"))
		return
	}
	if !s.allowAttempt(r, "oauth-start", name, 20) {
		w.Header().Set("Retry-After", "60")
		httpapi.Error(w, 429, errors.New("登录尝试过于频繁，请稍后再试"))
		return
	}
	state, err := randomToken()
	if err != nil {
		httpapi.Error(w, 500, errors.New("无法开始登录"))
		return
	}
	browser, err := randomToken()
	if err != nil {
		httpapi.Error(w, 500, errors.New("无法开始登录"))
		return
	}
	verifier := oauth2.GenerateVerifier()
	var userID *uuid.UUID
	var sessionHash []byte
	if purpose == "link" {
		p, ok := FromContext(r.Context())
		if !ok {
			httpapi.Error(w, 401, errors.New("请先登录原账号"))
			return
		}
		userID = &p.UserID
		sessionHash = sessionDigest(r)
		if sessionHash == nil {
			httpapi.Error(w, 401, errors.New("登录已失效"))
			return
		}
	}
	// State and the browser binding are hashed at rest; flows are shared by all
	// API replicas and expire in ten minutes. Each browser keeps one per provider.
	_, err = s.db.Exec(r.Context(), `DELETE FROM auth_oauth_flows WHERE expires_at<now()`)
	if err == nil {
		_, err = s.db.Exec(r.Context(), `INSERT INTO auth_oauth_flows(token_hash,browser_hash,provider,purpose,user_id,session_hash,verifier,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,now()+interval '10 minutes')`, tokenDigest(state), tokenDigest(browser), name, purpose, userID, sessionHash, verifier)
	}
	if err != nil {
		httpapi.Error(w, 500, errors.New("无法开始登录"))
		return
	}
	s.flowCookie(w, name, browser, 600)
	w.Header().Set("Cache-Control", "no-store")
	authorization := provider.Config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	if purpose == "link" {
		httpapi.JSON(w, 200, map[string]string{"url": authorization})
	} else {
		http.Redirect(w, r, authorization, http.StatusSeeOther)
	}
}

func (s *Service) oauthRedirect(w http.ResponseWriter, r *http.Request, purpose, code string) {
	target := s.options.WebOrigin + "/login"
	if purpose == "link" {
		target = s.options.WebOrigin + "/app/settings/profile"
	}
	if code != "" {
		target += "?auth_error=" + url.QueryEscape(code)
	} else if purpose == "link" {
		target += "?auth=linked"
	} else {
		target = s.options.WebOrigin + "/app"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
func (s *Service) OAuthCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	name := chi.URLParam(r, "provider")
	provider, ok := s.options.Providers[name]
	if !ok {
		s.oauthRedirect(w, r, "login", "provider_unavailable")
		return
	}
	cookie, err := r.Cookie(flowCookieName(name))
	state := r.URL.Query().Get("state")
	if err != nil || len(state) != 43 || len(cookie.Value) != 43 {
		s.oauthRedirect(w, r, "login", "invalid_state")
		return
	}
	var flow oauthFlow
	// Consume only the flow bound to this browser and provider. An invalid or
	// expired callback cannot replay it or delete another browser's flow.
	err = s.db.QueryRow(r.Context(), `DELETE FROM auth_oauth_flows WHERE token_hash=$1 AND browser_hash=$2 AND provider=$3 AND expires_at>now() RETURNING provider,purpose,user_id,session_hash,verifier`, tokenDigest(state), tokenDigest(cookie.Value), name).Scan(&flow.Provider, &flow.Purpose, &flow.UserID, &flow.SessionHash, &flow.Verifier)
	if err != nil {
		s.oauthRedirect(w, r, "login", "invalid_state")
		return
	}
	s.flowCookie(w, name, "", -1)
	if flow.Purpose == "link" && subtle.ConstantTimeCompare(flow.SessionHash, sessionDigest(r)) != 1 {
		s.oauthRedirect(w, r, "link", "session_expired")
		return
	}
	if r.URL.Query().Get("error") != "" {
		s.oauthRedirect(w, r, flow.Purpose, "access_denied")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" || len(code) > 4096 {
		s.oauthRedirect(w, r, flow.Purpose, "provider_error")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	client := s.options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
	token, err := provider.Config.Exchange(ctx, code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		s.oauthRedirect(w, r, flow.Purpose, "provider_error")
		return
	}
	identity, err := fetchProviderIdentity(ctx, client, name, provider, token.AccessToken)
	if err != nil {
		code := "provider_error"
		if errors.Is(err, errUnverifiedEmail) {
			code = "verified_email_required"
		}
		s.oauthRedirect(w, r, flow.Purpose, code)
		return
	}
	userID, created, session, err := s.completeOAuth(ctx, flow, identity)
	if err != nil {
		code := "provider_error"
		for _, known := range []error{errEmailConflict, errIdentityConflict, errRegistrationClosed, errAccountUnavailable} {
			if errors.Is(err, known) {
				code = known.Error()
			}
		}
		s.oauthRedirect(w, r, flow.Purpose, code)
		return
	}
	if created && identity.AvatarURL != "" {
		_ = s.importProviderAvatar(ctx, userID, identity.AvatarURL, false)
	}
	if flow.Purpose == "login" {
		s.setCookie(w, session)
	}
	s.oauthRedirect(w, r, flow.Purpose, "")
}

var errUnverifiedEmail = errors.New("verified email required")

func providerJSON(ctx context.Context, client *http.Client, endpoint, accessToken string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Lester-Agent")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("provider identity request failed")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return errors.New("provider response too large")
	}
	return json.Unmarshal(data, target)
}
func fetchProviderIdentity(ctx context.Context, client *http.Client, name string, provider OAuthProvider, token string) (providerIdentity, error) {
	var result providerIdentity
	if name == "google" {
		var user struct {
			Sub, Email, Name, Picture string
			EmailVerified             bool `json:"email_verified"`
		}
		if err := providerJSON(ctx, client, provider.UserInfoURL, token, &user); err != nil {
			return result, err
		}
		if !user.EmailVerified {
			return result, errUnverifiedEmail
		}
		result = providerIdentity{Subject: user.Sub, Email: user.Email, Name: user.Name, AvatarURL: user.Picture}
	} else {
		var user struct {
			ID          int64
			Login, Name string
			AvatarURL   string `json:"avatar_url"`
		}
		if err := providerJSON(ctx, client, provider.UserInfoURL, token, &user); err != nil {
			return result, err
		}
		if user.ID <= 0 {
			return result, errors.New("invalid GitHub subject")
		}
		var emails []struct {
			Email             string
			Primary, Verified bool
		}
		if err := providerJSON(ctx, client, provider.EmailsURL, token, &emails); err != nil {
			return result, err
		}
		result = providerIdentity{Subject: strconv.FormatInt(user.ID, 10), Name: user.Name, AvatarURL: user.AvatarURL}
		if result.Name == "" {
			result.Name = user.Login
		}
		for _, email := range emails {
			if email.Verified && (result.Email == "" || email.Primary) {
				result.Email = email.Email
				if email.Primary {
					break
				}
			}
		}
		if result.Email == "" {
			return result, errUnverifiedEmail
		}
	}
	var err error
	result.Email, err = normalizeEmail(result.Email)
	if err != nil {
		return result, errUnverifiedEmail
	}
	if result.Subject == "" || len(result.Subject) > 255 {
		return result, errors.New("invalid provider subject")
	}
	result.Name = strings.TrimSpace(result.Name)
	if result.Name == "" {
		result.Name = strings.Split(result.Email, "@")[0]
	}
	if len([]rune(result.Name)) > 60 {
		result.Name = string([]rune(result.Name)[:60])
	}
	if len(result.AvatarURL) > 2048 || !allowedAvatarURL(result.AvatarURL) {
		result.AvatarURL = ""
	}
	return result, nil
}

func (s *Service) completeOAuth(ctx context.Context, flow oauthFlow, identity providerIdentity) (uuid.UUID, bool, sessionRecord, error) {
	var userID uuid.UUID
	var session sessionRecord
	created := false
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return userID, false, session, err
	}
	defer tx.Rollback(ctx)
	// Serialize the same provider identity across API replicas. Email uniqueness
	// and per-user locks additionally protect registration and link/unlink races.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "oauth:"+flow.Provider+":"+identity.Subject); err != nil {
		return userID, false, session, err
	}
	if flow.Purpose == "link" {
		if flow.UserID == nil {
			return userID, false, session, errAccountUnavailable
		}
		userID = *flow.UserID
		if err = lockAccountSession(ctx, tx, userID, flow.SessionHash); err != nil {
			return userID, false, session, errAccountUnavailable
		}
		var owner uuid.UUID
		err = tx.QueryRow(ctx, `SELECT user_id FROM auth_identities WHERE provider=$1 AND subject=$2`, flow.Provider, identity.Subject).Scan(&owner)
		if err == nil && owner != userID {
			return userID, false, session, errIdentityConflict
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return userID, false, session, err
		}
		var existing string
		err = tx.QueryRow(ctx, `SELECT subject FROM auth_identities WHERE user_id=$1 AND provider=$2`, userID, flow.Provider).Scan(&existing)
		if err == nil && existing != identity.Subject {
			return userID, false, session, errIdentityConflict
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return userID, false, session, err
		}
	} else {
		err = tx.QueryRow(ctx, `SELECT user_id FROM auth_identities WHERE provider=$1 AND subject=$2`, flow.Provider, identity.Subject).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			if !s.options.RegistrationEnabled {
				return userID, false, session, errRegistrationClosed
			}
			var exists bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)`, identity.Email).Scan(&exists); err != nil {
				return userID, false, session, err
			}
			if exists {
				return userID, false, session, errEmailConflict
			}
			userID, _, err = createAccount(ctx, tx, identity.Email, identity.Name, nil, true, false)
			if err != nil {
				var pe *pgconn.PgError
				if errors.As(err, &pe) && pe.Code == "23505" {
					return userID, false, session, errEmailConflict
				}
				return userID, false, session, err
			}
			created = true
		} else if err != nil {
			return userID, false, session, err
		}
		var available bool
		if err = tx.QueryRow(ctx, `SELECT NOT disabled AND (NOT email_verification_required OR email_verified) FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&available); err != nil || !available {
			return userID, false, session, errAccountUnavailable
		}
		if !created {
			var stillLinked bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM auth_identities WHERE provider=$1 AND subject=$2 AND user_id=$3)`, flow.Provider, identity.Subject, userID).Scan(&stillLinked); err != nil || !stillLinked {
				return userID, false, session, errAccountUnavailable
			}
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO auth_identities(user_id,provider,subject,email,avatar_url) VALUES($1,$2,$3,$4,$5) ON CONFLICT(provider,subject) DO UPDATE SET email=EXCLUDED.email,avatar_url=EXCLUDED.avatar_url`, userID, flow.Provider, identity.Subject, identity.Email, identity.AvatarURL)
	if err != nil {
		return userID, false, session, errIdentityConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET email_verified=true WHERE id=$1 AND email=$2`, userID, identity.Email); err != nil {
		return userID, false, session, err
	}
	if flow.Purpose == "login" {
		session, err = newSession(s.ttl)
		if err == nil {
			err = insertSession(ctx, tx, userID, session)
		}
		if err != nil {
			return userID, false, session, err
		}
	}
	err = tx.Commit(ctx)
	return userID, created, session, err
}

func lockAccountSession(ctx context.Context, tx pgx.Tx, userID uuid.UUID, hash []byte) error {
	var available bool
	if err := tx.QueryRow(ctx, `SELECT NOT disabled AND (NOT email_verification_required OR email_verified) FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&available); err != nil || !available {
		return errAccountUnavailable
	}
	var valid bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE user_id=$1 AND token_hash=$2 AND expires_at>now())`, userID, hash).Scan(&valid); err != nil || !valid {
		return errAccountUnavailable
	}
	return nil
}

func (s *Service) Identities(w http.ResponseWriter, r *http.Request) {
	p, _ := FromContext(r.Context())
	rows, err := s.db.Query(r.Context(), `SELECT provider,email,avatar_url<>'' FROM auth_identities WHERE user_id=$1 ORDER BY provider`, p.UserID)
	if err != nil {
		httpapi.Error(w, 500, errors.New("无法读取登录方式"))
		return
	}
	defer rows.Close()
	type identity struct {
		Provider  string `json:"provider"`
		Email     string `json:"email"`
		HasAvatar bool   `json:"has_avatar"`
	}
	items := []identity{}
	for rows.Next() {
		var item identity
		if err = rows.Scan(&item.Provider, &item.Email, &item.HasAvatar); err != nil {
			httpapi.Error(w, 500, errors.New("无法读取登录方式"))
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		httpapi.Error(w, 500, errors.New("无法读取登录方式"))
		return
	}
	httpapi.JSON(w, 200, map[string]any{"identities": items})
}
func (s *Service) UnlinkIdentity(w http.ResponseWriter, r *http.Request) {
	p, _ := FromContext(r.Context())
	provider := chi.URLParam(r, "provider")
	if provider != "google" && provider != "github" {
		httpapi.Error(w, 404, errors.New("未知登录方式"))
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpapi.Error(w, 500, errors.New("无法解除绑定"))
		return
	}
	defer tx.Rollback(r.Context())
	if err = lockAccountSession(r.Context(), tx, p.UserID, sessionDigest(r)); err != nil {
		httpapi.Error(w, 401, errors.New("请重新登录"))
		return
	}
	var alternatives int
	availableProviders := []string{}
	for name := range s.options.Providers {
		availableProviders = append(availableProviders, name)
	}
	err = tx.QueryRow(r.Context(), `SELECT (CASE WHEN password_hash IS NOT NULL THEN 1 ELSE 0 END)+(SELECT count(*)::int FROM auth_identities WHERE user_id=$1 AND provider<>$2 AND provider=ANY($3::text[])) FROM users WHERE id=$1`, p.UserID, provider, availableProviders).Scan(&alternatives)
	if err != nil {
		httpapi.Error(w, 500, errors.New("无法解除绑定"))
		return
	}
	if alternatives == 0 {
		httpapi.Error(w, 409, errors.New("请先设置密码或绑定另一种登录方式，再解除此绑定"))
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM auth_identities WHERE user_id=$1 AND provider=$2`, p.UserID, provider); err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM auth_oauth_flows WHERE user_id=$1`, p.UserID)
	}
	var session sessionRecord
	if err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM sessions WHERE user_id=$1`, p.UserID)
	}
	if err == nil {
		session, err = newSession(s.ttl)
	}
	if err == nil {
		err = insertSession(r.Context(), tx, p.UserID, session)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpapi.Error(w, 500, errors.New("无法解除绑定"))
		return
	}
	s.setCookie(w, session)
	w.WriteHeader(204)
}

// Keep endpoint construction and token handling out of HTTP/domain callers.
func DefaultProviders(googleID, googleSecret, githubID, githubSecret string) map[string]OAuthProvider {
	providers := map[string]OAuthProvider{}
	if googleID != "" {
		providers["google"] = OAuthProvider{Config: oauth2.Config{ClientID: googleID, ClientSecret: googleSecret, Scopes: []string{"openid", "email", "profile"}, Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token", AuthStyle: oauth2.AuthStyleInParams}}, UserInfoURL: "https://openidconnect.googleapis.com/v1/userinfo"}
	}
	if githubID != "" {
		providers["github"] = OAuthProvider{Config: oauth2.Config{ClientID: githubID, ClientSecret: githubSecret, Scopes: []string{"read:user", "user:email"}, Endpoint: oauth2.Endpoint{AuthURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token", AuthStyle: oauth2.AuthStyleInParams}}, UserInfoURL: "https://api.github.com/user", EmailsURL: "https://api.github.com/user/emails"}
	}
	return providers
}
