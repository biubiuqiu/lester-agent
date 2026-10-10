package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

func responseCookie(t *testing.T, r *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range r.Result().Cookies() {
		if c.Name == name && c.Value != "" {
			return c
		}
	}
	t.Fatalf("missing %s", name)
	return nil
}
func tokenPair(t *testing.T, s *Service, email string) (*http.Cookie, *http.Cookie) {
	t.Helper()
	r := request(t, s, s.Register, "POST", "/api/v1/auth/register", map[string]string{"email": email, "password": "token-test-password", "displayName": "令牌测试"}, nil, false)
	expectStatus(t, r, 201)
	return responseCookie(t, r, accessCookieName), responseCookie(t, r, refreshCookieName)
}
func tokenRequestDigest(c *http.Cookie) []byte {
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(c)
	return cookieDigest(r, c.Name)
}

func TestAccessExpiryAndIndefiniteRollingRefresh(t *testing.T) {
	s := accountFixture(t)
	access, refresh := tokenPair(t, s, "rolling@example.test")
	if access.Path != "/" || refresh.Path != "/api/v1/auth" || !access.HttpOnly || !refresh.HttpOnly || access.Value == refresh.Value {
		t.Fatal("credential isolation lost")
	}
	if d := time.Until(access.Expires); d < 2*time.Hour-5*time.Second || d > 2*time.Hour {
		t.Fatalf("access lifetime=%v", d)
	}
	if d := time.Until(refresh.Expires); d < refreshLifetime-5*time.Second || d > refreshLifetime {
		t.Fatalf("refresh lifetime=%v", d)
	}
	owner := principal(t, s, access)
	// Expiry rejects the request before any mutation executes.
	_, err := s.db.Exec(context.Background(), `UPDATE auth_access_tokens SET expires_at=now()-interval '1 second' WHERE token_hash=$1`, tokenRequestDigest(access))
	if err != nil {
		t.Fatal(err)
	}
	called := false
	denied := request(t, s, func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) }, "POST", "/private", nil, access, true)
	expectStatus(t, denied, 401)
	if called || !json.Valid(denied.Body.Bytes()) {
		t.Fatal("expired access reached a protected handler")
	}
	// Each iteration has a short remaining refresh lifetime, even if the family
	// was originally created years ago. There is deliberately no absolute cutoff.
	for i := 0; i < 4; i++ {
		_, err = s.db.Exec(context.Background(), `UPDATE sessions SET expires_at=now()+interval '1 day',created_at=now()-interval '2 years' WHERE user_id=$1;`, owner.UserID)
		if err != nil {
			t.Fatal(err)
		}
		refreshed := request(t, s, s.Refresh, "POST", "/api/v1/auth/refresh", nil, refresh, false)
		expectStatus(t, refreshed, 200)
		nextAccess, nextRefresh := responseCookie(t, refreshed, accessCookieName), responseCookie(t, refreshed, refreshCookieName)
		if nextAccess.Value == access.Value || nextRefresh.Value == refresh.Value {
			t.Fatal("tokens did not rotate")
		}
		if time.Until(nextRefresh.Expires) < refreshLifetime-5*time.Second {
			t.Fatal("refresh did not extend to 30 days")
		}
		principal(t, s, nextAccess)
		status := request(t, s, s.SessionStatus, "GET", "/api/v1/auth/session", nil, nextAccess, true)
		expectStatus(t, status, 200)
		if status.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("credential status cached")
		}
		access, refresh = nextAccess, nextRefresh
	}
}

func TestLiveRequestsCloseWhenSessionOrAccountIsRevoked(t *testing.T) {
	for _, action := range []string{"logout", "disable", "membership"} {
		t.Run(action, func(t *testing.T) {
			s := accountFixture(t)
			access, refresh := tokenPair(t, s, "live@example.test")
			owner := principal(t, s, access)
			// The public principal JSON intentionally omits the stable family hash.
			owner.SessionHash = tokenRequestDigest(access)
			ctx, cancel := s.guardLongRequest(context.Background(), owner, 10*time.Millisecond)
			defer cancel()
			// Rotating access/refresh must not revoke an established stream.
			renewed := request(t, s, s.Refresh, "POST", "/api/v1/auth/refresh", nil, refresh, false)
			expectStatus(t, renewed, 200)
			select {
			case <-ctx.Done():
				t.Fatal("rolling refresh closed a valid connection")
			case <-time.After(50 * time.Millisecond):
			}
			var err error
			switch action {
			case "logout":
				response := request(t, s, s.Logout, "POST", "/api/v1/auth/logout", nil, responseCookie(t, renewed, accessCookieName), false)
				expectStatus(t, response, 204)
			case "disable":
				_, err = s.db.Exec(context.Background(), `UPDATE users SET disabled=true WHERE id=$1`, owner.UserID)
			case "membership":
				_, err = s.db.Exec(context.Background(), `DELETE FROM workspace_members WHERE user_id=$1`, owner.UserID)
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
				t.Fatal("revoked connection remained active")
			}
		})
	}
}

func TestConcurrentRefreshGraceAndReplayRevokesOnlyItsFamily(t *testing.T) {
	s := accountFixture(t)
	access, refresh := tokenPair(t, s, "replay@example.test")
	other := activeSession(t, s, principal(t, s, access).UserID)
	var wg sync.WaitGroup
	responses := make([]*httptest.ResponseRecorder, 2)
	for i := range responses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			responses[i] = request(t, s, s.Refresh, "POST", "/api/v1/auth/refresh", nil, refresh, false)
		}(i)
	}
	wg.Wait()
	var winner *httptest.ResponseRecorder
	for _, r := range responses {
		if r.Code == 200 {
			if winner != nil {
				t.Fatal("refresh issued twice")
			}
			winner = r
		} else {
			expectStatus(t, r, 409)
			if len(r.Result().Cookies()) != 0 {
				t.Fatal("race response overwrote rotated cookies")
			}
		}
	}
	if winner == nil {
		t.Fatal("no refresh winner")
	}
	nextAccess := responseCookie(t, winner, accessCookieName)
	nextRefresh := responseCookie(t, winner, refreshCookieName)
	// An ordinary rotation does not revoke an unexpired access or break links.
	principal(t, s, access)
	principal(t, s, nextAccess)
	_, err := s.db.Exec(context.Background(), `UPDATE auth_refresh_tokens SET consumed_at=now()-interval '6 seconds' WHERE token_hash=$1`, tokenRequestDigest(refresh))
	if err != nil {
		t.Fatal(err)
	}
	replay := request(t, s, s.Refresh, "POST", "/api/v1/auth/refresh", nil, refresh, false)
	expectStatus(t, replay, 401)
	expectStatus(t, request(t, s, s.Me, "GET", "/me", nil, nextAccess, true), 401)
	expectStatus(t, request(t, s, s.Refresh, "POST", "/api/v1/auth/refresh", nil, nextRefresh, false), 401)
	principal(t, s, other)
	var tokens int
	if err = s.db.QueryRow(context.Background(), `SELECT count(*) FROM auth_refresh_tokens`).Scan(&tokens); err != nil || tokens != 1 {
		t.Fatalf("family cascade: %d %v", tokens, err)
	}
}

func TestLogoutWithExpiredAccessAndRevokedRefresh(t *testing.T) {
	s := accountFixture(t)
	access, refresh := tokenPair(t, s, "logout@example.test")
	_, err := s.db.Exec(context.Background(), `UPDATE auth_access_tokens SET expires_at=now()-interval '1 second' WHERE token_hash=$1`, tokenRequestDigest(access))
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, s, s.Logout, "POST", "/api/v1/auth/logout", nil, refresh, false), 204)
	expectStatus(t, request(t, s, s.Refresh, "POST", "/api/v1/auth/refresh", nil, refresh, false), 401)
	var tokens int
	if err = s.db.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM auth_access_tokens)+(SELECT count(*) FROM auth_refresh_tokens)`).Scan(&tokens); err != nil || tokens != 0 {
		t.Fatalf("logout tokens: %d %v", tokens, err)
	}
	_, refresh = tokenPair(t, s, "disabled@example.test")
	if _, err = s.db.Exec(context.Background(), `UPDATE users SET disabled=true WHERE email='disabled@example.test'`); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, s, s.Refresh, "POST", "/api/v1/auth/refresh", nil, refresh, false), 401)
}

func TestRefreshKeepsStableOAuthLinkSessionAndOriginProtection(t *testing.T) {
	s := accountFixture(t)
	access, refresh := tokenPair(t, s, "link-refresh@example.test")
	var before, after []byte
	handler := func(w http.ResponseWriter, r *http.Request) { before = sessionDigest(r); w.WriteHeader(204) }
	expectStatus(t, request(t, s, handler, "GET", "/private", nil, access, true), 204)
	foreign := httptest.NewRequest("POST", "http://localhost:13000/api/v1/auth/refresh", nil)
	foreign.Header.Set("Origin", "http://evil.localhost:13000")
	foreign.AddCookie(refresh)
	rejected := httptest.NewRecorder()
	MutationOrigin("http://localhost:13000")(http.HandlerFunc(s.Refresh)).ServeHTTP(rejected, foreign)
	expectStatus(t, rejected, 403)
	response := request(t, s, s.Refresh, "POST", "/api/v1/auth/refresh", nil, refresh, false)
	expectStatus(t, response, 200)
	handler = func(w http.ResponseWriter, r *http.Request) { after = sessionDigest(r); w.WriteHeader(204) }
	expectStatus(t, request(t, s, handler, "GET", "/private", nil, responseCookie(t, response, accessCookieName), true), 204)
	browserRequest := httptest.NewRequest("GET", "/callback", nil)
	browserRequest.AddCookie(responseCookie(t, response, accessCookieName))
	browserHash, err := s.browserSessionDigest(browserRequest)
	if err != nil || string(browserHash) != string(before) {
		t.Fatalf("callback family changed after refresh: %v", err)
	}
	browserRequest = httptest.NewRequest("GET", "/callback", nil)
	browserRequest.AddCookie(responseCookie(t, response, refreshCookieName))
	browserHash, err = s.browserSessionDigest(browserRequest)
	if err != nil || string(browserHash) != string(before) {
		t.Fatalf("callback lost refresh-only session: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("access rotation broke OAuth link session binding")
	}
}

func TestTokenMigrationRevokesLegacyAndRollbackCredentialsOnly(t *testing.T) {
	s := accountFixture(t)
	access, _ := tokenPair(t, s, "migration@example.test")
	user := principal(t, s, access)
	for _, name := range []string{"000015_rotating_tokens.down.sql", "000015_rotating_tokens.up.sql"} {
		sql, err := os.ReadFile("../../migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.db.Exec(context.Background(), string(sql)); err != nil {
			t.Fatal(err)
		}
		if name == "000015_rotating_tokens.down.sql" {
			// Emulate a real legacy deployment between rollback and upgrade.
			if _, err = s.db.Exec(context.Background(), `INSERT INTO sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '30 days')`, user.UserID, tokenRequestDigest(access)); err != nil {
				t.Fatal(err)
			}
		}
	}
	var families int
	if err := s.db.QueryRow(context.Background(), `SELECT count(*) FROM sessions`).Scan(&families); err != nil || families != 0 {
		t.Fatalf("legacy credential survived upgrade: %d %v", families, err)
	}
	legacy := &http.Cookie{Name: "lester_session", Value: access.Value}
	expectStatus(t, request(t, s, s.Me, "GET", "/me", nil, legacy, true), 401)
	expectStatus(t, request(t, s, s.Me, "GET", "/me", nil, access, true), 401)
	var workspaces int
	if err := s.db.QueryRow(context.Background(), `SELECT count(*) FROM workspace_members WHERE user_id=$1`, user.UserID).Scan(&workspaces); err != nil || workspaces != 1 {
		t.Fatalf("migration lost account: %d %v", workspaces, err)
	}
}

func TestPasswordChangeRotatesBothTokensAndRevokesOtherDevices(t *testing.T) {
	s := accountFixture(t)
	access, refresh := tokenPair(t, s, "password-token@example.test")
	login := request(t, s, s.Login, "POST", "/api/v1/auth/login", map[string]string{"email": "password-token@example.test", "password": "token-test-password"}, nil, false)
	expectStatus(t, login, 204)
	otherRefresh := responseCookie(t, login, refreshCookieName)
	changed := request(t, s, s.ChangePassword, "POST", "/me/password", map[string]string{"current_password": "token-test-password", "password": "new-token-password"}, access, true)
	expectStatus(t, changed, 204)
	nextAccess, nextRefresh := responseCookie(t, changed, accessCookieName), responseCookie(t, changed, refreshCookieName)
	if nextAccess.Value == access.Value || nextRefresh.Value == refresh.Value {
		t.Fatal("password change did not rotate both credentials")
	}
	expectStatus(t, request(t, s, s.Refresh, "POST", "/api/v1/auth/refresh", nil, refresh, false), 401)
	expectStatus(t, request(t, s, s.Refresh, "POST", "/api/v1/auth/refresh", nil, otherRefresh, false), 401)
	expectStatus(t, request(t, s, s.Refresh, "POST", "/api/v1/auth/refresh", nil, nextRefresh, false), 200)
}
