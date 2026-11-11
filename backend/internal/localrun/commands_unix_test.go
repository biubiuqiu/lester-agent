//go:build !windows

package localrun

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCommandResultAndCredentialIsolation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOCALRUN_TEST_SECRET", "private-runner-credential")
	t.Setenv("BASH_ENV", filepath.Join(root, "injected-profile"))
	if err := os.WriteFile(filepath.Join(root, "injected-profile"), []byte("printf profile-loaded"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := ExecuteCommand(context.Background(), root, `printf '%s\n' "$PWD"; printf '%s' "${LOCALRUN_TEST_SECRET-unset}"; printf issue >&2; exit 7`, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != wantRoot+"\nunset" || result.Stderr != "issue" || result.ExitCode != 7 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestCommandCapsEachOutputStream(t *testing.T) {
	result, err := ExecuteCommand(context.Background(), t.TempDir(), `head -c 300000 /dev/zero | tr '\000' o; head -c 300000 /dev/zero | tr '\000' e >&2`, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Stdout) != commandOutputLimit || len(result.Stderr) != commandOutputLimit || !result.StdoutTruncated || !result.StderrTruncated {
		t.Fatalf("output limits: stdout=%d stderr=%d truncated=%t/%t", len(result.Stdout), len(result.Stderr), result.StdoutTruncated, result.StderrTruncated)
	}
	if strings.Trim(result.Stdout, "o") != "" || strings.Trim(result.Stderr, "e") != "" {
		t.Fatal("captured bytes changed")
	}
}

func TestCommandTimeoutStopsOwnedProcessGroup(t *testing.T) {
	unrelated := exec.Command("/bin/sleep", "30")
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() })
	started := time.Now()
	result, err := ExecuteCommand(context.Background(), t.TempDir(), `trap '' TERM; (trap '' TERM; sleep 30) & printf '%s %s\n' "$$" "$!"; wait`, 120*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || !result.TimedOut || time.Since(started) > 3*time.Second {
		t.Fatalf("timeout result: %#v, %v (elapsed %v)", result, err, time.Since(started))
	}
	for _, field := range strings.Fields(result.Stdout) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			t.Fatal(err)
		}
		waitForLocalProcessExit(t, pid)
	}
	if err := unrelated.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("unrelated process was affected: %v", err)
	}
}

func TestCommandCleansBackgroundChildren(t *testing.T) {
	result, err := ExecuteCommand(context.Background(), t.TempDir(), `sleep 30 & printf '%s\n' "$!"`, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(result.Stdout))
	if err != nil {
		t.Fatal(err)
	}
	waitForLocalProcessExit(t, pid)
}

func TestCommandArgvDoesNotInterpretShell(t *testing.T) {
	result, err := executeArgv(context.Background(), t.TempDir(), []string{"/bin/echo", "$(printf injected);*"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "$(printf injected);*\n" {
		t.Fatalf("argv interpreted: %q", result.Stdout)
	}
}

func waitForLocalProcessExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for localProcessAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("local child process %d survived cleanup", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func localProcessAlive(pid int) bool {
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if os.IsNotExist(err) {
			return false
		}
		if err == nil {
			// A container's PID 1 may defer reaping an already dead orphan.
			end := strings.LastIndex(string(data), ") ")
			if end >= 0 && strings.HasPrefix(string(data[end+2:]), "Z ") {
				return false
			}
		}
	}
	return syscall.Kill(pid, syscall.Signal(0)) == nil
}
