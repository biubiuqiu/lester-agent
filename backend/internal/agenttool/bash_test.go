package agenttool

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
)

type commandSandbox struct {
	fakeSandbox
	run func(context.Context, sandbox.Command) (*sandbox.CommandResult, error)
}

func (s *commandSandbox) Exec(ctx context.Context, _ string, command sandbox.Command) (*sandbox.CommandResult, error) {
	return s.run(ctx, command)
}

func TestBashRecordsFailureAndDescriptionWithoutStoppingRepair(t *testing.T) {
	computer := &commandSandbox{run: func(_ context.Context, command sandbox.Command) (*sandbox.CommandResult, error) {
		if command.TimeoutSeconds != 2 {
			t.Fatalf("timeout = %d", command.TimeoutSeconds)
		}
		return &sandbox.CommandResult{Shell: "bash", Status: "exited", ExitCodeKnown: true, ExitCode: 7, Stderr: "test failed"}, nil
	}}
	events := map[string]map[string]any{}
	result, err := (Bash{}).Execute(context.Background(), Environment{Sandboxes: computer, Emit: func(name string, payload map[string]any) { events[name] = payload }}, json.RawMessage(`{"command":"npm test","description":" Run tests ","timeout":1001}`))
	if err != nil {
		t.Fatalf("program failure must remain a repairable tool result: %v", err)
	}
	got := result.(map[string]any)
	if got["exit_code"] != 7 || got["status"] != "exited" || got["shell"] != "bash" || got["description"] != "Run tests" {
		t.Fatalf("result = %#v", got)
	}
	if events["COMMAND_STARTED"]["description"] != "Run tests" || events["COMMAND_COMPLETED"]["exit_code"] != 7 || events["COMMAND_COMPLETED"]["status"] != "exited" {
		t.Fatalf("events = %#v", events)
	}
}

func TestBashUnknownExitIsNotSuccessfulZero(t *testing.T) {
	computer := &commandSandbox{run: func(context.Context, sandbox.Command) (*sandbox.CommandResult, error) {
		return &sandbox.CommandResult{Shell: "bash", Status: "timed_out", ExitCode: -1, Stdout: "partial"}, nil
	}}
	result, err := (Bash{}).Execute(context.Background(), Environment{Sandboxes: computer, Emit: func(string, map[string]any) {}}, json.RawMessage(`{"command":"sleep 30"}`))
	if err != nil || result.(map[string]any)["exit_code"] != nil || result.(map[string]any)["exit_code_known"] != false || result.(map[string]any)["stdout"] != "partial" {
		t.Fatalf("timeout result = %#v, %v", result, err)
	}
}

func TestDockerBackgroundCommandUsesBash(t *testing.T) {
	image := os.Getenv("LESTER_TEST_DOCKER_IMAGE")
	if image == "" {
		t.Skip("set LESTER_TEST_DOCKER_IMAGE to a local runtime image")
	}
	id := uuid.NewString()
	name := "lester-sandbox-" + id
	if out, err := exec.Command("docker", "run", "-d", "--init", "--name", name, "--network", "none", "--pids-limit", "256", image, "sleep", "infinity").CombinedOutput(); err != nil {
		t.Fatalf("start fixture: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
	dir := "/workspace/conversations/" + id
	provider := sandbox.NewDockerProvider(image)
	provider.ToolboxSourcePath = os.Getenv("LESTER_TEST_TOOLBOX_PATH")
	if provider.ToolboxSourcePath == "" {
		provider.ToolboxSourcePath = filepath.Join(t.TempDir(), "lester-toolbox")
		if out, err := exec.Command("docker", "cp", name+":/usr/local/bin/lester-toolbox", provider.ToolboxSourcePath).CombinedOutput(); err != nil {
			t.Fatalf("copy native helper: %v %s", err, out)
		}
	}
	var handle sandbox.CommandTaskHandle
	result, err := (Bash{}).Execute(context.Background(), Environment{Sandboxes: provider, SandboxID: id, WorkDir: dir, ConversationID: uuid.MustParse(id), RunID: uuid.New(), RecordTask: func(_ context.Context, s *sandbox.CommandTaskSnapshot) error { handle = s.Handle; return nil }, Emit: func(string, map[string]any) {}}, json.RawMessage(`{"command":"items=(one two); printf '%s\\n' \"${items[1]}\"; set -o pipefail; false | true","run_in_background":true}`))
	if err != nil {
		t.Fatal(err)
	}
	payload := result.(map[string]any)
	if payload["shell"] != "bash" {
		t.Fatalf("background shell = %#v", payload)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, err := provider.InspectTask(context.Background(), id, handle, sandbox.CommandTaskReadOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Status == "exited" {
			if snapshot.ExitCode == nil || *snapshot.ExitCode != 1 || !strings.Contains(string(snapshot.Page.Data), "two\n") {
				t.Fatalf("Bash array output/status missing: %#v", snapshot)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("background Bash command did not finish: %#v", snapshot)
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func TestDockerManagedBashForegroundKeepsRawOutputAndRunsOnce(t *testing.T) {
	image := os.Getenv("LESTER_TEST_DOCKER_IMAGE")
	if image == "" {
		t.Skip("set LESTER_TEST_DOCKER_IMAGE to a local runtime")
	}
	id := uuid.NewString()
	name := "lester-sandbox-" + id
	if out, err := exec.Command("docker", "run", "-d", "--init", "--name", name, "--network", "none", "--cpus", "2", "--memory", "4g", "--pids-limit", "256", image, "sleep", "infinity").CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
	provider := sandbox.NewDockerProvider(image)
	provider.ToolboxSourcePath = os.Getenv("LESTER_TEST_TOOLBOX_PATH")
	if provider.ToolboxSourcePath == "" {
		provider.ToolboxSourcePath = filepath.Join(t.TempDir(), "lester-toolbox")
		if out, err := exec.Command("docker", "cp", name+":/usr/local/bin/lester-toolbox", provider.ToolboxSourcePath).CombinedOutput(); err != nil {
			t.Fatalf("helper: %v %s", err, out)
		}
	}
	dir := "/workspace/conversations/" + id
	payload := strings.Repeat("多语言🙂\n", 7000)
	command := "printf x >> executions.txt; printf '%s' '" + payload + "'; set -o pipefail; false | true"
	args, _ := json.Marshal(bashInput{Command: command, Description: "Large output fixture"})
	var handle sandbox.CommandTaskHandle
	result, err := (Bash{}).Execute(context.Background(), Environment{Sandboxes: provider, SandboxID: id, WorkDir: dir, ConversationID: uuid.MustParse(id), RunID: uuid.New(), RecordTask: func(_ context.Context, s *sandbox.CommandTaskSnapshot) error { handle = s.Handle; return nil }, Emit: func(string, map[string]any) {}}, args)
	if err != nil {
		t.Fatal(err)
	}
	view := result.(map[string]any)
	if view["exit_code"] != 1 || view["preview_truncated"] != true || view["truncated"] != false || view["full_output_saved"] != true {
		t.Fatalf("foreground output facts: status=%v code=%v preview=%v raw=%v", view["status"], view["exit_code"], view["preview_truncated"], view["truncated"])
	}
	stored, err := provider.InspectTask(context.Background(), id, handle, sandbox.CommandTaskReadOptions{Limit: 256 << 10})
	if err != nil || string(stored.Page.Data) != payload {
		t.Fatal("full original bytes were not retained")
	}
	count, err := exec.Command("docker", "exec", name, "cat", dir+"/executions.txt").Output()
	if err != nil || string(count) != "x" {
		t.Fatalf("command was run more than once: %q %v", count, err)
	}
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), handle.OwnershipToken) || strings.Contains(string(encoded), `"pid"`) {
		t.Fatal("process ownership was exposed to the model")
	}
}
