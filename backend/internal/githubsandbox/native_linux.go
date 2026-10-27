//go:build linux

package githubsandbox

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

const noFollowFlag = unix.O_NOFOLLOW
const nonBlockingFlag = unix.O_NONBLOCK

// os/exec chdirs before remapping ExtraFiles. The inherited original FD is
// still live at that point; it binds the child cwd to the opened directory inode.
func directoryPath(directory *os.File) string { return fmt.Sprintf("/proc/self/fd/%d", directory.Fd()) }
func renameNew(root *os.Root, old, new string) error {
	fd, err := root.Open(".")
	if err != nil {
		return err
	}
	defer fd.Close()
	return unix.Renameat2(int(fd.Fd()), old, int(fd.Fd()), new, unix.RENAME_NOREPLACE)
}
