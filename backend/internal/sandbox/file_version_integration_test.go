package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestDockerVersionedFileOperations(t *testing.T) {
	image := os.Getenv("LESTER_TEST_DOCKER_FILES_IMAGE")
	if image == "" {
		t.Skip("set LESTER_TEST_DOCKER_FILES_IMAGE to exercise current helper on real Docker")
	}
	ctx := context.Background()
	helper := filepath.Join(t.TempDir(), "lester-toolbox")
	build := exec.Command("go", "build", "-o", helper, "./cmd/lester-toolbox")
	build.Dir = "../.."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build current helper: %v: %s", err, output)
	}
	id := uuid.NewString()
	name, _ := containerName(id)
	volume, _ := volumeName(id)
	provider := NewDockerProvider(image)
	provider.ToolboxSourcePath = helper
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", name).Run()
		_ = exec.Command("docker", "volume", "rm", volume).Run()
	})
	if _, err := provider.Create(ctx, CreateOptions{ID: id}); err != nil {
		t.Fatal(err)
	}
	workDir := "/workspace/conversations/files-fixture"
	created, err := provider.WriteFileVersioned(ctx, id, workDir, "notes.txt", FileWriteRequest{Content: []byte("first\r\nsecond\r\n"), CreateOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	page, err := provider.ReadFileLines(ctx, id, workDir, "notes.txt", 2, 1)
	if err != nil || page.SHA256 != created.SHA256 || len(page.Lines) != 1 || page.Lines[0].Text != "second\r" {
		t.Fatalf("real page=%+v error=%v", page, err)
	}
	if _, err := provider.WriteFileVersioned(ctx, id, workDir, "notes.txt", FileWriteRequest{Content: []byte("overwrite"), CreateOnly: true}); err == nil {
		t.Fatal("real create-only overwrote existing file")
	}
	start := make(chan struct{})
	errors := make(chan error, 6)
	var tasks sync.WaitGroup
	for index := 0; index < 6; index++ {
		tasks.Add(1)
		go func(index int) {
			defer tasks.Done()
			<-start
			_, err := provider.WriteFileVersioned(ctx, id, workDir, "notes.txt", FileWriteRequest{Content: []byte(fmt.Sprintf("replacement %d", index)), ExpectedSHA256: created.SHA256})
			errors <- err
		}(index)
	}
	close(start)
	tasks.Wait()
	close(errors)
	successes := 0
	for err := range errors {
		if err == nil {
			successes++
		} else if !strings.Contains(err.Error(), "changed") {
			t.Fatalf("unexpected real CAS error=%v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("real Docker CAS successes=%d", successes)
	}
	if _, err := provider.EditFile(ctx, id, workDir, "notes.txt", FileEditRequest{OldString: "replacement", NewString: "updated", ExpectedSHA256: created.SHA256}); err == nil {
		t.Fatal("real stale edit was accepted")
	}
	result, err := provider.Exec(ctx, id, Command{WorkDir: workDir, Command: "ln -s /etc/passwd escape.txt"})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("symlink setup=%+v %v", result, err)
	}
	if _, err := provider.ReadFileLines(ctx, id, workDir, "escape.txt", 1, 2); err == nil {
		t.Fatal("real read escaped scope through symlink")
	}
}
