//go:build windows

package localrun

import (
	"os"

	"golang.org/x/sys/windows"
)

func lockState(file *os.File) error {
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, new(windows.Overlapped))
}

const stateOpenFlags = 0

func syncStateDirectory(root *os.Root) error { return nil }
