package toolboxfs

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCheckedWriteProcessWorker(t *testing.T) {
	root := os.Getenv("LESTER_TOOLBOX_CAS_WORKER_ROOT")
	if root == "" {
		return
	}
	fmt.Fprintln(os.Stdout, "ready")
	if _, err := bufio.NewReader(os.Stdin).ReadByte(); err != nil {
		t.Fatal(err)
	}
	_, err := WriteChecked(root, "shared", CheckedWriteRequest{Content: []byte(fmt.Sprintf("writer-%d", os.Getpid())), ExpectedSHA256: os.Getenv("LESTER_TOOLBOX_CAS_WORKER_HASH")})
	if err == nil {
		fmt.Fprintln(os.Stdout, "won")
	} else if strings.Contains(err.Error(), "changed") {
		fmt.Fprintln(os.Stdout, "conflict")
	} else {
		t.Fatal(err)
	}
}

func TestCheckedWriteLockSerializesIndependentHelperProcesses(t *testing.T) {
	root := t.TempDir()
	initial, err := Write(root, "shared", strings.NewReader("initial"), "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	type worker struct {
		command *exec.Cmd
		input   io.WriteCloser
		output  *bufio.Reader
	}
	workers := make([]worker, 0, 6)
	for index := 0; index < 6; index++ {
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCheckedWriteProcessWorker$")
		command.Env = append(os.Environ(), "LESTER_TOOLBOX_CAS_WORKER_ROOT="+root, "LESTER_TOOLBOX_CAS_WORKER_HASH="+initial.SHA256)
		input, _ := command.StdinPipe()
		output, _ := command.StdoutPipe()
		command.Stderr = os.Stderr
		if err = command.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if command.Process != nil {
				_ = command.Process.Kill()
			}
		})
		reader := bufio.NewReader(output)
		if ready, readErr := reader.ReadString('\n'); readErr != nil || ready != "ready\n" {
			t.Fatalf("worker did not become ready: %q %v", ready, readErr)
		}
		workers = append(workers, worker{command: command, input: input, output: reader})
	}
	for _, worker := range workers {
		_, _ = worker.input.Write([]byte{1})
		_ = worker.input.Close()
	}
	wins := 0
	for _, worker := range workers {
		output, _ := io.ReadAll(worker.output)
		if err := worker.command.Wait(); err != nil {
			t.Fatalf("worker failed: %v, %s", err, output)
		}
		if strings.Contains(string(output), "won\n") {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("independent process CAS winners=%d", wins)
	}
}

func TestConcurrentCheckedWritersAcceptOneObservedVersion(t *testing.T) {
	root := t.TempDir()
	initial, err := Write(root, "file.txt", strings.NewReader("initial"), "")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 20)
	var workers sync.WaitGroup
	for index := 0; index < 20; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, writeErr := WriteChecked(root, "file.txt", CheckedWriteRequest{Content: []byte("replacement"), ExpectedSHA256: initial.SHA256})
			results <- writeErr
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	winners := 0
	for writeErr := range results {
		if writeErr == nil {
			winners++
		} else if !strings.Contains(writeErr.Error(), "changed") {
			t.Fatalf("unexpected conditional-write failure: %v", writeErr)
		}
	}
	if winners != 1 {
		t.Fatalf("successful concurrent writers=%d, expected one", winners)
	}
	data, _ := os.ReadFile(filepath.Join(root, "file.txt"))
	if string(data) != "replacement" {
		t.Fatalf("file=%q", data)
	}
	leftovers, _ := filepath.Glob(filepath.Join(root, ".lester-write-*"))
	if len(leftovers) != 0 {
		t.Fatalf("leaked prepared files=%v", leftovers)
	}
}

func TestCreateOnlyCannotOverwriteExistingEmptyFile(t *testing.T) {
	root := t.TempDir()
	if _, err := WriteChecked(root, "empty", CheckedWriteRequest{Content: []byte{}, CreateOnly: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteChecked(root, "empty", CheckedWriteRequest{Content: []byte("lost"), CreateOnly: true}); err == nil {
		t.Fatal("existing empty file was overwritten")
	}
	data, _ := os.ReadFile(filepath.Join(root, "empty"))
	if len(data) != 0 {
		t.Fatal("create-only changed existing file")
	}
}

func TestReadPageDigestUsesOriginalBytesAndRemainsBounded(t *testing.T) {
	root := t.TempDir()
	content := []byte("first\r\n" + strings.Repeat("界", 2000) + "\r\n" + strings.Repeat("next\r\n", 5000))
	if err := os.WriteFile(filepath.Join(root, "file"), content, 0o640); err != nil {
		t.Fatal(err)
	}
	page, err := ReadLines(root, "file", 2, 2000)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if page.SHA256 != hex.EncodeToString(sum[:]) || page.ByteSize != int64(len(content)) || !page.EndsWithNewline {
		t.Fatalf("incorrect byte receipt=%+v", page)
	}
	if len(page.Lines) == 0 || !strings.HasSuffix(page.Lines[0].Text, "界") || page.Lines[0].OmittedCharacters != 1 {
		t.Fatal("original CR bytes / truncated line semantics changed")
	}
	encoded, _ := json.Marshal(page)
	if len(encoded) > 256<<10 {
		t.Fatalf("helper page exceeded transport cap: %d", len(encoded))
	}
	if page.TotalLines != 5002 {
		t.Fatalf("total lines=%d", page.TotalLines)
	}
}

func TestHelperStagedRequestIsRemovedOnFailure(t *testing.T) {
	root := t.TempDir()
	file, err := os.CreateTemp(os.TempDir(), ".lester-input-*.json")
	if err != nil {
		t.Fatal(err)
	}
	name := file.Name()
	defer os.Remove(name)
	_, _ = file.WriteString(`{"content":"","create_only":false}`)
	_ = file.Close()
	var result bytes.Buffer
	if err = Run([]string{"write-checked", "--root", root, "--path", "file", "--input-file", name}, strings.NewReader(""), &result); err == nil {
		t.Fatal("invalid write guard accepted")
	}
	if _, statErr := os.Stat(name); !os.IsNotExist(statErr) {
		t.Fatal("staged payload remained after failure")
	}
}

func TestNativeEditProtocolRejectsMissingAndNullReplacement(t *testing.T) {
	root := t.TempDir()
	if _, err := Write(root, "file", strings.NewReader("retain"), ""); err != nil {
		t.Fatal(err)
	}
	for _, request := range []string{`{"old_string":"retain"}`, `{"old_string":"retain","new_string":null}`} {
		var result bytes.Buffer
		if err := Run([]string{"edit", "--root", root, "--path", "file"}, strings.NewReader(request), &result); err == nil {
			t.Fatal("native missing/null replacement accepted")
		}
	}
	data, _ := os.ReadFile(filepath.Join(root, "file"))
	if string(data) != "retain" {
		t.Fatal("invalid native edit changed file")
	}
}

func TestReadLinesRejectsBinaryRatherThanInventingTextReceipt(t *testing.T) {
	for _, content := range [][]byte{{'x', 0, 'y'}, {0xff, 'x'}} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "binary"), content, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadLines(root, "binary", 1, 2); err == nil || !strings.Contains(err.Error(), "binary") {
			t.Fatalf("binary file accepted as text: %v", err)
		}
	}
}
