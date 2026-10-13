package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	return model.Tool{Name: "bash", Description: "Run a Bash command in the current conversation directory, with an explicit sh fallback if the image lacks Bash. Results report the actual shell, exit status, and whether an exit code is known. A failed command returns its output so you can diagnose and repair it. Large stdout/stderr is truncated with a continuation notice.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{
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
	description := strings.TrimSpace(input.Description)
	environment.Emit("COMMAND_STARTED", map[string]any{"command": command, "description": description})
	if manager, supported := environment.Sandboxes.(sandbox.ManagedCommandProvider); supported {
		return executeManagedBash(ctx, environment, input, command, description, manager)
	}
	if input.RunInBackground {
		result, err := startBackgroundCommand(ctx, environment, command, description, input.Timeout)
		if err != nil {
			environment.Emit("COMMAND_COMPLETED", map[string]any{"command": command, "description": description, "status": "unknown", "exit_code_known": false, "error": err.Error()})
		}
		return result, err
	}
	timeout := input.Timeout
	if timeout == 0 {
		timeout = 120000
	}
	result, err := environment.Sandboxes.Exec(ctx, environment.SandboxID, sandbox.Command{Command: command, WorkDir: environment.WorkDir, TimeoutSeconds: millisecondsToSeconds(timeout)})
	if result == nil {
		if err == nil {
			err = errors.New("sandbox returned no command result")
		}
		environment.Emit("COMMAND_COMPLETED", map[string]any{"command": command, "description": description, "status": "unknown", "exit_code_known": false, "error": err.Error()})
		return nil, err
	}
	// Older providers expose ordinary program exits without an explicit status.
	// Never manufacture an exit code after transport failure or cancellation.
	if result.Status == "" {
		result.Status = "unknown"
		if err == nil {
			result.Status, result.ExitCodeKnown = "exited", true
		}
	}
	if result.Shell == "" {
		result.Shell = "unknown"
	}
	limited := limitCommandResult(result)
	limited["shell"], limited["status"], limited["exit_code_known"] = result.Shell, result.Status, result.ExitCodeKnown
	if !result.ExitCodeKnown {
		limited["exit_code"] = nil
	}
	limited["description"] = description
	if result.CleanupConfirmed != nil {
		limited["cleanup_confirmed"] = *result.CleanupConfirmed
	}
	environment.Emit("COMMAND_OUTPUT", map[string]any{"stdout": limited["stdout"], "stderr": limited["stderr"], "shell": result.Shell})
	completed := map[string]any{"command": command, "description": description, "exit_code": limited["exit_code"], "exit_code_known": result.ExitCodeKnown, "shell": result.Shell, "status": result.Status, "duration_ms": result.DurationMS}
	if result.CleanupConfirmed != nil {
		completed["cleanup_confirmed"] = *result.CleanupConfirmed
	}
	environment.Emit("COMMAND_COMPLETED", completed)
	return limited, err
}

func startBackgroundCommand(ctx context.Context, environment Environment, command, description string, timeoutMS int) (any, error) {
	taskID := uuid.NewString()
	logPath := ".lester/tasks/" + taskID + ".log"
	inner := command + "\nexit_code=$?\nprintf '\\n[Lester background task exited with code %s]\\n' \"$exit_code\"\nexit \"$exit_code\""
	runner := `"$lester_task_shell" -lc ` + shellQuote(inner)
	if timeoutMS > 0 {
		runner = fmt.Sprintf("timeout %ds %s", millisecondsToSeconds(timeoutMS), runner)
	}
	launch := `if command -v bash >/dev/null 2>&1; then lester_task_shell=bash; else lester_task_shell=sh; fi; ` + "mkdir -p .lester/tasks || exit $?; nohup " + runner + " > " + shellQuote(logPath) + " 2>&1 < /dev/null & printf '%s %s\\n' \"$!\" \"$lester_task_shell\""
	result, err := environment.Sandboxes.Exec(ctx, environment.SandboxID, sandbox.Command{Command: launch, WorkDir: environment.WorkDir, TimeoutSeconds: 30})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("sandbox returned no background launch result")
	}
	if result.Status != "" && result.Status != "exited" || result.ExitCode != 0 {
		return nil, fmt.Errorf("start background command: %s", strings.TrimSpace(result.Stderr))
	}
	fields := strings.Fields(result.Stdout)
	if len(fields) != 2 || fields[1] != "bash" && fields[1] != "sh" {
		return nil, errors.New("sandbox did not confirm the background command shell")
	}
	if pid, err := strconv.Atoi(fields[0]); err != nil || pid <= 1 {
		return nil, errors.New("sandbox did not confirm the background command PID")
	}
	payload := map[string]any{"task_id": taskID, "pid": fields[0], "log_path": logPath, "shell": fields[1], "description": description, "status": "running", "exit_code_known": false}
	environment.Emit("BACKGROUND_STARTED", payload)
	return payload, nil
}

func millisecondsToSeconds(milliseconds int) int { return (milliseconds + 999) / 1000 }
func shellQuote(value string) string             { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
