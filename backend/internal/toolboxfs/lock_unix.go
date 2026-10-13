//go:build !windows

package toolboxfs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const nonBlockingOpenFlag = unix.O_NONBLOCK
const noFollowOpenFlag = unix.O_NOFOLLOW

// Lock files are never unlinked: replacing the lock inode would split the
// serialization domain of already-running helper processes.
func lockFile(root, destination string) (func(), error) {
	directory := filepath.Join(root, ".agent", "fs-locks")
	_, realParent, _, err := resolveWriteTarget(root, filepath.Join(directory, "placeholder"))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(destination))
	scoped, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer scoped.Close()
	lockRelative, err := filepath.Rel(root, filepath.Join(realParent, hex.EncodeToString(sum[:])+".lock"))
	if err != nil {
		return nil, err
	}
	file, err := scoped.OpenFile(lockRelative, os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open file-operation lock: %w", err)
	}
	if info, statErr := file.Stat(); statErr != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("file-operation lock is not a regular file")
	}
	if err = unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		file.Close()
		return nil, fmt.Errorf("lock file operation: %w", err)
	}
	return func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN); _ = file.Close() }, nil
}
