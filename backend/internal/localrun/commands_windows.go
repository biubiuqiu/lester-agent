package localrun

import (
	"context"
	"time"
)

type CommandResult struct {
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExitCode        int    `json:"exit_code"`
	TimedOut        bool   `json:"timed_out"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
}

// Native Windows execution is intentionally unsupported. The desktop shell
// starts the Linux runner in WSL rather than emulating POSIX behavior here.
func ExecuteCommand(context.Context, string, string, time.Duration) (CommandResult, error) {
	return CommandResult{ExitCode: -1}, ErrUnsupported
}

func executeArgv(context.Context, string, []string, time.Duration) (CommandResult, error) {
	return CommandResult{ExitCode: -1}, ErrUnsupported
}
