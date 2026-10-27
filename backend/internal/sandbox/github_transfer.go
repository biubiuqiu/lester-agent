package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// GitHubTransferRequest is a narrow, native file protocol. It cannot carry an
// arbitrary command, executable, shell, environment or working directory.
type GitHubTransferRequest struct {
	Operation  string `json:"operation"`
	Directory  string `json:"directory"`
	Archive    string `json:"archive,omitempty"`
	CheckoutID string `json:"checkout_id,omitempty"`
	BaseSHA    string `json:"base_sha,omitempty"`
	Manifest   string `json:"manifest,omitempty"`
	Output     string `json:"output,omitempty"`
}

// GitHubTransferer never falls back to Exec: user-controlled shell startup files
// must not run during a read-only publication preview.
type GitHubTransferer interface {
	GitHubTransfer(context.Context, string, string, GitHubTransferRequest) (*CommandResult, error)
}

var githubSourceSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
var githubDirectory = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,239}$`)

func githubTransferArguments(workDir string, request GitHubTransferRequest) ([]string, error) {
	root, err := safeWorkDir(workDir)
	if err != nil || root != workDir || !strings.HasPrefix(root, "/workspace/conversations/") || !githubDirectory.MatchString(request.Directory) {
		return nil, errors.New("invalid GitHub transfer scope")
	}
	args := []string{"github-" + request.Operation, "--root=" + root, "--directory=" + request.Directory}
	switch request.Operation {
	case "import":
		id, err := uuid.Parse(request.CheckoutID)
		if err != nil || id.String() != request.CheckoutID || !githubSourceSHA.MatchString(request.BaseSHA) || request.Archive != ".lester/github/"+request.CheckoutID+".zip" || request.Manifest != "" || request.Output != "" {
			return nil, errors.New("invalid GitHub import request")
		}
		return append(args, "--archive="+request.Archive, "--checkout-id="+request.CheckoutID, "--base-sha="+request.BaseSHA), nil
	case "export":
		name := strings.TrimSuffix(strings.TrimPrefix(request.Output, ".lester/github/"), ".json")
		id, err := uuid.Parse(name)
		if err != nil || id.String() != name || request.Output != ".lester/github/"+name+".json" || request.Manifest != ".lester/github/"+name+"-paths.json" || request.Archive != "" || request.CheckoutID != "" || request.BaseSHA != "" {
			return nil, errors.New("invalid GitHub export request")
		}
		return append(args, "--manifest="+request.Manifest, "--output="+request.Output), nil
	default:
		return nil, errors.New("unsupported GitHub transfer operation")
	}
}

func (p *DockerProvider) GitHubTransfer(ctx context.Context, id, workDir string, request GitHubTransferRequest) (*CommandResult, error) {
	args, err := githubTransferArguments(workDir, request)
	if err != nil {
		return nil, err
	}
	name, err := containerName(id)
	if err != nil {
		return nil, err
	}
	if err = p.ensureToolbox(ctx, id); err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	// Direct argv execution never resolves a user PATH or starts a shell. The
	// static helper sanitizes Git's environment independently on import.
	command := exec.CommandContext(runCtx, "docker", append([]string{"exec", name, toolboxContainerPath}, args...)...)
	command.WaitDelay = time.Second
	stdout, stderr := newBoundedCapture(64<<10), newBoundedCapture(64<<10)
	command.Stdout, command.Stderr = stdout, stderr
	started := time.Now()
	err = command.Run()
	result := &CommandResult{ExitCode: -1, Shell: "native", Status: "unknown", Stdout: stdout.String(), Stderr: stderr.String(), DurationMS: time.Since(started).Milliseconds(), StdoutTruncated: stdout.Truncated(), StderrTruncated: stderr.Truncated()}
	if command.ProcessState != nil && runCtx.Err() == nil {
		result.ExitCode, result.ExitCodeKnown, result.Status = command.ProcessState.ExitCode(), true, "exited"
	}
	if stdout.Truncated() {
		return result, errors.New("GitHub transfer response exceeds protocol bounds")
	}
	return result, err
}

func (p *ACSProvider) GitHubTransfer(ctx context.Context, id, workDir string, request GitHubTransferRequest) (*CommandResult, error) {
	args, err := githubTransferArguments(workDir, request)
	if err != nil {
		return nil, err
	}
	client, err := p.runtime(ctx, id)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	stdout, stderr := newBoundedCapture(64<<10), newBoundedCapture(64<<10)
	started := time.Now()
	result, err := runACSToolbox(runCtx, client, args, func(value string) { _, _ = stdout.Write([]byte(value)) }, func(value string) { _, _ = stderr.Write([]byte(value)) })
	response := &CommandResult{ExitCode: -1, Shell: "native", Status: "unknown", DurationMS: time.Since(started).Milliseconds()}
	if result != nil {
		if stdout.String() == "" {
			_, _ = stdout.Write([]byte(result.Stdout))
		}
		if stderr.String() == "" {
			_, _ = stderr.Write([]byte(result.Stderr))
		}
		if err == nil && runCtx.Err() == nil {
			response.ExitCode, response.ExitCodeKnown, response.Status = result.ExitCode, true, "exited"
		}
	}
	response.Stdout, response.Stderr = stdout.String(), stderr.String()
	response.StdoutTruncated, response.StderrTruncated = stdout.Truncated(), stderr.Truncated()
	if stdout.Truncated() {
		return response, errors.New("GitHub transfer response exceeds protocol bounds")
	}
	return response, err
}

func (c *Client) GitHubTransfer(ctx context.Context, id, workDir string, request GitHubTransferRequest) (*CommandResult, error) {
	if _, err := githubTransferArguments(workDir, request); err != nil {
		return nil, err
	}
	var result CommandResult
	err := c.json(ctx, http.MethodPost, "/v1/sandboxes/"+url.PathEscape(id)+"/github-transfer?work_dir="+url.QueryEscape(workDir), request, &result)
	return &result, err
}

func (h *ServiceHandler) githubTransfer(w http.ResponseWriter, r *http.Request) {
	var request GitHubTransferRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		httpapi.Error(w, 400, errors.New("invalid GitHub transfer request"))
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		httpapi.Error(w, 400, errors.New("expected one GitHub transfer request"))
		return
	}
	workDir := r.URL.Query().Get("work_dir")
	if _, err := githubTransferArguments(workDir, request); err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	provider, ok := h.provider.(GitHubTransferer)
	if !ok {
		httpapi.Error(w, http.StatusNotImplemented, errors.New("provider requires native GitHub transfer support"))
		return
	}
	result, err := provider.GitHubTransfer(r.Context(), chi.URLParam(r, "id"), workDir, request)
	if err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, result)
}
