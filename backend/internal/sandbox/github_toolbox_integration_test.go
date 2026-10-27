package sandbox

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestDockerGitHubToolboxImportsEditsTestsAndExportsWithoutCredentials(t *testing.T) {
	image, toolbox := os.Getenv("LESTER_TEST_DOCKER_IMAGE"), os.Getenv("LESTER_TEST_GITHUB_TOOLBOX")
	if image == "" || toolbox == "" {
		t.Skip("set LESTER_TEST_DOCKER_IMAGE and LESTER_TEST_GITHUB_TOOLBOX for isolated Docker GitHub transfer")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	p := NewDockerProvider(image)
	p.ToolboxSourcePath = toolbox
	id := "github-code-" + uuid.NewString()
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		_ = p.Destroy(cleanup, id)
		volume, _ := volumeName(id)
		_ = exec.CommandContext(cleanup, "docker", "volume", "rm", volume).Run()
	})
	if _, err := p.Create(ctx, CreateOptions{ID: id, CPUs: "1", Memory: "256m"}); err != nil {
		t.Fatal(err)
	}
	workDir := "/workspace/conversations/" + uuid.NewString()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, body := range map[string]string{"README.md": "original code\n", "test.sh": "[ \"$(cat README.md)\" = 'updated code' ]\n", ".gitignore": "generated.txt\n", "generated.txt": "tracked source despite ignore\n"} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	checkout := uuid.NewString()
	zipPath := ".lester/github/" + checkout + ".zip"
	if err := p.WriteFile(ctx, id, workDir, zipPath, archive.Bytes()); err != nil {
		t.Fatal(err)
	}
	run := func(command string) *CommandResult {
		t.Helper()
		result, err := p.Exec(ctx, id, Command{Command: command, WorkDir: workDir, TimeoutSeconds: 30})
		if err != nil || result.ExitCode != 0 {
			t.Fatalf("trusted GitHub task command failed: %#v %v", result, err)
		}
		return result
	}

	name, _ := containerName(id)
	native := func(script string) {
		t.Helper()
		if output, err := exec.CommandContext(ctx, "docker", "exec", name, "/bin/sh", "-c", script).CombinedOutput(); err != nil {
			t.Fatalf("native fixture setup: %v %s", err, output)
		}
	}
	profiles := func(enable bool) {
		t.Helper()
		if enable {
			native(`printf 'touch /tmp/github-profile-ran\n' > "$HOME/.bash_profile"; cp "$HOME/.bash_profile" "$HOME/.profile"`)
		} else {
			native(`rm -f "$HOME/.bash_profile" "$HOME/.profile"`)
		}
	}
	profiles(true)
	imported, err := p.GitHubTransfer(ctx, id, workDir, GitHubTransferRequest{Operation: "import", Directory: "repo", Archive: zipPath, CheckoutID: checkout, BaseSHA: strings.Repeat("a", 40)})
	if err != nil || imported == nil || imported.ExitCode != 0 {
		t.Fatalf("native import: %#v %v", imported, err)
	}
	native("test ! -e /tmp/github-profile-ran")
	profiles(false)
	var receipt struct {
		Directory string `json:"directory"`
		LocalHead string `json:"local_head"`
		FileCount int    `json:"file_count"`
	}
	if json.Unmarshal([]byte(imported.Stdout), &receipt) != nil || receipt.Directory != "repo" || len(receipt.LocalHead) != 40 || receipt.FileCount != 4 {
		t.Fatal("invalid real import receipt", imported.Stdout)
	}
	// The imported repo is usable by ordinary commands, with the source baseline
	// tracked even when a source .gitignore matches one of its original files.
	run("cd repo && test -z \"$(git remote)\" && git ls-files --error-unmatch generated.txt && printf 'updated code\\n' > README.md && sh test.sh && git diff --exit-code --quiet " + receipt.LocalHead + " -- test.sh")
	proposal := uuid.NewString()
	manifest := ".lester/github/" + proposal + "-paths.json"
	output := ".lester/github/" + proposal + ".json"
	if err := p.WriteFile(ctx, id, workDir, manifest, []byte(`{"files":["README.md"],"delete_files":[]}`)); err != nil {
		t.Fatal(err)
	}

	profiles(true)
	exported, err := p.GitHubTransfer(ctx, id, workDir, GitHubTransferRequest{Operation: "export", Directory: "repo", Manifest: manifest, Output: output})
	if err != nil || exported == nil || exported.ExitCode != 0 {
		t.Fatalf("native export: %#v %v", exported, err)
	}
	native("test ! -e /tmp/github-profile-ran")
	profiles(false)
	data, err := p.ReadFile(ctx, id, workDir, output)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Files []struct{ Path, Content, Mode string }
	}
	if json.Unmarshal(data, &snapshot) != nil || len(snapshot.Files) != 1 || snapshot.Files[0].Path != "README.md" || snapshot.Files[0].Mode != "100644" {
		t.Fatal("invalid exported change snapshot")
	}
	decoded, err := base64.StdEncoding.DecodeString(snapshot.Files[0].Content)
	if err != nil || string(decoded) != "updated code\n" {
		t.Fatal("real edit did not reach exported immutable bytes", err)
	}
	// No authenticated remote or connector credential is needed inside Computer.
	run("cd repo && test -z \"$(git remote)\" && ! git config --local --get credential.helper")
}
