package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
)

type Bash struct{}

type bashInput struct {
	Command         string `json:"command"`
	Description     string `json:"description,omitempty"`
	Timeout         int    `json:"timeout,omitempty"`
	RunInBackground bool   `json:"run_in_background,omitempty"`
}

func (Bash) Definition() model.Tool {
	return model.Tool{Name: "bash", Description: "Run a Bash command in the current conversation directory. Use it for listing, searching, tests, builds, and long-running processes. Large stdout/stderr is truncated with a continuation notice.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{
		"command":           map[string]any{"type": "string", "description": "The Bash command to execute."},
		"description":       map[string]any{"type": "string", "description": "A short description of what the command does."},
		"timeout":           map[string]any{"type": "integer", "minimum": 1000, "maximum": 600000, "description": "Foreground timeout in milliseconds. Defaults to 120000. For background tasks, only applied when explicitly provided."},
		"run_in_background": map[string]any{"type": "boolean", "description": "Run asynchronously and return a task ID plus a log file path."},
	}, "required": []string{"command"}, "additionalProperties": false}}
}

func (Bash) Execute(ctx context.Context, environment Environment, raw json.RawMessage) (any, error) {
	var input bashInput
	if err := decodeArguments(raw, &input); err != nil {
		return nil, err
	}
	command, err := required(input.Command, "command")
	if err != nil {
		return nil, err
	}
	if input.Timeout != 0 && (input.Timeout < 1000 || input.Timeout > 600000) {
		return nil, errors.New("timeout must be between 1000 and 600000 milliseconds")
	}
	environment.Emit("COMMAND_STARTED", map[string]any{"command": command})
	if input.RunInBackground {
		return startBackgroundCommand(ctx, environment, command, input.Timeout)
	}
	timeout := input.Timeout
	if timeout == 0 {
		timeout = 120000
	}
	result, err := environment.Sandboxes.Exec(ctx, environment.SandboxID, sandbox.Command{Command: command, WorkDir: environment.WorkDir, TimeoutSeconds: millisecondsToSeconds(timeout)})
	if err != nil {
		return result, err
	}
	limited := limitCommandResult(result)
	environment.Emit("COMMAND_OUTPUT", map[string]any{"stdout": limited["stdout"], "stderr": limited["stderr"]})
	environment.Emit("COMMAND_COMPLETED", map[string]any{"exit_code": result.ExitCode})
	return limited, nil
}

func startBackgroundCommand(ctx context.Context, environment Environment, command string, timeoutMS int) (any, error) {
	taskID := uuid.NewString()
	logPath := ".lester/tasks/" + taskID + ".log"
	inner := command + "\nexit_code=$?\nprintf '\\n[Lester background task exited with code %s]\\n' \"$exit_code\"\nexit \"$exit_code\""
	runner := "sh -lc " + shellQuote(inner)
	if timeoutMS > 0 {
		runner = fmt.Sprintf("timeout %ds %s", millisecondsToSeconds(timeoutMS), runner)
	}
	launch := "mkdir -p .lester/tasks; nohup " + runner + " > " + shellQuote(logPath) + " 2>&1 < /dev/null & echo $!"
	result, err := environment.Sandboxes.Exec(ctx, environment.SandboxID, sandbox.Command{Command: launch, WorkDir: environment.WorkDir, TimeoutSeconds: 30})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("start background command: %s", strings.TrimSpace(result.Stderr))
	}
	payload := map[string]any{"task_id": taskID, "pid": strings.TrimSpace(result.Stdout), "log_path": logPath, "status": "running"}
	environment.Emit("BACKGROUND_STARTED", payload)
	return payload, nil
}

func millisecondsToSeconds(milliseconds int) int { return (milliseconds + 999) / 1000 }
func shellQuote(value string) string             { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
