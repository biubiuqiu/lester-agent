package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// A dropped ACS stream detaches the observer, rather than killing the remote
// process. Exercise the provider's real shell/ownership scripts with that
// transport behavior, without ACS credentials or an external service.
type localACSCommandRuntime struct {
	fakeACSRuntime
	dir string
}

type observedCommandOutput struct {
	mu       sync.Mutex
	data     bytes.Buffer
	callback func(string)
}

func (o *observedCommandOutput) Write(data []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.callback != nil {
		o.callback(string(data))
	}
	return o.data.Write(data)
}

func (o *observedCommandOutput) detach() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.callback = nil
	return o.data.String()
}

func (r *localACSCommandRuntime) Run(ctx context.Context, command, _ string, stdout, stderr func(string)) (*acsCommandResult, error) {
	// Map the sandbox root into the isolated fixture; GitHub runners do not
	// have a writable /workspace, and tests must never create one on the host.
	command = strings.ReplaceAll(command, shellQuote("/workspace"), shellQuote(r.dir))
	cmd := exec.Command("bash", "-lc", command)
	cmd.Dir = r.dir
	out, errOut := &observedCommandOutput{callback: stdout}, &observedCommandOutput{callback: stderr}
	cmd.Stdout, cmd.Stderr = out, errOut
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		out.detach()
		errOut.detach()
		return nil, ctx.Err()
	case err := <-done:
		result := &acsCommandResult{Shell: "bash", Stdout: out.detach(), Stderr: errOut.detach()}
		if cmd.ProcessState != nil {
			result.ExitCode = cmd.ProcessState.ExitCode()
		}
		return result, err
	}
}

func TestACSCommandShellAndDetachedProcessCleanup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("owned process inspection requires Linux procfs")
	}
	for _, command := range []string{"bash", "setsid"} {
		if _, err := exec.LookPath(command); err != nil {
			t.Skipf("%s unavailable", command)
		}
	}
	dir := t.TempDir()
	runtimeClient := &localACSCommandRuntime{dir: dir}
	provider := &ACSProvider{config: normalizeACSConfig(ACSConfig{}), backend: &fakeACSBackend{runtime: runtimeClient}}
	result, err := provider.Exec(context.Background(), "test", Command{Command: `items=(first second); printf '%s\n' "${items[1]}"; set -o pipefail; false | true`})
	if err != nil || result.Shell != "bash" || result.Status != "exited" || !result.ExitCodeKnown || result.ExitCode != 1 || strings.TrimSpace(result.Stdout) != "second" {
		t.Fatalf("Bash arrays/pipefail: %#v, %v", result, err)
	}
	result, err = provider.Exec(context.Background(), "test", Command{Command: `printf '%s\n' "$$" > parent.pid; setsid bash -c 'trap "" TERM; sleep 30' & printf '%s\n' "$!" > child.pid; printf 'ACS_PARTIAL_OUTPUT\n'; wait`, TimeoutSeconds: 1})
	if err != nil || result.Status != "timed_out" || result.ExitCodeKnown || result.CleanupConfirmed == nil || !*result.CleanupConfirmed || !strings.Contains(result.Stdout, "ACS_PARTIAL_OUTPUT") {
		t.Fatalf("ACS stream timeout: %#v, %v", result, err)
	}
	for _, name := range []string{"parent.pid", "child.pid"} {
		pid, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%s/stat", strings.TrimSpace(string(pid))))
		if err == nil {
			fields := strings.Fields(string(stat)[strings.LastIndex(string(stat), ") ")+2:])
			if fields[0] != "Z" {
				t.Fatalf("ACS command %s survived timeout: %s", pid, stat)
			}
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}
