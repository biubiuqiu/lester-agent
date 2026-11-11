//go:build !windows

package localrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const commandOutputLimit = 256 << 10

type CommandResult struct {
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	TimedOut        bool   `json:"timed_out"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
}

// ExecuteCommand runs an approved local command. The working directory and
// environment are scoped, but this is deliberately not an OS sandbox.
func ExecuteCommand(ctx context.Context, root, command string, timeout time.Duration) (CommandResult, error) {
	if strings.TrimSpace(command) == "" || len(command) > 128<<10 || strings.ContainsRune(command, 0) {
		return CommandResult{ExitCode: -1}, errors.New("invalid local command")
	}
	return executeArgv(ctx, root, []string{"/bin/bash", "--noprofile", "--norc", "-c", command}, timeout)
}

func executeArgv(ctx context.Context, root string, argv []string, timeout time.Duration) (CommandResult, error) {
	result := CommandResult{ExitCode: -1}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(argv) == 0 || !filepath.IsAbs(argv[0]) {
		return result, errors.New("local executable must have an absolute path")
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd, cleanup, err := newLocalProcess(root, argv)
	if err != nil {
		return result, err
	}
	defer cleanup()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	stdout, stdoutWrite, err := os.Pipe()
	if err != nil {
		return result, fmt.Errorf("create command stdout: %w", err)
	}
	defer stdout.Close()
	defer stdoutWrite.Close()
	stderr, stderrWrite, err := os.Pipe()
	if err != nil {
		return result, fmt.Errorf("create command stderr: %w", err)
	}
	defer stderr.Close()
	defer stderrWrite.Close()
	cmd.Stdout, cmd.Stderr = stdoutWrite, stderrWrite
	if err := cmd.Start(); err != nil {
		return result, fmt.Errorf("start local command: %w", err)
	}
	_ = stdoutWrite.Close()
	_ = stderrWrite.Close()
	var out, errOut boundedCommandOutput
	var readers sync.WaitGroup
	readers.Add(2)
	go func() { defer readers.Done(); _, _ = io.Copy(&out, stdout) }()
	go func() { defer readers.Done(); _, _ = io.Copy(&errOut, stderr) }()
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	var waitErr, contextErr error
	groups := []int{cmd.Process.Pid}
	select {
	case waitErr = <-finished:
	case <-ctx.Done():
		contextErr = ctx.Err()
		groups = localProcessGroups(cmd.Process.Pid)
		signalLocalGroups(groups, syscall.SIGTERM)
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case waitErr = <-finished:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
			signalLocalGroups(groups, syscall.SIGKILL)
			waitErr = <-finished
		}
	}
	// Background children still in this newly created process group must not
	// outlive the foreground operation or retain its output pipes.
	signalLocalGroups(groups, syscall.SIGKILL)
	readDone := make(chan struct{})
	go func() { readers.Wait(); close(readDone) }()
	select {
	case <-readDone:
	case <-time.After(time.Second):
		// A deliberately detached child can escape process-group ownership.
		// Closing our descriptors bounds cleanup without claiming confinement.
		_ = stdout.Close()
		_ = stderr.Close()
		<-readDone
	}
	result.Stdout, result.StdoutTruncated = out.String(), out.truncated
	result.Stderr, result.StderrTruncated = errOut.String(), errOut.truncated
	result.ExitCode = localExitCode(cmd.ProcessState)
	result.TimedOut = errors.Is(contextErr, context.DeadlineExceeded)
	if contextErr != nil {
		return result, contextErr
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		return result, fmt.Errorf("wait for local command: %w", waitErr)
	}
	return result, nil
}

type boundedCommandOutput struct {
	buffer    bytes.Buffer
	truncated bool
}

func (b *boundedCommandOutput) String() string { return b.buffer.String() }

func (b *boundedCommandOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := max(0, commandOutputLimit-b.buffer.Len())
	if len(p) > remaining {
		b.truncated = true
		p = p[:remaining]
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}

func newLocalShell(root string, args ...string) (*exec.Cmd, func(), error) {
	return newLocalProcess(root, append([]string{"/bin/bash", "--noprofile", "--norc"}, args...))
}

func newLocalProcess(root string, argv []string) (*exec.Cmd, func(), error) {
	path, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve local working directory: %w", err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve local working directory: %w", err)
	}
	dir, err := os.OpenRoot(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open local working directory: %w", err)
	}
	_ = dir.Close()
	home, err := os.MkdirTemp("", "lester-local-home-")
	if err != nil {
		return nil, nil, fmt.Errorf("create local command environment: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(home) }
	if err := os.Mkdir(filepath.Join(home, "tmp"), 0700); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("create local temporary directory: %w", err)
	}
	// Never resolve a shell through the runner's potentially credential-bearing
	// environment, and never load user profiles or BASH_ENV.
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = path
	locale := "C.UTF-8"
	if runtime.GOOS == "darwin" {
		locale = "en_US.UTF-8"
	}
	cmd.Env = []string{
		"PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",
		"HOME=" + home,
		"TMPDIR=" + filepath.Join(home, "tmp"),
		"LANG=" + locale, "LC_ALL=" + locale, "TERM=xterm-256color",
		"HISTFILE=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
	}
	return cmd, cleanup, nil
}

func signalLocalGroup(pid int, signal syscall.Signal) {
	if pid > 1 {
		_ = syscall.Kill(-pid, signal)
	}
}

func signalLocalGroups(groups []int, signal syscall.Signal) {
	for _, group := range groups {
		signalLocalGroup(group, signal)
	}
}

// Interactive job control creates additional groups. Observe descendants
// before stopping their parent so their owned groups can be closed as well.
// Already detached/reparented processes require an OS sandbox to contain.
func localProcessGroups(pid int) []int {
	groups := []int{pid}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,ppid=,pgid=")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	var output boundedCommandOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return groups
	}
	type process struct{ pid, parent, group int }
	processes := make([]process, 0)
	for _, line := range strings.Split(output.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		p, e1 := strconv.Atoi(fields[0])
		parent, e2 := strconv.Atoi(fields[1])
		group, e3 := strconv.Atoi(fields[2])
		if e1 == nil && e2 == nil && e3 == nil {
			processes = append(processes, process{p, parent, group})
		}
	}
	owned := map[int]bool{pid: true}
	for changed := true; changed; {
		changed = false
		for _, p := range processes {
			if !owned[p.pid] && owned[p.parent] {
				owned[p.pid] = true
				changed = true
			}
		}
	}
	seen := map[int]bool{pid: true}
	for _, p := range processes {
		if owned[p.pid] && p.group > 1 && !seen[p.group] {
			seen[p.group] = true
			groups = append(groups, p.group)
		}
	}
	return groups
}

func localExitCode(state *os.ProcessState) int {
	if state == nil {
		return -1
	}
	if code := state.ExitCode(); code >= 0 {
		return code
	}
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return -1
}
