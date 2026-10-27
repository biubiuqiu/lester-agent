package githubsandbox

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

type zipItem struct {
	name string
	data []byte
	mode os.FileMode
}

func putArchive(t *testing.T, root string, items ...zipItem) ImportRequest {
	t.Helper()
	id := uuid.NewString()
	directory := filepath.Join(root, ".lester", "github")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, item := range items {
		h := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
		if item.mode != 0 {
			h.SetMode(item.mode)
		}
		file, err := writer.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write(item.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	relative := ".lester/github/" + id + ".zip"
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(relative)), buffer.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return ImportRequest{Root: root, Archive: relative, Directory: "repo", CheckoutID: id, BaseSHA: strings.Repeat("a", 40)}
}
func requireRuntime(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux Computer runtime")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("test requires Git", err)
	}
}
func gitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("/usr/bin/git", append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "-C", root}, args...)...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL="}
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal("Git fixture failed", err, string(data))
	}
	return strings.TrimSpace(string(data))
}
func putManifest(t *testing.T, root string, manifest Manifest) (string, string) {
	t.Helper()
	id := uuid.NewString()
	data, _ := json.Marshal(manifest)
	name := ".lester/github/" + id + "-paths.json"
	if err := os.MkdirAll(filepath.Join(root, ".lester/github"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return name, ".lester/github/" + id + ".json"
}

func TestImportMakesAnOfflineGitBaselineAndExportsOnlySelectedChanges(t *testing.T) {
	requireRuntime(t)
	root := t.TempDir()
	maliciousHome := t.TempDir()
	marker := filepath.Join(t.TempDir(), "hook-ran")
	if err := os.WriteFile(filepath.Join(maliciousHome, ".gitconfig"), []byte("[init]\n templateDir = /must-not-load\n[core]\n hooksPath = /must-not-run\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", maliciousHome)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	t.Setenv("GIT_CONFIG_VALUE_0", marker)
	t.Setenv("GIT_INDEX_FILE", "/must-not-use")
	t.Setenv("GITHUB_TOKEN", "fixture-secret-must-not-enter-the-repository")
	request := putArchive(t, root, zipItem{"main.go", []byte("package main\nfunc main() {}\n"), 0o644}, zipItem{".gitignore", []byte("tracked.txt\n"), 0o644}, zipItem{"tracked.txt", []byte("tracked source\n"), 0o644}, zipItem{"scripts/test.sh", []byte("#!/bin/sh\nexit 0\n"), 0o755})
	result, err := Import(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo")
	if result.Directory != "repo" || result.FileCount != 4 || !shaPattern.MatchString(result.LocalHead) {
		t.Fatal("invalid import receipt", result)
	}
	if gitOutput(t, repo, "rev-parse", "HEAD") != result.LocalHead {
		t.Fatal("wrong baseline head")
	}
	if gitOutput(t, repo, "status", "--porcelain") != "" {
		t.Fatal("snapshot started with a dirty worktree")
	}
	if !strings.Contains(gitOutput(t, repo, "ls-files"), "tracked.txt") {
		t.Fatal("snapshot omitted a provider-tracked ignored file")
	}
	config, err := os.ReadFile(filepath.Join(repo, ".git/config"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "remote") || strings.Contains(string(config), "fixture-secret") || strings.Contains(string(config), "must-not") {
		t.Fatal("import stored external configuration or credentials")
	}
	receipt, err := os.ReadFile(filepath.Join(repo, ".git/lester-checkout.json"))
	if err != nil || !bytes.Contains(receipt, []byte(request.CheckoutID)) {
		t.Fatal("missing local checkout marker", err)
	}
	info, err := os.Stat(filepath.Join(repo, "scripts/test.sh"))
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatal("executable mode not preserved", err)
	}
	if err = os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\nfunc main() { println(\"changed\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(repo, "tracked.txt")); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(repo, "new.txt"), []byte("selected addition"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(repo, "private.txt"), []byte("unselected data"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, output := putManifest(t, root, Manifest{Files: []string{"new.txt", "main.go"}, DeleteFiles: []string{"tracked.txt"}})
	var stdout bytes.Buffer
	if err = Run([]string{"github-export", "--root=" + root, "--directory=repo", "--manifest=" + manifest, "--output=" + output}, &stdout); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "selected addition") || strings.Contains(stdout.String(), "package main") || strings.Contains(stdout.String(), "c2VsZWN0ZWQ") {
		t.Fatal("CLI printed private source bytes")
	}
	var exported ExportResult
	payload, err := os.ReadFile(filepath.Join(root, output))
	if err != nil || json.Unmarshal(payload, &exported) != nil {
		t.Fatal("invalid export", err)
	}
	if len(exported.Files) != 2 || exported.Files[0].Path != "main.go" || exported.Files[0].Mode != "100644" || string(exported.Files[1].Content) != "selected addition" || len(exported.DeleteFiles) != 1 || exported.DeleteFiles[0] != "tracked.txt" {
		t.Fatal("wrong export selection", exported)
	}
	if bytes.Contains(payload, []byte("private.txt")) {
		t.Fatal("export included an unselected file")
	}
	if _, err = Export(root, "repo", manifest, output); err == nil {
		t.Fatal("overwrote an existing immutable export")
	}
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("a Git hook was executed")
	}
	if _, err = Import(t.Context(), request); err == nil {
		t.Fatal("overwrote an existing repository")
	}
	if got := gitOutput(t, repo, "diff", "--", "main.go"); !strings.Contains(got, "changed") {
		t.Fatal("import retry lost local edits")
	}
}

func TestImportRejectsUnsafeArchivePathsAndLeavesNoDestination(t *testing.T) {
	requireRuntime(t)
	for _, name := range []string{"../escape", "/absolute", "nested/../../escape", "a\\b", ".git/config", "src/.Git/hooks/post-checkout", ".agent/secret", ".lester/state.json", "node_modules/package.js", ".venv/activate", "a//b", "a/./b", "a:\\drive", "bad\nname"} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			root := t.TempDir()
			req := putArchive(t, root, zipItem{name, []byte("private content"), 0o644})
			if _, err := Import(t.Context(), req); err == nil {
				t.Fatal("accepted unsafe archive path", name)
			}
			if _, err := os.Stat(filepath.Join(root, "repo")); !os.IsNotExist(err) {
				t.Fatal("invalid archive created the destination")
			}
		})
	}
	for _, items := range [][]zipItem{{{"a", nil, 0o644}, {"a", nil, 0o644}}, {{"a", nil, 0o644}, {"a/b", nil, 0o644}}, {{"link", []byte("/etc/passwd"), os.ModeSymlink | 0o777}}, {{"fifo", nil, os.ModeNamedPipe | 0o600}}} {
		root := t.TempDir()
		if _, err := Import(t.Context(), putArchive(t, root, items...)); err == nil {
			t.Fatal("accepted duplicate, conflicting or special entries")
		}
	}
}

func TestImportRejectsLargeArchivesAndAtomicDestinationRaces(t *testing.T) {
	requireRuntime(t)
	root := t.TempDir()
	req := putArchive(t, root, zipItem{"oversize", bytes.Repeat([]byte("x"), MaxImportBytes+1), 0o644})
	if _, err := Import(t.Context(), req); err == nil {
		t.Fatal("accepted oversized uncompressed content")
	}
	many := make([]zipItem, MaxFiles+1)
	for i := range many {
		many[i] = zipItem{fmt.Sprintf("file-%d", i), nil, 0o644}
	}
	req = putArchive(t, root, many...)
	if _, err := Import(t.Context(), req); err == nil {
		t.Fatal("accepted excess file count")
	}
	req = putArchive(t, root, zipItem{"file", []byte("source"), 0o644})
	var workers sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		workers.Add(1)
		go func() { defer workers.Done(); _, err := Import(context.Background(), req); results <- err }()
	}
	workers.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatal("atomic import allowed more than one owner", successes)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".lester-github-import-") {
			t.Fatal("failed import left staging content")
		}
	}
}

func TestExportRejectsTraversalSymlinksUnverifiedDeletionsAndLargeSelections(t *testing.T) {
	requireRuntime(t)
	root := t.TempDir()
	if _, err := Import(t.Context(), putArchive(t, root, zipItem{"file.txt", []byte("source"), 0o644})); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "private"), []byte("private data"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo")
	if err := os.Symlink(outside, filepath.Join(repo, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "private"), filepath.Join(repo, "linked-file")); err != nil {
		t.Fatal(err)
	}
	for _, selection := range []Manifest{{Files: []string{"../.lester/github/private"}}, {Files: []string{".git/config"}}, {Files: []string{"linked/private"}}, {Files: []string{"linked-file"}}, {DeleteFiles: []string{"linked/missing"}}, {DeleteFiles: []string{"file.txt"}}, {Files: []string{"file.txt"}, DeleteFiles: []string{"file.txt"}}} {
		manifest, output := putManifest(t, root, selection)
		if _, err := Export(root, "repo", manifest, output); err == nil {
			t.Fatal("accepted unsafe export", selection)
		}
		if _, err := os.Stat(filepath.Join(root, output)); !os.IsNotExist(err) {
			t.Fatal("failed export created an immutable receipt")
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "large"), bytes.Repeat([]byte("x"), MaxExportBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, output := putManifest(t, root, Manifest{Files: []string{"large"}})
	if _, err := Export(root, "repo", manifest, output); err == nil {
		t.Fatal("accepted a large export")
	}
	tooMany := make([]string, MaxChanges+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("new-%d", i)
	}
	manifest, output = putManifest(t, root, Manifest{DeleteFiles: tooMany})
	if _, err := Export(root, "repo", manifest, output); err == nil {
		t.Fatal("accepted too many deletions")
	}
	manifest, output = putManifest(t, root, Manifest{DeleteFiles: []string{"missing-dir/file.txt"}})
	if _, err := Export(root, "repo", manifest, output); err != nil {
		t.Fatal("missing ancestor should verify an absent deletion", err)
	}
}

func TestTransferRefusesSymlinkedRootAndPrivateControlDirectories(t *testing.T) {
	requireRuntime(t)
	realRoot := t.TempDir()
	req := putArchive(t, realRoot, zipItem{"file", []byte("source"), 0o644})
	linkRoot := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Fatal(err)
	}
	req.Root = linkRoot
	if _, err := Import(t.Context(), req); err == nil {
		t.Fatal("accepted a symbolic conversation root")
	}
	req.Root = realRoot
	if err := os.Rename(filepath.Join(realRoot, ".lester"), filepath.Join(realRoot, "private")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(realRoot, "private"), filepath.Join(realRoot, ".lester")); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(t.Context(), req); err == nil {
		t.Fatal("accepted symbolic private control directories")
	}
}

func TestCLIImportAndExportUsePrivateReceiptsAndRejectMismatchedOperations(t *testing.T) {
	requireRuntime(t)
	root := t.TempDir()
	req := putArchive(t, root, zipItem{"readme.txt", []byte("source"), 0o644})
	req.Directory = "repositories/project"
	var stdout bytes.Buffer
	if err := Run([]string{"github-import", "--root=" + root, "--archive=" + req.Archive, "--directory=" + req.Directory, "--checkout-id=" + req.CheckoutID, "--base-sha=" + req.BaseSHA}, &stdout); err != nil {
		t.Fatal(err)
	}
	var result ImportResult
	if json.Unmarshal(stdout.Bytes(), &result) != nil || result.Directory != req.Directory || result.FileCount != 1 || !shaPattern.MatchString(result.LocalHead) {
		t.Fatal("invalid CLI import receipt", stdout.String())
	}
	if strings.Contains(stdout.String(), "source") {
		t.Fatal("import printed source content")
	}
	manifest, output := putManifest(t, root, Manifest{Files: []string{"readme.txt"}})
	for _, args := range [][]string{
		{"github-export", "--root=" + root, "--directory=" + req.Directory, "--manifest=" + manifest, "--output=.lester/github/" + uuid.NewString() + ".json"},
		{"github-export", "--root=" + root, "--directory=" + req.Directory, "--manifest=" + manifest, "--output=overwrite.txt"},
		{"github-export", "--root=" + root, "--directory=" + req.Directory, "--manifest=" + manifest, "--output=" + output, "unexpected"},
		{"github-import", "--root=" + root, "--archive=" + req.Archive, "--directory=other", "--checkout-id=" + uuid.NewString(), "--base-sha=" + req.BaseSHA},
		{"github-import", "--root=" + root, "--archive=" + req.Archive, "--directory=other", "--checkout-id=" + req.CheckoutID, "--base-sha=unknown"},
	} {
		if err := Run(args, &bytes.Buffer{}); err == nil {
			t.Fatal("accepted mismatched/unsafe CLI options", args)
		}
	}
	private := filepath.Join(root, ".lester/github")
	if err := os.Symlink(filepath.Join(root, "repositories/project/readme.txt"), filepath.Join(private, filepath.Base(output))); err != nil {
		t.Fatal(err)
	}
	if _, err := Export(root, req.Directory, manifest, output); err == nil {
		t.Fatal("followed or overwrote an existing output symlink")
	}
	if content, err := os.ReadFile(filepath.Join(root, "repositories/project/readme.txt")); err != nil || string(content) != "source" {
		t.Fatal("private output replaced source", err)
	}
}
