package connector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestGitHubOnlyExactCodeBlobReadsHaveLargerResponseBound(t *testing.T) {
	f := newFixture(t)
	f.connectCode(t)
	c, r := f.task(t, f.actor.UserID)
	grant, err := f.s.GitHubGrantForTask(context.Background(), c, r, true)
	if err != nil {
		t.Fatal(err)
	}
	content := base64.StdEncoding.EncodeToString(make([]byte, 2<<20))
	large, _ := json.Marshal(map[string]any{"content": content, "encoding": "base64"})
	var oversized atomic.Bool
	f.githubAPI(t, func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if oversized.Load() {
			w.Write([]byte(`{"content":"` + strings.Repeat("x", maxGitHubBlobResponseBytes) + `"}`))
			return
		}
		w.Write(large)
	})
	path := "/repos/acme/project/git/blobs/" + strings.Repeat("a", 40)
	value, err := f.s.GitHubRequestForTask(context.Background(), c, r, grant, false, "GET", path, nil)
	if err != nil || len(value) <= maxResponseBytes {
		t.Fatal("bounded large code blob was rejected", len(value), err)
	}
	for _, ordinary := range []string{path + "?query=1", "/repos/acme/project/git/blobs/not-a-sha", "/repos/acme/project/contents/file"} {
		if _, err = f.s.GitHubRequestForTask(context.Background(), c, r, grant, false, "GET", ordinary, nil); !errors.Is(err, ErrResponse) {
			t.Fatal("non-blob code endpoint escaped the ordinary response bound", ordinary, err)
		}
	}
	if _, err = f.s.Read(context.Background(), f.actor, ReadRequest{Provider: "github", Action: "search", Query: "project"}); !errors.Is(err, ErrResponse) {
		t.Fatal("read_app escaped the ordinary 2 MiB response bound", err)
	}
	oversized.Store(true)
	if _, err = f.s.GitHubRequestForTask(context.Background(), c, r, grant, false, "GET", path, nil); !errors.Is(err, ErrResponse) {
		t.Fatal("oversized code blob response exceeded its 6 MiB bound", err)
	}
}

func (f *fixture) connectCode(t *testing.T) {
	t.Helper()
	u, c := f.start(t, "github", true)
	w := f.callback("github", u, c)
	if w.Code != 303 || !strings.Contains(w.Header().Get("Location"), "connected=github") {
		t.Fatal("code authorization failed", w.Header().Get("Location"))
	}
}
func (f *fixture) githubCode(t *testing.T) bool {
	t.Helper()
	connections, err := f.s.List(context.Background(), f.actor)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range connections {
		if item.Provider == "github" {
			return item.GitHubCodeEnabled
		}
	}
	t.Fatal("missing GitHub connection metadata")
	return false
}
func (f *fixture) task(t *testing.T, actor uuid.UUID) (uuid.UUID, uuid.UUID) {
	t.Helper()
	var project, conversation, run uuid.UUID
	ctx := context.Background()
	if err := f.s.DB.QueryRow(ctx, `SELECT id FROM projects WHERE workspace_id=$1 AND is_default`, f.actor.WorkspaceID).Scan(&project); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DB.QueryRow(ctx, `INSERT INTO conversations(workspace_id,project_id,created_by,agent_slug,title) VALUES($1,$2,$3,'lester','GitHub coding task') RETURNING id`, f.actor.WorkspaceID, project, f.actor.UserID).Scan(&conversation); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DB.QueryRow(ctx, `INSERT INTO runs(conversation_id,status,submitted_by) VALUES($1,'running',$2) RETURNING id`, conversation, actor).Scan(&run); err != nil {
		t.Fatal(err)
	}
	return conversation, run
}
func (f *fixture) githubAPI(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	p := f.s.providers["github"]
	p.APIURL = server.URL
	f.s.providers["github"] = p
}

func TestGitHubCodeRequiresExplicitSuccessfulOAuthAndCanDowngrade(t *testing.T) {
	f := newFixture(t)
	f.connect(t, "github")
	if f.githubCode(t) {
		t.Fatal("ordinary connection silently enabled code writes")
	}
	u, c := f.start(t, "github", true)
	if f.githubCode(t) {
		t.Fatal("pending consent enabled write access before OAuth completion")
	}
	var pending bool
	if err := f.s.DB.QueryRow(context.Background(), `SELECT github_code_enabled FROM app_connection_oauth_flows WHERE state_hash=$1`, digest(u.Query().Get("state"))).Scan(&pending); err != nil || !pending {
		t.Fatal("code choice not bound to OAuth flow", err)
	}
	w := f.request("GET", "/api/v1/connectors/github/callback?state="+u.Query().Get("state")+"&error=access_denied", "", c)
	if !strings.Contains(w.Header().Get("Location"), "access_denied") || f.githubCode(t) {
		t.Fatal("canceled authorization enabled code")
	}
	f.connectCode(t)
	if !f.githubCode(t) {
		t.Fatal("successful explicit consent not saved")
	}
	// A failed attempt to reduce permission does not mutate the previous grant.
	u, c = f.start(t, "github", false)
	_ = f.request("GET", "/api/v1/connectors/github/callback?state="+u.Query().Get("state")+"&error=access_denied", "", c)
	if !f.githubCode(t) {
		t.Fatal("denied reconnect destroyed the accepted grant")
	}
	u, c = f.start(t, "github", false)
	w = f.callback("github", u, c)
	if !strings.Contains(w.Header().Get("Location"), "connected=github") || f.githubCode(t) {
		t.Fatal("explicit readonly reconnect did not downgrade code consent")
	}
	f.connectCode(t)
	items, _ := f.s.List(context.Background(), f.actor)
	for _, item := range items {
		if item.Provider == "github" {
			if err := f.s.Disconnect(context.Background(), f.actor, "github", item.Revision); err != nil {
				t.Fatal(err)
			}
		}
	}
	if f.githubCode(t) {
		t.Fatal("disconnect retained code permission")
	}
}
func TestCodeConsentCannotBeRequestedForAnotherApp(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"gmail", "x", "feishu", "slack"} {
		w := f.request("POST", "/api/v1/connectors/"+name+"/connect", `{"revision":0,"github_code":true}`)
		if w.Code != 400 {
			t.Fatalf("%s accepted code consent: %d", name, w.Code)
		}
	}
	var count int
	if err := f.s.DB.QueryRow(context.Background(), `SELECT count(*) FROM app_connection_oauth_flows`).Scan(&count); err != nil || count != 0 {
		t.Fatal("invalid app consent created OAuth flow", err)
	}
}
func TestGitHubGatewaySeparatesReadAccessAndCodeConsent(t *testing.T) {
	f := newFixture(t)
	f.connect(t, "github")
	c, r := f.task(t, f.actor.UserID)
	var calls atomic.Int32
	f.githubAPI(t, func(w http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.Header.Get("Authorization") != "Bearer fixture-provider-access" {
			t.Error("gateway did not apply server-only credential")
		}
		w.Write([]byte(`[{"id":123,"full_name":"acme/project"}]`))
	})
	grant, err := f.s.GitHubGrantForTask(context.Background(), c, r, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.GitHubGrantForTask(context.Background(), c, r, true); !errors.Is(err, ErrPermission) {
		t.Fatal("read-only grant was accepted as code consent", err)
	}
	data, err := f.s.GitHubRequestForTask(context.Background(), c, r, grant, false, "GET", "/user/repos?per_page=20", nil)
	if err != nil || !strings.HasPrefix(string(data), "[") {
		t.Fatal("gateway could not return repository arrays", string(data), err)
	}
	encoded, _ := json.Marshal(grant)
	if strings.Contains(string(encoded), "fixture-provider") || strings.Contains(string(encoded), "secret") {
		t.Fatal("public grant fence contains credentials")
	}
	for _, write := range []bool{false, true} {
		if _, err = f.s.GitHubRequestForTask(context.Background(), c, r, grant, write, "POST", "/graphql", map[string]any{"query": "mutation fixture"}); !errors.Is(err, ErrPermission) {
			t.Fatal("write bypassed explicit code consent", write, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("rejected writes reached GitHub")
	}
}
func TestGitHubGatewayPinsActorGenerationAndLiveRun(t *testing.T) {
	for _, change := range []string{"reconnect", "stop", "disable", "membership", "other_actor"} {
		t.Run(change, func(t *testing.T) {
			f := newFixture(t)
			f.connectCode(t)
			ctx := context.Background()
			c, r := f.task(t, f.actor.UserID)
			grant, err := f.s.GitHubGrantForTask(ctx, c, r, true)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "reconnect":
				f.connectCode(t)
			case "stop":
				_, err = f.s.DB.Exec(ctx, `UPDATE runs SET status='cancelled' WHERE id=$1`, r)
			case "disable":
				_, err = f.s.DB.Exec(ctx, `UPDATE users SET disabled=true WHERE id=$1`, f.actor.UserID)
			case "membership":
				_, err = f.s.DB.Exec(ctx, `DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.actor.WorkspaceID, f.actor.UserID)
			case "other_actor":
				other := uuid.New()
				if _, err = f.s.DB.Exec(ctx, `INSERT INTO users(id,email,display_name,password_hash) VALUES($1,'other-code@example.test','Other','unused')`, other); err != nil {
					t.Fatal(err)
				}
				if _, err = f.s.DB.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2)`, f.actor.WorkspaceID, other); err != nil {
					t.Fatal(err)
				}
				c, r = f.task(t, other)
			}
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			f.githubAPI(t, func(w http.ResponseWriter, request *http.Request) { calls.Add(1); w.Write([]byte(`{}`)) })
			if _, err = f.s.GitHubRequestForTask(ctx, c, r, grant, true, "POST", "/graphql", map[string]string{"query": "mutation fixture"}); err == nil {
				t.Fatal("accepted a stale or foreign task grant")
			}
			if calls.Load() != 0 {
				t.Fatal("stale authorization reached GitHub")
			}
		})
	}
}
func TestGitHubGatewayReturnsSafeStatusesAndInvalidatesUnauthorizedAccess(t *testing.T) {
	for _, status := range []int{401, 403, 404, 409, 422, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newFixture(t)
			f.connectCode(t)
			c, r := f.task(t, f.actor.UserID)
			grant, err := f.s.GitHubGrantForTask(context.Background(), c, r, true)
			if err != nil {
				t.Fatal(err)
			}
			f.githubAPI(t, func(w http.ResponseWriter, request *http.Request) {
				w.WriteHeader(status)
				w.Write([]byte(`{"message":"unsafe-provider-body-secret"}`))
			})
			_, err = f.s.GitHubRequestForTask(context.Background(), c, r, grant, false, "GET", "/repos/acme/project", nil)
			if err == nil || GitHubStatusCode(err) != status || strings.Contains(err.Error(), "unsafe-provider") {
				t.Fatal("lost safe status classification", err)
			}
			items, err := f.s.List(context.Background(), f.actor)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range items {
				if item.Provider == "github" {
					want := "connected"
					if status == 401 {
						want = "reauthorization_required"
					}
					if item.Status != want {
						t.Fatal("wrong grant invalidation", item.Status)
					}
				}
			}
		})
	}
}
func TestGitHubGatewayRejectsForeignURLsTraversalAndUnsupportedMutations(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		write        bool
	}{
		{"GET", "https://foreign.example/repos/acme/project", false}, {"GET", "//foreign.example/repos/acme/project", false}, {"GET", "/repos/acme/project/../secret", false}, {"GET", "/repos/acme/project/%2e%2e/secret", false}, {"GET", "/repos/acme/project#token", false}, {"GET", "/repos/acme/project\n", false}, {"DELETE", "/repos/acme/project", true}, {"POST", "/user/keys", true}, {"POST", "/graphql", false}, {"PATCH", "/repos/acme/project", true},
	} {
		if _, err := githubTaskPath(tc.method, tc.path, tc.write); err == nil {
			t.Fatal("accepted unsafe endpoint", tc)
		}
	}
	for _, tc := range []struct {
		method, path string
		write        bool
	}{{"GET", "/repos/acme/project/git/ref/heads/feature%2Ftest", false}, {"POST", "/graphql", true}, {"POST", "/repos/acme/project/git/refs", true}, {"PATCH", "/repos/acme/project/git/refs/heads/feature/test", true}, {"POST", "/repos/acme/project/pulls", true}} {
		if _, err := githubTaskPath(tc.method, tc.path, tc.write); err != nil {
			t.Fatal("rejected fixed coding API", tc, err)
		}
	}
	if GitHubStatusCode(ErrProvider) != 0 {
		t.Fatal("invented HTTP status")
	}
}
func TestGitHubGraphQLDoesNotTreatErrorsAsSuccessfulWrites(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want error
	}{{"FORBIDDEN", ErrPermission}, {"INSUFFICIENT_SCOPES", ErrPermission}, {"UNAUTHENTICATED", ErrReauthorize}, {"RATE_LIMITED", ErrRateLimit}, {"UNPROCESSABLE", ErrConflict}, {"UNKNOWN", ErrProvider}} {
		f := newFixture(t)
		f.connectCode(t)
		c, r := f.task(t, f.actor.UserID)
		grant, err := f.s.GitHubGrantForTask(context.Background(), c, r, true)
		if err != nil {
			t.Fatal(err)
		}
		f.githubAPI(t, func(w http.ResponseWriter, request *http.Request) {
			if request.Method != "POST" || request.URL.Path != "/graphql" {
				t.Error("wrong GraphQL request")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": nil, "errors": []any{map[string]any{"type": tc.kind, "message": "unsafe-provider-body"}}})
		})
		if _, err = f.s.GitHubRequestForTask(context.Background(), c, r, grant, true, "POST", "/graphql", map[string]string{"query": "mutation fixture"}); !errors.Is(err, tc.want) || strings.Contains(err.Error(), "unsafe-provider") {
			t.Fatal("GraphQL error leaked or was accepted", tc.kind, err)
		}
	}
}

func TestMalformedGraphQLErrorsAreNotAccepted(t *testing.T) {
	for _, value := range []any{map[string]any{"errors": "provider error"}, map[string]any{"errors": map[string]any{"type": "FORBIDDEN"}}} {
		if !errors.Is(githubGraphQLError(value), ErrResponse) {
			t.Fatal("accepted malformed errors")
		}
	}
}

func TestGitHubGatewayStopIsNotBlockedByAnInFlightProviderRequest(t *testing.T) {
	f := newFixture(t)
	f.connectCode(t)
	c, r := f.task(t, f.actor.UserID)
	grant, err := f.s.GitHubGrantForTask(context.Background(), c, r, true)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	releaseRequest := func() { once.Do(func() { close(release) }) }
	defer releaseRequest()
	f.githubAPI(t, func(w http.ResponseWriter, request *http.Request) {
		close(entered)
		<-release
		w.Write([]byte(`{"data":{"accepted":true}}`))
	})
	done := make(chan error, 1)
	go func() {
		_, err := f.s.GitHubRequestForTask(context.Background(), c, r, grant, true, "POST", "/graphql", map[string]string{"query": "mutation fixture"})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("provider request did not start")
	}
	stopping, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err = f.s.DB.Exec(stopping, `UPDATE runs SET status='cancelled' WHERE id=$1`, r); err != nil {
		t.Fatal("Stop waited for provider HTTP", err)
	}
	releaseRequest()
	select {
	case err := <-done:
		if !errors.Is(err, ErrSession) {
			t.Fatal("accepted stale task output after Stop", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("provider request did not finish")
	}
}
