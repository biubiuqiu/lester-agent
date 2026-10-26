package connector

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"
)

type Handler struct{ Service *Service }

func principal(r *http.Request) (Actor, bool) {
	p, ok := auth.FromContext(r.Context())
	return Actor{WorkspaceID: p.WorkspaceID, UserID: p.UserID, SessionHash: p.SessionHash}, ok
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		httpapi.Error(w, 400, ErrArguments)
		return false
	}
	return true
}
func failure(w http.ResponseWriter, err error) {
	status := 503
	switch {
	case errors.Is(err, ErrArguments):
		status = 400
	case errors.Is(err, ErrUnavailable):
		status = 404
	case errors.Is(err, ErrConflict):
		status = 409
	case errors.Is(err, ErrSession):
		status = 401
	default:
		err = ErrProvider
	}
	httpapi.Error(w, status, err)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a, ok := principal(r)
	if !ok {
		failure(w, ErrSession)
		return
	}
	items, err := h.Service.List(r.Context(), a)
	if err != nil {
		failure(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"connections": items})
}
func (h *Handler) Connect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a, ok := principal(r)
	if !ok {
		failure(w, ErrSession)
		return
	}
	var input struct {
		Revision int64  `json:"revision"`
		ReturnTo string `json:"return_to"`
	}
	if !decode(w, r, &input) {
		return
	}
	u, browser, err := h.Service.begin(r.Context(), a, chi.URLParam(r, "provider"), input.Revision, input.ReturnTo)
	if err != nil {
		failure(w, err)
		return
	}
	h.Service.flowCookie(w, chi.URLParam(r, "provider"), browser, 600)
	httpapi.JSON(w, 200, map[string]string{"url": u})
}
func (h *Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a, ok := principal(r)
	if !ok {
		failure(w, ErrSession)
		return
	}
	var input struct {
		Revision int64 `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if err := h.Service.Disconnect(r.Context(), a, chi.URLParam(r, "provider"), input.Revision); err != nil {
		failure(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Service) flowCookie(w http.ResponseWriter, provider, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: "lester_app_oauth_" + provider, Value: value, Path: "/api/v1/connectors/" + provider + "/callback", HttpOnly: true, Secure: s.options.SecureCookie, SameSite: http.SameSiteLaxMode, MaxAge: age})
}

var returnPattern = regexp.MustCompile(`^/app(?:/new|/p/[A-Za-z0-9_-]+(?:/overview)?|/c/[A-Za-z0-9_-]+)?(?:\?agent=[A-Za-z0-9_-]+)?$`)

func safeReturn(value string) string {
	if len(value) <= 512 && returnPattern.MatchString(value) {
		return value
	}
	return "/app"
}
func (s *Service) callbackURL(provider string) string {
	return s.options.WebOrigin + "/api/v1/connectors/" + provider + "/callback"
}
func (s *Service) begin(ctx context.Context, a Actor, name string, revision int64, back string) (string, string, error) {
	p, ok := s.providers[name]
	if !ok {
		return "", "", ErrUnavailable
	}
	u, err := url.Parse(s.options.WebOrigin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Scheme != "https" && (u.Scheme != "http" || u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") {
		return "", "", ErrUnavailable
	}
	if u.Scheme == "https" && !s.options.SecureCookie {
		return "", "", ErrUnavailable
	}
	state, err := randomToken()
	if err != nil {
		return "", "", err
	}
	browser, err := randomToken()
	if err != nil {
		return "", "", err
	}
	verifier := oauth2.GenerateVerifier()
	encrypted, nonce, err := s.seal(verifier, "app-flow:"+state)
	if err != nil {
		return "", "", err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(ctx)
	if len(a.SessionHash) != 32 {
		return "", "", ErrSession
	}
	if err = authorize(ctx, tx, a); err != nil {
		return "", "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO app_connections(workspace_id,user_id,provider) VALUES($1,$2,$3) ON CONFLICT(workspace_id,user_id,provider) DO NOTHING`, a.WorkspaceID, a.UserID, name)
	if err != nil {
		return "", "", err
	}
	var id uuid.UUID
	var current, generation int64
	err = tx.QueryRow(ctx, `SELECT id,revision,generation FROM app_connections WHERE workspace_id=$1 AND user_id=$2 AND provider=$3 FOR UPDATE`, a.WorkspaceID, a.UserID, name).Scan(&id, &current, &generation)
	if err != nil {
		return "", "", err
	}
	if current != revision {
		return "", "", ErrConflict
	}
	_, err = tx.Exec(ctx, `UPDATE app_connections SET generation=generation+1,revision=revision+1,updated_at=now() WHERE id=$1`, id)
	if err == nil {
		_, err = tx.Exec(ctx, `DELETE FROM app_connection_oauth_flows WHERE connection_id=$1`, id)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO app_connection_oauth_flows(state_hash,browser_hash,connection_id,session_hash,generation,return_to,verifier_ciphertext,verifier_nonce,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,now()+interval '10 minutes')`, digest(state), digest(browser), id, a.SessionHash, generation+1, safeReturn(back), encrypted, nonce)
	}
	if err != nil {
		return "", "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", "", err
	}
	authorization, err := url.Parse(p.AuthURL)
	if err != nil {
		return "", "", ErrUnavailable
	}
	query := authorization.Query()
	query.Set("client_id", p.ClientID)
	query.Set("redirect_uri", s.callbackURL(name))
	query.Set("response_type", "code")
	query.Set("state", state)
	if p.Name == "slack" {
		query.Set("user_scope", strings.Join(p.Scopes, ","))
	} else if len(p.Scopes) > 0 {
		query.Set("scope", strings.Join(p.Scopes, " "))
	}
	if p.PKCE {
		query.Set("code_challenge_method", "S256")
		query.Set("code_challenge", oauth2.S256ChallengeFromVerifier(verifier))
	}
	if p.Name == "gmail" {
		query.Set("access_type", "offline")
		query.Set("prompt", "consent")
		query.Set("include_granted_scopes", "false")
	}
	authorization.RawQuery = query.Encode()
	return authorization.String(), browser, nil
}

func (s *Service) redirect(w http.ResponseWriter, r *http.Request, provider, code, back string) {
	values := url.Values{"returnTo": {safeReturn(back)}}
	if code != "" {
		values.Set("connection_error", code)
	} else {
		values.Set("connected", provider)
	}
	http.Redirect(w, r, s.options.WebOrigin+"/app/settings/connectors?"+values.Encode(), http.StatusSeeOther)
}
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	s := h.Service
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	name := chi.URLParam(r, "provider")
	state := r.URL.Query().Get("state")
	cookie, err := r.Cookie("lester_app_oauth_" + name)
	if err != nil || len(state) != 43 || len(cookie.Value) != 43 {
		s.redirect(w, r, name, "invalid_state", "/app")
		return
	}
	p, ok := s.providers[name]
	if !ok {
		s.redirect(w, r, name, "unavailable", "/app")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	var id uuid.UUID
	var a Actor
	var generation int64
	var back string
	var ciphertext, nonce, hash []byte
	err = s.DB.QueryRow(ctx, `SELECT c.id,c.workspace_id,c.user_id,f.session_hash,f.generation,f.return_to,f.verifier_ciphertext,f.verifier_nonce,f.browser_hash FROM app_connection_oauth_flows f JOIN app_connections c ON c.id=f.connection_id WHERE f.state_hash=$1 AND c.provider=$2 AND f.expires_at>now()`, digest(state), name).Scan(&id, &a.WorkspaceID, &a.UserID, &a.SessionHash, &generation, &back, &ciphertext, &nonce, &hash)
	if err != nil || subtle.ConstantTimeCompare(hash, digest(cookie.Value)) != 1 {
		s.redirect(w, r, name, "invalid_state", "/app")
		return
	}
	if err = s.consumeFlow(ctx, r, a, id, state, name, generation, hash); err != nil {
		s.redirect(w, r, name, "session_expired", back)
		return
	}
	s.flowCookie(w, name, "", -1)
	if oauthError := r.URL.Query().Get("error"); oauthError != "" {
		outcome := "provider_error"
		if oauthError == "access_denied" {
			outcome = "access_denied"
		}
		s.redirect(w, r, name, outcome, back)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" || len(code) > 4096 {
		s.redirect(w, r, name, "provider_error", back)
		return
	}
	var verifier string
	if s.open(ciphertext, nonce, "app-flow:"+state, &verifier) != nil {
		s.redirect(w, r, name, "provider_error", back)
		return
	}
	t, err := s.exchange(ctx, p, code, verifier, "")
	if err != nil {
		s.redirect(w, r, name, "provider_error", back)
		return
	}
	identity, err := s.fetchIdentity(ctx, p, t.Access)
	if err != nil {
		s.redirect(w, r, name, "provider_error", back)
		return
	}
	err = s.saveGrant(ctx, r, a, id, name, generation, t, identity)
	if err != nil {
		s.redirect(w, r, name, "state_changed", back)
		return
	}
	s.redirect(w, r, name, "", back)
}
func currentBrowser(ctx context.Context, tx pgx.Tx, r *http.Request, a Actor) error {
	accessHash := auth.AccessCredentialHash(r)
	if len(accessHash) != 32 {
		return ErrSession
	}
	var family []byte
	err := tx.QueryRow(ctx, `SELECT s.token_hash FROM auth_access_tokens a JOIN sessions s ON s.id=a.session_id WHERE a.token_hash=$1 AND a.expires_at>now() AND s.user_id=$2 AND s.token_hash=$3 AND s.expires_at>now()`, accessHash, a.UserID, a.SessionHash).Scan(&family)
	if err != nil {
		return ErrSession
	}
	return nil
}
func (s *Service) consumeFlow(ctx context.Context, r *http.Request, a Actor, id uuid.UUID, state, name string, generation int64, browserHash []byte) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = authorize(ctx, tx, a); err != nil {
		return err
	}
	if err = currentBrowser(ctx, tx, r, a); err != nil {
		return err
	}
	var current int64
	if err = tx.QueryRow(ctx, `SELECT generation FROM app_connections WHERE id=$1 AND provider=$2 FOR UPDATE`, id, name).Scan(&current); err != nil || current != generation {
		return ErrConflict
	}
	result, err := tx.Exec(ctx, `DELETE FROM app_connection_oauth_flows WHERE state_hash=$1 AND browser_hash=$2 AND connection_id=$3 AND generation=$4 AND expires_at>now()`, digest(state), browserHash, id, generation)
	if err != nil || result.RowsAffected() != 1 {
		return ErrConflict
	}
	return tx.Commit(ctx)
}
func (s *Service) saveGrant(ctx context.Context, r *http.Request, a Actor, id uuid.UUID, name string, generation int64, t token, identity identity) error {
	ciphertext, nonce, err := s.seal(t, connectionAAD(id, a, name))
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = authorize(ctx, tx, a); err != nil {
		return err
	}
	if err = currentBrowser(ctx, tx, r, a); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE app_connections SET status='connected',refresh_pending=false,account_id=$4,account_name=$5,token_ciphertext=$6,token_nonce=$7,scopes=$8,expires_at=$9,revision=revision+1,updated_at=now() WHERE id=$1 AND workspace_id=$2 AND user_id=$3 AND generation=$10`, id, a.WorkspaceID, a.UserID, identity.ID, identity.Name, ciphertext, nonce, t.Scopes, expires(t), generation)
	if err != nil || result.RowsAffected() != 1 {
		return ErrConflict
	}
	return tx.Commit(ctx)
}
