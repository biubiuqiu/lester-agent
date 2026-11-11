//go:build !windows

package localrun

import (
	"errors"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestProjectFSRejectsSpecialFiles(t *testing.T) {
	directory := t.TempDir()
	fs := newTestProjectFS(t, directory)
	if err := unix.Mkfifo(filepath.Join(directory, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Read("pipe"); !errors.Is(err, ErrPathDenied) {
		t.Fatalf("FIFO read: %v", err)
	}
	if _, err := fs.Write("pipe", "bytes", nil, true); !errors.Is(err, ErrPathDenied) {
		t.Fatalf("FIFO write: %v", err)
	}
	entries, err := fs.List(".")
	if err != nil || len(entries) != 0 {
		t.Fatalf("FIFO must be absent from ordinary file inventory: %#v, %v", entries, err)
	}
}
