package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/githubwork"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
)

type githubSandboxFixture struct{ *fakeSandbox }

func (f *githubSandboxFixture) GitHubTransfer(context.Context, string, string, sandbox.GitHubTransferRequest) (*sandbox.CommandResult, error) {
	return nil, errors.New("unexpected transfer: workflow fixture owns its source behavior")
}

type fakeGitHubWorkflow struct {
	calls     []string
	task      githubwork.Task
	validated githubwork.PublishRequest
	reject    bool
}

func (f *fakeGitHubWorkflow) ListRepositories(_ context.Context, c, r uuid.UUID, _ string) (any, error) {
	f.calls = append(f.calls, "list")
	f.task.ConversationID = c
	f.task.RunID = r
	return map[string]any{"repositories": []any{}}, nil
}
func (f *fakeGitHubWorkflow) Checkout(_ context.Context, task githubwork.Task, _ githubwork.CheckoutRequest) (any, error) {
	f.calls = append(f.calls, "checkout")
	f.task = task
	return map[string]any{"checkout_id": uuid.NewString()}, nil
}
func (f *fakeGitHubWorkflow) Prepare(_ context.Context, task githubwork.Task, _ githubwork.PrepareRequest) (any, error) {
	f.calls = append(f.calls, "prepare")
	f.task = task
	return map[string]any{"digest": strings.Repeat("a", 64)}, nil
}
func (f *fakeGitHubWorkflow) ValidatePublish(_ context.Context, task githubwork.Task, input githubwork.PublishRequest) error {
	f.calls = append(f.calls, "validate")
	f.task = task
	f.validated = input
	if f.reject {
		return githubwork.ErrProposal
	}
	return nil
}
func (f *fakeGitHubWorkflow) Publish(_ context.Context, task githubwork.Task, input githubwork.PublishRequest) (any, error) {
	f.calls = append(f.calls, "publish")
	f.task = task
	return map[string]any{"status": "completed", "proposal_id": input.ProposalID}, nil
}

func TestGitHubHandlersRejectModelChosenScopeAndPreserveServerTask(t *testing.T) {
	source := &fakeGitHubWorkflow{}
	env := Environment{RunID: uuid.New(), ConversationID: uuid.New(), CallID: "durable-call", SandboxID: "fixture-computer", WorkDir: "/workspace/conversations/fixture", Sandboxes: &githubSandboxFixture{&fakeSandbox{}}, GitHub: source}
	for _, handler := range []Handler{ListGitHubRepositories{}, GitHubCheckout{}, GitHubPreparePublish{}, GitHubPublish{}} {
		if _, err := handler.Execute(context.Background(), env, json.RawMessage(`{"user_id":"other","sandbox_id":"other"}`)); err == nil {
			t.Fatal("model supplied trusted scope", handler.Definition().Name)
		}
	}
	if len(source.calls) != 0 {
		t.Fatal("invalid scope reached source")
	}
	for _, item := range []struct{ name, args string }{{"list_github_repositories", `{}`}, {"github_checkout", `{"repository":"acme/project","directory":"repo"}`}, {"github_prepare_publish", `{"checkout_id":"` + uuid.NewString() + `","files":["main.go"],"commit_message":"Fix main","create_pr":true}`}} {
		if _, err := NewDefaultRegistry(nil).Execute(context.Background(), item.name, json.RawMessage(item.args), env); err != nil {
			t.Fatal(err)
		}
		if source.task.ConversationID != env.ConversationID || source.task.RunID != env.RunID {
			t.Fatal("handler lost task scope")
		}
		if item.name != "list_github_repositories" && (source.task.CallID != env.CallID || any(source.task.Sandbox) != any(env.Sandboxes) || source.task.SandboxID != env.SandboxID || source.task.WorkDir != env.WorkDir) {
			t.Fatal("handler replaced server computer identity")
		}
	}
	for name, want := range map[string]bool{"list_github_repositories": true, "github_prepare_publish": true, "github_checkout": false, "github_publish": false} {
		if PlanReadOnlyTool(name) != want {
			t.Fatal("wrong Plan boundary", name)
		}
	}
}

func TestGitHubPublishValidatesImmutableMetadataBeforePermissionPrompt(t *testing.T) {
	source := &fakeGitHubWorkflow{reject: true}
	args := json.RawMessage(`{"proposal_id":"` + uuid.NewString() + `","digest":"` + strings.Repeat("a", 64) + `","repository":"acme/project","branch":"lester/task","summary":"main.go: approved bytes"}`)
	original := append([]byte(nil), args...)
	approvals := 0
	env := Environment{RunID: uuid.New(), ConversationID: uuid.New(), CallID: "publish-call", SandboxID: "fixture", WorkDir: "/workspace/conversations/fixture", Sandboxes: &githubSandboxFixture{&fakeSandbox{}}, GitHub: source, AuthorizeTool: func(_ context.Context, name string, raw json.RawMessage) error {
		approvals++
		source.calls = append(source.calls, "approve")
		if name != "github_publish" || !reflect.DeepEqual([]byte(raw), original) {
			t.Fatal("permission did not use exact durable call")
		}
		return nil
	}}
	registry := NewDefaultRegistry(nil)
	if _, err := registry.Execute(context.Background(), "github_publish", args, env); !errors.Is(err, githubwork.ErrProposal) {
		t.Fatal("forged preview reached approval", err)
	}
	if approvals != 0 || !reflect.DeepEqual(source.calls, []string{"validate"}) {
		t.Fatal("validation ran after consent", source.calls)
	}
	source.reject = false
	source.calls = nil
	if _, err := registry.Execute(context.Background(), "github_publish", args, env); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(source.calls, []string{"validate", "approve", "publish"}) || !reflect.DeepEqual([]byte(args), original) {
		t.Fatal("publish order or durable args changed", source.calls)
	}
	if source.task.CallID != env.CallID {
		t.Fatal("publish lost durable call identity")
	}
}

func TestGitHubComputerOperationsRequireNativeTransferBeforeApproval(t *testing.T) {
	source := &fakeGitHubWorkflow{}
	approvals := 0
	env := Environment{RunID: uuid.New(), ConversationID: uuid.New(), CallID: "durable-call", SandboxID: "fixture", WorkDir: "/workspace/conversations/fixture", Sandboxes: &fakeSandbox{}, GitHub: source, AuthorizeTool: func(context.Context, string, json.RawMessage) error {
		approvals++
		return nil
	}}
	ctx := context.Background()
	args := json.RawMessage(`{"proposal_id":"` + uuid.NewString() + `","digest":"` + strings.Repeat("a", 64) + `","repository":"acme/project","branch":"lester/task","summary":"main.go: approved bytes"}`)
	if _, err := NewDefaultRegistry(nil).Execute(ctx, "github_publish", args, env); err == nil || !strings.Contains(err.Error(), "Computer 原生文件传输") {
		t.Fatal("ordinary shell source was accepted for publication", err)
	}
	if approvals != 0 || len(source.calls) != 0 {
		t.Fatal("missing native source reached approval or workflow")
	}
	if _, err := (GitHubCheckout{}).Execute(ctx, env, json.RawMessage(`{"repository":"acme/project","directory":"repo"}`)); err == nil {
		t.Fatal("ordinary shell source was accepted for checkout")
	}
	if _, err := (GitHubPreparePublish{}).Execute(ctx, env, json.RawMessage(`{"checkout_id":"`+uuid.NewString()+`","files":["main.go"],"commit_message":"Fix main","create_pr":true}`)); err == nil {
		t.Fatal("ordinary shell source was accepted for preparation")
	}
	// Listing repositories needs no Computer or native transfer capability.
	if _, err := (ListGitHubRepositories{}).Execute(ctx, env, json.RawMessage(`{}`)); err != nil {
		t.Fatal("read-only repository listing depended on Computer transfer", err)
	}
}
