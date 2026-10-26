package connector

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestProtocolRejectsRedirectsOversizedAndProviderSecrets(t *testing.T) {
	var mode string
	leaked := 0
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked++; w.Write([]byte(`{}`)) }))
	defer receiver.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "redirect":
			http.Redirect(w, r, receiver.URL, 307)
		case "oversize":
			w.Write([]byte(strings.Repeat("x", maxResponseBytes+1)))
		case "invalid":
			w.Write([]byte(`{} {}`))
		case "denied":
			w.WriteHeader(403)
			w.Write([]byte(`{"error":"secret-provider-body"}`))
		case "revoked":
			w.Write([]byte(`{"ok":false,"error":"token_revoked"}`))
		case "bot":
			w.Write([]byte(`{"ok":true,"access_token":"bot-secret-credential","token_type":"bot","scope":"search:read"}`))
		case "scope":
			w.Write([]byte(`{"access_token":"provider-token","scope":"` + strings.Repeat("s ", 65) + `"}`))
		}
	}))
	defer server.Close()
	s, err := New(nil, Options{MasterKey: []byte(strings.Repeat("k", 32)), HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		mode string
		want error
	}{{"redirect", ErrProvider}, {"oversize", ErrResponse}, {"invalid", ErrResponse}, {"denied", ErrPermission}, {"revoked", ErrReauthorize}} {
		mode = tc.mode
		_, err = s.request(context.Background(), "GET", server.URL, "secret-bearer", nil, "", nil)
		if !errors.Is(err, tc.want) || strings.Contains(err.Error(), "secret-") {
			t.Fatalf("%s: %v", mode, err)
		}
	}
	if leaked != 0 {
		t.Fatal("followed a provider redirect with a credential")
	}
	mode = "bot"
	if _, err = s.exchange(context.Background(), Provider{Name: "slack", TokenURL: server.URL}, "code", "", ""); err == nil {
		t.Fatal("accepted bot credentials as user authorization")
	}
	mode = "scope"
	if _, err = s.exchange(context.Background(), Provider{Name: "github", TokenURL: server.URL}, "code", "", ""); !errors.Is(err, ErrResponse) {
		t.Fatal("unbounded scopes", err)
	}
}
func TestEncryptedGrantAndMigrationRollbackBoundaries(t *testing.T) {
	f := newFixture(t)
	f.connect(t, "gmail")
	var encrypted, nonce []byte
	if err := f.s.DB.QueryRow(context.Background(), `SELECT token_ciphertext,token_nonce FROM app_connections WHERE provider='gmail'`).Scan(&encrypted, &nonce); err != nil {
		t.Fatal(err)
	}
	var saved token
	if f.s.open(encrypted, nonce, "app-connection:another-row", &saved) == nil {
		t.Fatal("accepted swapped grant")
	}
	sql, err := os.ReadFile("../../migrations/000032_app_connections.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.DB.Exec(context.Background(), string(sql)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = f.s.DB.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE id=$1`, f.actor.UserID).Scan(&count); err != nil || count != 1 {
		t.Fatal("rollback removed owner", err)
	}
	if err = f.s.DB.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE token_hash=$1`, f.actor.SessionHash).Scan(&count); err != nil || count != 1 {
		t.Fatal("rollback removed session", err)
	}
}
func TestUncertainRefreshIsNeverReplayed(t *testing.T) {
	f := newFixture(t)
	f.connect(t, "github")
	f.expireToken(t, "github")
	failed := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { failed++; w.Write([]byte(`{"access_token":`)) }))
	defer server.Close()
	p := f.s.providers["github"]
	p.TokenURL = server.URL
	f.s.providers["github"] = p
	for i := 0; i < 2; i++ {
		if _, err := f.s.Read(context.Background(), f.actor, ReadRequest{Provider: "github", Action: "search", Query: "project"}); !errors.Is(err, ErrReauthorize) {
			t.Fatal("uncertain refresh must require reconnect", err)
		}
	}
	if failed != 1 {
		t.Fatal("replayed a possibly consumed refresh token")
	}
	var count int
	if err := f.s.DB.QueryRow(context.Background(), `SELECT count(*) FROM app_connections WHERE token_ciphertext IS NOT NULL`).Scan(&count); err != nil || count != 0 {
		t.Fatal("retained stale credentials", err)
	}
}
func TestMailBoundsAndAttachmentExclusion(t *testing.T) {
	item, truncated := mailItem(map[string]any{"payload": map[string]any{"mimeType": "text/plain", "body": map[string]any{"attachmentId": "body-not-fetched"}}}, true)
	if !truncated || item.Text != "" {
		t.Fatal("unfetched body represented as a full read")
	}
	item, truncated = mailItem(map[string]any{"payload": map[string]any{"filename": "secret.txt", "mimeType": "text/plain", "body": map[string]any{"data": "c2VjcmV0"}}}, true)
	if truncated || item.Text != "" {
		t.Fatal("read an attachment as body")
	}
}

func TestUnauthorizedReadInvalidatesButPermissionFailureRetainsGrant(t *testing.T) {
	for _, status := range []int{401, 403} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := newFixture(t)
			f.connect(t, "github")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer server.Close()
			p := f.s.providers["github"]
			p.APIURL = server.URL
			f.s.providers["github"] = p
			want := ErrPermission
			if status == 401 {
				want = ErrReauthorize
			}
			if _, err := f.s.Read(context.Background(), f.actor, ReadRequest{Provider: "github", Action: "search", Query: "project"}); !errors.Is(err, want) {
				t.Fatal(err)
			}
			entries, err := f.s.List(context.Background(), f.actor)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.Provider == "github" {
					expected := "connected"
					if status == 401 {
						expected = "reauthorization_required"
					}
					if entry.Status != expected {
						t.Fatal("wrong credential status", entry.Status)
					}
				}
			}
		})
	}
}

func TestRefreshIntentSurvivesCrashAndCancellation(t *testing.T) {
	t.Run("durable unfinished intent", func(t *testing.T) {
		f := newFixture(t)
		f.connect(t, "github")
		f.expireToken(t, "github")
		if _, err := f.s.DB.Exec(context.Background(), `UPDATE app_connections SET refresh_pending=true WHERE provider='github'`); err != nil {
			t.Fatal(err)
		}
		before := f.tokens.Load()
		if _, err := f.s.Read(context.Background(), f.actor, ReadRequest{Provider: "github", Action: "search", Query: "project"}); !errors.Is(err, ErrReauthorize) {
			t.Fatal(err)
		}
		if f.tokens.Load() != before {
			t.Fatal("replayed refresh after an unfinished durable intent")
		}
	})
	t.Run("canceled after token consumption", func(t *testing.T) {
		f := newFixture(t)
		f.connect(t, "github")
		f.expireToken(t, "github")
		started := make(chan struct{})
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-release
			w.Write([]byte(`{"access_token":"consumed-new-access","refresh_token":"consumed-new-refresh","expires_in":3600}`))
		}))
		defer server.Close()
		p := f.s.providers["github"]
		p.TokenURL = server.URL
		f.s.providers["github"] = p
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, err := f.s.Read(ctx, f.actor, ReadRequest{Provider: "github", Action: "search", Query: "project"})
			done <- err
		}()
		<-started
		cancel()
		close(release)
		if err := <-done; err == nil {
			t.Fatal("canceled read claimed success")
		}
		if _, err := f.s.Read(context.Background(), f.actor, ReadRequest{Provider: "github", Action: "search", Query: "project"}); !errors.Is(err, ErrReauthorize) {
			t.Fatal("cancellation retained a replayable refresh grant", err)
		}
	})
}

func TestRepositorySearchReadsRepositoryMetadataWithoutInventingAReadme(t *testing.T) {
	f := newFixture(t)
	f.connect(t, "github")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search/repositories":
			w.Write([]byte(`{"total_count":1,"items":[{"id":1,"full_name":"acme/project","description":"Real repository"}]}`))
		case "/repos/acme/project":
			w.Write([]byte(`{"id":1,"full_name":"acme/project","description":"Real repository"}`))
		default:
			t.Error("invented resource path", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	p := f.s.providers["github"]
	p.APIURL = server.URL
	f.s.providers["github"] = p
	result, err := f.s.Read(context.Background(), f.actor, ReadRequest{Provider: "github", Action: "search", Kind: "repositories", Query: "project"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].Resource != "acme/project" {
		t.Fatal("incorrect repository resource", result)
	}
	result, err = f.s.Read(context.Background(), f.actor, ReadRequest{Provider: "github", Action: "read", Resource: result.Items[0].Resource})
	if err != nil || result.Items[0].Text != "Real repository" {
		t.Fatal("could not read repository metadata", result, err)
	}
}
func TestMalformedSearchCannotClaimNoResults(t *testing.T) {
	f := newFixture(t)
	f.connect(t, "github")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	defer server.Close()
	p := f.s.providers["github"]
	p.APIURL = server.URL
	f.s.providers["github"] = p
	if _, err := f.s.Read(context.Background(), f.actor, ReadRequest{Provider: "github", Action: "search", Query: "project"}); !errors.Is(err, ErrResponse) {
		t.Fatal("malformed search looked empty", err)
	}
}

func TestCJKResultsRemainBoundedStructuredData(t *testing.T) {
	value, err := finish("gmail", &ReadResult{Items: []Item{{ID: "mail1", Resource: "mail1", Text: strings.Repeat("测试资料", 5000)}}})
	if err != nil || !value.Truncated || value.Items[0].Resource != "mail1" || len(value.Items[0].Text) == 0 {
		t.Fatal("long CJK source was not honestly truncated", err)
	}
}
