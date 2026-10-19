package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func nativeCode(t *testing.T, s *Service, email string) (string, string) {
	t.Helper()
	cookie := registerAccount(t, s, email)
	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	response := request(t, s, s.NativeHandoff, "POST", "/auth/native/handoff", map[string]string{"challenge": challenge}, cookie, true)
	expectStatus(t, response, 201)
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || len(payload.Code) != 43 {
		t.Fatal("invalid handoff")
	}
	return payload.Code, verifier
}
func TestNativeSignInPKCEIsSingleUseAndKeepsTokensInHttpOnlyCookies(t *testing.T) {
	s := accountFixture(t)
	code, verifier := nativeCode(t, s, "native@example.test")
	if response := request(t, s, s.NativeExchange, "POST", "/auth/native/exchange", map[string]string{"code": code, "verifier": strings.Repeat("x", 43)}, nil, false); response.Code != 401 {
		t.Fatal("wrong verifier accepted")
	}
	response := request(t, s, s.NativeExchange, "POST", "/auth/native/exchange", map[string]string{"code": code, "verifier": verifier}, nil, false)
	expectStatus(t, response, 204)
	if response.Body.Len() != 0 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("credential response must not expose a body or be cacheable")
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatal("missing token pair")
	}
	for _, cookie := range cookies {
		if !cookie.HttpOnly {
			t.Fatal("readable credential")
		}
		if cookie.Name == refreshCookieName && cookie.Path != "/api/v1/auth" {
			t.Fatal("refresh path")
		}
	}
	if principal(t, s, sessionCookie(response)).Email != "native@example.test" {
		t.Fatal("wrong actor")
	}
	expectStatus(t, request(t, s, s.NativeExchange, "POST", "/auth/native/exchange", map[string]string{"code": code, "verifier": verifier}, nil, false), 401)
	var count int
	if err := s.db.QueryRow(context.Background(), `SELECT count(*) FROM native_signin_handoffs`).Scan(&count); err != nil || count != 0 {
		t.Fatal("consumed handoff retained", err)
	}
}
func TestNativeSignInConcurrentExchangeHasOneWinner(t *testing.T) {
	s := accountFixture(t)
	code, verifier := nativeCode(t, s, "race-native@example.test")
	var winners atomic.Int32
	var group sync.WaitGroup
	for range 5 {
		group.Go(func() {
			response := request(t, s, s.NativeExchange, "POST", "/auth/native/exchange", map[string]string{"code": code, "verifier": verifier}, nil, false)
			if response.Code == 204 {
				winners.Add(1)
			} else if response.Code != 401 {
				t.Errorf("unexpected exchange status %d", response.Code)
			}
		})
	}
	group.Wait()
	if winners.Load() != 1 {
		t.Fatal("duplicate sessions created", winners.Load())
	}
}
func TestNativeSignInRejectsExpiredRevokedDisabledAndUnverifiedSource(t *testing.T) {
	for _, change := range []string{"expired", "logout", "disabled", "unverified", "membership"} {
		t.Run(change, func(t *testing.T) {
			s := accountFixture(t)
			code, verifier := nativeCode(t, s, change+"-native@example.test")
			ctx := context.Background()
			query := map[string]string{
				"expired":    `UPDATE native_signin_handoffs SET expires_at=now()-interval '1 second'`,
				"logout":     `DELETE FROM sessions`,
				"disabled":   `UPDATE users SET disabled=true`,
				"unverified": `UPDATE users SET email_verification_required=true,email_verified=false`,
				"membership": `DELETE FROM workspace_members`,
			}[change]
			if _, err := s.db.Exec(ctx, query); err != nil {
				t.Fatal(err)
			}
			expectStatus(t, request(t, s, s.NativeExchange, "POST", "/auth/native/exchange", map[string]string{"code": code, "verifier": verifier}, nil, false), 401)
		})
	}
}
func TestNativeHandoffStoresOnlyHashesBoundsPendingAndRejectsUntrustedRedirects(t *testing.T) {
	s := accountFixture(t)
	cookie := registerAccount(t, s, "bound-native@example.test")
	challenge := strings.Repeat("a", 43)
	expectStatus(t, request(t, s, s.NativeHandoff, "POST", "/auth/native/handoff", map[string]string{"challenge": challenge, "redirect_uri": "https://evil.example"}, cookie, true), 400)
	for index := 0; index < 8; index++ {
		response := request(t, s, s.NativeHandoff, "POST", "/auth/native/handoff", map[string]string{"challenge": challenge}, cookie, true)
		expectStatus(t, response, 201)
		var body map[string]string
		_ = json.Unmarshal(response.Body.Bytes(), &body)
		var stored []byte
		if err := s.db.QueryRow(context.Background(), `SELECT token_hash FROM native_signin_handoffs ORDER BY created_at DESC LIMIT 1`).Scan(&stored); err != nil || len(stored) != 32 || string(stored) == body["code"] {
			t.Fatal("plaintext handoff persisted")
		}
	}
	expectStatus(t, request(t, s, s.NativeHandoff, "POST", "/auth/native/handoff", map[string]string{"challenge": challenge}, cookie, true), 429)
}
