package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	pathpkg "path"
	"strings"
	"sync"
	"time"
)

var ErrSandboxNotFound = errors.New("sandbox not found")

const (
	defaultToolboxSourcePath = "/usr/local/libexec/lester-toolbox"
	toolboxContainerPath     = "/usr/local/bin/lester-toolbox"
)

type DockerProvider struct {
	Image             string
	ToolboxSourcePath string
	toolboxMu         sync.Mutex
	toolboxInstalled  map[string]bool
}

func NewDockerProvider(image string) *DockerProvider {
	if image == "" {
		image = "python:3.12-slim"
	}
	return &DockerProvider{Image: image, ToolboxSourcePath: defaultToolboxSourcePath, toolboxInstalled: map[string]bool{}}
}

func containerName(id string) (string, error) {
	if id == "" || strings.ContainsAny(id, "/\\ ") {
		return "", errors.New("invalid sandbox id")
	}
	return "lester-sandbox-" + id, nil
}

func volumeName(id string) (string, error) {
	name, err := containerName(id)
	return name + "-workspace", err
}

func (p *DockerProvider) Create(ctx context.Context, opts CreateOptions) (*Sandbox, error) {
	name, err := containerName(opts.ID)
	if err != nil {
		return nil, err
	}
	volume, _ := volumeName(opts.ID)
	image := opts.Image
	if image == "" {
		image = p.Image
	}
	cpus := opts.CPUs
	if cpus == "" {
		cpus = "2"
	}
	memory := opts.Memory
	if memory == "" {
		memory = "4g"
	}
	if output, createErr := exec.CommandContext(ctx, "docker", "volume", "create", volume).CombinedOutput(); createErr != nil {
		return nil, fmt.Errorf("create docker workspace volume: %w: %s", createErr, output)
	}
	if _, inspectErr := p.Inspect(ctx, opts.ID); errors.Is(inspectErr, ErrSandboxNotFound) {
		cmd := exec.CommandContext(ctx, "docker", "create", "--name", name, "--network", "none", "--cpus", cpus, "--memory", memory, "--pids-limit", "256", "--security-opt", "no-new-privileges", "-v", volume+":/workspace", "-w", "/workspace", image, "sleep", "infinity")
		if output, createErr := cmd.CombinedOutput(); createErr != nil {
			return nil, fmt.Errorf("create docker sandbox: %w: %s", createErr, output)
		}
	} else if inspectErr != nil {
		return nil, inspectErr
	}
	if err = p.Start(ctx, opts.ID); err != nil {
		return nil, err
	}
	return p.Inspect(ctx, opts.ID)
}

func (p *DockerProvider) Inspect(ctx context.Context, id string) (*Sandbox, error) {
	name, err := containerName(id)
	if err != nil {
		return nil, err
	}
	output, inspectErr := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{json .State}}", name).CombinedOutput()
	if inspectErr != nil {
		if strings.Contains(strings.ToLower(string(output)), "no such") {
			return nil, ErrSandboxNotFound
		}
		return nil, fmt.Errorf("inspect docker sandbox: %w: %s", inspectErr, output)
	}
	var state struct {
		Status  string `json:"Status"`
		Running bool   `json:"Running"`
		Error   string `json:"Error"`
		Health  *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	}
	if err = json.Unmarshal(output, &state); err != nil {
		return nil, fmt.Errorf("decode docker sandbox state: %w", err)
	}
	status := "stopped"
	if state.Running {
		status = "running"
	}
	if state.Health != nil && state.Health.Status == "unhealthy" {
		status = "unhealthy"
	}
	return &Sandbox{ID: id, ProviderRef: name, Status: status, LastActiveAt: time.Now()}, nil
}

func (p *DockerProvider) Start(ctx context.Context, id string) error {
	name, err := containerName(id)
	if err != nil {
		return err
	}
	if output, startErr := exec.CommandContext(ctx, "docker", "start", name).CombinedOutput(); startErr != nil {
		return fmt.Errorf("start docker sandbox: %w: %s", startErr, output)
	}
	return p.ensureToolbox(ctx, id)
}

func (p *DockerProvider) Suspend(ctx context.Context, id string) error {
	name, err := containerName(id)
	if err != nil {
		return err
	}
	if output, stopErr := exec.CommandContext(ctx, "docker", "stop", "-t", "5", name).CombinedOutput(); stopErr != nil {
		return fmt.Errorf("suspend docker sandbox: %w: %s", stopErr, output)
	}
	return nil
}

func (p *DockerProvider) Resume(ctx context.Context, id string) error { return p.Start(ctx, id) }

func (p *DockerProvider) Destroy(ctx context.Context, id string) error {
	name, err := containerName(id)
	if err != nil {
		return err
	}
	if output, destroyErr := exec.CommandContext(ctx, "docker", "rm", "-f", name).CombinedOutput(); destroyErr != nil {
		return fmt.Errorf("destroy docker sandbox: %w: %s", destroyErr, output)
	}
	p.toolboxMu.Lock()
	delete(p.toolboxInstalled, id)
	p.toolboxMu.Unlock()
	return nil
}

func (p *DockerProvider) Exec(ctx context.Context, id string, command Command) (*CommandResult, error) {
	name, err := containerName(id)
	if err != nil {
		return nil, err
	}
	workDir, err := safeWorkDir(command.WorkDir)
	if err != nil {
		return nil, err
	}
	if err = exec.CommandContext(ctx, "docker", "exec", name, "mkdir", "-p", workDir).Run(); err != nil {
		return nil, fmt.Errorf("prepare sandbox work directory: %w", err)
	}
	timeoutSeconds := command.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 120
	}
	if timeoutSeconds > 600 {
		timeoutSeconds = 600
	}
	timeout := time.Duration(timeoutSeconds) * time.Second
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	cmd := exec.CommandContext(runCtx, "docker", "exec", "-w", workDir, name, "sh", "-lc", command.Command)
	stdout, stderr := newBoundedCapture(commandCaptureLimit), newBoundedCapture(commandCaptureLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, err
		}
	}
	return &CommandResult{ExitCode: exitCode, Stdout: stdout.String(), Stderr: stderr.String(), DurationMS: time.Since(start).Milliseconds(), StdoutTruncated: stdout.Truncated(), StderrTruncated: stderr.Truncated(), StdoutOmittedBytes: stdout.OmittedBytes(), StderrOmittedBytes: stderr.OmittedBytes()}, nil
}

func (p *DockerProvider) ListFiles(ctx context.Context, id, workDir, filePath string) ([]FileEntry, error) {
	base, target, err := scopedPath(workDir, filePath)
	if err != nil {
		return nil, err
	}
	output, err := p.runToolbox(ctx, id, "list", base, target, nil)
	if err != nil {
		return nil, err
	}
	var entries []FileEntry
	if err = json.Unmarshal(output, &entries); err != nil {
		return nil, fmt.Errorf("decode toolbox directory listing: %w", err)
	}
	return entries, nil
}

func (p *DockerProvider) ReadFile(ctx context.Context, id, workDir, filePath string) ([]byte, error) {
	base, target, err := scopedPath(workDir, filePath)
	if err != nil {
		return nil, err
	}
	return p.runToolbox(ctx, id, "read", base, target, nil)
}

func (p *DockerProvider) ReadFileLines(ctx context.Context, id, workDir, filePath string, offset, limit int) (*FileLines, error) {
	base, target, err := scopedPath(workDir, filePath)
	if err != nil {
		return nil, err
	}
	if offset < 1 || limit < 1 || limit > 2000 {
		return nil, errors.New("invalid file line range")
	}
	output, err := p.runToolbox(ctx, id, "read-lines", base, target, nil, "--offset", fmt.Sprint(offset), "--limit", fmt.Sprint(limit))
	if err != nil {
		return nil, err
	}
	var result FileLines
	if err = json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("decode toolbox file lines: %w", err)
	}
	return &result, nil
}

func (p *DockerProvider) WriteFile(ctx context.Context, id, workDir, filePath string, data []byte) error {
	if len(data) > 25<<20 {
		return errors.New("file exceeds the 25 MiB write limit")
	}
	base, target, err := scopedPath(workDir, filePath)
	if err != nil {
		return err
	}
	_, err = p.runToolbox(ctx, id, "write", base, target, data)
	return err
}

func (p *DockerProvider) EditFile(ctx context.Context, id, workDir, filePath string, request FileEditRequest) (*FileEditResult, error) {
	base, target, err := scopedPath(workDir, filePath)
	if err != nil {
		return nil, err
	}
	input, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode toolbox edit request: %w", err)
	}
	output, err := p.runToolbox(ctx, id, "edit", base, target, input)
	if err != nil {
		return nil, err
	}
	var result FileEditResult
	if err = json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("decode toolbox edit result: %w", err)
	}
	return &result, nil
}

func (p *DockerProvider) runToolbox(ctx context.Context, id, operation, root, target string, input []byte, extra ...string) ([]byte, error) {
	if err := p.ensureToolbox(ctx, id); err != nil {
		return nil, err
	}
	name, err := containerName(id)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		arguments := []string{"exec"}
		if input != nil {
			arguments = append(arguments, "-i")
		}
		arguments = append(arguments, name, toolboxContainerPath, operation, "--root", root, "--path", target)
		arguments = append(arguments, extra...)
		command := exec.CommandContext(ctx, "docker", arguments...)
		if input != nil {
			command.Stdin = bytes.NewReader(input)
		}
		stdoutLimit := commandCaptureLimit
		if operation == "read" {
			stdoutLimit = (25 << 20) + 1
		}
		stdout, stderr := newBoundedCapture(stdoutLimit), newBoundedCapture(commandCaptureLimit)
		command.Stdout, command.Stderr = stdout, stderr
		runErr := command.Run()
		if runErr == nil {
			if stdout.Truncated() {
				return nil, fmt.Errorf("toolbox %s output exceeds the %d byte limit", operation, stdoutLimit)
			}
			return []byte(stdout.String()), nil
		}
		if attempt == 0 && toolboxUnavailable(runErr, stderr.String()) {
			p.markToolboxMissing(id)
			if installErr := p.ensureToolbox(ctx, id); installErr != nil {
				return nil, installErr
			}
			continue
		}
		return nil, fmt.Errorf("toolbox %s: %w: %s", operation, runErr, strings.TrimSpace(stderr.String()))
	}
	return nil, fmt.Errorf("toolbox %s could not be started", operation)
}

func (p *DockerProvider) ensureToolbox(ctx context.Context, id string) error {
	p.toolboxMu.Lock()
	defer p.toolboxMu.Unlock()
	if p.toolboxInstalled == nil {
		p.toolboxInstalled = map[string]bool{}
	}
	if p.toolboxInstalled[id] {
		return nil
	}
	name, err := containerName(id)
	if err != nil {
		return err
	}
	source := p.ToolboxSourcePath
	if source == "" {
		source = defaultToolboxSourcePath
	}
	if _, err = os.Stat(source); err != nil {
		return fmt.Errorf("locate lester-toolbox: %w", err)
	}
	if output, mkdirErr := exec.CommandContext(ctx, "docker", "exec", "-u", "0", name, "mkdir", "-p", pathpkg.Dir(toolboxContainerPath)).CombinedOutput(); mkdirErr != nil {
		return fmt.Errorf("prepare lester-toolbox directory: %w: %s", mkdirErr, output)
	}
	destination := name + ":" + toolboxContainerPath
	if output, copyErr := exec.CommandContext(ctx, "docker", "cp", source, destination).CombinedOutput(); copyErr != nil {
		return fmt.Errorf("install lester-toolbox: %w: %s", copyErr, output)
	}
	if output, chmodErr := exec.CommandContext(ctx, "docker", "exec", "-u", "0", name, "chmod", "0755", toolboxContainerPath).CombinedOutput(); chmodErr != nil {
		return fmt.Errorf("prepare lester-toolbox: %w: %s", chmodErr, output)
	}
	p.toolboxInstalled[id] = true
	return nil
}

func (p *DockerProvider) markToolboxMissing(id string) {
	p.toolboxMu.Lock()
	delete(p.toolboxInstalled, id)
	p.toolboxMu.Unlock()
}

func toolboxUnavailable(err error, stderr string) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && (exitErr.ExitCode() == 126 || exitErr.ExitCode() == 127) {
		return true
	}
	message := strings.ToLower(stderr)
	return strings.Contains(message, toolboxContainerPath) && (strings.Contains(message, "no such file") || strings.Contains(message, "executable file not found"))
}

func safeWorkDir(workDir string) (string, error) {
	if workDir == "" {
		return "/workspace", nil
	}
	clean := pathpkg.Clean("/" + strings.TrimPrefix(workDir, "/"))
	if clean == "/workspace" {
		return clean, nil
	}
	parts := strings.Split(strings.TrimPrefix(clean, "/workspace/conversations/"), "/")
	if !strings.HasPrefix(clean, "/workspace/conversations/") || len(parts) != 1 || parts[0] == "" || strings.ContainsAny(parts[0], "\\ ") {
		return "", errors.New("invalid conversation work directory")
	}
	return clean, nil
}

func scopedPath(workDir, filePath string) (string, string, error) {
	base, err := safeWorkDir(workDir)
	if err != nil {
		return "", "", err
	}
	requested := strings.TrimSpace(filePath)
	if requested == "" || requested == "." || requested == "/workspace" {
		return base, base, nil
	}

	var target string
	if strings.HasPrefix(requested, "/") {
		target = pathpkg.Clean(requested)
		if target == base || strings.HasPrefix(target, base+"/") {
			return base, target, nil
		}
		if strings.HasPrefix(target, "/workspace/conversations/") {
			return "", "", errors.New("path escapes conversation")
		}
		if strings.HasPrefix(target, "/workspace/") {
			target = pathpkg.Clean(base + "/" + strings.TrimPrefix(target, "/workspace/"))
		} else {
			return "", "", errors.New("path escapes conversation")
		}
	} else {
		target = pathpkg.Clean(base + "/" + requested)
	}
	if target != base && !strings.HasPrefix(target, base+"/") {
		return "", "", errors.New("path escapes conversation")
	}
	return base, target, nil
}
