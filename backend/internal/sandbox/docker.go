package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

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
	_ = exec.CommandContext(ctx, "docker", "volume", "create", volume).Run()
	inspect := exec.CommandContext(ctx, "docker", "inspect", name)
	if inspect.Run() != nil {
		cmd := exec.CommandContext(ctx, "docker", "create", "--name", name, "--network", "none", "--cpus", cpus, "--memory", memory, "--pids-limit", "256", "--security-opt", "no-new-privileges", "-v", volume+":/workspace", "-w", "/workspace", image, "sleep", "infinity")
		if output, e := cmd.CombinedOutput(); e != nil {
			return nil, fmt.Errorf("create docker sandbox: %w: %s", e, output)
		}
	}
	if err = p.Start(ctx, opts.ID); err != nil {
		return nil, err
	}
	return &Sandbox{ID: opts.ID, ProviderRef: name, Status: "running", LastActiveAt: time.Now()}, nil
}
func (p *DockerProvider) Start(ctx context.Context, id string) error {
	name, err := containerName(id)
	if err != nil {
		return err
	}
	return exec.CommandContext(ctx, "docker", "start", name).Run()
}
func (p *DockerProvider) Suspend(ctx context.Context, id string) error {
	name, err := containerName(id)
	if err != nil {
		return err
	}
	return exec.CommandContext(ctx, "docker", "stop", "-t", "5", name).Run()
}
func (p *DockerProvider) Resume(ctx context.Context, id string) error { return p.Start(ctx, id) }
func (p *DockerProvider) Destroy(ctx context.Context, id string) error {
	name, err := containerName(id)
	if err != nil {
		return err
	}
	return exec.CommandContext(ctx, "docker", "rm", "-f", name).Run()
}
func (p *DockerProvider) Exec(ctx context.Context, id string, command Command) (*CommandResult, error) {
	name, err := containerName(id)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(max(command.TimeoutSeconds, 60)) * time.Second
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	cmd := exec.CommandContext(runCtx, "docker", "exec", name, "sh", "-lc", command.Command)
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
func (p *DockerProvider) ListFiles(ctx context.Context, id, path string) ([]FileEntry, error) {
	safe, err := safePath(path)
	if err != nil {
		return nil, err
	}
	script := `import json,os,sys,datetime;p=sys.argv[1];r=[]
for n in os.listdir(p):
 q=os.path.join(p,n);s=os.stat(q);r.append({"Name":n,"Path":os.path.relpath(q,"/workspace"),"IsDir":os.path.isdir(q),"Size":s.st_size,"ModifiedAt":datetime.datetime.fromtimestamp(s.st_mtime,datetime.timezone.utc).isoformat()})
print(json.dumps(r))`
	result, err := p.Exec(ctx, id, Command{Command: "python -c " + shellQuote(script) + " " + shellQuote(safe)})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, errors.New(result.Stderr)
	}
	var entries []FileEntry
	if err = json.Unmarshal([]byte(result.Stdout), &entries); err != nil {
		return nil, err
	}
	return entries, nil
}
func (p *DockerProvider) ReadFile(ctx context.Context, id, path string) ([]byte, error) {
	safe, err := safePath(path)
	if err != nil {
		return nil, err
	}
	name, _ := containerName(id)
	return exec.CommandContext(ctx, "docker", "exec", name, "cat", safe).Output()
}
func (p *DockerProvider) WriteFile(ctx context.Context, id, path string, data []byte) error {
	safe, err := safePath(path)
	if err != nil {
		return err
	}
	name, _ := containerName(id)
	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", name, "sh", "-c", "mkdir -p \"$(dirname \"$1\")\" && cat > \"$1\"", "sh", safe)
	cmd.Stdin = bytes.NewReader(data)
	return cmd.Run()
}
func safePath(path string) (string, error) {
	path = strings.TrimPrefix(path, "/")
	clean := filepath.Clean("/workspace/" + path)
	if clean != "/workspace" && !strings.HasPrefix(clean, "/workspace/") {
		return "", errors.New("path escapes workspace")
	}
	return clean, nil
}
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

var _ = strconv.Itoa
