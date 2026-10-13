package sandbox

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type localACSTaskRuntime struct {
	localACSCommandRuntime
	helper string
}

func (r *localACSTaskRuntime) Run(ctx context.Context, command, workDir string, out, errOut func(string)) (*acsCommandResult, error) {
	command = strings.ReplaceAll(command, shellQuote(toolboxContainerPath), shellQuote(r.helper))
	return r.localACSCommandRuntime.Run(ctx, command, workDir, out, errOut)
}
func (r *localACSTaskRuntime) Write(_ context.Context, path string, data []byte) error {
	return os.WriteFile(path, data, 0600)
}
func (r *localACSTaskRuntime) Remove(_ context.Context, path string) error { return os.Remove(path) }

func TestACSManagedTaskProtocolWithLocalRuntime(t *testing.T) {
	helper := os.Getenv("LESTER_TEST_TOOLBOX_PATH")
	if helper == "" {
		t.Skip("set LESTER_TEST_TOOLBOX_PATH to a built native helper")
	}
	id := uuid.NewString()
	dir := "/workspace/conversations/" + id
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	runtime := &localACSTaskRuntime{localACSCommandRuntime: localACSCommandRuntime{dir: t.TempDir()}, helper: helper}
	provider := &ACSProvider{config: normalizeACSConfig(ACSConfig{}), backend: &fakeACSBackend{runtime: runtime}}
	r := CommandTaskRequest{TaskID: uuid.NewString(), RunID: uuid.NewString(), ConversationID: id, OwnershipToken: uuid.NewString(), WorkDir: dir, Command: "printf 'ACS_RAW'; sleep 30"}
	s, err := provider.StartTask(context.Background(), "test", r)
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != "running" || s.Shell != "bash" {
		t.Fatalf("native task start: status=%s shell=%s", s.Status, s.Shell)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		page, e := provider.InspectTask(context.Background(), "test", s.Handle, CommandTaskReadOptions{Limit: 100})
		if e != nil {
			t.Fatal(e)
		}
		if string(page.Page.Data) == "ACS_RAW" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("task did not produce its initial output")
		}
		time.Sleep(20 * time.Millisecond)
	}
	stopped, err := provider.StopTask(context.Background(), "test", s.Handle)
	if err != nil || stopped.Status != "cancelled" || stopped.CleanupConfirmed == nil || !*stopped.CleanupConfirmed {
		t.Fatalf("native task stop: %v status=%s", err, stopped.Status)
	}
	page, err := provider.InspectTask(context.Background(), "test", s.Handle, CommandTaskReadOptions{Limit: 100})
	if err != nil || string(page.Page.Data) != "ACS_RAW" {
		t.Fatalf("raw output after stop: %v %q", err, page.Page.Data)
	}
}
