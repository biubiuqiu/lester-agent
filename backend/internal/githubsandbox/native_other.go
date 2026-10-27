//go:build !linux

package githubsandbox

import (
	"errors"
	"os"
)

const noFollowFlag = 0
const nonBlockingFlag = 0

func directoryPath(*os.File) string { return "/nonexistent" }
func renameNew(*os.Root, string, string) error {
	return errors.New("GitHub sandbox transfers require the Linux Computer runtime")
}
