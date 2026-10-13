package sandbox

import (
	"context"

	"github.com/biubiuqiu/lester-agent/backend/internal/commandtask"
)

type CommandTaskRequest = commandtask.Request
type CommandTaskHandle = commandtask.Handle
type CommandTaskSnapshot = commandtask.Snapshot
type CommandTaskReadOptions = commandtask.ReadOptions
type CommandTaskPage = commandtask.Page

// Optional so lightweight/custom providers can retain ordinary Exec support.
type ManagedCommandProvider interface {
	StartTask(context.Context, string, CommandTaskRequest) (*CommandTaskSnapshot, error)
	InspectTask(context.Context, string, CommandTaskHandle, CommandTaskReadOptions) (*CommandTaskSnapshot, error)
	StopTask(context.Context, string, CommandTaskHandle) (*CommandTaskSnapshot, error)
}
