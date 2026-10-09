package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
)

func accountFixture(t *testing.T) *Service {
	t.Helper()
	dsn := os.Getenv("LESTER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LESTER_TEST_DATABASE_URL for account integration tests")
	}
	ctx := context.Background()
	root, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = root.Exec(ctx, `BEGIN; SELECT pg_advisory_xact_lock(hashtextextended('lester:test:pgcrypto',0)); CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public; COMMIT;`); err != nil {
		t.Fatal(err)
	}
	schema := "account_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = root.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		root.Exec(ctx, `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`)
		root.Close()
	})
	files, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		data, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(ctx, string(data)); e != nil {
			t.Fatalf("%s: %v", file, e)
		}
	}
	s := New(db, nil, time.Hour, false)
	if err = s.Configure(Options{WebOrigin: "http://localhost:13000", RegistrationEnabled: true}); err != nil {
		t.Fatal(err)
	}
	return s
}
func request(t *testing.T, s *Service, handler http.HandlerFunc, method, path string, body any, cookie *http.Cookie, private bool) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	result := httptest.NewRecorder()
	if private {
		s.Middleware(handler).ServeHTTP(result, req)
	} else {
		handler.ServeHTTP(result, req)
	}
	return result
}
func expectStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status=%d, expected %d; body=%s", response.Code, status, response.Body.String())
	}
}
func sessionCookie(response *httptest.ResponseRecorder) *http.Cookie {
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "lester_session" {
			return cookie
		}
	}
	return nil
}
func registerAccount(t *testing.T, s *Service, email string) *http.Cookie {
	t.Helper()
	response := request(t, s, s.Register, "POST", "/auth/register", map[string]string{"email": email, "password": "test-password-123", "displayName": "账户测试"}, nil, false)
	expectStatus(t, response, 201)
	cookie := sessionCookie(response)
	if cookie == nil {
		t.Fatal("missing session")
	}
	return cookie
}
func principal(t *testing.T, s *Service, cookie *http.Cookie) Principal {
	t.Helper()
	response := request(t, s, s.Me, "GET", "/me", nil, cookie, true)
	expectStatus(t, response, 200)
	var p Principal
	if err := json.Unmarshal(response.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func activeSession(t *testing.T, s *Service, userID uuid.UUID) *http.Cookie {
	t.Helper()
	session, err := newSession(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err = insertSession(context.Background(), s.db, userID, session); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "lester_session", Value: base64.RawURLEncoding.EncodeToString(session.raw)}
}

type capturedMailer struct {
	messages []string
	fail     bool
}

func (m *capturedMailer) Send(_ context.Context, to, subject, body string) error {
	if m.fail {
		return errors.New("mail unavailable")
	}
	m.messages = append(m.messages, to+"\n"+subject+"\n"+body)
	return nil
}
func (m *capturedMailer) token(t *testing.T) string {
	t.Helper()
	if len(m.messages) == 0 {
		t.Fatal("no email sent")
	}
	matches := regexp.MustCompile(`#token=([A-Za-z0-9_-]{43})`).FindStringSubmatch(m.messages[len(m.messages)-1])
	if len(matches) != 2 {
		t.Fatal("invalid email link")
	}
	return matches[1]
}

func TestRegistrationVerificationAndRecovery(t *testing.T) {
	s := accountFixture(t)
	mailer := &capturedMailer{}
	s.options.Mailer = mailer
	response := request(t, s, s.Register, "POST", "/auth/register", map[string]string{"email": " New@Example.test ", "password": "test-password-123", "displayName": "新用户"}, nil, false)
	expectStatus(t, response, 202)
	if sessionCookie(response) != nil {
		t.Fatal("unverified registration created a login")
	}
	token := mailer.token(t)
	if strings.Contains(response.Body.String(), token) {
		t.Fatal("token exposed in API")
	}
	var userID uuid.UUID
	var count int
	s.db.QueryRow(context.Background(), `SELECT id FROM users WHERE email='new@example.test'`).Scan(&userID)
	s.db.QueryRow(context.Background(), `SELECT count(*) FROM projects p JOIN workspace_members wm ON wm.workspace_id=p.workspace_id WHERE wm.user_id=$1 AND p.is_default`, userID).Scan(&count)
	if count != 1 {
		t.Fatalf("default projects=%d", count)
	}
	login := func(password string) *httptest.ResponseRecorder {
		return request(t, s, s.Login, "POST", "/auth/login", map[string]string{"email": "new@example.test", "password": password}, nil, false)
	}
	expectStatus(t, login("test-password-123"), 403)
	expectStatus(t, request(t, s, s.Me, "GET", "/me", nil, activeSession(t, s, userID), true), 401)
	expectStatus(t, request(t, s, s.VerifyEmail, "POST", "/verify", map[string]string{"token": token}, nil, false), 204)
	expectStatus(t, request(t, s, s.VerifyEmail, "POST", "/verify", map[string]string{"token": token}, nil, false), 400)
	signedIn := login("test-password-123")
	expectStatus(t, signedIn, 204)
	cookie := sessionCookie(signedIn)
	if !principal(t, s, cookie).EmailVerified {
		t.Fatal("verification not persisted")
	}
	for _, email := range []string{"unknown@example.test", "new@example.test"} {
		r := request(t, s, s.ForgotPassword, "POST", "/forgot", map[string]string{"email": email}, nil, false)
		expectStatus(t, r, 202)
		if strings.Contains(r.Body.String(), email) {
			t.Fatal("account disclosed")
		}
	}
	resetToken := mailer.token(t)
	expectStatus(t, request(t, s, s.ForgotPassword, "POST", "/forgot", map[string]string{"email": "new@example.test"}, nil, false), 202)
	replacement := mailer.token(t)
	expectStatus(t, request(t, s, s.ResetPassword, "POST", "/reset", map[string]string{"token": resetToken, "password": "changed-password-456"}, nil, false), 400)
	expectStatus(t, request(t, s, s.ResetPassword, "POST", "/reset", map[string]string{"token": replacement, "password": "changed-password-456"}, nil, false), 204)
	expectStatus(t, request(t, s, s.Me, "GET", "/me", nil, cookie, true), 401)
	expectStatus(t, login("test-password-123"), 401)
	expectStatus(t, login("changed-password-456"), 204)
	expectStatus(t, request(t, s, s.ResetPassword, "POST", "/reset", map[string]string{"token": replacement, "password": "another-password-789"}, nil, false), 400)
	expectStatus(t, request(t, s, s.ForgotPassword, "POST", "/forgot", map[string]string{"email": "new@example.test"}, nil, false), 202)
	expired := mailer.token(t)
	s.db.Exec(context.Background(), `UPDATE auth_email_tokens SET expires_at=now()-interval '1 second'`)
	expectStatus(t, request(t, s, s.ResetPassword, "POST", "/reset", map[string]string{"token": expired, "password": "another-password-789"}, nil, false), 400)
	s.options.RegistrationEnabled = false
	expectStatus(t, request(t, s, s.Register, "POST", "/register", map[string]string{"email": "closed@example.test", "password": "test-password-123", "displayName": "关闭"}, nil, false), 403)
	expectStatus(t, login("changed-password-456"), 204)
}
func TestRegistrationValidationAndMailFailure(t *testing.T) {
	s := accountFixture(t)
	for _, input := range []map[string]string{
		{"email": "x@y@z", "password": "test-password-123", "displayName": "Valid"},
		{"email": "Name <user@example.test>", "password": "test-password-123", "displayName": "Valid"},
		{"email": "u@example.test", "password": strings.Repeat("x", 1025), "displayName": "Valid"},
		{"email": "u@example.test", "password": "test-password-123", "displayName": strings.Repeat("名", 61)},
	} {
		expectStatus(t, request(t, s, s.Register, "POST", "/register", input, nil, false), 400)
	}
	cookie := registerAccount(t, s, "legacy@example.test")
	p := principal(t, s, cookie)
	if p.EmailVerified || !p.HasPassword {
		t.Fatal("unexpected legacy status")
	}
	mailer := &capturedMailer{fail: true}
	s.options.Mailer = mailer
	response := request(t, s, s.Register, "POST", "/register", map[string]string{"email": "pending@example.test", "password": "test-password-123", "displayName": "Pending"}, nil, false)
	expectStatus(t, response, 503)
	if sessionCookie(response) != nil {
		t.Fatal("mail failure grants login")
	}
	// Legacy accounts are not suddenly locked out when SMTP is enabled.
	expectStatus(t, request(t, s, s.Login, "POST", "/login", map[string]string{"email": "legacy@example.test", "password": "test-password-123"}, nil, false), 204)
	mailer.fail = false
	expectStatus(t, request(t, s, s.ResendVerification, "POST", "/resend", map[string]string{"email": "pending@example.test"}, nil, false), 202)
	expectStatus(t, request(t, s, s.VerifyEmail, "POST", "/verify", map[string]string{"token": mailer.token(t)}, nil, false), 204)
}

func oauthRouter(s *Service) *chi.Mux {
	router := chi.NewRouter()
	router.Get("/api/v1/auth/oauth/{provider}/start", s.OAuthStart)
	router.Get("/api/v1/auth/oauth/{provider}/callback", s.OAuthCallback)
	router.With(s.Middleware).Post("/me/identities/{provider}/link", s.OAuthLink)
	router.With(s.Middleware).Delete("/me/identities/{provider}", s.UnlinkIdentity)
	return router
}
func startFlow(t *testing.T, s *Service, purpose string, session *http.Cookie) (url.Values, *http.Cookie) {
	t.Helper()
	method, path := "GET", "/api/v1/auth/oauth/google/start"
	if purpose == "link" {
		method, path = "POST", "/me/identities/google/link"
	}
	req := httptest.NewRequest(method, path, nil)
	if session != nil {
		req.AddCookie(session)
	}
	result := httptest.NewRecorder()
	oauthRouter(s).ServeHTTP(result, req)
	var authorization string
	if purpose == "link" {
		expectStatus(t, result, 200)
		var body map[string]string
		json.Unmarshal(result.Body.Bytes(), &body)
		authorization = body["url"]
	} else {
		expectStatus(t, result, 303)
		authorization = result.Header().Get("Location")
	}
	u, err := url.Parse(authorization)
	if err != nil {
		t.Fatal(err)
	}
	values := u.Query()
	if values.Get("code_challenge_method") != "S256" || values.Get("state") == "" || values.Get("code_challenge") == "" {
		t.Fatal("missing state/PKCE")
	}
	cookies := result.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/api/v1/auth/oauth/google" {
		t.Fatalf("invalid flow cookie: %#v", cookies)
	}
	return values, cookies[0]
}
func callback(s *Service, state string, flow, session *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/v1/auth/oauth/google/callback?state="+url.QueryEscape(state)+"&code=test-code", nil)
	if flow != nil {
		req.AddCookie(flow)
	}
	if session != nil {
		req.AddCookie(session)
	}
	result := httptest.NewRecorder()
	oauthRouter(s).ServeHTTP(result, req)
	return result
}
func TestOAuthPKCEBrowserBindingAndReplay(t *testing.T) {
	s := accountFixture(t)
	var expectedChallenge string
	var exchanges atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			r.ParseForm()
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != expectedChallenge || r.Form.Get("client_secret") != "test-secret" {
				http.Error(w, "bad verifier", 400)
				return
			}
			exchanges.Add(1)
			io.WriteString(w, `{"access_token":"test-access-token","token_type":"Bearer"}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-access-token" {
			http.Error(w, "bad token", 401)
			return
		}
		io.WriteString(w, `{"sub":"stable-google-id","email":"social@example.test","name":"Social User","email_verified":true}`)
	}))
	defer provider.Close()
	s.options.Providers = map[string]OAuthProvider{"google": {Config: oauth2.Config{ClientID: "test-id", ClientSecret: "test-secret", RedirectURL: "http://localhost:13000/api/v1/auth/oauth/google/callback", Endpoint: oauth2.Endpoint{AuthURL: provider.URL + "/authorize", TokenURL: provider.URL + "/token", AuthStyle: oauth2.AuthStyleInParams}}, UserInfoURL: provider.URL + "/user"}}
	values, flow := startFlow(t, s, "login", nil)
	expectedChallenge = values.Get("code_challenge")
	wrong := *flow
	wrong.Value = strings.Repeat("x", 43)
	response := callback(s, values.Get("state"), &wrong, nil)
	if !strings.Contains(response.Header().Get("Location"), "invalid_state") || exchanges.Load() != 0 {
		t.Fatal("cross-browser flow accepted")
	}
	response = callback(s, values.Get("state"), flow, nil)
	expectStatus(t, response, 303)
	if response.Header().Get("Location") != "http://localhost:13000/app" {
		t.Fatalf("login redirect=%s", response.Header().Get("Location"))
	}
	cookie := sessionCookie(response)
	if cookie == nil {
		t.Fatal("no OAuth session")
	}
	p := principal(t, s, cookie)
	if !p.EmailVerified || p.HasPassword || p.Role != "member" {
		t.Fatalf("wrong social profile: %#v", p)
	}
	replay := callback(s, values.Get("state"), flow, nil)
	if !strings.Contains(replay.Header().Get("Location"), "invalid_state") || exchanges.Load() != 1 {
		t.Fatal("callback replay accepted")
	}
	var count int
	s.db.QueryRow(context.Background(), `SELECT count(*) FROM projects WHERE workspace_id=$1 AND is_default`, p.WorkspaceID).Scan(&count)
	if count != 1 {
		t.Fatal("OAuth account missing default project")
	}
	values, flow = startFlow(t, s, "login", nil)
	expectedChallenge = values.Get("code_challenge")
	s.db.Exec(context.Background(), `UPDATE auth_oauth_flows SET expires_at=now()-interval '1 second'`)
	if result := callback(s, values.Get("state"), flow, nil); !strings.Contains(result.Header().Get("Location"), "invalid_state") {
		t.Fatal("expired flow accepted")
	}
}
func TestOAuthAccountIsolationAndSecurityChanges(t *testing.T) {
	s := accountFixture(t)
	ctx := context.Background()
	cookie := registerAccount(t, s, "owner@example.test")
	owner := principal(t, s, cookie)
	identity := providerIdentity{Subject: "first-google", Email: "owner@example.test", Name: "Provider Name"}
	if _, _, _, err := s.completeOAuth(ctx, oauthFlow{Provider: "google", Purpose: "login"}, identity); !errors.Is(err, errEmailConflict) {
		t.Fatalf("automatic email merge allowed: %v", err)
	}
	link := oauthFlow{Provider: "google", Purpose: "link", UserID: &owner.UserID, SessionHash: sessionDigest(httptest.NewRequest("GET", "/", nil))}
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)
	link.SessionHash = sessionDigest(req)
	if _, _, _, err := s.completeOAuth(ctx, link, identity); err != nil {
		t.Fatal(err)
	}
	if p := principal(t, s, cookie); p.DisplayName != owner.DisplayName || p.WorkspaceID != owner.WorkspaceID || !p.EmailVerified {
		t.Fatalf("link rewrote account: %#v", p)
	}
	if _, _, _, err := s.completeOAuth(ctx, link, providerIdentity{Subject: "second-google", Email: "different@example.test", Name: "Different"}); !errors.Is(err, errIdentityConflict) {
		t.Fatal("provider identity silently replaced")
	}
	otherCookie := registerAccount(t, s, "other@example.test")
	other := principal(t, s, otherCookie)
	req = httptest.NewRequest("GET", "/", nil)
	req.AddCookie(otherCookie)
	if _, _, _, err := s.completeOAuth(ctx, oauthFlow{Provider: "google", Purpose: "link", UserID: &other.UserID, SessionHash: sessionDigest(req)}, identity); !errors.Is(err, errIdentityConflict) {
		t.Fatal("identity moved to another user")
	}
	s.options.RegistrationEnabled = false
	if _, _, _, err := s.completeOAuth(ctx, oauthFlow{Provider: "google", Purpose: "login"}, providerIdentity{Subject: "new-google", Email: "new@example.test", Name: "New"}); !errors.Is(err, errRegistrationClosed) {
		t.Fatal("OAuth bypasses closed registration")
	}
	if id, created, _, err := s.completeOAuth(ctx, oauthFlow{Provider: "google", Purpose: "login"}, identity); err != nil || created || id != owner.UserID {
		t.Fatalf("existing OAuth login blocked: %v", err)
	}
	s.db.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, owner.UserID)
	if _, _, _, err := s.completeOAuth(ctx, link, identity); !errors.Is(err, errAccountUnavailable) {
		t.Fatal("revoked session can link identity")
	}
	s.db.Exec(ctx, `UPDATE users SET disabled=true WHERE id=$1`, owner.UserID)
	if _, _, _, err := s.completeOAuth(ctx, oauthFlow{Provider: "google", Purpose: "login"}, identity); !errors.Is(err, errAccountUnavailable) {
		t.Fatal("disabled OAuth account signed in")
	}
}
func TestConcurrentOAuthFirstLoginAndLastMethod(t *testing.T) {
	s := accountFixture(t)
	ctx := context.Background()
	identity := providerIdentity{Subject: "parallel-google", Email: "parallel@example.test", Name: "Parallel"}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	ids := make(chan uuid.UUID, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, _, _, err := s.completeOAuth(ctx, oauthFlow{Provider: "google", Purpose: "login"}, identity)
			errs <- err
			ids <- id
		}()
	}
	wg.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var id uuid.UUID
	for current := range ids {
		if id != uuid.Nil && id != current {
			t.Fatal("duplicate users")
		}
		id = current
	}
	cookie := activeSession(t, s, id)
	router := oauthRouter(s)
	unlink := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("DELETE", "/me/identities/google", nil)
		r.AddCookie(cookie)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, r)
		return response
	}
	expectStatus(t, unlink(), 409)
	response := request(t, s, s.ChangePassword, "PATCH", "/me/password", map[string]string{"password": "new-password-123"}, cookie, true)
	expectStatus(t, response, 204)
	cookie = sessionCookie(response)
	expectStatus(t, unlink(), 204)
	if _, _, _, err := s.completeOAuth(ctx, oauthFlow{Provider: "google", Purpose: "login"}, identity); !errors.Is(err, errEmailConflict) {
		t.Fatal("unlinked identity automatically reattached")
	}
	var count int
	s.db.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE id IN (SELECT workspace_id FROM workspace_members WHERE user_id=$1)`, id).Scan(&count)
	if count != 1 {
		t.Fatal("multiple personal workspaces")
	}
}
func TestPasswordChangeRevokesSessionsAndPendingTokens(t *testing.T) {
	s := accountFixture(t)
	cookie := registerAccount(t, s, "password@example.test")
	p := principal(t, s, cookie)
	other := activeSession(t, s, p.UserID)
	tx, err := s.db.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	token, err := issueEmailToken(context.Background(), tx, p.UserID, "reset")
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, s, s.ChangePassword, "PATCH", "/me/password", map[string]string{"current_password": "wrong-password", "password": "replacement-password"}, cookie, true), 400)
	response := request(t, s, s.ChangePassword, "PATCH", "/me/password", map[string]string{"current_password": "test-password-123", "password": "replacement-password"}, cookie, true)
	expectStatus(t, response, 204)
	expectStatus(t, request(t, s, s.Me, "GET", "/me", nil, other, true), 401)
	expectStatus(t, request(t, s, s.Me, "GET", "/me", nil, cookie, true), 401)
	principal(t, s, sessionCookie(response))
	expectStatus(t, request(t, s, s.ResetPassword, "POST", "/reset", map[string]string{"token": token, "password": "malicious-replacement"}, nil, false), 400)
}

type avatarStore struct {
	objects map[string][]byte
	fail    bool
}

func (s *avatarStore) Ensure(context.Context) error { return nil }
func (s *avatarStore) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	if s.fail {
		return errors.New("storage unavailable")
	}
	data, err := io.ReadAll(r)
	s.objects[key] = data
	return err
}
func (s *avatarStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, errors.New("missing")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
func (s *avatarStore) Delete(_ context.Context, key string) error { delete(s.objects, key); return nil }
func TestAvatarUploadPrivacyFallbackAndRollback(t *testing.T) {
	s := accountFixture(t)
	store := &avatarStore{objects: map[string][]byte{}}
	s.options.Avatars = store
	cookie := registerAccount(t, s, "avatar@example.test")
	photo := image.NewNRGBA(image.Rect(0, 0, 600, 350))
	photo.Set(300, 175, color.NRGBA{R: 250, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, photo); err != nil {
		t.Fatal(err)
	}
	upload := func(data []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		file, _ := writer.CreateFormFile("avatar", "photo.png")
		file.Write(data)
		writer.Close()
		r := httptest.NewRequest("POST", "/me/avatar", &body)
		r.Header.Set("Content-Type", writer.FormDataContentType())
		r.AddCookie(cookie)
		result := httptest.NewRecorder()
		s.Middleware(http.HandlerFunc(s.UploadAvatar)).ServeHTTP(result, r)
		return result
	}
	expectStatus(t, upload([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)), 400)
	expectStatus(t, upload(bytes.Repeat([]byte("x"), maxAvatarBytes+1)), 400)
	expectStatus(t, upload(encoded.Bytes()), 200)
	p := principal(t, s, cookie)
	if !strings.HasPrefix(p.AvatarURL, "/api/v1/me/avatar?v=") {
		t.Fatal("avatar URL not private")
	}
	response := request(t, s, s.Avatar, "GET", p.AvatarURL, nil, cookie, true)
	expectStatus(t, response, 200)
	config, format, err := image.DecodeConfig(response.Body)
	if err != nil || config.Width != 256 || config.Height != 256 || format != "png" {
		t.Fatalf("unsafe avatar: %#v %s %v", config, format, err)
	}
	if response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("avatar safety headers missing")
	}
	expectStatus(t, request(t, s, s.Avatar, "GET", p.AvatarURL, nil, nil, true), 401)
	other := registerAccount(t, s, "avatar-other@example.test")
	expectStatus(t, request(t, s, s.Avatar, "GET", p.AvatarURL, nil, other, true), 404)
	store.fail = true
	expectStatus(t, upload(encoded.Bytes()), 500)
	if principal(t, s, cookie).AvatarURL != p.AvatarURL {
		t.Fatal("failed upload loses old avatar")
	}
	store.fail = false
	expectStatus(t, upload(encoded.Bytes()), 200)
	if len(store.objects) != 1 {
		t.Fatal("old avatar not deleted")
	}
	response = request(t, s, s.UpdateProfile, "PATCH", "/me", map[string]any{"display_name": "New Name", "avatar_key": "ocean", "use_default_avatar": true}, cookie, true)
	expectStatus(t, response, 200)
	if principal(t, s, cookie).AvatarURL != "" || len(store.objects) != 0 {
		t.Fatal("built-in theme did not clear photo")
	}
	expectStatus(t, upload(encoded.Bytes()), 200)
	expectStatus(t, request(t, s, s.RemoveAvatar, "DELETE", "/avatar", nil, cookie, true), 200)
	if principal(t, s, cookie).AvatarURL != "" || len(store.objects) != 0 {
		t.Fatal("remove avatar left storage reference")
	}
}
func TestAccountMigrationRollbackProtectsSocialAccounts(t *testing.T) {
	s := accountFixture(t)
	ctx := context.Background()
	_, _, _, err := s.completeOAuth(ctx, oauthFlow{Provider: "google", Purpose: "login"}, providerIdentity{Subject: "rollback-google", Email: "rollback@example.test", Name: "Rollback"})
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/000013_account_identity.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(ctx, string(down)); err == nil {
		t.Fatal("rollback strands identity-only account")
	}
	var count int
	s.db.QueryRow(ctx, `SELECT count(*) FROM auth_identities`).Scan(&count)
	if count != 1 {
		t.Fatal("failed rollback damaged identities")
	}
	s.db.Exec(ctx, `UPDATE users SET password_hash='test-hash' WHERE password_hash IS NULL`)
	if _, err = s.db.Exec(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	up, _ := os.ReadFile("../../migrations/000013_account_identity.up.sql")
	if _, err = s.db.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
}
