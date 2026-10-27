package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/githubwork"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
)

type githubExecutionFixture struct {
	t                                     *testing.T
	conversation, run, checkout, proposal uuid.UUID
	files                                 *transcriptSandboxFixture
	tested                                *atomic.Bool
	prepared                              string
	calls                                 []string
	published                             atomic.Int32
}

func (g *githubExecutionFixture) scope(task githubwork.Task) error {
	if task.ConversationID != g.conversation || task.RunID != g.run || task.CallID == "" || task.Sandbox == nil {
		return errors.New("wrong task scope")
	}
	return nil
}
func (g *githubExecutionFixture) ListRepositories(_ context.Context, c, r uuid.UUID, _ string) (any, error) {
	if c != g.conversation || r != g.run {
		return nil, errors.New("wrong list scope")
	}
	g.calls = append(g.calls, "list")
	return map[string]any{"repositories": []any{map[string]any{"repository": "acme/project"}}}, nil
}
func (g *githubExecutionFixture) Checkout(_ context.Context, task githubwork.Task, input githubwork.CheckoutRequest) (any, error) {
	if err := g.scope(task); err != nil {
		return nil, err
	}
	if input.Repository != "acme/project" || input.Directory != "repo" {
		return nil, errors.New("wrong repository")
	}
	g.calls = append(g.calls, "checkout")
	return map[string]any{"checkout_id": g.checkout, "repository": "acme/project", "directory": "repo"}, nil
}
func (g *githubExecutionFixture) Prepare(_ context.Context, task githubwork.Task, input githubwork.PrepareRequest) (any, error) {
	if err := g.scope(task); err != nil {
		return nil, err
	}
	if input.CheckoutID != g.checkout || !reflect.DeepEqual(input.Files, []string{"main.go"}) || !input.CreatePR || !g.tested.Load() {
		return nil, errors.New("publication skipped actual tests or selected another source")
	}
	g.prepared = g.files.content("repo/main.go")
	g.calls = append(g.calls, "prepare")
	return map[string]any{"publish_arguments": g.publishInput()}, nil
}
func (g *githubExecutionFixture) publishInput() githubwork.PublishRequest {
	return githubwork.PublishRequest{ProposalID: g.proposal, Digest: strings.Repeat("a", 64), Repository: "acme/project", Branch: "lester/task", Summary: "main.go: reviewed changes"}
}
func (g *githubExecutionFixture) ValidatePublish(_ context.Context, task githubwork.Task, input githubwork.PublishRequest) error {
	if err := g.scope(task); err != nil {
		return err
	}
	if input != g.publishInput() || g.prepared == "" {
		return errors.New("changed immutable publication metadata")
	}
	return nil
}
func (g *githubExecutionFixture) Publish(_ context.Context, task githubwork.Task, input githubwork.PublishRequest) (any, error) {
	if err := g.ValidatePublish(context.Background(), task, input); err != nil {
		return nil, err
	}
	g.calls = append(g.calls, "publish")
	g.published.Add(1)
	return map[string]any{"status": "completed", "commit_url": "https://github.com/acme/project/commit/fixture", "pr_url": "https://github.com/acme/project/pull/1", "published_content": g.prepared}, nil
}

func TestGitHubCodingToolsComposeThroughActualTaskExecutor(t *testing.T) {
	f := newTranscriptFixture(t, false)
	applyTestMigration(t, f.service.db, "000013_account_identity.up.sql")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	run := f.startRun(t, "Change the chosen GitHub repository, run its tests, and create a PR")
	if _, err := f.service.db.Exec(ctx, `UPDATE runs SET submitted_by=$2 WHERE id=$1`, run, f.userID); err != nil {
		t.Fatal(err)
	}
	const before = "package main\nfunc main() {}\n"
	const after = "package main\nfunc main() { println(\"updated\") }\n"
	files := &transcriptSandboxFixture{files: map[string]string{"repo/main.go": before}}
	var tested atomic.Bool
	var command *sandbox.CommandTaskSnapshot
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/command-tasks") {
			var input sandbox.CommandTaskRequest
			if json.NewDecoder(r.Body).Decode(&input) != nil || input.Command != "cd repo && go test ./..." || files.content("repo/main.go") != after {
				t.Error("tests ran against the wrong command or bytes")
			}
			tested.Store(true)
			zero := 0
			now := time.Now()
			command = &sandbox.CommandTaskSnapshot{Handle: sandbox.CommandTaskHandle{TaskID: input.TaskID, RunID: input.RunID, ConversationID: input.ConversationID, WorkDir: input.WorkDir, OwnershipToken: input.OwnershipToken, PID: 123, StartTime: 1}, Status: "exited", Shell: "bash", ExitCode: &zero, StartedAt: now, FinishedAt: &now, LogPath: ".lester/tasks/" + input.TaskID + "/output.log", MaxOutputBytes: input.MaxOutputBytes, StoredBytes: 20, Page: sandbox.CommandTaskPage{Data: []byte("ok acme/project 0.1s\n"), StoredBytes: 20, NextOffset: 20}}
			_ = json.NewEncoder(w).Encode(command)
			return
		}
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/command-tasks/inspect") {
			_ = json.NewEncoder(w).Encode(command)
			return
		}
		files.serveHTTP(w, r)
	}))
	defer server.Close()
	f.service.sandboxes = sandbox.NewClient(server.URL, "")
	source := &githubExecutionFixture{t: t, conversation: f.conversationID, run: run, checkout: uuid.New(), proposal: uuid.New(), files: files, tested: &tested}
	f.service.SetGitHubWorkflow(source)
	calls := []model.ToolCall{
		{ID: "gh-list", Name: "list_github_repositories", Arguments: json.RawMessage(`{}`)},
		{ID: "gh-checkout", Name: "github_checkout", Arguments: json.RawMessage(`{"repository":"acme/project","directory":"repo"}`)},
		{ID: "gh-read", Name: "read", Arguments: json.RawMessage(`{"file_path":"repo/main.go"}`)},
	}
	writeArgs, _ := json.Marshal(map[string]any{"file_path": "repo/main.go", "content": after})
	calls = append(calls, model.ToolCall{ID: "gh-write", Name: "write", Arguments: writeArgs}, model.ToolCall{ID: "gh-test", Name: "bash", Arguments: json.RawMessage(`{"command":"cd repo && go test ./..."}`)})
	prepareArgs, _ := json.Marshal(githubwork.PrepareRequest{CheckoutID: source.checkout, Files: []string{"main.go"}, CommitMessage: "Update main", CreatePR: true, PRTitle: "Update main"})
	calls = append(calls, model.ToolCall{ID: "gh-prepare", Name: "github_prepare_publish", Arguments: prepareArgs})
	publishArgs, _ := json.Marshal(source.publishInput())
	calls = append(calls, model.ToolCall{ID: "gh-publish", Name: "github_publish", Arguments: publishArgs})
	client := &scriptedModel{respond: func(step int, request model.ModelRequest) []model.ModelEvent {
		if step > 0 {
			last := request.Messages[len(request.Messages)-1]
			if last.Role != "tool" || last.ToolCallID != calls[step-1].ID || !json.Valid([]byte(last.Content)) {
				t.Fatal("model did not receive the matching tool receipt")
			}
			var result map[string]any
			if json.Unmarshal([]byte(last.Content), &result) != nil || result["error"] != nil {
				t.Fatal("coding chain tool failed", calls[step-1].Name, last.Content)
			}
			if step == 5 && (result["exit_code"] != float64(0) || result["exit_code_known"] != true || !strings.Contains(last.Content, "ok acme/project")) {
				t.Fatal("model did not observe actual test status")
			}
			if step == len(calls) && result["published_content"] != after {
				t.Fatal("publication used mutable bytes instead of prepared bytes")
			}
		}
		if step < len(calls) {
			return []model.ModelEvent{{ToolCall: &calls[step]}}
		}
		return []model.ModelEvent{{Delta: "Tests passed. PR: https://github.com/acme/project/pull/1"}}
	}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.service.executor.executeTurns(ctx, f.conversationID, run, &Computer{SandboxID: "fixture", WorkDir: conversationWorkDir(f.conversationID)}, client, model.ModelRequest{Model: "fixture", Tools: f.service.tools.Definitions(), Messages: modelHistory(f.messages(t))})
	}()
	item := pendingPermission(t, f)
	if item.Tool != "github_publish" || source.published.Load() != 0 || !tested.Load() {
		t.Fatal("publish did not wait after editing/testing", item)
	}
	// A terminal or another editor may change the working tree while approval is
	// open. The immutable preview, not these later bytes, is what gets published.
	files.mu.Lock()
	files.files["repo/main.go"] = "later unapproved local edit"
	files.mu.Unlock()
	if _, err := f.service.respondPermission(ctx, f.workspaceID, f.userID, f.conversationID, item.ID, item.RequestDigest, "once"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("coding chain did not finish", ctx.Err())
	}
	if source.published.Load() != 1 || !reflect.DeepEqual(source.calls, []string{"list", "checkout", "prepare", "publish"}) {
		t.Fatal("coding service sequence", source.calls)
	}
	var status string
	if err := f.service.db.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1`, run).Scan(&status); err != nil || status != "completed" {
		t.Fatal("coding task failed", status, err)
	}
	rows, err := f.service.db.Query(ctx, `SELECT tool_call_id FROM messages WHERE run_id=$1 AND role='tool' ORDER BY seq`, run)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil || i >= len(calls) || id != calls[i].ID {
			t.Fatal("durable tool receipts lost order", id, err)
		}
		i++
	}
	if rows.Err() != nil || i != len(calls) {
		t.Fatal("coding audit incomplete", rows.Err(), i)
	}
}
