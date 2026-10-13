package sandbox

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDockerManagedTasksRetainRawOutputAndOwnStop(t *testing.T) {
	image := os.Getenv("LESTER_TEST_DOCKER_IMAGE")
	if image == "" {
		t.Skip("set LESTER_TEST_DOCKER_IMAGE to the runtime")
	}
	id := uuid.NewString()
	name, _ := containerName(id)
	if out, err := exec.Command("docker", "run", "-d", "--init", "--name", name, "--network", "none", "--cpus", "2", "--memory", "4g", "--pids-limit", "256", "--security-opt", "no-new-privileges", image, "sleep", "infinity").CombinedOutput(); err != nil {
		t.Fatalf("start fixture: %v %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
	provider := NewDockerProvider(image)
	provider.ToolboxSourcePath = os.Getenv("LESTER_TEST_TOOLBOX_PATH")
	if provider.ToolboxSourcePath == "" {
		provider.ToolboxSourcePath = filepath.Join(t.TempDir(), "lester-toolbox")
		if out, err := exec.Command("docker", "cp", name+":"+toolboxContainerPath, provider.ToolboxSourcePath).CombinedOutput(); err != nil {
			t.Fatalf("copy helper: %v %s", err, out)
		}
	}
	req := func(command string) CommandTaskRequest {
		return CommandTaskRequest{TaskID: uuid.NewString(), RunID: uuid.NewString(), ConversationID: id, OwnershipToken: uuid.NewString(), WorkDir: "/workspace/conversations/" + id, Command: command}
	}
	wait := func(h CommandTaskHandle) *CommandTaskSnapshot {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			s, err := provider.InspectTask(context.Background(), id, h, CommandTaskReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if s.Terminal() {
				return s
			}
			if time.Now().After(deadline) {
				t.Fatalf("task did not finish: %#v", s)
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	payload := strings.Repeat("原文🙂\n", 11000)
	s, err := provider.StartTask(context.Background(), id, req("printf '%s' '"+payload+"'; set -o pipefail; false | true"))
	if err != nil {
		t.Fatal(err)
	}
	s = wait(s.Handle)
	if s.Status != "exited" || s.ExitCode == nil || *s.ExitCode != 1 || s.Truncated {
		t.Fatalf("task status: %#v", s)
	}
	var collected []byte
	offset := int64(0)
	for {
		page, err := provider.InspectTask(context.Background(), id, s.Handle, CommandTaskReadOptions{Offset: offset, Limit: 17003})
		if err != nil {
			t.Fatal(err)
		}
		collected = append(collected, page.Page.Data...)
		if !page.Page.HasMore {
			break
		}
		offset = page.Page.NextOffset
	}
	if !bytes.Equal(collected, []byte(payload)) {
		t.Fatal("raw UTF-8 log changed across pages")
	}
	long, err := provider.StartTask(context.Background(), id, req(`setsid bash -c 'trap "" TERM; sleep 30' & printf '%s\n' "$!" > detached.pid; printf READY; wait`))
	if err != nil {
		t.Fatal(err)
	}
	other, err := provider.StartTask(context.Background(), id, req("sleep 30"))
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := provider.StopTask(context.Background(), id, long.Handle)
	if err != nil || stopped.Status != "cancelled" || stopped.CleanupConfirmed == nil || !*stopped.CleanupConfirmed {
		t.Fatalf("stop: %#v %v", stopped, err)
	}
	unrelated, err := provider.InspectTask(context.Background(), id, other.Handle, CommandTaskReadOptions{})
	if err != nil || unrelated.Status != "running" {
		t.Fatalf("stopping one task affected another: %#v %v", unrelated, err)
	}
	// A restarted Sandbox Service has no helper-installation cache. Installing
	// the helper again must not overwrite a running supervisor's executable or
	// turn a trusted running receipt into an unknown task.
	restarted := NewDockerProvider(image)
	restarted.ToolboxSourcePath = provider.ToolboxSourcePath
	recovered, err := restarted.InspectTask(context.Background(), id, other.Handle, CommandTaskReadOptions{})
	if err != nil || recovered.Status != "running" || recovered.Handle != other.Handle {
		t.Fatalf("helper replacement lost running task: %#v %v", recovered, err)
	}
	recovered, err = restarted.StopTask(context.Background(), id, other.Handle)
	if err != nil || recovered.Status != "cancelled" || recovered.CleanupConfirmed == nil || !*recovered.CleanupConfirmed {
		t.Fatalf("stop after helper replacement: %#v %v", recovered, err)
	}
	unconfirmed := long.Handle
	unconfirmed.PID = 0
	unconfirmed.StartTime = 0
	unknown, err := provider.StopTask(context.Background(), id, unconfirmed)
	if err != nil || unknown.Status != "unknown" || unknown.Handle.PID != 0 {
		t.Fatalf("unconfirmed task metadata granted ownership: %#v %v", unknown, err)
	}
}
