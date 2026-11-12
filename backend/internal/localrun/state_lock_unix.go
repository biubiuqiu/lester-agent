//go:build !windows

package localrun

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockState(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

const stateOpenFlags = unix.O_NOFOLLOW | unix.O_NONBLOCK

func syncStateDirectory(root *os.Root) error {
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
