package sandbox

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/google/uuid"
)

// Exercise Bash/readline through a real PTY, rather than mocking keystrokes.
func TestInteractiveTerminal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local Unix PTY unavailable; use Docker integration checks")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("Bash unavailable")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "completion-target.txt"), []byte("TAB_COMPLETED\n"), 0600); err != nil {
		t.Fatal(err)
	}
	open := func() TerminalSession {
		command := exec.Command("sh", terminalShellArgs()...)
		command.Dir = dir
		command.Env = append(os.Environ(), "HOME="+dir)
		file, err := pty.Start(command)
		if err != nil {
			t.Fatal(err)
		}
		go func() { _ = command.Wait() }()
		return &dockerTerminal{command: command, file: file}
	}
	exerciseTerminal(t, open)
	// A second conversation using the same account home must not import history.
	other := t.TempDir()
	command := exec.Command("sh", terminalShellArgs()...)
	command.Dir = other
	command.Env = append(os.Environ(), "HOME="+dir)
	file, err := pty.Start(command)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = command.Wait() }()
	session := &dockerTerminal{command: command, file: file}
	defer session.Close()
	output := captureTerminal(session)
	output.wait(t, "lester", 5*time.Second)
	_, err = session.Write([]byte("history; printf '%s%s\\n' ISOLATION _DONE\r"))
	if err != nil {
		t.Fatal(err)
	}
	if text := output.wait(t, "ISOLATION_DONE", 5*time.Second); strings.Contains(text, "PERSISTED") {
		t.Fatalf("another conversation imported history: %q", text)
	}
}

func TestTerminalWithoutBash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local Unix PTY unavailable")
	}
	dir := t.TempDir()
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh unavailable")
	}
	if err := os.Symlink(shell, filepath.Join(dir, "sh")); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(shell, terminalShellArgs()...)
	command.Dir = dir
	command.Env = append(os.Environ(), "PATH="+dir, "ENV=")
	file, err := pty.Start(command)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = command.Wait() }()
	session := &dockerTerminal{command: command, file: file}
	defer session.Close()
	output := captureTerminal(session)
	output.wait(t, "未安装 Bash", 5*time.Second)
	if _, err := session.Write([]byte("printf '%s%s\\n' FALLBACK _WORKS\r")); err != nil {
		t.Fatal(err)
	}
	output.wait(t, "FALLBACK_WORKS", 5*time.Second)
}

// Opt in for Docker-dependent integration checks; normal CI remains portable.
func TestDockerInteractiveTerminal(t *testing.T) {
	image := os.Getenv("LESTER_TEST_DOCKER_IMAGE")
	if image == "" {
		t.Skip("set LESTER_TEST_DOCKER_IMAGE to a locally available Bash image")
	}
	id := uuid.NewString()
	name, _ := containerName(id)
	runDocker := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
			t.Fatalf("docker %v: %v: %s", args, err, out)
		}
	}
	runDocker("run", "-d", "--name", name, "--network", "none", "--pids-limit", "256", image, "sleep", "infinity")
	defer exec.Command("docker", "rm", "-f", name).Run()
	dir := "/workspace/conversations/" + id
	runDocker("exec", name, "mkdir", "-p", dir)
	command := exec.Command("docker", "exec", "-i", name, "sh", "-c", `cat > "$1/completion-target.txt"`, "fixture", dir)
	command.Stdin = strings.NewReader("TAB_COMPLETED\n")
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("write fixture: %v %s", err, out)
	}
	provider := NewDockerProvider(image)
	open := func() TerminalSession {
		session, err := provider.OpenTerminal(context.Background(), id, dir)
		if err != nil {
			t.Fatal(err)
		}
		return session
	}
	exerciseTerminal(t, open)
	// Closing an attached Docker CLI must also terminate the remote shell,
	// including a foreground command; CLI detachment alone leaks processes.
	assertCloses := func(input string) {
		t.Helper()
		attached := open()
		defer attached.Close()
		capture := captureTerminal(attached)
		capture.wait(t, "lester", 5*time.Second)
		dockerSession := attached.(*dockerTerminal)
		marker, err := exec.Command("docker", "exec", name, "cat", dockerSession.pidFile).Output()
		if err != nil {
			t.Fatal(err)
		}
		pid := strings.Fields(string(marker))[0]
		if _, err := attached.Write([]byte(input)); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(input, "sleep") {
			capture.wait(t, "sleep 30", 5*time.Second)
		}
		if err := attached.Close(); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			state, _ := exec.Command("docker", "exec", name, "sh", "-c", `if IFS= read -r stat 2>/dev/null < "/proc/$1/stat"; then rest=${stat##*) }; printf '%s' "${rest%% *}"; fi`, "fixture", pid).Output()
			if len(state) == 0 || string(state) == "Z" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("remote Shell %s remains alive after close (%s)", pid, state)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	assertCloses("sleep 30\r")
	assertCloses("\x1b")
	// Also close before Bash has emitted a prompt, covering startup cancellation.
	early := open()
	if err := early.Close(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("LESTER_TEST_DOCKER_COMPLETION") == "1" {
		session := open()
		defer session.Close()
		output := captureTerminal(session)
		output.wait(t, "lester", 5*time.Second)
		if _, err := session.Write([]byte("git --ver\t\r")); err != nil {
			t.Fatal(err)
		}
		output.wait(t, "git version", 5*time.Second)
		if _, err := session.Write([]byte("less completion-target.txt\r")); err != nil {
			t.Fatal(err)
		}
		output.wait(t, "TAB_COMPLETED", 5*time.Second)
		if _, err := session.Write([]byte("q")); err != nil {
			t.Fatal(err)
		}
		output.wait(t, "lester", 5*time.Second)
		if _, err := session.Write([]byte("printf '%s%s\\n' FULLSCREEN _EXIT\r")); err != nil {
			t.Fatal(err)
		}
		output.wait(t, "FULLSCREEN_EXIT", 5*time.Second)
	}
}

func exerciseTerminal(t *testing.T, open func() TerminalSession) {
	t.Helper()
	session := open()
	defer session.Close()
	output := captureTerminal(session)
	output.wait(t, "lester", 10*time.Second)
	send := func(data string) {
		t.Helper()
		if _, err := session.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	send("cat completion-t\t\r")
	output.wait(t, "TAB_COMPLETED", 5*time.Second)
	send("test -t 0 && test -t 1 && printf '%s%s\\n' REAL _TTY\r")
	output.wait(t, "REAL_TTY", 5*time.Second)
	if err := session.Resize(context.Background(), 93, 37); err != nil {
		t.Fatal(err)
	}
	// Docker CLI forwards SIGWINCH to the daemon asynchronously. Observe
	// the remote TTY size rather than racing the resize RPC with one command.
	for attempts := 0; attempts < 20; attempts++ {
		send("stty size\r")
		time.Sleep(100 * time.Millisecond)
		output.mu.Lock()
		resized := strings.Contains(output.data, "37 93")
		output.mu.Unlock()
		if resized {
			break
		}
	}
	output.wait(t, "37 93", 5*time.Second)
	send("printf '%s%s\\n' HISTORY _RECALLED\r")
	output.wait(t, "HISTORY_RECALLED", 5*time.Second)
	send("\x1b[A\r")
	output.wait(t, "HISTORY_RECALLED", 5*time.Second)
	send("\x12HISTORY\r")
	output.wait(t, "HISTORY_RECALLED", 5*time.Second)
	send(" HIST_IGNORED=lester_private_history_fixture; printf '%s%s\\n' SPACE _IGNORED\r")
	output.wait(t, "SPACE_IGNORED", 5*time.Second)
	send("history | grep -F \"$HIST_IGNORED\" >/dev/null || printf '%s%s\\n' HISTORY _PRIVATE\r")
	output.wait(t, "HISTORY_PRIVATE", 5*time.Second)
	send("sleep 30\r")
	output.wait(t, "sleep 30", 5*time.Second)
	send("\x03printf '%s%s\\n' INTERRUPT _OK\r")
	output.wait(t, "INTERRUPT_OK", 5*time.Second)
	// Bracketed multiline paste must wait for explicit Enter.
	send("\x1b[200~printf '%s%s\\n' PASTE _SAFE\n\x1b[201~")
	pasted := output.wait(t, "PASTE _SAFE", 5*time.Second)
	if strings.Contains(pasted, "PASTE_SAFE") {
		t.Fatal("paste executed without Enter")
	}
	send("\r")
	output.wait(t, "PASTE_SAFE", 5*time.Second)
	send("printf '%s%s\\n' PERSISTED _HISTORY\r")
	output.wait(t, "PERSISTED_HISTORY", 5*time.Second)
	send("\x04")
	select {
	case <-output.done:
	case <-time.After(5 * time.Second):
		t.Fatal("Ctrl+D did not exit the shell")
	}
	session.Close()
	session = open()
	defer session.Close()
	output = captureTerminal(session)
	output.wait(t, "lester", 10*time.Second)
	if _, err := session.Write([]byte("\x1b[A\r")); err != nil {
		t.Fatal(err)
	}
	output.wait(t, "PERSISTED_HISTORY", 5*time.Second)
}

type terminalCapture struct {
	mu   sync.Mutex
	data string
	done chan struct{}
}

func captureTerminal(session io.Reader) *terminalCapture {
	capture := &terminalCapture{done: make(chan struct{})}
	go func() {
		defer close(capture.done)
		buffer := make([]byte, 4096)
		for {
			n, err := session.Read(buffer)
			capture.mu.Lock()
			capture.data += string(buffer[:n])
			capture.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return capture
}
func (c *terminalCapture) take() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	data := c.data
	c.data = ""
	return data
}
func (c *terminalCapture) wait(t *testing.T, text string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		found := strings.Contains(c.data, text)
		data := c.data
		if found {
			c.data = ""
		}
		c.mu.Unlock()
		if found {
			return data
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("terminal did not produce %q: %q", text, c.take())
	return ""
}
