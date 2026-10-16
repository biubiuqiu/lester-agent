package automation

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/go-chi/chi/v5"
)

func TestWebhookHTTPRequiresHeaderCredentialBoundedJSONAndStableEventIdentity(t *testing.T) {
	f := newFixture(t)
	d, secret := f.save(t, "webhook")
	router := chi.NewRouter()
	router.Use(auth.MutationOrigin("https://lester.example"))
	router.Post("/hooks/{id}", (&Handler{Service: f.service}).Webhook)
	send := func(token, key, body, origin string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("POST", "/hooks/"+d.ID.String(), strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-Lester-Event-ID", key)
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	if result := send("wrong", "key", `{"event":1}`, ""); result.Code != 401 {
		t.Fatal(result.Code, result.Body.String())
	}
	first := send(secret, "event-1", `{"issue": 123, "data": "中文"}`, "")
	second := send(secret, "event-1", `{"data":"中文","issue":123}`, "")
	var a, b Delivery
	if first.Code != 202 || second.Code != 202 {
		t.Fatal(first.Code, second.Code, first.Body.String())
	}
	_ = json.Unmarshal(first.Body.Bytes(), &a)
	_ = json.Unmarshal(second.Body.Bytes(), &b)
	if a.ID != b.ID {
		t.Fatal("JSON formatting caused a duplicate")
	}
	if strings.Contains(first.Body.String(), secret) || first.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("secret response/cache")
	}
	for _, body := range []string{`[]`, `null`, `{"x":1} {"x":2}`, `{"x":"` + strings.Repeat("a", 33000) + `"}`, `not-json`} {
		if result := send(secret, "bad-body", body, ""); result.Code != 400 {
			t.Fatalf("accepted invalid body %d", result.Code)
		}
	}
	if result := send(secret, "", `{"x":1}`, ""); result.Code != 400 {
		t.Fatal("event identity not required")
	}
	if result := send(secret, "foreign-origin", `{"x":1}`, "https://foreign.example"); result.Code != 403 {
		t.Fatal("cross-site browser request accepted")
	}
	request := httptest.NewRequest("POST", "/hooks/"+d.ID.String()+"?token="+secret, strings.NewReader(`{}`))
	request.Header.Set("Cookie", "lester_access_token=fixture")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 401 {
		t.Fatal("cookie/query authenticated webhook")
	}
}
