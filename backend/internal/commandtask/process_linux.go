//go:build linux

package commandtask

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type processIdentity struct {
	PID, PPID, Group, Session int
	Start                     uint64
	State                     string
}

func processInfo(pid int) (processIdentity, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return processIdentity{}, err
	}
	text := string(data)
	end := strings.LastIndex(text, ") ")
	if end < 0 {
		return processIdentity{}, errors.New("invalid process identity")
	}
	fields := strings.Fields(text[end+2:])
	if len(fields) < 20 {
		return processIdentity{}, errors.New("invalid process stat")
	}
	p := processIdentity{PID: pid, State: fields[0]}
	p.PPID, _ = strconv.Atoi(fields[1])
	p.Group, _ = strconv.Atoi(fields[2])
	p.Session, _ = strconv.Atoi(fields[3])
	p.Start, err = strconv.ParseUint(fields[19], 10, 64)
	return p, err
}

func sameProcess(pid int, start uint64) bool {
	p, e := processInfo(pid)
	return e == nil && p.Start == start && p.State != "Z" && pid > 1
}

func nativeSupervisor(h Handle) bool {
	p, e := processInfo(h.PID)
	if e != nil || p.Start != h.StartTime || p.State == "Z" || p.Group != h.PID || p.Session != h.PID {
		return false
	}
	data, e := os.ReadFile("/proc/" + strconv.Itoa(h.PID) + "/cmdline")
	if e != nil {
		return false
	}
	args := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
	n := len(args)
	if n < 3 || args[n-3] != "task-run" || args[n-2] != "--task-id" || args[n-1] != h.TaskID {
		return false
	}
	executable, e := os.Executable()
	if e != nil {
		return false
	}
	actual, e := os.Readlink("/proc/" + strconv.Itoa(h.PID) + "/exe")
	return e == nil && strings.TrimSuffix(actual, " (deleted)") == executable
}
func setSession(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
func noFollowFlag() int      { return syscall.O_NOFOLLOW }
func singleLink(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1
}
func atMostOneLink(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink <= 1
}
func commandExitCode(state *os.ProcessState) (int, bool) {
	if code := state.ExitCode(); code >= 0 {
		return code, true
	}
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal()), true
	}
	return -1, false
}
func signalProcess(p processIdentity, sig syscall.Signal) {
	if sameProcess(p.PID, p.Start) {
		_ = syscall.Kill(p.PID, sig)
	}
}

func descendants(owner processIdentity) []processIdentity {
	paths, _ := filepath.Glob("/proc/[0-9]*/stat")
	all := map[int]processIdentity{}
	for _, path := range paths {
		pid, _ := strconv.Atoi(filepath.Base(filepath.Dir(path)))
		if p, e := processInfo(pid); e == nil && p.State != "Z" {
			all[pid] = p
		}
	}
	owned := map[int]bool{owner.PID: true}
	for _, p := range all {
		if p.Group == owner.PID && p.Session == owner.PID {
			owned[p.PID] = true
		}
	}
	for change := true; change; {
		change = false
		for _, p := range all {
			if !owned[p.PID] && owned[p.PPID] {
				owned[p.PID] = true
				change = true
			}
		}
	}
	result := []processIdentity{}
	for pid := range owned {
		if pid != owner.PID {
			if p, ok := all[pid]; ok {
				result = append(result, p)
			}
		}
	}
	return result
}

// Only observed ancestry/session members are selected. An already detached
// double-fork that escaped both relationships requires cgroup ownership.
func stopDescendants(owner processIdentity) bool {
	records := descendants(owner)
	for _, p := range records {
		signalProcess(p, syscall.SIGTERM)
	}
	time.Sleep(150 * time.Millisecond)
	for _, p := range descendants(owner) {
		found := false
		for _, old := range records {
			if old.PID == p.PID && old.Start == p.Start {
				found = true
				break
			}
		}
		if !found {
			records = append(records, p)
		}
	}
	for _, p := range records {
		signalProcess(p, syscall.SIGKILL)
	}
	deadline := time.Now().Add(time.Second)
	for {
		alive := false
		for _, p := range records {
			if sameProcess(p.PID, p.Start) {
				alive = true
				break
			}
		}
		if !alive {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}
