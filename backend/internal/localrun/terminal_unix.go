//go:build !windows

package localrun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

type TerminalManager struct {
	mu       sync.Mutex
	sessions map[string]*localTerminal
	closed   bool
}

type localTerminal struct {
	cmd      *exec.Cmd
	pty      *os.File
	done     chan struct{}
	readDone chan struct{}
	stop     sync.Once
	cleanup  func()
	writeMu  sync.Mutex
	onData   func([]byte)
	onExit   func(int)
}

func NewTerminalManager() *TerminalManager {
	return &TerminalManager{sessions: make(map[string]*localTerminal)}
}

func (m *TerminalManager) Open(ctx context.Context, root string, cols, rows uint16, onData func([]byte), onExit func(int)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if cols == 0 || rows == 0 {
		return "", errors.New("invalid terminal dimensions")
	}
	cmd, cleanup, err := newLocalShell(root, "-i")
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		cleanup()
		return "", errors.New("terminal manager is closed")
	}
	if m.sessions == nil {
		m.sessions = make(map[string]*localTerminal)
	}
	if len(m.sessions) >= 8 {
		cleanup()
		return "", errors.New("local terminal session limit reached")
	}
	file, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		cleanup()
		return "", fmt.Errorf("start local terminal: %w", err)
	}
	id := uuid.NewString()
	s := &localTerminal{cmd: cmd, pty: file, done: make(chan struct{}), readDone: make(chan struct{}), cleanup: cleanup, onData: onData, onExit: onExit}
	m.sessions[id] = s
	go s.read()
	go func() {
		_ = cmd.Wait()
		// Let the reader deliver the final shell output before closing the PTY.
		select {
		case <-s.readDone:
		case <-time.After(100 * time.Millisecond):
		}
		// Closing the controlling PTY hangs up its foreground job. Clean any
		// remaining shell-group children before making the session unavailable.
		s.shutdown()
		s.cleanup()
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
		close(s.done)
		if s.onExit != nil {
			s.onExit(localExitCode(cmd.ProcessState))
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			s.shutdown()
		case <-s.done:
		}
	}()
	return id, nil
}

func (s *localTerminal) read() {
	defer close(s.readDone)
	data := make([]byte, 32<<10)
	for {
		n, err := s.pty.Read(data)
		if n > 0 && s.onData != nil {
			s.onData(append([]byte(nil), data[:n]...))
		}
		if err != nil {
			return
		}
	}
}

func (m *TerminalManager) session(id string) (*localTerminal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, errors.New("local terminal session not found")
	}
	return s, nil
}

func (m *TerminalManager) Input(id string, data []byte) error {
	if len(data) > 1<<20 {
		return errors.New("terminal input exceeds limit")
	}
	s, err := m.session(id)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = s.pty.Write(data)
	return err
}

func (m *TerminalManager) Resize(id string, cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return errors.New("invalid terminal dimensions")
	}
	s, err := m.session(id)
	if err != nil {
		return err
	}
	return pty.Setsize(s.pty, &pty.Winsize{Cols: cols, Rows: rows})
}

func (m *TerminalManager) Close(id string) error {
	s, err := m.session(id)
	if err != nil {
		return err
	}
	s.shutdown()
	<-s.done
	return nil
}

func (m *TerminalManager) CloseAll() {
	m.mu.Lock()
	m.closed = true
	sessions := make([]*localTerminal, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()
	for _, s := range sessions {
		s.shutdown()
	}
	for _, s := range sessions {
		<-s.done
	}
}

func (s *localTerminal) shutdown() {
	s.stop.Do(func() {
		// Job control may put the active program in a different process group
		// from Bash. Only target the group attached to this owned terminal.
		foreground, _ := unix.IoctlGetInt(int(s.pty.Fd()), unix.TIOCGPGRP)
		groups := localProcessGroups(s.cmd.Process.Pid)
		if foreground > 1 && foreground != s.cmd.Process.Pid {
			groups = append(groups, foreground)
		}
		signalLocalGroups(groups, syscall.SIGHUP)
		signalLocalGroups(groups, syscall.SIGTERM)
		_ = s.pty.Close()
		time.Sleep(150 * time.Millisecond)
		signalLocalGroups(groups, syscall.SIGKILL)
	})
}
