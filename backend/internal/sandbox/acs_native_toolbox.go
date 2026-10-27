package sandbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/openkruise/agents-api/sdk/proto/envd/process"
	acsruntime "github.com/openkruise/agents-api/sdk/runtime"
)

// Native Toolbox execution must not use SDK Commands.Run: that method launches
// bash -l -c and therefore executes user-writable shell profiles even for reads.
type acsNativeToolbox interface {
	RunToolbox(context.Context, []string, func(string), func(string)) (*acsCommandResult, error)
}

func runACSToolbox(ctx context.Context, runtime acsRuntime, arguments []string, stdout, stderr func(string)) (*acsCommandResult, error) {
	native, ok := runtime.(acsNativeToolbox)
	if !ok {
		return nil, errors.New("ACS runtime requires native Toolbox execution support")
	}
	return native.RunToolbox(ctx, arguments, stdout, stderr)
}

func (r *openKruiseRuntime) RunToolbox(ctx context.Context, arguments []string, stdout, stderr func(string)) (*acsCommandResult, error) {
	cwd := "/workspace"
	request := connect.NewRequest(&process.StartRequest{Process: &process.ProcessConfig{
		Cmd: toolboxContainerPath, Args: arguments, Cwd: &cwd,
		Envs: map[string]string{"PATH": "/usr/bin:/bin", "HOME": "/nonexistent", "LANG": "C", "LC_ALL": "C"},
	}})
	for key, value := range r.client.Config().SandboxHeaders(r.client.SandboxID()) {
		request.Header().Set(key, value)
	}
	stream, err := r.client.Commands.Rpc.Start(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("start native Toolbox: %w", err)
	}
	defer stream.Close()
	if !stream.Receive() {
		if err := stream.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("native Toolbox stream closed before start")
	}
	event := stream.Msg().GetEvent().GetStart()
	if event == nil {
		return nil, errors.New("native Toolbox returned no start event")
	}
	pid := event.GetPid()
	handle := acsruntime.NewCommandHandle(pid, stream, func() bool {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		killed, _ := r.client.Commands.Kill(cleanup, pid)
		return killed
	})
	result, err := handle.Wait(stdout, stderr)
	if ctx.Err() != nil {
		handle.Kill()
	}
	if result == nil {
		return nil, err
	}
	return &acsCommandResult{ExitCode: int(result.ExitCode), Stdout: result.Stdout, Stderr: result.Stderr, Shell: "native"}, err
}
