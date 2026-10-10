package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Opt in with the built Lester image; ordinary unit tests need no Docker.
func TestDockerRuntimeWorkspace(t *testing.T) {
	image := os.Getenv("LESTER_TEST_RUNTIME_IMAGE")
	if image == "" {
		t.Skip("set LESTER_TEST_RUNTIME_IMAGE to the built sandbox runtime")
	}
	ctx := context.Background()
	id := uuid.NewString()
	name, _ := containerName(id)
	volume, _ := volumeName(id)
	helper := name + "-helper"
	docker := func(args ...string) {
		t.Helper()
		if output, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
			t.Fatalf("docker %v: %v: %s", args, err, output)
		}
	}
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", name, helper).Run()
		_ = exec.Command("docker", "volume", "rm", volume).Run()
	})
	// Exercise Sandbox Service's helper installation into a non-root image.
	docker("create", "--name", helper, image)
	source := filepath.Join(t.TempDir(), "lester-toolbox")
	docker("cp", helper+":"+toolboxContainerPath, source)
	docker("rm", helper)
	provider := NewDockerProvider(image)
	provider.ToolboxSourcePath = source
	if _, err := provider.Create(ctx, CreateOptions{ID: id}); err != nil {
		t.Fatal(err)
	}
	result, err := provider.Exec(ctx, id, Command{WorkDir: "/workspace", Command: "id -u && mkdir -p conversations/fixture && go version && pnpm --version"})
	if err != nil || result.ExitCode != 0 || !strings.HasPrefix(result.Stdout, "1000\n") {
		t.Fatalf("non-root workspace initialization: result=%+v error=%v", result, err)
	}
	dir := "/workspace/conversations/fixture"
	if err := provider.WriteFile(ctx, id, dir, "notes.txt", []byte("preserved workspace")); err != nil {
		t.Fatal(err)
	}
	// Recreating only the container must retain the user's volume and access.
	if err := provider.Destroy(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Create(ctx, CreateOptions{ID: id}); err != nil {
		t.Fatal(err)
	}
	contents, err := provider.ReadFile(ctx, id, dir, "notes.txt")
	if err != nil || string(contents) != "preserved workspace" {
		t.Fatalf("workspace recovery: contents=%q error=%v", contents, err)
	}
}
