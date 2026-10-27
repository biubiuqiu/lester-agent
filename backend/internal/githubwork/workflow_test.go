package githubwork

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/connector"
	"github.com/biubiuqiu/lester-agent/backend/internal/githubsandbox"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const baseCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const baseTree = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

type remoteCommit struct{ SHA, Message, Parent, Tree string }
type remotePR struct {
	Number           int64
	Body, Head, Base string
}
type remote struct {
	t                             *testing.T
	mu                            sync.Mutex
	source                        map[string][]byte
	branches                      map[string]string
	commits                       map[string]remoteCommit
	trees                         map[string]map[string][]byte
	pulls                         []remotePR
	refs, mutations, pullsCreated int
	dropRef, dropCommit, dropPR   bool
	refStatus                     int
	moveBeforeCommit              bool
}

func (r *remote) serve(w http.ResponseWriter, q *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if q.Header.Get("Authorization") != "Bearer fixture-github-code-token" {
		r.t.Error("GitHub gateway lost its private bearer")
	}
	send := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	fail := func(status int) { w.WriteHeader(status); send(map[string]string{"message": "fixture failure"}) }
	switch {
	case q.Method == "GET" && q.URL.Path == "/user/installations":
		send(map[string]any{"installations": []any{map[string]any{"id": 1}}})
	case q.Method == "GET" && q.URL.Path == "/user/repos":
		send([]any{map[string]any{"full_name": "acme/app", "default_branch": "main", "permissions": map[string]bool{"push": true}}})
	case q.Method == "GET" && q.URL.Path == "/repos/acme/app":
		send(map[string]any{"full_name": "acme/app", "default_branch": "main"})
	case q.Method == "GET" && strings.HasPrefix(q.URL.Path, "/repos/acme/app/git/ref/heads/"):
		if r.refStatus != 0 {
			fail(r.refStatus)
			return
		}
		branch := strings.TrimPrefix(q.URL.Path, "/repos/acme/app/git/ref/heads/")
		sha, ok := r.branches[branch]
		if !ok {
			fail(404)
			return
		}
		send(map[string]any{"object": map[string]string{"sha": sha, "type": "commit"}})
	case q.Method == "GET" && q.URL.Path == "/repos/acme/app/git/commits/"+baseCommit:
		send(map[string]any{"tree": map[string]string{"sha": baseTree}})
	case q.Method == "GET" && strings.HasPrefix(q.URL.Path, "/repos/acme/app/git/trees/"):
		sha := strings.TrimPrefix(q.URL.Path, "/repos/acme/app/git/trees/")
		files, ok := r.trees[sha]
		if !ok {
			fail(404)
			return
		}
		var tree []treeEntry
		for name, data := range files {
			tree = append(tree, treeEntry{Path: name, Type: "blob", Mode: "100644", SHA: blobSHA(data), Size: int64(len(data))})
		}
		sort.Slice(tree, func(i, j int) bool { return tree[i].Path < tree[j].Path })
		send(map[string]any{"truncated": false, "tree": tree})
	case q.Method == "GET" && strings.HasPrefix(q.URL.Path, "/repos/acme/app/git/blobs/"):
		sha := strings.TrimPrefix(q.URL.Path, "/repos/acme/app/git/blobs/")
		for _, data := range r.source {
			if blobSHA(data) == sha {
				send(map[string]any{"sha": sha, "encoding": "base64", "content": base64.StdEncoding.EncodeToString(data), "size": len(data)})
				return
			}
		}
		fail(404)
	case q.Method == "POST" && q.URL.Path == "/repos/acme/app/git/refs":
		var req struct{ Ref, SHA string }
		if json.NewDecoder(q.Body).Decode(&req) != nil {
			r.t.Error("bad ref body")
		}
		name := strings.TrimPrefix(req.Ref, "refs/heads/")
		if _, ok := r.branches[name]; ok {
			fail(422)
			return
		}
		r.refs++
		r.branches[name] = req.SHA
		if r.dropRef {
			r.dropRef = false
			_, _ = w.Write([]byte(`{"broken":`))
			return
		}
		send(map[string]any{"object": map[string]string{"sha": req.SHA}})
	case q.Method == "POST" && q.URL.Path == "/graphql":
		var req struct {
			Query     string
			Variables struct {
				Input struct {
					Branch           struct{ RepositoryNameWithOwner, BranchName string }
					ExpectedHeadOID  string
					Message          struct{ Headline, Body string }
					ClientMutationID string
					FileChanges      struct {
						Additions []struct{ Path, Contents string }
						Deletions []struct{ Path string }
					}
				}
			}
		}
		if json.NewDecoder(q.Body).Decode(&req) != nil {
			r.t.Error("bad GraphQL body")
		}
		input := req.Variables.Input
		if !strings.Contains(req.Query, "expectedHeadOid") && !strings.Contains(req.Query, "CreateCommitOnBranchInput") {
			r.t.Error("mutation lacks expected-head type")
		}
		r.mutations++
		if r.moveBeforeCommit {
			r.moveBeforeCommit = false
			r.branches[input.Branch.BranchName] = strings.Repeat("e", 40)
		}
		if input.Branch.RepositoryNameWithOwner != "acme/app" || r.branches[input.Branch.BranchName] != input.ExpectedHeadOID {
			send(map[string]any{"errors": []any{map[string]string{"type": "UNPROCESSABLE", "message": "head changed"}}})
			return
		}
		files := map[string][]byte{}
		for name, data := range r.source {
			files[name] = append([]byte{}, data...)
		}
		for _, file := range input.FileChanges.Additions {
			data, err := base64.StdEncoding.DecodeString(file.Contents)
			if err != nil {
				r.t.Error(err)
			}
			files[file.Path] = data
		}
		for _, file := range input.FileChanges.Deletions {
			delete(files, file.Path)
		}
		tree := blobSHA([]byte(input.ClientMutationID + "tree"))
		sha := blobSHA([]byte(input.ClientMutationID + "commit"))
		r.trees[tree] = files
		r.commits[sha] = remoteCommit{SHA: sha, Message: input.Message.Headline + "\n\n" + input.Message.Body, Parent: input.ExpectedHeadOID, Tree: tree}
		r.branches[input.Branch.BranchName] = sha
		if r.dropCommit {
			r.dropCommit = false
			_, _ = w.Write([]byte(`{"broken":`))
			return
		}
		send(map[string]any{"data": map[string]any{"createCommitOnBranch": map[string]any{"clientMutationId": input.ClientMutationID, "commit": map[string]string{"oid": sha}}}})
	case q.Method == "GET" && q.URL.Path == "/repos/acme/app/commits":
		values := []any{}
		sha := r.branches[q.URL.Query().Get("sha")]
		if c, ok := r.commits[sha]; ok {
			values = append(values, map[string]any{"sha": c.SHA, "commit": map[string]any{"message": c.Message, "tree": map[string]string{"sha": c.Tree}}, "parents": []any{map[string]string{"sha": c.Parent}}})
		}
		send(values)
	case q.Method == "POST" && q.URL.Path == "/repos/acme/app/pulls":
		var req struct{ Title, Body, Head, Base string }
		if json.NewDecoder(q.Body).Decode(&req) != nil {
			r.t.Error("bad PR body")
		}
		r.pullsCreated++
		number := int64(len(r.pulls) + 1)
		r.pulls = append(r.pulls, remotePR{number, req.Body, req.Head, req.Base})
		if r.dropPR {
			r.dropPR = false
			_, _ = w.Write([]byte(`{"broken":`))
			return
		}
		send(map[string]any{"number": number})
	case q.Method == "GET" && q.URL.Path == "/repos/acme/app/pulls":
		values := []any{}
		for _, pr := range r.pulls {
			values = append(values, map[string]any{"number": pr.Number, "body": pr.Body, "head": map[string]string{"ref": pr.Head}, "base": map[string]string{"ref": pr.Base}})
		}
		send(values)
	default:
		r.t.Error("unexpected GitHub request", q.Method, q.URL.Path)
		fail(404)
	}
}

type nativeSource struct {
	t         *testing.T
	root      string
	commands  []string
	transfers []sandbox.GitHubTransferRequest
}

func (s *nativeSource) WriteFile(_ context.Context, _ string, root, name string, data []byte) error {
	if root != s.root || strings.Contains(name, "..") {
		return ErrArguments
	}
	full := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0600)
}
func (s *nativeSource) ReadFile(_ context.Context, _ string, root, name string) ([]byte, error) {
	if root != s.root || strings.Contains(name, "..") {
		return nil, ErrArguments
	}
	return os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
}

// Dispatch the real trusted CLI without any shell, user profile or PATH lookup.
func (s *nativeSource) GitHubTransfer(_ context.Context, _ string, root string, request sandbox.GitHubTransferRequest) (*sandbox.CommandResult, error) {
	if root != s.root {
		return nil, ErrArguments
	}
	s.transfers = append(s.transfers, request)
	parts := []string{"github-" + request.Operation, "--root", root, "--directory", request.Directory}
	switch request.Operation {
	case "import":
		parts = append(parts, "--archive", request.Archive, "--checkout-id", request.CheckoutID, "--base-sha", request.BaseSHA)
	case "export":
		parts = append(parts, "--manifest", request.Manifest, "--output", request.Output)
	default:
		return nil, ErrArguments
	}
	var output bytes.Buffer
	err := githubsandbox.Run(parts, &output)
	result := &sandbox.CommandResult{ExitCodeKnown: true, Stdout: output.String()}
	if err != nil {
		result.ExitCode = 1
		result.Stderr = err.Error()
	}
	return result, nil
}
func (s *nativeSource) Exec(ctx context.Context, _ string, command sandbox.Command) (*sandbox.CommandResult, error) {
	s.commands = append(s.commands, command.Command)
	cmd := exec.CommandContext(ctx, "sh", "-c", command.Command)
	cmd.Dir = command.WorkDir
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := &sandbox.CommandResult{ExitCodeKnown: true, Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		var code *exec.ExitError
		if errors.As(err, &code) {
			result.ExitCode = code.ExitCode()
		} else {
			return nil, err
		}
	}
	return result, nil
}

type fixture struct {
	service *Service
	task    Task
	source  *nativeSource
	remote  *remote
	grant   connector.GitHubGrant
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("LESTER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("LESTER_TEST_DATABASE_URL required for GitHub work PostgreSQL tests")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `BEGIN;SELECT pg_advisory_xact_lock(hashtextextended('lester:test:pgcrypto',0));CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;COMMIT;`); err != nil {
		t.Fatal(err)
	}
	schema := "github_work_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	cfg.MaxConns = 4
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	migrations, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(migrations)
	for _, name := range migrations {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, string(data)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	f := &fixture{grant: connector.GitHubGrant{ID: uuid.New(), WorkspaceID: uuid.New(), UserID: uuid.New(), Generation: 1, AccountID: "123", AccountName: "developer"}}
	f.task = Task{ConversationID: uuid.New(), RunID: uuid.New(), CallID: uuid.NewString(), SandboxID: "fixture", WorkDir: t.TempDir()}
	f.source = &nativeSource{t: t, root: f.task.WorkDir}
	f.task.Sandbox = f.source
	key := []byte(strings.Repeat("k", 32))
	block, _ := aes.NewCipher(key)
	vault, _ := cipher.NewGCM(block)
	nonce := make([]byte, vault.NonceSize())
	_, _ = rand.Read(nonce)
	plain, _ := json.Marshal(map[string]any{"access": "fixture-github-code-token", "expires": time.Now().Add(time.Hour), "scopes": []string{}})
	encrypted := vault.Seal(nil, nonce, plain, []byte("app-connection:"+f.grant.ID.String()+":"+f.grant.WorkspaceID.String()+":"+f.grant.UserID.String()+":github"))
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users(id,email,display_name,password_hash) VALUES($1,'code@example.test','Code user','unused')`, []any{f.grant.UserID}},
		{`INSERT INTO workspaces(id,name) VALUES($1,'Code workspace')`, []any{f.grant.WorkspaceID}},
		{`INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2)`, []any{f.grant.WorkspaceID, f.grant.UserID}},
		{`INSERT INTO conversations(id,workspace_id,created_by,agent_slug,title) VALUES($1,$2,$3,'lester','GitHub code task')`, []any{f.task.ConversationID, f.grant.WorkspaceID, f.grant.UserID}},
		{`INSERT INTO runs(id,conversation_id,status,submitted_by) VALUES($1,$2,'running',$3)`, []any{f.task.RunID, f.task.ConversationID, f.grant.UserID}},
		{`INSERT INTO app_connections(id,workspace_id,user_id,provider,status,account_id,account_name,token_ciphertext,token_nonce,generation,github_code_enabled) VALUES($1,$2,$3,'github','connected','123','developer',$4,$5,1,true)`, []any{f.grant.ID, f.grant.WorkspaceID, f.grant.UserID, encrypted, nonce}},
	} {
		if _, err = db.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	source := map[string][]byte{"go.mod": []byte("module fixture.example/app\n\ngo 1.26.9\n"), "sum.go": []byte("package app\nfunc Sum(a,b int) int { return a-b }\n"), "sum_test.go": []byte("package app\nimport \"testing\"\nfunc TestSum(t *testing.T) { if Sum(2,3)!=5 { t.Fatal(\"wrong sum\") } }\n")}
	f.remote = &remote{t: t, source: source, branches: map[string]string{"main": baseCommit}, commits: map[string]remoteCommit{}, trees: map[string]map[string][]byte{baseTree: source}}
	server := httptest.NewServer(http.HandlerFunc(f.remote.serve))
	t.Cleanup(server.Close)
	providers := connector.DefaultProviders(map[string][2]string{"github": {"client-id", "client-secret"}})
	for i := range providers {
		if providers[i].Name == "github" {
			providers[i].APIURL = server.URL
		}
	}
	connections, err := connector.New(db, connector.Options{MasterKey: key, Providers: providers, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	f.service, err = New(db, connections, key)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func resultMap(t *testing.T, value any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil {
		t.Fatal("not a safe object", string(raw))
	}
	return result
}
func (f *fixture) checkout(t *testing.T) uuid.UUID {
	t.Helper()
	value, err := f.service.Checkout(context.Background(), f.task, CheckoutRequest{Repository: "acme/app", Directory: "repo"})
	if err != nil {
		t.Fatal(err)
	}
	raw := resultMap(t, value)
	id, err := uuid.Parse(raw["checkout_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func (f *fixture) prepare(t *testing.T, id uuid.UUID, createPR bool) PublishRequest {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.task.WorkDir, "repo", "sum.go"), []byte("package app\nfunc Sum(a,b int) int { return a+b }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	f.task.CallID = uuid.NewString()
	value, err := f.service.Prepare(context.Background(), f.task, PrepareRequest{CheckoutID: id, Files: []string{"sum.go"}, CommitMessage: "Fix sum", PRTitle: "Fix addition", CreatePR: createPR})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(resultMap(t, value)["publish_arguments"])
	var args PublishRequest
	if json.Unmarshal(raw, &args) != nil {
		t.Fatal("missing publish arguments")
	}
	return args
}

func TestGitHubCodeTaskUsesRealOfflineGitImportTestsSnapshotAndPublication(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	repos, err := f.service.ListRepositories(ctx, f.task.ConversationID, f.task.RunID, "")
	if err != nil || len(resultMap(t, repos)["repositories"].([]any)) != 1 {
		t.Fatal("repository selection unavailable", err)
	}
	id := f.checkout(t)
	before, err := f.source.Exec(ctx, "fixture", sandbox.Command{Command: "go test ./...", WorkDir: filepath.Join(f.task.WorkDir, "repo")})
	if err != nil || before.ExitCode == 0 {
		t.Fatal("fixture should fail before edit", before, err)
	}
	args := f.prepare(t, id, true)
	tests, err := f.source.Exec(ctx, "fixture", sandbox.Command{Command: "go test ./...", WorkDir: filepath.Join(f.task.WorkDir, "repo")})
	if err != nil || tests.ExitCode != 0 {
		t.Fatal("real sandbox-side Go tests failed", tests, err)
	}
	diff := exec.Command("git", "diff", "--stat")
	diff.Dir = filepath.Join(f.task.WorkDir, "repo")
	if output, err := diff.Output(); err != nil || !strings.Contains(string(output), "sum.go") {
		t.Fatal("real Git did not retain changes", string(output), err)
	}
	if f.remote.refs != 0 || f.remote.mutations != 0 || f.remote.pullsCreated != 0 {
		t.Fatal("prepare mutated GitHub")
	}
	if err = f.service.ValidatePublish(ctx, f.task, args); err != nil {
		t.Fatal(err)
	}
	// The approved artifact, not a later worktree edit, is what gets published.
	if err = os.WriteFile(filepath.Join(f.task.WorkDir, "repo", "sum.go"), []byte("package app\nfunc Sum(a,b int)int{return 999}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := f.service.Publish(ctx, f.task, args)
	if err != nil {
		t.Fatal(err)
	}
	receipt := resultMap(t, result)
	if receipt["status"] != "completed" || receipt["commit_sha"] == "" || receipt["pr_url"] != "https://github.com/acme/app/pull/1" {
		t.Fatal("publication receipt", receipt)
	}
	commit := f.remote.commits[receipt["commit_sha"].(string)]
	if !bytes.Contains(f.remote.trees[commit.Tree]["sum.go"], []byte("return a+b")) {
		t.Fatal("publication did not use the immutable tested snapshot")
	}
	if f.remote.refs != 1 || f.remote.mutations != 1 || f.remote.pullsCreated != 1 {
		t.Fatal("incorrect external mutation count")
	}
	second, err := f.service.Publish(ctx, f.task, args)
	if err != nil || resultMap(t, second)["commit_sha"] != receipt["commit_sha"] || f.remote.mutations != 1 || f.remote.pullsCreated != 1 {
		t.Fatal("receipt retry repeated a write", err)
	}
	_ = filepath.WalkDir(f.task.WorkDir, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			data, _ := os.ReadFile(name)
			if bytes.Contains(data, []byte("fixture-github-code-token")) || bytes.Contains(data, []byte("client-secret")) {
				t.Error("credential reached the Computer", name)
			}
		}
		return nil
	})
	if len(f.source.transfers) != 2 || f.source.transfers[0].Operation != "import" || f.source.transfers[1].Operation != "export" {
		t.Fatal("source transfer did not use the narrow trusted transport", f.source.transfers)
	}
	for _, command := range f.source.commands {
		if strings.Contains(command, "lester-toolbox") {
			t.Fatal("source transfer used the ordinary user command shell", command)
		}
	}
}

func TestCheckoutImportsLargeBinaryIntoRealGitBaselineWithinBounds(t *testing.T) {
	f := newFixture(t)
	data := make([]byte, 2<<20)
	for i := range data {
		data[i] = byte(i % 251)
	}
	f.remote.mu.Lock()
	f.remote.source["walkthrough.mp4"] = data
	f.remote.mu.Unlock()
	_ = f.checkout(t)
	actual, err := os.ReadFile(filepath.Join(f.task.WorkDir, "repo", "walkthrough.mp4"))
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("large binary did not reach the sandbox unchanged", err)
	}
	cmd := exec.Command("git", "show", "HEAD:walkthrough.mp4")
	cmd.Dir = filepath.Join(f.task.WorkDir, "repo")
	baseline, err := cmd.Output()
	if err != nil || !bytes.Equal(baseline, data) {
		t.Fatal("large binary was not retained by the real Git baseline", err)
	}
}

func TestCheckoutRejectsBlobExceedingFourMiBBeforeImport(t *testing.T) {
	f := newFixture(t)
	f.remote.mu.Lock()
	f.remote.source["oversized.bin"] = make([]byte, maxCheckoutBlobBytes+1)
	f.remote.mu.Unlock()
	if _, err := f.service.Checkout(context.Background(), f.task, CheckoutRequest{Repository: "acme/app", Directory: "repo"}); !errors.Is(err, ErrBounds) {
		t.Fatal("oversized source blob was accepted", err)
	}
	if len(f.source.transfers) != 0 {
		t.Fatal("oversized source reached the native import boundary")
	}
	var count int
	if err := f.service.DB.QueryRow(context.Background(), `SELECT count(*) FROM github_checkouts`).Scan(&count); err != nil || count != 0 {
		t.Fatal("oversized source created checkout state", count, err)
	}
}
func TestPublicationLostResponsesAreOnlyReconciledNeverReplayed(t *testing.T) {
	for _, lost := range []string{"ref", "commit", "pr"} {
		t.Run(lost, func(t *testing.T) {
			f := newFixture(t)
			id := f.checkout(t)
			args := f.prepare(t, id, true)
			f.remote.mu.Lock()
			switch lost {
			case "ref":
				f.remote.dropRef = true
			case "commit":
				f.remote.dropCommit = true
			case "pr":
				f.remote.dropPR = true
			}
			f.remote.mu.Unlock()
			result, err := f.service.Publish(context.Background(), f.task, args)
			if err != nil {
				t.Fatal(err)
			}
			receipt := resultMap(t, result)
			if lost == "ref" {
				if receipt["status"] != "unknown" || f.remote.mutations != 0 {
					t.Fatal("unconfirmed branch creation published code", receipt)
				}
			} else if receipt["commit_sha"] == "" {
				t.Fatal("lost commit response was not observed through its exact tree", receipt)
			}
			if lost == "pr" && receipt["pr_url"] == "" {
				t.Fatal("lost PR response was not found by its unique marker")
			}
			raw, _ := json.Marshal(result)
			for _, secret := range []string{"Payload", "Content", "content", "fixture-github-code-token", base64.StdEncoding.EncodeToString([]byte("package app\nfunc Sum(a,b int) int { return a+b }\n"))} {
				if bytes.Contains(raw, []byte(secret)) {
					t.Fatal("internal publication payload escaped into tool response", secret, string(raw))
				}
			}
			refs, commits, pulls := f.remote.refs, f.remote.mutations, f.remote.pullsCreated
			if _, err = f.service.Publish(context.Background(), f.task, args); err != nil {
				t.Fatal(err)
			}
			if f.remote.refs != refs || f.remote.mutations != commits || f.remote.pullsCreated != pulls {
				t.Fatal("unknown-response retry repeated a remote write")
			}
		})
	}
}
func TestPublicationCannotChangeApprovedIdentityOrUseAnotherActor(t *testing.T) {
	f := newFixture(t)
	args := f.prepare(t, f.checkout(t), false)
	for _, mutate := range []func(*PublishRequest){func(v *PublishRequest) { v.Repository = "other/repo" }, func(v *PublishRequest) { v.Branch = "main" }, func(v *PublishRequest) { v.Summary = "Pretend this is read-only" }, func(v *PublishRequest) { v.Digest = strings.Repeat("0", 64) }} {
		wrong := args
		mutate(&wrong)
		if err := f.service.ValidatePublish(context.Background(), f.task, wrong); err == nil {
			t.Fatal("spoofed approval metadata accepted")
		}
		if _, err := f.service.Publish(context.Background(), f.task, wrong); err == nil {
			t.Fatal("spoofed publication accepted")
		}
	}
	other := uuid.New()
	ctx := context.Background()
	if _, err := f.service.DB.Exec(ctx, `INSERT INTO users(id,email,display_name,password_hash) VALUES($1,'other-code@example.test','Other','unused')`, other); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.DB.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2)`, f.grant.WorkspaceID, other); err != nil {
		t.Fatal(err)
	}
	otherTask := f.task
	otherTask.RunID = uuid.New()
	if _, err := f.service.DB.Exec(ctx, `INSERT INTO runs(id,conversation_id,status,submitted_by) VALUES($1,$2,'running',$3)`, otherTask.RunID, f.task.ConversationID, other); err != nil {
		t.Fatal(err)
	}
	if err := f.service.ValidatePublish(ctx, otherTask, args); err == nil {
		t.Fatal("another member read or published the prepared private artifact")
	}
	if f.remote.refs != 0 || f.remote.mutations != 0 {
		t.Fatal("rejected request wrote GitHub")
	}
}
func TestChangedBranchAndRevokedTaskAuthorityPreventPublication(t *testing.T) {
	for _, barrier := range []string{"branch", "consent", "generation", "account", "membership", "disabled", "stopped"} {
		t.Run(barrier, func(t *testing.T) {
			f := newFixture(t)
			args := f.prepare(t, f.checkout(t), false)
			if barrier == "branch" {
				f.remote.mu.Lock()
				f.remote.branches[args.Branch] = strings.Repeat("d", 40)
				f.remote.mu.Unlock()
			} else {
				statement := map[string]string{
					"consent":    `UPDATE app_connections SET github_code_enabled=false WHERE id=$1`,
					"generation": `UPDATE app_connections SET generation=generation+1 WHERE id=$1`,
					"account":    `UPDATE app_connections SET account_id='changed-account' WHERE id=$1`,
					"membership": `DELETE FROM workspace_members WHERE user_id=$1`,
					"disabled":   `UPDATE users SET disabled=true WHERE id=$1`,
					"stopped":    `UPDATE runs SET status='cancelling' WHERE id=$1`,
				}[barrier]
				argument := f.grant.ID
				if barrier == "membership" || barrier == "disabled" {
					argument = f.grant.UserID
				} else if barrier == "stopped" {
					argument = f.task.RunID
				}
				if _, err := f.service.DB.Exec(context.Background(), statement, argument); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.service.Publish(context.Background(), f.task, args); err == nil {
				t.Fatal("publication passed a changed branch or revoked grant")
			}
			if f.remote.mutations != 0 || f.remote.pullsCreated != 0 {
				t.Fatal("invalidated publication wrote source code")
			}
		})
	}
}

func TestExpectedHeadPreventsOverwritingConcurrentBranchUpdate(t *testing.T) {
	f := newFixture(t)
	args := f.prepare(t, f.checkout(t), true)
	f.remote.mu.Lock()
	f.remote.moveBeforeCommit = true
	f.remote.mu.Unlock()
	value, err := f.service.Publish(context.Background(), f.task, args)
	if err != nil {
		t.Fatal(err)
	}
	if resultMap(t, value)["status"] != "unknown" || len(f.remote.commits) != 0 || f.remote.pullsCreated != 0 || f.remote.branches[args.Branch] != strings.Repeat("e", 40) {
		t.Fatal("concurrent branch head was overwritten", value)
	}
	requests := f.remote.mutations
	if _, err = f.service.Publish(context.Background(), f.task, args); err != nil {
		t.Fatal(err)
	}
	if f.remote.mutations != requests {
		t.Fatal("failed expected-head mutation was replayed")
	}
}

func TestUnavailableBranchLookupDoesNotImplyBranchIsAbsent(t *testing.T) {
	f := newFixture(t)
	id := f.checkout(t)
	f.remote.mu.Lock()
	f.remote.refStatus = http.StatusServiceUnavailable
	f.remote.mu.Unlock()
	f.task.CallID = uuid.NewString()
	_, err := f.service.Prepare(context.Background(), f.task, PrepareRequest{CheckoutID: id, Files: []string{"sum.go"}, CommitMessage: "Fix sum"})
	if connector.GitHubStatusCode(err) != http.StatusServiceUnavailable {
		t.Fatal("provider outage became a valid new-branch preview", err)
	}
	var count int
	if err = f.service.DB.QueryRow(context.Background(), `SELECT count(*) FROM github_publish_proposals`).Scan(&count); err != nil || count != 0 {
		t.Fatal("stored a publishable proposal after branch lookup failed", count, err)
	}
}
func TestPublicationSnapshotCannotRunShadowToolboxFromPath(t *testing.T) {
	f := newFixture(t)
	id := f.checkout(t)
	directory := t.TempDir()
	marker := filepath.Join(directory, "executed")
	if err := os.WriteFile(filepath.Join(directory, "lester-toolbox"), []byte("#!/bin/sh\ntouch '"+strings.ReplaceAll(marker, "'", "'\"'\"'")+"'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	_ = f.prepare(t, id, false)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("user-controlled PATH executed during preview preparation")
	}
}
func TestRollbackRefusesUncertainWritesAndUnconsumedPublicationApprovals(t *testing.T) {
	f := newFixture(t)
	args := f.prepare(t, f.checkout(t), false)
	data, err := os.ReadFile("../../migrations/000033_github_code_workflows.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.DB.Exec(context.Background(), `UPDATE github_publish_proposals SET status='unknown' WHERE id=$1`, args.ProposalID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.DB.Exec(context.Background(), string(data)); err == nil {
		t.Fatal("rollback discarded an uncertain GitHub write")
	}
	if _, err = f.service.DB.Exec(context.Background(), `UPDATE github_publish_proposals SET status='pushed',stage='pr_intent',pr_url='' WHERE id=$1`, args.ProposalID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.DB.Exec(context.Background(), string(data)); err == nil {
		t.Fatal("rollback discarded an unconfirmed PR creation after a confirmed push")
	}
	if _, err = f.service.DB.Exec(context.Background(), `UPDATE github_publish_proposals SET status='failed',stage='checking_branch' WHERE id=$1`, args.ProposalID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.DB.Exec(context.Background(), `INSERT INTO run_permissions(workspace_id,conversation_id,run_id,tool_call_id,tool,request_digest,summary,status) VALUES($1,$2,$3,'publish-approval','github_publish',$4,'{}','pending')`, f.grant.WorkspaceID, f.task.ConversationID, f.task.RunID, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.DB.Exec(context.Background(), string(data)); err == nil {
		t.Fatal("rollback discarded a live publication approval")
	}
	if _, err = f.service.DB.Exec(context.Background(), `UPDATE run_permissions SET status='cancelled' WHERE tool='github_publish'`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.DB.Exec(context.Background(), string(data)); err != nil {
		t.Fatal("safe rollback failed", err)
	}
	var count int
	if err = f.service.DB.QueryRow(context.Background(), `SELECT count(*) FROM conversations WHERE id=$1`, f.task.ConversationID).Scan(&count); err != nil || count != 1 {
		t.Fatal("rollback removed task data", err)
	}
	if _, err = os.Stat(filepath.Join(f.task.WorkDir, "repo", "sum.go")); err != nil {
		t.Fatal("rollback removed Computer files")
	}
}
