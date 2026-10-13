package commandtask

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTaskWorkerProcess(t *testing.T) {
	if os.Getenv("LESTER_TASK_WORKER_TEST") != "1" {
		return
	}
	index := 0
	for i, arg := range os.Args {
		if arg == "--" {
			index = i + 1
			break
		}
	}
	if index == 0 {
		os.Exit(2)
	}
	if RunCLI(os.Args[index:], os.Stdin, os.Stdout) != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func taskFixture(t *testing.T, command string, maxBytes int64) *Snapshot {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("native task ownership requires Linux")
	}
	t.Setenv("LESTER_TASK_WORKER_TEST", "1")
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	r := Request{TaskID: uuid.NewString(), RunID: uuid.NewString(), ConversationID: uuid.NewString(), OwnershipToken: uuid.NewString(), WorkDir: t.TempDir(), Command: command, MaxOutputBytes: maxBytes}
	s, e := Start(r, exe, "-test.run=^TestTaskWorkerProcess$", "--")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if !s.Terminal() {
			_, _ = Stop(s.Handle)
		}
	})
	return s
}

func waitFixture(t *testing.T, s *Snapshot) *Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	done, e := Wait(ctx, s.Handle, func(_ context.Context, h Handle, o ReadOptions) (*Snapshot, error) { return Inspect(h, o) })
	if e != nil {
		t.Fatal(e)
	}
	return done
}

func TestRawTaskOutputStableAcrossPagesAndNoRelaunch(t *testing.T) {
	payload := strings.Repeat("多语言🙂\n", 2000)
	s := taskFixture(t, "printf '%s' '"+payload+"'; set -o pipefail; false | true", DefaultLogLimit)
	s = waitFixture(t, s)
	if s.Status != "exited" || s.Shell != "bash" || s.ExitCode == nil || *s.ExitCode != 1 || s.Truncated || s.StoredBytes != int64(len(payload)) {
		t.Fatalf("task status: %#v", s)
	}
	var joined []byte
	var offset int64
	for {
		page, e := Inspect(s.Handle, ReadOptions{Offset: offset, Limit: 257})
		if e != nil {
			t.Fatal(e)
		}
		joined = append(joined, page.Page.Data...)
		if !page.Page.HasMore {
			break
		}
		offset = page.Page.NextOffset
	}
	if !bytes.Equal(joined, []byte(payload)) {
		t.Fatal("raw multilingual bytes changed across pages")
	}
	root, e := openTask(s.Handle, false)
	if e != nil {
		t.Fatal(e)
	}
	var req Request
	e = readJSON(root, "request.json", &req)
	root.Close()
	if e != nil {
		t.Fatal(e)
	}
	exe, _ := os.Executable()
	again, e := Start(req, exe, "-test.run=^TestTaskWorkerProcess$", "--")
	if e != nil || again.StoredBytes != s.StoredBytes || again.Handle.PID != 0 || again.Status != "unknown" {
		t.Fatalf("existing task was relaunched: %#v %v", again, e)
	}
}

func TestTaskRetentionLimitIsExplicit(t *testing.T) {
	payload := strings.Repeat("x", 15000)
	s := waitFixture(t, taskFixture(t, "printf '%s' '"+payload+"'", 4096))
	if s.Status != "exited" || !s.Truncated || s.StoredBytes != 4096 || s.OmittedBytes != int64(len(payload)-4096) {
		t.Fatalf("retention result: %#v", s)
	}
	data, e := os.ReadFile(filepath.Join(taskDirectory(s.Handle), "output.log"))
	if e != nil || len(data) != 4096 {
		t.Fatalf("retained log: %d, %v", len(data), e)
	}
}

func TestManagedStopOwnsDetachedDescendants(t *testing.T) {
	s := taskFixture(t, `setsid bash -c 'trap "" TERM; sleep 30' & printf '%s\n' "$!" > child.pid; printf 'PARTIAL'; wait`, DefaultLogLimit)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, e := os.Stat(filepath.Join(s.Handle.WorkDir, "child.pid")); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	stopped, e := Stop(s.Handle)
	if e != nil || stopped.Status != "cancelled" || stopped.CleanupConfirmed == nil || !*stopped.CleanupConfirmed || stopped.ExitCode != nil {
		t.Fatalf("stop result: %#v %v", stopped, e)
	}
	pidBytes, _ := os.ReadFile(filepath.Join(s.Handle.WorkDir, "child.pid"))
	pid, _ := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if info, e := processInfo(pid); e == nil && info.State != "Z" {
		t.Fatal("detached child survived stop")
	}
	foreign := s.Handle
	foreign.OwnershipToken = uuid.NewString()
	if _, e := Stop(foreign); e == nil {
		t.Fatal("forged ownership token was accepted")
	}
}

func TestTaskLogCannotExposePrivateRecordThroughLink(t *testing.T) {
	s := waitFixture(t, taskFixture(t, "printf done", DefaultLogLimit))
	log := filepath.Join(taskDirectory(s.Handle), "output.log")
	if e := os.Remove(log); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink("request.json", log); e != nil {
		t.Fatal(e)
	}
	if _, e := Inspect(s.Handle, ReadOptions{}); e == nil {
		t.Fatal("private request was exposed through a log symlink")
	}
}
