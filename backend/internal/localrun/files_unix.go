//go:build !windows

package localrun

import "golang.org/x/sys/unix"

const localFileReadFlags = unix.O_NONBLOCK | unix.O_NOFOLLOW
