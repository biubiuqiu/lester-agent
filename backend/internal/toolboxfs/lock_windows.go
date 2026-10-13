//go:build windows

package toolboxfs

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

const nonBlockingOpenFlag = 0
const noFollowOpenFlag = 0

func lockFile(root, destination string) (func(), error) {
	directory := filepath.Join(root, ".agent", "fs-locks")
	_, realParent, _, err := resolveWriteTarget(root, filepath.Join(directory, "placeholder"))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(destination))
	file, err := os.OpenFile(filepath.Join(realParent, hex.EncodeToString(sum[:])+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	overlapped := new(windows.Overlapped)
	if err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, overlapped); err != nil {
		file.Close()
		return nil, err
	}
	return func() { _ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped); _ = file.Close() }, nil
}
