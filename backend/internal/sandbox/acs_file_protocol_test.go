package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/toolboxfs"
)

// This exercises the real native helper protocol behind a deterministic ACS
// transport fixture. It is not a claim of connectivity to a deployed ACS.
type helperACSRuntime struct {
	fakeACSRuntime
	root   string
	staged []string
}

func (r *helperACSRuntime) Run(_ context.Context, command, _ string, _, _ func(string)) (*acsCommandResult, error) {
	if strings.HasSuffix(command, " version") {
		return &acsCommandResult{Stdout: toolboxfs.ProtocolVersion + "\n"}, nil
	}
	quoted := regexp.MustCompile("'([^']*)'").FindAllStringSubmatch(command, -1)
	var arguments []string
	if strings.Contains(command, " prepare-input --name ") {
		arguments = []string{"prepare-input", "--name", quoted[len(quoted)-1][1]}
	} else {
		for _, value := range quoted[1:] {
			arguments = append(arguments, value[1])
		}
		for index := 0; index < len(arguments)-1; index++ {
			if arguments[index] == "--root" || arguments[index] == "--path" {
				arguments[index+1] = filepath.Join(r.root, strings.TrimPrefix(arguments[index+1], "/"))
				index++
			}
		}
	}
	var output bytes.Buffer
	if err := toolboxfs.Run(arguments, strings.NewReader(""), &output); err != nil {
		return &acsCommandResult{ExitCode: 1, Stderr: err.Error()}, nil
	}
	return &acsCommandResult{Stdout: output.String()}, nil
}

func (r *helperACSRuntime) Write(_ context.Context, target string, data []byte) error {
	if !strings.HasPrefix(target, "/tmp/.lester-input-") {
		return fmt.Errorf("unexpected unsafe native write to %s", target)
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("staged input is not private: %o", info.Mode().Perm())
	}
	r.staged = append(r.staged, target)
	return os.WriteFile(target, data, 0o644) // Existing 0600 is preserved, as by native Write.
}

func (r *helperACSRuntime) Remove(_ context.Context, target string) error { return os.Remove(target) }

func TestACSFilesUseNativeHelperForAtomicWriteReadAndVersionCheck(t *testing.T) {
	runtime := &helperACSRuntime{root: t.TempDir()}
	provider := &ACSProvider{config: normalizeACSConfig(ACSConfig{}), backend: &fakeACSBackend{createdID: "fixture", runtime: runtime}}
	ctx := context.Background()
	directory := "/workspace/conversations/c1"
	if err := provider.WriteFile(ctx, "fixture", directory, "notes", []byte("first\r\nsecond\r\n")); err != nil {
		t.Fatal(err)
	}
	page, err := provider.ReadFileLines(ctx, "fixture", directory, "notes", 2, 1)
	if err != nil || page.SHA256 != sha256String([]byte("first\r\nsecond\r\n")) || len(page.Lines) != 1 || page.Lines[0].Text != "second\r" {
		t.Fatalf("page=%+v error=%v", page, err)
	}
	if runtime.readPath != "" {
		t.Fatal("paged read loaded the whole file through native Read")
	}
	edited, err := provider.EditFile(ctx, "fixture", directory, "notes", FileEditRequest{OldString: "second", NewString: "changed", ExpectedSHA256: page.SHA256})
	if err != nil || !edited.OK {
		t.Fatalf("edit=%+v error=%v", edited, err)
	}
	if _, err := provider.WriteFileVersioned(ctx, "fixture", directory, "notes", FileWriteRequest{Content: []byte("stale"), ExpectedSHA256: page.SHA256}); err == nil {
		t.Fatal("ACS stale write was accepted")
	}
	if _, err := provider.WriteFileVersioned(ctx, "fixture", directory, "notes", FileWriteRequest{Content: []byte("exists"), CreateOnly: true}); err == nil {
		t.Fatal("ACS existing file overwritten without observation")
	}
	if err := provider.WriteFile(ctx, "fixture", directory, "empty", nil); err != nil {
		t.Fatal("legacy nil/empty upload changed semantics:", err)
	}
	for _, staged := range runtime.staged {
		if _, err := os.Stat(staged); !os.IsNotExist(err) {
			t.Fatalf("staged input left behind: %s", staged)
		}
	}
}

func TestACSLegacyRuntimeFailsBeforeProtectedMutation(t *testing.T) {
	runtime := &fakeACSRuntime{helperVersion: "1"}
	provider := &ACSProvider{config: normalizeACSConfig(ACSConfig{}), backend: &fakeACSBackend{createdID: "fixture", runtime: runtime}}
	_, err := provider.WriteFileVersioned(context.Background(), "fixture", "/workspace/conversations/c1", "notes", FileWriteRequest{Content: []byte("new"), CreateOnly: true})
	if err == nil || !strings.Contains(err.Error(), "protocol 2") {
		t.Fatalf("legacy runtime was not clearly rejected: %v", err)
	}
}
