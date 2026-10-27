package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/githubwork"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
)

type githubPublicationProbe struct {
	conversation, run    uuid.UUID
	validated, published atomic.Int32
}

func (p *githubPublicationProbe) ListRepositories(context.Context, uuid.UUID, uuid.UUID, string) (any, error) {
	return map[string]any{}, nil
}
func (p *githubPublicationProbe) Checkout(context.Context, githubwork.Task, githubwork.CheckoutRequest) (any, error) {
	return nil, errors.New("not used")
}
func (p *githubPublicationProbe) Prepare(context.Context, githubwork.Task, githubwork.PrepareRequest) (any, error) {
	return nil, errors.New("not used")
}
func (p *githubPublicationProbe) ValidatePublish(_ context.Context, task githubwork.Task, _ githubwork.PublishRequest) error {
	if task.ConversationID != p.conversation || task.RunID != p.run || task.CallID == "" {
		return errors.New("lost task scope")
	}
	p.validated.Add(1)
	return nil
}
func (p *githubPublicationProbe) Publish(_ context.Context, task githubwork.Task, _ githubwork.PublishRequest) (any, error) {
	if task.ConversationID != p.conversation || task.RunID != p.run || task.CallID == "" {
		return nil, errors.New("lost task scope")
	}
	p.published.Add(1)
	return map[string]any{"status": "completed"}, nil
}
func publicationArguments() string {
	return `{"proposal_id":"` + uuid.NewString() + `","digest":"` + strings.Repeat("a", 64) + `","repository":"acme/project","branch":"lester/task","summary":"main.go: selected changes"}`
}
func setupPublicationProbe(t *testing.T, mode string) (transcriptFixture, uuid.UUID, *githubPublicationProbe) {
	t.Helper()
	f := newTranscriptFixture(t, false)
	applyTestMigration(t, f.service.db, "000013_account_identity.up.sql")
	run := f.startRun(t, "publish selected changes")
	if _, err := f.service.db.Exec(context.Background(), `UPDATE runs SET permission_mode=$2,submitted_by=$3 WHERE id=$1`, run, mode, f.userID); err != nil {
		t.Fatal(err)
	}
	probe := &githubPublicationProbe{conversation: f.conversationID, run: run}
	f.service.SetGitHubWorkflow(probe)
	f.service.sandboxes = sandbox.NewClient("http://unused.invalid", "")
	return f, run, probe
}
func TestGitHubPublicationAlwaysRequiresAcceptedActorAllowOnce(t *testing.T) {
	for _, mode := range []string{"standard", "auto", "full"} {
		t.Run(mode, func(t *testing.T) {
			f, run, probe := setupPublicationProbe(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			other := uuid.New()
			if _, err := f.service.db.Exec(ctx, `INSERT INTO users(id,email,display_name,password_hash)VALUES($1,'other-publisher@example.invalid','Other','unused')`, other); err != nil {
				t.Fatal(err)
			}
			if _, err := f.service.db.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id)VALUES($1,$2)`, f.workspaceID, other); err != nil {
				t.Fatal(err)
			}
			if _, err := f.service.db.Exec(ctx, `UPDATE conversations SET created_by=$2 WHERE id=$1`, f.conversationID, other); err != nil {
				t.Fatal(err)
			}
			call := recordedPermissionCall(t, f, run, "github_publish", publicationArguments())
			done := make(chan error, 1)
			go func() {
				_, err := f.service.executor.runTool(ctx, run, f.conversationID, &Computer{SandboxID: "fixture", WorkDir: conversationWorkDir(f.conversationID)}, call)
				done <- err
			}()
			item := pendingPermission(t, f)
			if probe.published.Load() != 0 || probe.validated.Load() != 1 || item.AllowRun || item.Status != "pending" || item.DecisionSource == "llm" {
				t.Fatal("publication escaped explicit user consent", item)
			}
			var summary map[string]any
			if json.Unmarshal(item.Summary, &summary) != nil || summary["repository"] != "acme/project" || summary["branch"] != "lester/task" || summary["summary"] != "main.go: selected changes" || summary["digest"] != strings.Repeat("a", 64) {
				t.Fatal("missing exact destination in approval", string(item.Summary))
			}
			if _, err := f.service.respondPermission(ctx, f.workspaceID, other, f.conversationID, item.ID, item.RequestDigest, "once"); !errors.Is(err, ErrInteractionNotFound) {
				t.Fatal("conversation creator approved another actor's GitHub", err)
			}
			for _, decision := range []string{"run", "full"} {
				if _, err := f.service.respondPermission(ctx, f.workspaceID, f.userID, f.conversationID, item.ID, item.RequestDigest, decision); !errors.Is(err, ErrInteractionConflict) {
					t.Fatal("publication got a broad grant", decision, err)
				}
			}
			if _, err := f.service.respondPermission(ctx, f.workspaceID, f.userID, f.conversationID, item.ID, item.RequestDigest, "once"); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if probe.published.Load() != 1 {
				t.Fatal("approved publication missing")
			}
			if err := f.service.authorizePermission(ctx, f.conversationID, run, call.ID, call.Name, call.Arguments); err == nil {
				t.Fatal("publication approval was replayed")
			}
		})
	}
}
func TestLiveFullAccessCannotApprovePendingGitHubPublication(t *testing.T) {
	f, run, probe := setupPublicationProbe(t, "standard")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	call := recordedPermissionCall(t, f, run, "github_publish", publicationArguments())
	done := make(chan error, 1)
	go func() {
		_, err := f.service.executor.runTool(ctx, run, f.conversationID, &Computer{SandboxID: "fixture", WorkDir: conversationWorkDir(f.conversationID)}, call)
		done <- err
	}()
	item := pendingPermission(t, f)
	if _, err := f.service.changeRunPermissionMode(ctx, f.workspaceID, f.userID, f.conversationID, run, "full", 0); err != nil {
		t.Fatal(err)
	}
	item = pendingPermission(t, f)
	if item.Revision != 1 || item.Status != "pending" || probe.published.Load() != 0 {
		t.Fatal("live full-access mode approved publication", item)
	}
	if _, err := f.service.respondPermission(ctx, f.workspaceID, f.userID, f.conversationID, item.ID, item.RequestDigest, "once"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if probe.published.Load() != 1 {
		t.Fatal("once approval did not continue original tool")
	}
}
func TestGitHubMutationsRemainUnavailableInPlan(t *testing.T) {
	f, run, probe := setupPublicationProbe(t, "full")
	ctx := context.Background()
	if _, err := f.service.db.Exec(ctx, `UPDATE runs SET execution_mode='plan' WHERE id=$1`, run); err != nil {
		t.Fatal(err)
	}
	tools := modelToolsForMode(f.service.tools.Definitions(), "plan")
	for _, name := range []string{"github_checkout", "github_publish"} {
		if slices.ContainsFunc(tools, func(tool model.Tool) bool { return tool.Name == name }) {
			t.Fatal("Plan exposed GitHub mutation", name)
		}
	}
	for _, name := range []string{"list_github_repositories", "github_prepare_publish"} {
		if !slices.ContainsFunc(tools, func(tool model.Tool) bool { return tool.Name == name }) {
			t.Fatal("Plan lost read-only GitHub tool", name)
		}
	}
	call := recordedPermissionCall(t, f, run, "github_publish", publicationArguments())
	if _, err := f.service.executor.runTool(ctx, run, f.conversationID, &Computer{SandboxID: "fixture", WorkDir: conversationWorkDir(f.conversationID)}, call); err == nil || probe.published.Load() != 0 {
		t.Fatal("Plan executed external write", err)
	}
	items, err := f.service.runPermissions(ctx, f.workspaceID, f.conversationID)
	if err != nil || len(items) != 0 {
		t.Fatal("Plan incorrectly offered publication consent", err)
	}
}
