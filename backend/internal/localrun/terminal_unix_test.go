//go:build !windows

package localrun

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type localTerminalCapture struct {
	mu   sync.Mutex
	data strings.Builder
}

func (c *localTerminalCapture) append(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = c.data.Write(data)
}

func (c *localTerminalCapture) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.data.String()
}

func waitForTerminalText(t *testing.T, capture *localTerminalCapture, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(capture.text(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("terminal never produced %q: %q", want, capture.text())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTerminalRealPTYResizeAndEnvironment(t *testing.T) {
	t.Setenv("LOCALRUN_TEST_SECRET", "private-runner-credential")
	manager := NewTerminalManager()
	t.Cleanup(manager.CloseAll)
	var capture localTerminalCapture
	exited := make(chan int, 1)
	id, err := manager.Open(context.Background(), t.TempDir(), 80, 24, capture.append, func(code int) { exited <- code })
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Resize(id, 101, 31); err != nil {
		t.Fatal(err)
	}
	if err := manager.Input(id, []byte("test -t 0 && printf 'PTY-%s\\n' yes; stty size; printf 'SECRET-%s\\n' \"${LOCALRUN_TEST_SECRET-unset}\"\n")); err != nil {
		t.Fatal(err)
	}
	waitForTerminalText(t, &capture, "PTY-yes")
	waitForTerminalText(t, &capture, "31 101")
	waitForTerminalText(t, &capture, "SECRET-unset")
	if err := manager.Input(id, []byte("exit 9\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-exited:
		if code != 9 {
			t.Fatalf("terminal exit code = %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("terminal exit callback missing")
	}
	if err := manager.Input(id, []byte("ignored")); err == nil {
		t.Fatal("input accepted for exited terminal")
	}
}

func TestTerminalCancellationReleasesOwnedChildren(t *testing.T) {
	manager := NewTerminalManager()
	t.Cleanup(manager.CloseAll)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var capture localTerminalCapture
	exited := make(chan int, 1)
	id, err := manager.Open(ctx, t.TempDir(), 80, 24, capture.append, func(code int) { exited <- code })
	if err != nil {
		t.Fatal(err)
	}
	// A background job has a separate process group under Bash job control.
	// Ignore its polite signals so cleanup must escalate for this owned group.
	if err := manager.Input(id, []byte("(trap '' HUP TERM; sleep 30) & printf 'CHILD=%s\\n' \"$!\"; wait\n")); err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`CHILD=(\d+)`)
	var pid int
	deadline := time.Now().Add(3 * time.Second)
	for pid == 0 {
		if match := pattern.FindStringSubmatch(capture.text()); len(match) == 2 {
			pid, _ = strconv.Atoi(match[1])
		}
		if time.Now().After(deadline) {
			t.Fatalf("terminal child did not start: %q", capture.text())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled terminal was not released")
	}
	waitForLocalProcessExit(t, pid)
	if err := manager.Resize(id, 80, 24); err == nil {
		t.Fatal("cancelled terminal remains registered")
	}
}

func TestTerminalCloseAllRejectsNewSessions(t *testing.T) {
	manager := NewTerminalManager()
	manager.CloseAll()
	if _, err := manager.Open(context.Background(), t.TempDir(), 80, 24, nil, nil); err == nil {
		t.Fatal("closed terminal manager accepted new session")
	}
}
