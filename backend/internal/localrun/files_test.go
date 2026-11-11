package localrun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func newTestProjectFS(t *testing.T, directory string) *ProjectFS {
	t.Helper()
	fs, err := NewProjectFS(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	return fs
}

func TestProjectFSConfinesPathsAndSecrets(t *testing.T) {
	directory := t.TempDir()
	fs := newTestProjectFS(t, directory)
	if err := os.WriteFile(filepath.Join(directory, ".env"), []byte("TOKEN=private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, ".ssh", "key"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../secret", "nested/../../secret", "/etc/passwd", `C:\secret`, `nested\secret`, ".git/config", ".ssh/key", ".config/credentials"} {
		if _, err := fs.Read(name); !errors.Is(err, ErrPathDenied) {
			t.Errorf("Read(%q): %v", name, err)
		}
		if _, err := fs.Write(name, "change", nil, true); !errors.Is(err, ErrPathDenied) {
			t.Errorf("Write(%q): %v", name, err)
		}
	}
	if _, err := fs.Read(".env"); !errors.Is(err, ErrSensitivePath) {
		t.Fatalf("sensitive read: %v", err)
	}
	approved, err := fs.ReadApproved(".env")
	if err != nil || approved.Content != "TOKEN=private" {
		t.Fatalf("approved read = %#v, %v", approved, err)
	}
	if _, err := fs.ReadApproved(".ssh/key"); !errors.Is(err, ErrPathDenied) {
		t.Fatalf("approved read must still protect directories: %v", err)
	}
	if _, err := fs.Write(".env", "leak", &approved.SHA256, false); !errors.Is(err, ErrSensitivePath) {
		t.Fatalf("sensitive write: %v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"escape": outside, "alias": ".ssh", "env-alias": ".env"} {
		if err := os.Symlink(target, filepath.Join(directory, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"escape/secret", "alias/key", "env-alias"} {
		if _, err := fs.Read(name); !errors.Is(err, ErrPathDenied) {
			t.Errorf("symlink Read(%q): %v", name, err)
		}
		if _, err := fs.Write(name, "change", nil, true); !errors.Is(err, ErrPathDenied) {
			t.Errorf("symlink Write(%q): %v", name, err)
		}
	}
	content, err := os.ReadFile(filepath.Join(outside, "secret"))
	if err != nil || string(content) != "outside" {
		t.Fatalf("outside content changed: %q, %v", content, err)
	}
}

func TestProjectFSTextBounds(t *testing.T) {
	directory := t.TempDir()
	fs := newTestProjectFS(t, directory)
	for name, content := range map[string][]byte{
		"large": []byte(strings.Repeat("a", MaxLocalFileBytes+1)),
		"nul":   {'a', 0, 'b'},
		"utf8":  {0xff},
	} {
		if err := os.WriteFile(filepath.Join(directory, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
		want := ErrBinary
		if name == "large" {
			want = ErrTooLarge
		}
		if _, err := fs.Read(name); !errors.Is(err, want) {
			t.Errorf("Read(%q): %v", name, err)
		}
		if _, err := fs.Write("new-"+name, string(content), nil, true); !errors.Is(err, want) {
			t.Errorf("Write(%q): %v", name, err)
		}
	}
	content, err := fs.Write("empty", "", nil, true)
	if err != nil || content.Size != 0 || len(content.SHA256) != 64 {
		t.Fatalf("empty file: %#v, %v", content, err)
	}
}

func TestProjectFSConditionalWriteAndEdit(t *testing.T) {
	directory := t.TempDir()
	fs := newTestProjectFS(t, directory)
	first, err := fs.Write("file.txt", "one one\n", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, createOnly := range []bool{false, true} {
		if _, err := fs.Write("file.txt", "lost update", nil, createOnly); !errors.Is(err, ErrConflict) {
			t.Fatalf("unchecked overwrite: %v", err)
		}
	}
	badSHA := strings.Repeat("0", 64)
	if _, err := fs.Write("file.txt", "lost update", &badSHA, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale write: %v", err)
	}
	if _, err := fs.Edit("file.txt", "one", "two", first.SHA256, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("ambiguous edit: %v", err)
	}
	second, err := fs.Edit("file.txt", "one", "two", first.SHA256, true)
	if err != nil || second.Content != "two two\n" {
		t.Fatalf("replace all: %#v, %v", second, err)
	}
	if _, err := fs.Edit("file.txt", "two", "three", first.SHA256, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale edit: %v", err)
	}
	third, err := fs.Write("file.txt", "final\n", &second.SHA256, false)
	if err != nil || third.Content != "final\n" {
		t.Fatalf("checked write: %#v, %v", third, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "file.txt" {
		t.Fatalf("temporary files leaked: %v, %v", entries, err)
	}
}

func TestProjectFSConcurrentConditionalWrites(t *testing.T) {
	directory := t.TempDir()
	firstFS := newTestProjectFS(t, directory)
	secondFS := newTestProjectFS(t, directory)
	initial, err := firstFS.Write("file", "initial", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	results := make(chan error, 2)
	for index, fs := range []*ProjectFS{firstFS, secondFS} {
		group.Add(1)
		go func(index int, fs *ProjectFS) {
			defer group.Done()
			_, err := fs.Write("file", fmt.Sprintf("write-%d", index), &initial.SHA256, false)
			results <- err
		}(index, fs)
	}
	group.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}

func TestProjectFSDirectoryPagination(t *testing.T) {
	directory := t.TempDir()
	fs := newTestProjectFS(t, directory)
	for index := 502; index >= 0; index-- {
		if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("file-%03d", index)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{".env", ".ENV.production", ".lester-write-hidden"} {
		if err := os.WriteFile(filepath.Join(directory, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(directory, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	first, err := fs.ListPage(".", "", 500)
	if err != nil || len(first.Entries) != 500 || !first.Truncated || first.NextCursor != "file-499" {
		t.Fatalf("first page entries=%d cursor=%q truncated=%v: %v", len(first.Entries), first.NextCursor, first.Truncated, err)
	}
	second, err := fs.ListPage("", first.NextCursor, 500)
	if err != nil || len(second.Entries) != 3 || second.Truncated || second.NextCursor != "" {
		t.Fatalf("second page: %#v, %v", second, err)
	}
	if second.Entries[0].Path != "file-500" || second.Entries[2].Path != "file-502" {
		t.Fatalf("unexpected ordered paths: %#v", second.Entries)
	}
	if _, err := fs.ListPage(".", "", 501); err == nil {
		t.Fatal("accepted an oversized page")
	}
}

func TestProjectFSGitDiffOmitsSecretsAndCannotRunFilters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git diff uses the Unix runner")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	directory := t.TempDir()
	fs := newTestProjectFS(t, directory)
	gitCommand := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = directory
		cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v, %s", args, err, output)
		}
	}
	gitCommand("init", "--quiet")
	for _, name := range []string{"visible.txt", ".env", ".ENV.production"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("before\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gitCommand("add", "--", ".")
	if err := os.WriteFile(filepath.Join(directory, ".gitattributes"), []byte("*.txt filter=unsafe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand("config", "filter.unsafe.clean", "touch FILTER_EXECUTED; cat")
	for _, name := range []string{"visible.txt", ".env", ".ENV.production"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("after\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	diff, err := fs.GitDiff(context.Background())
	if err != nil || !strings.Contains(diff, "+after") || !strings.Contains(diff, "visible.txt") {
		t.Fatalf("diff=%q: %v", diff, err)
	}
	if strings.Contains(strings.ToLower(diff), ".env") {
		t.Fatalf("secret path leaked: %s", diff)
	}
	if _, err := os.Stat(filepath.Join(directory, "FILTER_EXECUTED")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Git clean filter ran: %v", err)
	}
}
