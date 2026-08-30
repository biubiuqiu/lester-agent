package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	pathpkg "path"
	"strings"
	"time"
)

var ErrSandboxNotFound = errors.New("sandbox not found")

type DockerProvider struct{ Image string }

func NewDockerProvider(image string) *DockerProvider {
	if image == "" {
		image = "python:3.12-slim"
	}
	return &DockerProvider{Image: image}
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
	return nil
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
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
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
	return &CommandResult{ExitCode: exitCode, Stdout: stdout.String(), Stderr: stderr.String(), DurationMS: time.Since(start).Milliseconds()}, nil
}

func (p *DockerProvider) ListFiles(ctx context.Context, id, workDir, filePath string) ([]FileEntry, error) {
	base, target, err := scopedPath(workDir, filePath)
	if err != nil {
		return nil, err
	}
	script := `import datetime,json,os,sys
base=os.path.realpath(sys.argv[1]); target=os.path.realpath(sys.argv[2])
if os.path.commonpath([base,target]) != base: raise PermissionError("path escapes conversation")
result=[]
for name in os.listdir(target):
 item=os.path.join(target,name); stat=os.stat(item)
 result.append({"Name":name,"Path":os.path.relpath(item,base),"IsDir":os.path.isdir(item),"Size":stat.st_size,"ModifiedAt":datetime.datetime.fromtimestamp(stat.st_mtime,datetime.timezone.utc).isoformat()})
print(json.dumps(result))`
	result, err := p.Exec(ctx, id, Command{WorkDir: "/workspace", Command: "python -c " + shellQuote(script) + " " + shellQuote(base) + " " + shellQuote(target)})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, errors.New(strings.TrimSpace(result.Stderr))
	}
	var entries []FileEntry
	if err = json.Unmarshal([]byte(result.Stdout), &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (p *DockerProvider) ReadFile(ctx context.Context, id, workDir, filePath string) ([]byte, error) {
	base, target, err := scopedPath(workDir, filePath)
	if err != nil {
		return nil, err
	}
	name, _ := containerName(id)
	script := `import os,sys
base=os.path.realpath(sys.argv[1]); target=os.path.realpath(sys.argv[2])
if os.path.commonpath([base,target]) != base: raise PermissionError("path escapes conversation")
with open(target,"rb") as handle: sys.stdout.buffer.write(handle.read())`
	output, readErr := exec.CommandContext(ctx, "docker", "exec", name, "python", "-c", script, base, target).Output()
	if readErr != nil {
		return nil, fmt.Errorf("read sandbox file: %w", readErr)
	}
	return output, nil
}

func (p *DockerProvider) WriteFile(ctx context.Context, id, workDir, filePath string, data []byte) error {
	base, target, err := scopedPath(workDir, filePath)
	if err != nil {
		return err
	}
	name, _ := containerName(id)
	script := `import os,sys
base=os.path.realpath(sys.argv[1]); target=os.path.normpath(sys.argv[2]); parent=os.path.dirname(target)
probe=parent
while not os.path.exists(probe):
 next_probe=os.path.dirname(probe)
 if next_probe == probe: break
 probe=next_probe
if os.path.commonpath([base,os.path.realpath(probe)]) != base: raise PermissionError("path escapes conversation")
os.makedirs(parent,exist_ok=True)
if os.path.commonpath([base,os.path.realpath(parent)]) != base: raise PermissionError("path escapes conversation")
with open(target,"wb") as handle: handle.write(sys.stdin.buffer.read())`
	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", name, "python", "-c", script, base, target)
	cmd.Stdin = bytes.NewReader(data)
	if output, writeErr := cmd.CombinedOutput(); writeErr != nil {
		return fmt.Errorf("write sandbox file: %w: %s", writeErr, output)
	}
	return nil
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

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
