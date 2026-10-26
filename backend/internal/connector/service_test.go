package connector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
)

const fixtureOrigin = "http://127.0.0.1:13020"

type fixture struct {
	s                        *Service
	actor                    Actor
	access                   string
	router                   http.Handler
	api                      *httptest.Server
	tokens, refreshes, reads atomic.Int32
	failRead                 atomic.Bool
	tokenWait                chan struct{}
	tokenStarted             chan struct{}
	challenge                sync.Map
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	databaseURL := os.Getenv("LESTER_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LESTER_TEST_DATABASE_URL required for app connection PostgreSQL tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `BEGIN;SELECT pg_advisory_xact_lock(hashtextextended('lester:test:pgcrypto',0));CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;COMMIT;`); err != nil {
		t.Fatal(err)
	}
	schema := "connector_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	cfg.MaxConns = 2
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	files, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, string(raw)); err != nil {
			t.Fatalf("migration %s: %v", file, err)
		}
	}
	f := &fixture{actor: Actor{UserID: uuid.New(), WorkspaceID: uuid.New(), SessionHash: digest(uuid.NewString())}}
	f.access, _ = randomToken()
	accessRequest := httptest.NewRequest("GET", fixtureOrigin, nil)
	accessRequest.AddCookie(&http.Cookie{Name: "lester_access_token", Value: f.access})
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users(id,email,display_name,password_hash) VALUES($1,'connector@example.test','Connected user','unused')`, []any{f.actor.UserID}},
		{`INSERT INTO workspaces(id,name) VALUES($1,'Connector workspace')`, []any{f.actor.WorkspaceID}},
		{`INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2)`, []any{f.actor.WorkspaceID, f.actor.UserID}},
		{`INSERT INTO sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 day')`, []any{f.actor.UserID, f.actor.SessionHash}},
		{`INSERT INTO auth_access_tokens(token_hash,session_id,expires_at) SELECT $1,id,now()+interval '2 hours' FROM sessions WHERE token_hash=$2`, []any{auth.AccessCredentialHash(accessRequest), f.actor.SessionHash}},
	} {
		if _, err = db.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	f.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.provider(t, w, r) }))
	t.Cleanup(f.api.Close)
	clients := map[string][2]string{}
	for _, name := range providerOrder {
		clients[name] = [2]string{"client-" + name, "secret-" + name}
	}
	providers := DefaultProviders(clients)
	for i := range providers {
		p := &providers[i]
		p.AuthURL = f.api.URL + "/authorize/" + p.Name
		p.TokenURL = f.api.URL + "/token/" + p.Name
		p.ProfileURL = f.api.URL + "/profile/" + p.Name
		p.APIURL = f.api.URL + "/api/" + p.Name
	}
	f.s, err = New(db, Options{WebOrigin: fixtureOrigin, MasterKey: []byte(strings.Repeat("k", 32)), Providers: providers, HTTPClient: f.api.Client()})
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{Service: f.s}
	router := chi.NewRouter()
	router.Use(auth.MutationOrigin(fixtureOrigin))
	router.Get("/api/v1/connectors/{provider}/callback", h.Callback)
	authService := auth.New(db, nil, 2*time.Hour, false)
	router.Group(func(r chi.Router) {
		r.Use(authService.Middleware)
		r.Get("/api/v1/connectors", h.List)
		r.Post("/api/v1/connectors/{provider}/connect", h.Connect)
		r.Delete("/api/v1/connectors/{provider}", h.Disconnect)
	})
	f.router = router
	return f
}
func (f *fixture) request(method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, fixtureOrigin+path, strings.NewReader(body))
	r.Header.Set("Origin", fixtureOrigin)
	r.AddCookie(&http.Cookie{Name: "lester_access_token", Value: f.access})
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	return w
}
func (f *fixture) start(t *testing.T, provider string) (*url.URL, *http.Cookie) {
	t.Helper()
	connections, err := f.s.List(context.Background(), f.actor)
	if err != nil {
		t.Fatal(err)
	}
	var revision int64
	for _, c := range connections {
		if c.Provider == provider {
			revision = c.Revision
		}
	}
	w := f.request("POST", "/api/v1/connectors/"+provider+"/connect", fmt.Sprintf(`{"revision":%d,"return_to":"/app/p/project-1/overview"}`, revision))
	if w.Code != 200 {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	var result struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal("missing OAuth URL")
	}
	u, err := url.Parse(result.URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("redirect_uri") != fixtureOrigin+"/api/v1/connectors/"+provider+"/callback" {
		t.Fatal("wrong callback origin")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("missing bound HttpOnly OAuth cookie")
	}
	f.challenge.Store(provider, u.Query().Get("code_challenge"))
	return u, cookies[0]
}
func (f *fixture) callback(provider string, u *url.URL, c *http.Cookie) *httptest.ResponseRecorder {
	return f.request("GET", "/api/v1/connectors/"+provider+"/callback?state="+url.QueryEscape(u.Query().Get("state"))+"&code=fixture-code", "", c)
}
func (f *fixture) connect(t *testing.T, provider string) {
	t.Helper()
	u, c := f.start(t, provider)
	w := f.callback(provider, u, c)
	if w.Code != 303 || !strings.Contains(w.Header().Get("Location"), "connected="+provider) {
		t.Fatalf("callback: %d %s", w.Code, w.Header().Get("Location"))
	}
}
func (f *fixture) provider(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 {
		w.WriteHeader(404)
		return
	}
	name := parts[1]
	p := f.s.providers[name]
	if parts[0] == "token" {
		f.tokens.Add(1)
		if r.Method != "POST" {
			t.Error("token exchange must use POST")
		}
		values := map[string]string{}
		if name == "feishu" {
			_ = json.NewDecoder(r.Body).Decode(&values)
		} else {
			_ = r.ParseForm()
			for key, v := range r.PostForm {
				values[key] = v[0]
			}
		}
		if name == "x" || name == "slack" {
			client, secret, ok := r.BasicAuth()
			if !ok || client != p.ClientID || secret != p.ClientSecret {
				t.Error("missing confidential client authentication")
			}
		} else if values["client_id"] != p.ClientID || values["client_secret"] != p.ClientSecret {
			t.Error("wrong token client")
		}
		access, refresh := "fixture-provider-access", "fixture-provider-refresh"
		if values["grant_type"] == "refresh_token" {
			f.refreshes.Add(1)
			access, refresh = "fixture-provider-access-rotated", "fixture-provider-refresh-rotated"
		} else {
			if values["code"] != "fixture-code" || values["redirect_uri"] != fixtureOrigin+"/api/v1/connectors/"+name+"/callback" {
				t.Error("invalid authorization code exchange")
			}
			if p.PKCE {
				challenge, _ := f.challenge.Load(name)
				if oauth2.S256ChallengeFromVerifier(values["code_verifier"]) != challenge {
					t.Error("missing matching PKCE verifier")
				}
			}
		}
		if f.tokenStarted != nil {
			select {
			case f.tokenStarted <- struct{}{}:
			default:
			}
		}
		if f.tokenWait != nil {
			<-f.tokenWait
		}
		wire := map[string]any{"access_token": access, "refresh_token": refresh, "expires_in": 3600, "scope": strings.Join(p.Scopes, " "), "token_type": "Bearer"}
		if name == "slack" {
			wire["token_type"] = "user"
			wire["scope"] = strings.Join(p.Scopes, ",")
			wire = map[string]any{"ok": true, "authed_user": wire}
		}
		if name == "feishu" {
			wire["code"] = 0
		}
		_ = json.NewEncoder(w).Encode(wire)
		return
	}
	if r.Header.Get("Authorization") == "" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer fixture-provider-access") {
		t.Error("resource request missing provider bearer")
	}
	if parts[0] == "profile" {
		var value any
		switch name {
		case "github":
			value = map[string]any{"id": 123, "login": "connected-github"}
		case "gmail":
			value = map[string]any{"sub": "google-user-1", "email": "mail@example.test"}
		case "x":
			value = map[string]any{"data": map[string]any{"id": "42", "username": "connected_x"}}
		case "slack":
			value = map[string]any{"ok": true, "team_id": "T1234", "user_id": "U1234", "team": "Test team", "user": "connected_slack"}
		case "feishu":
			value = map[string]any{"code": 0, "data": map[string]any{"open_id": "ou_user", "tenant_key": "tenant", "name": "Connected Feishu"}}
		}
		_ = json.NewEncoder(w).Encode(value)
		return
	}
	if r.Method != "GET" {
		t.Error("work-app adapters must never mutate resources")
	}
	f.reads.Add(1)
	if f.failRead.Load() {
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"error":"fixture unavailable"}`))
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/"+name)
	var value any
	switch name {
	case "github":
		if strings.HasPrefix(path, "/search/") {
			value = map[string]any{"total_count": 1, "items": []any{map[string]any{"id": 1, "number": 12, "title": "Source issue", "body": "untrusted external source", "repository_url": "https://api.github.com/repos/acme/project"}}}
		} else if strings.Contains(path, "/contents/") {
			value = map[string]any{"sha": "content-sha", "path": "README.md", "encoding": "base64", "size": 8, "content": base64.StdEncoding.EncodeToString([]byte("contents"))}
		} else {
			value = map[string]any{"id": 1, "title": "Source issue", "body": "source detail"}
		}
	case "gmail":
		if path == "/messages" {
			value = map[string]any{"messages": []any{map[string]any{"id": "mail1"}}, "nextPageToken": "next-mail"}
		} else {
			payload := map[string]any{"mimeType": "text/plain", "headers": []any{map[string]any{"name": "Subject", "value": "Mail subject"}, map[string]any{"name": "From", "value": "sender@example.test"}}, "body": map[string]any{"data": base64.RawURLEncoding.EncodeToString([]byte("Mail body source"))}}
			value = map[string]any{"id": "mail1", "snippet": "mail preview", "payload": payload}
		}
	case "x":
		tweet := map[string]any{"id": "42", "text": "Tweet source", "author_id": "7"}
		if path == "/tweets/search/recent" {
			value = map[string]any{"data": []any{tweet}, "meta": map[string]any{"next_token": "next-x"}}
		} else {
			value = map[string]any{"data": tweet}
		}
	case "slack":
		message := map[string]any{"ts": "1700000000.000001", "text": "Slack source", "user": "U1234", "channel": map[string]any{"id": "C1234", "name": "project"}}
		if path == "/search.messages" {
			value = map[string]any{"ok": true, "messages": map[string]any{"matches": []any{message}, "paging": map[string]any{"pages": 1}}}
		} else {
			value = map[string]any{"ok": true, "messages": []any{message}}
		}
	case "feishu":
		data := map[string]any{}
		if strings.Contains(path, "/chats") {
			data["items"] = []any{map[string]any{"chat_id": "oc_chat", "name": "Project chat"}}
		} else if strings.Contains(path, "/docx/") {
			data["content"] = "Feishu document source"
		} else {
			data["items"] = []any{map[string]any{"message_id": "om_message", "sender": map[string]any{"id": "ou_user"}, "body": map[string]any{"content": `{"text":"Feishu source"}`}}}
		}
		value = map[string]any{"code": 0, "data": data}
	}
	_ = json.NewEncoder(w).Encode(value)
}

func TestOAuthAndReadOnlyAdapters(t *testing.T) {
	f := newFixture(t)
	for _, name := range providerOrder {
		t.Run(name, func(t *testing.T) {
			f.connect(t, name)
			metadata, err := f.s.List(context.Background(), f.actor)
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(metadata)
			if strings.Contains(string(encoded), "fixture-provider-") || strings.Contains(string(encoded), "secret-") {
				t.Fatal("credential exposed in metadata")
			}
			var request = ReadRequest{Provider: name, Action: "search", Query: "project"}
			if name == "github" {
				request.Kind = "issues"
			}
			result, err := f.s.Read(context.Background(), f.actor, request)
			if err != nil || len(result.Items) == 0 {
				t.Fatalf("search: %#v %v", result, err)
			}
			resource := result.Items[0].Resource
			read, err := f.s.Read(context.Background(), f.actor, ReadRequest{Provider: name, Action: "read", Resource: resource})
			if err != nil || len(read.Items) == 0 {
				t.Fatalf("read: %#v %v", read, err)
			}
			if read.Notice == "" {
				t.Fatal("external source authority boundary missing")
			}
			var ciphertext, nonce []byte
			var id uuid.UUID
			if err = f.s.DB.QueryRow(context.Background(), `SELECT id,token_ciphertext,token_nonce FROM app_connections WHERE user_id=$1 AND provider=$2`, f.actor.UserID, name).Scan(&id, &ciphertext, &nonce); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(ciphertext), "fixture-provider-") {
				t.Fatal("stored plaintext token")
			}
			var saved token
			if f.s.open(ciphertext, nonce, connectionAAD(id, f.actor, name), &saved) != nil || saved.Access == "" {
				t.Fatal("cannot decrypt saved token")
			}
		})
	}
	for _, resource := range []string{"doc:document_id", "message:om_message"} {
		if _, err := f.s.Read(context.Background(), f.actor, ReadRequest{Provider: "feishu", Action: "read", Resource: resource}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.s.Read(context.Background(), f.actor, ReadRequest{Provider: "github", Action: "read", Resource: "acme/project/contents/README.md"}); err != nil {
		t.Fatal(err)
	}
}

func TestOAuthBrowserBindingExpiryReplayAndDenial(t *testing.T) {
	f := newFixture(t)
	u, c := f.start(t, "gmail")
	wrong := *c
	wrong.Value = strings.Repeat("z", 43)
	if w := f.callback("gmail", u, &wrong); !strings.Contains(w.Header().Get("Location"), "invalid_state") {
		t.Fatal("accepted another browser")
	}
	if f.tokens.Load() != 0 {
		t.Fatal("exchanged a code before browser binding")
	}
	w := f.callback("gmail", u, c)
	if !strings.Contains(w.Header().Get("Location"), "connected=gmail") || !strings.Contains(w.Header().Get("Location"), "returnTo=%2Fapp%2Fp%2Fproject-1%2Foverview") {
		t.Fatal("lost connection or original task return")
	}
	count := f.tokens.Load()
	if w = f.callback("gmail", u, c); !strings.Contains(w.Header().Get("Location"), "invalid_state") || f.tokens.Load() != count {
		t.Fatal("replayed an OAuth code")
	}
	u, c = f.start(t, "gmail")
	w = f.request("GET", "/api/v1/connectors/gmail/callback?state="+u.Query().Get("state")+"&error=access_denied", "", c)
	if !strings.Contains(w.Header().Get("Location"), "access_denied") || f.tokens.Load() != count {
		t.Fatal("did not retain denied authorization boundary")
	}
	connections, _ := f.s.List(context.Background(), f.actor)
	for _, item := range connections {
		if item.Provider == "gmail" && item.Status != "connected" {
			t.Fatal("denied reconnect destroyed a previous valid grant")
		}
	}
	u, c = f.start(t, "x")
	w = f.request("GET", "/api/v1/connectors/x/callback?state="+u.Query().Get("state")+"&error=server_error", "", c)
	if !strings.Contains(w.Header().Get("Location"), "provider_error") || f.tokens.Load() != count {
		t.Fatal("provider failure was represented as user cancellation")
	}
	u, c = f.start(t, "x")
	_, _ = f.s.DB.Exec(context.Background(), `UPDATE app_connection_oauth_flows SET expires_at=now()-interval '1 second' WHERE state_hash=$1`, digest(u.Query().Get("state")))
	if w = f.callback("x", u, c); !strings.Contains(w.Header().Get("Location"), "invalid_state") || f.tokens.Load() != count {
		t.Fatal("accepted an expired flow")
	}
}

func TestCallbackCannotOutliveDisconnectLogoutOrDisable(t *testing.T) {
	for _, barrier := range []string{"disconnect", "logout", "disable", "newer_flow"} {
		t.Run(barrier, func(t *testing.T) {
			f := newFixture(t)
			u, c := f.start(t, "github")
			release := make(chan struct{})
			var once sync.Once
			releaseToken := func() { once.Do(func() { close(release) }) }
			defer releaseToken()
			f.tokenWait = release
			f.tokenStarted = make(chan struct{}, 1)
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- f.callback("github", u, c) }()
			select {
			case <-f.tokenStarted:
			case <-time.After(5 * time.Second):
				t.Fatal("OAuth exchange did not start")
			}
			switch barrier {
			case "disconnect":
				items, err := f.s.List(context.Background(), f.actor)
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range items {
					if item.Provider == "github" {
						if err = f.s.Disconnect(context.Background(), f.actor, "github", item.Revision); err != nil {
							t.Fatal(err)
						}
					}
				}
			case "logout":
				if _, err := f.s.DB.Exec(context.Background(), `UPDATE sessions SET expires_at=now()-interval '1 second' WHERE token_hash=$1`, f.actor.SessionHash); err != nil {
					t.Fatal(err)
				}
			case "disable":
				if _, err := f.s.DB.Exec(context.Background(), `UPDATE users SET disabled=true WHERE id=$1`, f.actor.UserID); err != nil {
					t.Fatal(err)
				}
			case "newer_flow":
				_, _ = f.start(t, "github")
			}
			releaseToken()
			select {
			case w := <-done:
				if !strings.Contains(w.Header().Get("Location"), "state_changed") {
					t.Fatal("stale callback reported success")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("stale callback did not finish")
			}
			var grants int
			if err := f.s.DB.QueryRow(context.Background(), `SELECT count(*) FROM app_connections WHERE token_ciphertext IS NOT NULL`).Scan(&grants); err != nil || grants != 0 {
				t.Fatal("stale OAuth callback restored a credential", err)
			}
		})
	}
}

func (f *fixture) expireToken(t *testing.T, name string) {
	t.Helper()
	var id uuid.UUID
	var encrypted, nonce []byte
	ctx := context.Background()
	if err := f.s.DB.QueryRow(ctx, `SELECT id,token_ciphertext,token_nonce FROM app_connections WHERE user_id=$1 AND provider=$2`, f.actor.UserID, name).Scan(&id, &encrypted, &nonce); err != nil {
		t.Fatal(err)
	}
	var current token
	if err := f.s.open(encrypted, nonce, connectionAAD(id, f.actor, name), &current); err != nil {
		t.Fatal(err)
	}
	current.Expires = time.Now().Add(-time.Minute)
	encrypted, nonce, err := f.s.seal(current, connectionAAD(id, f.actor, name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.DB.Exec(ctx, `UPDATE app_connections SET token_ciphertext=$2,token_nonce=$3,expires_at=$4 WHERE id=$1`, id, encrypted, nonce, current.Expires); err != nil {
		t.Fatal(err)
	}
}
func TestRefreshCommitsBeforeFailedReadAndSerializesAcrossReaders(t *testing.T) {
	f := newFixture(t)
	f.connect(t, "github")
	f.expireToken(t, "github")
	f.failRead.Store(true)
	_, err := f.s.Read(context.Background(), f.actor, ReadRequest{Provider: "github", Action: "search", Query: "project"})
	if !errors.Is(err, ErrProvider) {
		t.Fatal("did not disclose failed read", err)
	}
	if f.refreshes.Load() != 1 {
		t.Fatal("refresh missing")
	}
	f.failRead.Store(false)
	if _, err = f.s.Read(context.Background(), f.actor, ReadRequest{Provider: "github", Action: "search", Query: "project"}); err != nil {
		t.Fatal(err)
	}
	if f.refreshes.Load() != 1 {
		t.Fatal("rolled back a successfully rotated credential after the failed GET")
	}
	f.expireToken(t, "github")
	var wg sync.WaitGroup
	failures := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.s.Read(context.Background(), f.actor, ReadRequest{Provider: "github", Action: "search", Query: "project"})
			failures <- err
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.refreshes.Load() != 2 {
		t.Fatal("concurrent readers consumed one refresh token more than once")
	}
}

func TestPrivateConnectionsUseAcceptedRunActor(t *testing.T) {
	f := newFixture(t)
	f.connect(t, "gmail")
	ctx := context.Background()
	other := uuid.New()
	if _, err := f.s.DB.Exec(ctx, `INSERT INTO users(id,email,display_name,password_hash) VALUES($1,'other@example.test','Other','unused')`, other); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.DB.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2)`, f.actor.WorkspaceID, other); err != nil {
		t.Fatal(err)
	}
	items, err := f.s.List(ctx, Actor{WorkspaceID: f.actor.WorkspaceID, UserID: other})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Status != "disconnected" || item.AccountName != "" {
			t.Fatal("shared a personal app grant with another workspace member")
		}
	}
	var project, conversation, run uuid.UUID
	if err = f.s.DB.QueryRow(ctx, `SELECT id FROM projects WHERE workspace_id=$1 AND is_default`, f.actor.WorkspaceID).Scan(&project); err != nil {
		t.Fatal(err)
	}
	if err = f.s.DB.QueryRow(ctx, `INSERT INTO conversations(workspace_id,project_id,created_by,agent_slug,title) VALUES($1,$2,$3,'lester','Source owner differs from run actor') RETURNING id`, f.actor.WorkspaceID, project, f.actor.UserID).Scan(&conversation); err != nil {
		t.Fatal(err)
	}
	if err = f.s.DB.QueryRow(ctx, `INSERT INTO runs(conversation_id,status,submitted_by) VALUES($1,'running',$2) RETURNING id`, conversation, other).Scan(&run); err != nil {
		t.Fatal(err)
	}
	before := f.reads.Load()
	if _, err = f.s.ReadForTask(ctx, conversation, run, ReadRequest{Provider: "gmail", Action: "search", Query: "project"}); !errors.Is(err, ErrNotConnected) {
		t.Fatal("used the conversation owner's grant instead of its run actor", err)
	}
	if f.reads.Load() != before {
		t.Fatal("called external API with another actor's credentials")
	}
	if _, err = f.s.DB.Exec(ctx, `UPDATE runs SET status='completed' WHERE id=$1`, run); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ListForTask(ctx, conversation, run); !errors.Is(err, ErrSession) {
		t.Fatal("accepted a completed run", err)
	}
}

func TestOAuthOriginRevisionAndReadArgumentBoundaries(t *testing.T) {
	f := newFixture(t)
	r := httptest.NewRequest("POST", fixtureOrigin+"/api/v1/connectors/github/connect", strings.NewReader(`{"revision":0}`))
	r.AddCookie(&http.Cookie{Name: "lester_access_token", Value: f.access})
	r.Header.Set("Origin", "https://foreign.example")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	if w.Code != 403 || f.tokens.Load() != 0 {
		t.Fatal("accepted a foreign-origin authorization start")
	}
	f.connect(t, "github")
	count := f.reads.Load()
	for _, request := range []ReadRequest{
		{Provider: "github", Action: "write", Resource: "acme/project/contents/README.md"},
		{Provider: "github", Action: "read", Resource: "https://foreign.example/secret"},
		{Provider: "github", Action: "read", Resource: "acme/project/contents/../secret"},
		{Provider: "github", Action: "read", Resource: "acme/project/contents/%2e%2e/secret"},
		{Provider: "github", Action: "search", Query: "project", Limit: 1000},
	} {
		if _, err := f.s.Read(context.Background(), f.actor, request); err == nil {
			t.Fatalf("accepted invalid request: %#v", request)
		}
	}
	if f.reads.Load() != count {
		t.Fatal("sent invalid read arguments to external API")
	}
	if err := f.s.Disconnect(context.Background(), f.actor, "github", 0); !errors.Is(err, ErrConflict) {
		t.Fatal("accepted stale disconnect revision", err)
	}
	items, _ := f.s.List(context.Background(), f.actor)
	for _, item := range items {
		if item.Provider == "github" {
			if err := f.s.Disconnect(context.Background(), f.actor, "github", item.Revision); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := f.s.Read(context.Background(), f.actor, ReadRequest{Provider: "github", Action: "search", Query: "project"}); !errors.Is(err, ErrNotConnected) {
		t.Fatal("read after disconnect", err)
	}
}
