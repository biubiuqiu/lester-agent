package conversation

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestPrivateHTMLBytesCannotExecuteAtApplicationOrigin(t *testing.T) {
	f := newTranscriptFixture(t, false)
	applyTestMigration(t, f.service.db, "000005_user_profiles.up.sql")
	applyTestMigration(t, f.service.db, "000013_account_identity.up.sql")
	applyTestMigration(t, f.service.db, "000015_rotating_tokens.up.sql")
	content := "<!doctype html><script>localStorage.getItem('private-data')</script>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/files/content"):
			_, _ = io.WriteString(w, content)
		case strings.HasSuffix(r.URL.Path, "/exec"):
			_, _ = io.WriteString(w, `{"stdout":"","stderr":"","exit_code":0}`)
		default:
			_, _ = io.WriteString(w, `{"status":"running","provider":"docker","provider_ref":"test-computer"}`)
		}
	}))
	defer server.Close()
	f.service.sandboxes = sandbox.NewClient(server.URL, "")
	rawArray := sha256.Sum256([]byte(uuid.NewString()))
	raw := rawArray[:]
	digest := sha256.Sum256(raw)
	if _, err := f.service.db.Exec(context.Background(), `WITH family AS (INSERT INTO sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '30 days') RETURNING id) INSERT INTO auth_access_tokens(token_hash,session_id,expires_at) SELECT $2,id,now()+interval '2 hours' FROM family`, f.userID, digest[:]); err != nil {
		t.Fatal(err)
	}
	handler := &Handler{service: f.service, sandboxes: f.service.sandboxes}
	router := chi.NewRouter()
	router.Use(auth.New(f.service.db, nil, time.Hour, false).Middleware)
	router.Get("/conversations/{id}/files/content", handler.ReadFile)
	router.Get("/conversations/{id}/preview/*", handler.PreviewFile)
	call := func(suffix string, authenticated bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest("GET", "/conversations/"+f.conversationID.String()+suffix, nil)
		if authenticated {
			request.AddCookie(&http.Cookie{Name: "lester_access_token", Value: base64.RawURLEncoding.EncodeToString(raw)})
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	response := call("/files/content?path=index.html", true)
	if response.Code != 200 || response.Body.String() != content {
		t.Fatalf("file byte transport changed: %d %s", response.Code, response.Body)
	}
	if response.Header().Get("Content-Type") != "application/octet-stream" || response.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(response.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("raw HTML can execute in the application origin: %v", response.Header())
	}
	preview := call("/preview/index.html", true)
	if preview.Code != 200 || !strings.HasPrefix(preview.Header().Get("Content-Type"), "text/html") || !strings.HasPrefix(preview.Header().Get("Content-Security-Policy"), "sandbox allow-scripts;") {
		t.Fatalf("rendered preview lost its isolated origin: %d %v", preview.Code, preview.Header())
	}
	if unauthenticated := call("/files/content?path=index.html", false); unauthenticated.Code != 401 {
		t.Fatalf("private file exposed without authentication: %d", unauthenticated.Code)
	}
}
