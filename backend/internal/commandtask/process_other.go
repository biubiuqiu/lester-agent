//go:build !linux

package commandtask

import (
	"errors"
	"os"
	"os/exec"
)

type processIdentity struct {
	PID, PPID, Group, Session int
	Start                     uint64
	State                     string
}

func processInfo(int) (processIdentity, error) {
	return processIdentity{}, errors.New("native managed tasks require Linux procfs")
}
func sameProcess(int, uint64) bool   { return false }
func nativeSupervisor(Handle) bool   { return false }
func setSession(*exec.Cmd)           {}
func noFollowFlag() int              { return 0 }
func singleLink(os.FileInfo) bool    { return true }
func atMostOneLink(os.FileInfo) bool { return true }
func commandExitCode(state *os.ProcessState) (int, bool) {
	code := state.ExitCode()
	return code, code >= 0
}
func stopDescendants(processIdentity) bool { return false }
