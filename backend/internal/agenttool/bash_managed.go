package agenttool

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/commandtask"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
)

func executeManagedBash(ctx context.Context, environment Environment, input bashInput, command, description string, manager sandbox.ManagedCommandProvider) (any, error) {
	if environment.RunID == uuid.Nil || environment.ConversationID == uuid.Nil {
		return nil, errors.New("managed commands require a current conversation and run")
	}
	timeout := input.Timeout
	if timeout == 0 && !input.RunInBackground {
		timeout = 120000
	}
	request := sandbox.CommandTaskRequest{TaskID: uuid.NewString(), RunID: environment.RunID.String(), ConversationID: environment.ConversationID.String(), OwnershipToken: uuid.NewString(), WorkDir: environment.WorkDir, Command: command, Description: description, TimeoutMS: timeout, MaxOutputBytes: commandtask.DefaultLogLimit}
	initial := &sandbox.CommandTaskSnapshot{Handle: sandbox.CommandTaskHandle{TaskID: request.TaskID, RunID: request.RunID, ConversationID: request.ConversationID, OwnershipToken: request.OwnershipToken, WorkDir: request.WorkDir}, Status: "starting", Shell: "unknown", Description: description, StartedAt: time.Now().UTC(), LogPath: ".lester/tasks/" + request.TaskID + "/output.log", MaxOutputBytes: request.MaxOutputBytes}
	record := func(snapshot *sandbox.CommandTaskSnapshot) error {
		if environment.RecordTask == nil {
			return nil
		}
		return environment.RecordTask(ctx, snapshot)
	}
	if err := record(initial); err != nil {
		return nil, fmt.Errorf("record command before launch: %w", err)
	}
	snapshot, err := manager.StartTask(ctx, environment.SandboxID, request)
	if err != nil || snapshot == nil {
		initial.Status = "unknown"
		initial.Error = "Launch response unavailable; inspect this task before starting the command again."
		_ = record(initial)
		payload := managedCommandView(initial)
		payload["notice"] = initial.Error
		environment.Emit("COMMAND_COMPLETED", payload)
		return payload, nil
	}
	if err = record(snapshot); err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		stopped, stopErr := manager.StopTask(cleanup, environment.SandboxID, snapshot.Handle)
		cancel()
		if stopErr == nil && stopped != nil {
			snapshot = stopped
			_ = record(snapshot)
		}
		payload := managedCommandView(snapshot)
		payload["notice"] = "Command state could not be recorded; a stop was requested for this task."
		environment.Emit("COMMAND_COMPLETED", payload)
		return payload, err
	}
	if input.RunInBackground {
		payload := managedCommandView(snapshot)
		environment.Emit("BACKGROUND_STARTED", payload)
		return payload, nil
	}
	final, waitErr := commandtask.Wait(ctx, snapshot.Handle, func(checkCtx context.Context, h commandtask.Handle, options commandtask.ReadOptions) (*commandtask.Snapshot, error) {
		return manager.InspectTask(checkCtx, environment.SandboxID, h, options)
	})
	if waitErr != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		stopped, stopErr := manager.StopTask(cleanup, environment.SandboxID, snapshot.Handle)
		cancel()
		if stopErr == nil && stopped != nil {
			final = stopped
		} else {
			final = snapshot
			final.Status = "unknown"
			final.ExitCode = nil
			final.Error = "Command observation failed and remote stop could not be confirmed."
		}
	}
	if final == nil {
		final = snapshot
		final.Status = "unknown"
		final.ExitCode = nil
	}
	_ = record(final)
	preview, previewTruncated, readErr := managedCommandPreview(ctx, environment, manager, final)
	payload := managedCommandView(final)
	payload["stdout"], payload["stderr"], payload["stream"] = preview, "", "combined"
	payload["output_saved"] = final.LogPath != ""
	payload["full_output_saved"] = !final.Truncated && final.Status != "unknown"
	payload["preview_truncated"] = previewTruncated
	if readErr != nil {
		payload["output_error"] = "Stored command output is temporarily unavailable; use task_output to try reading it."
	}
	if final.Truncated {
		payload["notice"] = fmt.Sprintf("The retained log reached its %d-byte limit; %d output bytes were omitted.", final.MaxOutputBytes, final.OmittedBytes)
	} else if previewTruncated {
		payload["notice"] = "The retained raw output is saved. Use task_output with this task_id and byte offsets to read more."
	}
	environment.Emit("COMMAND_OUTPUT", map[string]any{"task_id": final.Handle.TaskID, "stdout": preview, "stderr": "", "stream": "combined", "shell": final.Shell})
	environment.Emit("COMMAND_COMPLETED", payload)
	return payload, nil
}

func managedCommandView(s *sandbox.CommandTaskSnapshot) map[string]any {
	var code any
	if s.ExitCode != nil {
		code = *s.ExitCode
	}
	view := map[string]any{"task_id": s.Handle.TaskID, "status": s.Status, "shell": s.Shell, "description": s.Description, "exit_code": code, "exit_code_known": s.ExitCode != nil, "log_path": s.LogPath, "stored_bytes": s.StoredBytes, "omitted_bytes": s.OmittedBytes, "truncated": s.Truncated}
	if s.CleanupConfirmed != nil {
		view["cleanup_confirmed"] = *s.CleanupConfirmed
	}
	if s.FinishedAt != nil {
		view["duration_ms"] = s.FinishedAt.Sub(s.StartedAt).Milliseconds()
	}
	return view
}

func managedCommandPreview(ctx context.Context, e Environment, manager sandbox.ManagedCommandProvider, s *sandbox.CommandTaskSnapshot) (string, bool, error) {
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	first, err := manager.InspectTask(readCtx, e.SandboxID, s.Handle, sandbox.CommandTaskReadOptions{Limit: 75000})
	if err != nil {
		return "", false, err
	}
	data := first.Page.Data
	truncated := first.StoredBytes > 75000
	if first.StoredBytes > 75000 {
		last, err := manager.InspectTask(readCtx, e.SandboxID, s.Handle, sandbox.CommandTaskReadOptions{Offset: first.StoredBytes - 15000, Limit: 15000})
		if err != nil {
			preview, limited, _ := truncateText(string(data), outputCharacterLimit)
			return preview, truncated || limited, err
		}
		data = append(append(data[:min(len(data), 60000)], []byte("\n[Preview shortened; retained raw output is available through task_output.]\n")...), last.Page.Data...)
	}
	preview, limited, _ := truncateText(string(data), outputCharacterLimit)
	return preview, truncated || limited, nil
}
