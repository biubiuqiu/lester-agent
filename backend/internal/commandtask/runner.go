package commandtask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

func Start(request Request, executable string, prefix ...string) (*Snapshot, error) {
	if err := validateRequest(request); err != nil {
		return nil, err
	}
	if request.MaxOutputBytes == 0 {
		request.MaxOutputBytes = DefaultLogLimit
	}
	if err := os.MkdirAll(request.WorkDir, 0700); err != nil {
		return nil, err
	}
	h := requestHandle(request)
	root, err := openTask(h, true)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if err = writeJSON(root, "request.json", request, true); err != nil {
		if os.IsExist(err) {
			var old Request
			if e := readJSON(root, "request.json", &old); e != nil {
				return nil, e
			}
			if old != request {
				return nil, errors.New("task ID already belongs to another request")
			}
			return Inspect(h, ReadOptions{})
		}
		return nil, err
	}
	log, err := root.OpenFile("output.log", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	if err = log.Close(); err != nil {
		return nil, err
	}
	state := Snapshot{Handle: h, Status: "starting", Shell: "unknown", Description: request.Description, StartedAt: time.Now().UTC(), LogPath: ".lester/tasks/" + h.TaskID + "/output.log", MaxOutputBytes: request.MaxOutputBytes}
	if err = writeJSON(root, "state.json", state, false); err != nil {
		return nil, err
	}
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer readPipe.Close()
	defer writePipe.Close()
	args := append(append([]string{}, prefix...), "task-run", "--task-id", request.TaskID)
	cmd := exec.Command(executable, args...)
	cmd.Dir = request.WorkDir
	setSession(cmd)
	cmd.Stdin = readPipe
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	defer devnull.Close()
	cmd.Stdout, cmd.Stderr = devnull, devnull
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	go func() { _ = cmd.Wait() }()
	owner, err := processInfo(cmd.Process.Pid)
	if err != nil {
		return nil, err
	}
	state.Handle.PID, state.Handle.StartTime = owner.PID, owner.Start
	if err = writeJSON(root, "state.json", state, false); err != nil {
		return nil, err
	}
	if err = json.NewEncoder(writePipe).Encode(state.Handle); err != nil {
		return nil, err
	}
	_ = writePipe.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var current Snapshot
		if err = readJSON(root, "state.json", &current); err == nil && (current.Status != "starting" || !sameProcess(owner.PID, owner.Start)) {
			return Inspect(state.Handle, ReadOptions{})
		}
		if time.Now().After(deadline) {
			state.Status = "unknown"
			state.Error = "task startup was not confirmed; it was not relaunched"
			return &state, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
}

type capture struct {
	mu        sync.Mutex
	file      *os.File
	state     *Snapshot
	root      *os.Root
	lastState time.Time
	err       error
}

func (c *capture) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	received := len(data)
	remaining := max(int64(0), c.state.MaxOutputBytes-c.state.StoredBytes)
	keep := min(int64(len(data)), remaining)
	if keep > 0 && c.err == nil {
		n, err := c.file.Write(data[:keep])
		c.state.StoredBytes += int64(n)
		c.err = err
		if int64(n) < keep && err == nil {
			c.err = io.ErrShortWrite
		}
		keep = int64(n)
	} else if c.err != nil {
		keep = 0
	}
	c.state.OmittedBytes += int64(received) - keep
	c.state.Truncated = c.state.OmittedBytes > 0
	if time.Since(c.lastState) > 200*time.Millisecond {
		if err := writeJSON(c.root, "state.json", c.state, false); err != nil && c.err == nil {
			c.err = err
		}
		c.lastState = time.Now()
	}
	// Drain all bytes even after the retention limit, without making the user's
	// command fail with a broken pipe just because its stored log is bounded.
	return received, nil
}

func RunWorker(h Handle) error {
	owner, err := processInfo(os.Getpid())
	if err != nil {
		return err
	}
	if owner.PID != h.PID || owner.Start != h.StartTime || owner.Group != owner.PID || owner.Session != owner.PID {
		return errors.New("task worker process identity does not match")
	}
	root, err := openTask(h, false)
	if err != nil {
		return err
	}
	defer root.Close()
	var request Request
	if err = readJSON(root, "request.json", &request); err != nil {
		return err
	}
	if !sameIdentity(h, requestHandle(request)) {
		return errors.New("task worker ownership does not match")
	}
	var state Snapshot
	if err = readJSON(root, "state.json", &state); err != nil {
		return err
	}
	if !sameIdentity(h, state.Handle) || state.Handle.PID != h.PID || state.Handle.StartTime != h.StartTime {
		return errors.New("task startup ownership changed")
	}
	shell, err := exec.LookPath("bash")
	state.Shell = "bash"
	if err != nil {
		shell, err = exec.LookPath("sh")
		state.Shell = "sh"
	}
	if err != nil {
		state.Status = "unknown"
		state.Error = "no command shell is available"
		_ = writeJSON(root, "state.json", state, false)
		return err
	}
	log, err := openRecord(root, "output.log", os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	writer := &capture{file: log, state: &state, root: root}
	cmd := exec.Command(shell, "-lc", request.Command)
	cmd.Dir = request.WorkDir
	cmd.Stdout, cmd.Stderr = writer, writer
	cmd.WaitDelay = 2 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stops := make(chan os.Signal, 1)
	signal.Notify(stops, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(stops)
	if err = cmd.Start(); err != nil {
		state.Status = "unknown"
		state.Error = err.Error()
		_ = writeJSON(root, "state.json", state, false)
		return err
	}
	writer.mu.Lock()
	state.Status = "running"
	err = writeJSON(root, "state.json", state, false)
	writer.mu.Unlock()
	if err != nil {
		_ = stopDescendants(owner)
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var timer <-chan time.Time
	var deadline *time.Timer
	if request.TimeoutMS > 0 {
		deadline = time.NewTimer(time.Duration(request.TimeoutMS) * time.Millisecond)
		defer deadline.Stop()
		timer = deadline.C
	}
	var waitErr error
	reason := "exited"
	var confirmed *bool
	select {
	case waitErr = <-done:
	case <-stops:
		reason = "cancelled"
		cancel()
	case <-timer:
		reason = "timed_out"
		cancel()
	}
	if reason == "exited" && waitErr != nil {
		var exitError *exec.ExitError
		if !errors.As(waitErr, &exitError) {
			reason = "unknown"
			clean := stopDescendants(owner)
			confirmed = &clean
			state.Error = waitErr.Error()
		}
	}
	if ctx.Err() != nil {
		writer.mu.Lock()
		state.Status = "stopping"
		_ = writeJSON(root, "state.json", state, false)
		writer.mu.Unlock()
		clean := stopDescendants(owner)
		confirmed = &clean
		select {
		case waitErr = <-done:
		case <-time.After(3 * time.Second):
			clean = false
			waitErr = errors.New("command streams did not close after cleanup")
		}
		if !clean {
			reason = "unknown"
		}
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if err = log.Sync(); err != nil {
		reason = "unknown"
		state.Error = "task log flush failed: " + err.Error()
	}
	if writer.err != nil {
		reason = "unknown"
		state.Error = "task output retention failed: " + writer.err.Error()
	}
	state.Status = reason
	state.CleanupConfirmed = confirmed
	now := time.Now().UTC()
	state.FinishedAt = &now
	if reason == "exited" && cmd.ProcessState != nil {
		code, known := commandExitCode(cmd.ProcessState)
		if known {
			state.ExitCode = &code
		} else {
			state.Status = "unknown"
			state.Error = "command ended without a confirmed exit code"
		}
	}
	if reason == "unknown" && state.Error == "" && waitErr != nil {
		state.Error = waitErr.Error()
	}
	return writeJSON(root, "state.json", state, false)
}

func Stop(h Handle) (*Snapshot, error) {
	s, err := Inspect(h, ReadOptions{})
	if err != nil {
		return nil, err
	}
	if s.Terminal() {
		return s, nil
	}
	if !nativeSupervisor(h) {
		s.Status = "unknown"
		s.ExitCode = nil
		s.Error = "owned supervisor is unavailable; no replacement process was selected"
		return s, nil
	}
	process, err := os.FindProcess(h.PID)
	if err != nil {
		return nil, err
	}
	if err = process.Signal(syscall.SIGTERM); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, e := Inspect(h, ReadOptions{})
		if e != nil {
			return nil, e
		}
		if current.Terminal() {
			return current, nil
		}
		if time.Now().After(deadline) {
			current.Error = "stop requested, but completion is not yet confirmed"
			return current, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func Wait(ctx context.Context, h Handle, inspect func(context.Context, Handle, ReadOptions) (*Snapshot, error)) (*Snapshot, error) {
	for {
		s, err := inspect(ctx, h, ReadOptions{})
		if err != nil {
			return nil, err
		}
		if s.Terminal() {
			return s, nil
		}
		select {
		case <-ctx.Done():
			return s, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func decoderInput(input io.Reader, value any) error {
	d := json.NewDecoder(io.LimitReader(input, 2<<20))
	d.DisallowUnknownFields()
	if e := d.Decode(value); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("expected one task JSON request")
	}
	return nil
}

func RunCLI(args []string, input io.Reader, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("missing task operation")
	}
	operation := args[0]
	if operation == "task-version" {
		_, err := fmt.Fprintln(output, ProtocolVersion)
		return err
	}
	expectedTaskID := ""
	if operation == "task-run" && len(args) == 3 && args[1] == "--task-id" {
		expectedTaskID = args[2]
	} else if len(args) == 3 && args[1] == "--input-file" {
		path := args[2]
		if !strings.HasPrefix(path, "/tmp/.lester-input-") || len(path) > 180 || strings.Contains(strings.TrimPrefix(path, "/tmp/"), "/") {
			return errors.New("invalid private task request")
		}
		f, err := os.OpenFile(path, os.O_RDONLY|noFollowFlag(), 0)
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || !singleLink(info) || info.Mode().Perm()&0077 != 0 || info.Size() > 2<<20 {
			return errors.New("task request must be a bounded private regular file")
		}
		if err = os.Remove(path); err != nil {
			return err
		}
		input = f
	} else if len(args) != 1 {
		return errors.New("unexpected task arguments")
	}
	var result *Snapshot
	var err error
	switch operation {
	case "task-start":
		var r Request
		if err = decoderInput(input, &r); err == nil {
			var executable string
			executable, err = os.Executable()
			if err == nil {
				result, err = Start(r, executable)
			}
		}
	case "task-inspect":
		var r InspectRequest
		if err = decoderInput(input, &r); err == nil {
			result, err = Inspect(r.Handle, r.Read)
		}
	case "task-stop":
		var h Handle
		if err = decoderInput(input, &h); err == nil {
			result, err = Stop(h)
		}
	case "task-run":
		var h Handle
		if err = decoderInput(input, &h); err == nil {
			if expectedTaskID == "" || h.TaskID != expectedTaskID {
				return errors.New("task worker launch identity does not match")
			}
			err = RunWorker(h)
		}
		return err
	default:
		return errors.New("unsupported native task operation")
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}
