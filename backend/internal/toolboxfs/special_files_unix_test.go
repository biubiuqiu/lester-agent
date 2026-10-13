//go:build !windows

package toolboxfs

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadRejectsNamedPipeWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "pipe")
	if err := unix.Mkfifo(name, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := ReadLines(root, "pipe", 1, 1); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("named pipe accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("opening a named pipe blocked the file reader")
	}
}

func TestScopedOpenRejectsLinkChangedAfterResolution(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	rootReal, resolved, err := resolveExisting(root, "file")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(resolved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), resolved); err != nil {
		t.Fatal(err)
	}
	if file, err := openRegularFile(rootReal, resolved); err == nil {
		file.Close()
		t.Fatal("path check/open race escaped scoped root")
	}
}
