package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/commandtask"
	"github.com/google/uuid"
)

func validateTaskScope(h CommandTaskHandle) error {
	dir, err := safeWorkDir(h.WorkDir)
	if err != nil {
		return err
	}
	if dir != "/workspace/conversations/"+h.ConversationID {
		return errors.New("command task directory does not match its conversation")
	}
	return nil
}

func (p *DockerProvider) StartTask(ctx context.Context, id string, r CommandTaskRequest) (*CommandTaskSnapshot, error) {
	h := CommandTaskHandle{WorkDir: r.WorkDir, ConversationID: r.ConversationID}
	if err := validateTaskScope(h); err != nil {
		return nil, err
	}
	return p.taskCall(ctx, id, "task-start", r)
}
func (p *DockerProvider) InspectTask(ctx context.Context, id string, h CommandTaskHandle, read CommandTaskReadOptions) (*CommandTaskSnapshot, error) {
	if err := validateTaskScope(h); err != nil {
		return nil, err
	}
	return p.taskCall(ctx, id, "task-inspect", commandtask.InspectRequest{Handle: h, Read: read})
}
func (p *DockerProvider) StopTask(ctx context.Context, id string, h CommandTaskHandle) (*CommandTaskSnapshot, error) {
	if err := validateTaskScope(h); err != nil {
		return nil, err
	}
	return p.taskCall(ctx, id, "task-stop", h)
}

func (p *DockerProvider) taskCall(ctx context.Context, id, operation string, input any) (*CommandTaskSnapshot, error) {
	name, err := containerName(id)
	if err != nil {
		return nil, err
	}
	if err = p.ensureToolbox(ctx, id); err != nil {
		return nil, err
	}
	probe, err := exec.CommandContext(ctx, "docker", "exec", name, toolboxContainerPath, "task-version").Output()
	if err != nil || strings.TrimSpace(string(probe)) != "1" {
		p.markToolboxMissing(id)
		if installErr := p.ensureToolbox(ctx, id); installErr != nil {
			return nil, installErr
		}
		probe, err = exec.CommandContext(ctx, "docker", "exec", name, toolboxContainerPath, "task-version").Output()
	}
	if err != nil || strings.TrimSpace(string(probe)) != "1" {
		return nil, errors.New("Computer requires native command task protocol 1; update the toolbox/runtime before starting tasks")
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	if len(data) > 2<<20 {
		return nil, errors.New("native task request exceeds 2 MiB")
	}
	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", name, toolboxContainerPath, operation)
	cmd.Stdin = bytes.NewReader(data)
	cmd.WaitDelay = time.Second
	stdout, stderr := newBoundedCapture(512<<10), newBoundedCapture(64<<10)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err = cmd.Run(); err != nil {
		return nil, fmt.Errorf("native command task %s: %w: %s", operation, err, strings.TrimSpace(stderr.String()))
	}
	if stdout.Truncated() {
		return nil, errors.New("native task response exceeds protocol limit")
	}
	var result CommandTaskSnapshot
	if err = json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *ACSProvider) StartTask(ctx context.Context, id string, r CommandTaskRequest) (*CommandTaskSnapshot, error) {
	if err := validateTaskScope(CommandTaskHandle{WorkDir: r.WorkDir, ConversationID: r.ConversationID}); err != nil {
		return nil, err
	}
	return p.taskCall(ctx, id, "task-start", r)
}
func (p *ACSProvider) InspectTask(ctx context.Context, id string, h CommandTaskHandle, read CommandTaskReadOptions) (*CommandTaskSnapshot, error) {
	if err := validateTaskScope(h); err != nil {
		return nil, err
	}
	return p.taskCall(ctx, id, "task-inspect", commandtask.InspectRequest{Handle: h, Read: read})
}
func (p *ACSProvider) StopTask(ctx context.Context, id string, h CommandTaskHandle) (*CommandTaskSnapshot, error) {
	if err := validateTaskScope(h); err != nil {
		return nil, err
	}
	return p.taskCall(ctx, id, "task-stop", h)
}

func (p *ACSProvider) taskCall(ctx context.Context, id, operation string, input any) (*CommandTaskSnapshot, error) {
	client, err := p.runtime(ctx, id)
	if err != nil {
		return nil, err
	}
	probe, err := client.Run(ctx, shellQuote(toolboxContainerPath)+" task-version", "/workspace", nil, nil)
	if err != nil || probe == nil || probe.ExitCode != 0 || strings.TrimSpace(probe.Stdout) != "1" {
		return nil, errors.New("Computer requires native command task protocol 1; rebuild the ACS runtime template before starting tasks")
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	if len(data) > 2<<20 {
		return nil, errors.New("native task request exceeds 2 MiB")
	}
	staged := "/tmp/.lester-input-" + uuid.NewString() + ".json"
	if remover, ok := client.(interface {
		Remove(context.Context, string) error
	}); ok {
		defer func() {
			cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer stop()
			_ = remover.Remove(cleanup, staged)
		}()
	}
	prepared, err := client.Run(ctx, shellQuote(toolboxContainerPath)+" prepare-input --name "+shellQuote(strings.TrimPrefix(staged, "/tmp/")), "/workspace", nil, nil)
	if err != nil || prepared == nil || prepared.ExitCode != 0 || strings.TrimSpace(prepared.Stdout) != staged {
		return nil, errors.New("could not prepare a private native task request")
	}
	if err = client.Write(ctx, staged, data); err != nil {
		return nil, err
	}
	stdout, stderr := newBoundedCapture(512<<10), newBoundedCapture(64<<10)
	result, runErr := client.Run(ctx, shellQuote(toolboxContainerPath)+" "+operation+" --input-file "+shellQuote(staged), "/workspace", func(s string) { _, _ = stdout.Write([]byte(s)) }, func(s string) { _, _ = stderr.Write([]byte(s)) })
	if result == nil {
		return nil, fmt.Errorf("native task helper did not return: %w", runErr)
	}
	if stdout.String() == "" {
		_, _ = stdout.Write([]byte(result.Stdout))
	}
	if stderr.String() == "" {
		_, _ = stderr.Write([]byte(result.Stderr))
	}
	if runErr != nil || result.ExitCode != 0 {
		return nil, fmt.Errorf("native command task %s: %s", operation, strings.TrimSpace(stderr.String()))
	}
	if stdout.Truncated() {
		return nil, errors.New("native task response exceeds protocol limit")
	}
	var snapshot CommandTaskSnapshot
	if err = json.Unmarshal([]byte(stdout.String()), &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}
