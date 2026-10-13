package sandbox

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDockerCommandShellAndCancellation(t *testing.T) {
	image := os.Getenv("LESTER_TEST_DOCKER_IMAGE")
	if image == "" {
		t.Skip("set LESTER_TEST_DOCKER_IMAGE to a local runtime image")
	}
	id := uuid.NewString()
	name, _ := containerName(id)
	if out, err := exec.Command("docker", "run", "-d", "--name", name, "--network", "none", "--pids-limit", "256", image, "sleep", "infinity").CombinedOutput(); err != nil {
		t.Fatalf("start fixture: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
	provider := NewDockerProvider(image)
	workDir := "/workspace/conversations/" + id
	result, err := provider.Exec(context.Background(), id, Command{Command: `items=(one two); printf '%s\n' "${items[1]}"; set -o pipefail; false | true`, WorkDir: workDir})
	if err != nil || result.Shell != "bash" || result.Status != "exited" || !result.ExitCodeKnown || result.ExitCode != 1 || strings.TrimSpace(result.Stdout) != "two" {
		t.Fatalf("Bash arrays/pipefail: %#v, %v", result, err)
	}

	for _, mode := range []string{"cancelled", "timed_out"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			command := Command{WorkDir: workDir, Command: `printf '%s\n' "$$" > parent.pid; setsid bash -c 'trap "" TERM; sleep 30' & printf '%s\n' "$!" > child.pid; printf 'PARTIAL_OUTPUT\n'; wait`}
			if mode == "timed_out" {
				command.TimeoutSeconds = 1
			}
			done := make(chan struct{})
			var result *CommandResult
			var runErr error
			go func() { result, runErr = provider.Exec(ctx, id, command); close(done) }()
			deadline := time.Now().Add(5 * time.Second)
			for {
				if exec.Command("docker", "exec", name, "test", "-s", workDir+"/child.pid").Run() == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("command did not start")
				}
				time.Sleep(30 * time.Millisecond)
			}
			if mode == "cancelled" {
				cancel()
			}
			select {
			case <-done:
			case <-time.After(8 * time.Second):
				t.Fatal("cancel/timeout did not return")
			}
			if runErr != nil || result.Status != mode || result.ExitCodeKnown || result.CleanupConfirmed == nil || !*result.CleanupConfirmed || !strings.Contains(result.Stdout, "PARTIAL_OUTPUT") {
				t.Fatalf("cancel/timeout result: %#v, %v", result, runErr)
			}
			// Both the Bash process and a TERM-ignoring child must stop; killing
			// docker exec alone would leave this command running in the container.
			out, err := exec.Command("docker", "exec", name, "sh", "-c", `for file in "$1/parent.pid" "$1/child.pid"; do
  read -r pid < "$file"
  if IFS= read -r stat 2>/dev/null < "/proc/$pid/stat"; then
    rest=${stat##*) }; [ "${rest%% *}" = Z ] || { printf 'alive: %s\n' "$pid"; exit 1; }
  fi
done`, "fixture", workDir).CombinedOutput()
			if err != nil {
				t.Fatalf("owned command process remains alive: %v: %s", err, out)
			}
			if out, err := exec.Command("docker", "exec", name, "sh", "-c", `find /tmp -maxdepth 1 -name '.lester-command-*' -print`).CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "" {
				t.Fatalf("ownership marker leaked: %v: %s", err, out)
			}
		})
	}
	t.Run("recycled_identity_is_not_signalled", func(t *testing.T) {
		// Another command/session belongs to the same container, but not to
		// this invocation. A mismatched start time must never select it.
		protected := workDir + "/protected.pid"
		if out, err := exec.Command("docker", "exec", "-d", name, "setsid", "sh", "-c", `printf '%s\n' "$$" > "$1"; exec sleep 30`, "fixture", protected).CombinedOutput(); err != nil {
			t.Fatalf("start unrelated session: %v: %s", err, out)
		}
		deadline := time.Now().Add(3 * time.Second)
		for exec.Command("docker", "exec", name, "test", "-s", protected).Run() != nil {
			if time.Now().After(deadline) {
				t.Fatal("unrelated session did not start")
			}
			time.Sleep(30 * time.Millisecond)
		}
		pidFile := "/tmp/.lester-command-wrong-identity.pid"
		if out, err := exec.Command("docker", "exec", name, "sh", "-c", `read -r pid < "$1"; printf '%s 0\n' "$pid" > "$2"`, "fixture", protected, pidFile).CombinedOutput(); err != nil {
			t.Fatalf("write mismatched identity: %v: %s", err, out)
		}
		if exec.Command("docker", "exec", name, "sh", "-c", dockerCommandStop, "fixture", pidFile).Run() == nil {
			t.Fatal("cleanup incorrectly confirmed a process with mismatched identity")
		}
		if out, err := exec.Command("docker", "exec", name, "sh", "-c", `read -r pid < "$1"; kill -0 "$pid"`, "fixture", protected).CombinedOutput(); err != nil {
			t.Fatalf("unrelated session was signalled: %v: %s", err, out)
		}
	})
}

func TestDockerCommandWithoutBash(t *testing.T) {
	image := os.Getenv("LESTER_TEST_DOCKER_SH_IMAGE")
	if image == "" {
		t.Skip("set LESTER_TEST_DOCKER_SH_IMAGE to a local image without Bash")
	}
	id := uuid.NewString()
	name, _ := containerName(id)
	if out, err := exec.Command("docker", "run", "-d", "--name", name, "--network", "none", image, "sleep", "infinity").CombinedOutput(); err != nil {
		t.Fatalf("start fixture: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
	result, err := NewDockerProvider(image).Exec(context.Background(), id, Command{Command: "printf fallback"})
	if err != nil || result.Shell != "sh" || result.Status != "exited" || !result.ExitCodeKnown || result.ExitCode != 0 || result.Stdout != "fallback" {
		t.Fatalf("explicit sh fallback: %#v, %v", result, err)
	}
}
