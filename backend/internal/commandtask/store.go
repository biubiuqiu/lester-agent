package commandtask

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

func validateRequest(r Request) error {
	for _, id := range []string{r.TaskID, r.RunID, r.ConversationID, r.OwnershipToken} {
		if _, err := uuid.Parse(id); err != nil {
			return errors.New("task identity must be platform UUIDs")
		}
	}
	if !filepath.IsAbs(r.WorkDir) || filepath.Clean(r.WorkDir) != r.WorkDir || r.Command == "" || len(r.Command) > 1<<20 {
		return errors.New("invalid task directory or command")
	}
	if r.TimeoutMS < 0 || r.TimeoutMS > 600000 {
		return errors.New("invalid task timeout")
	}
	if r.MaxOutputBytes < 0 || r.MaxOutputBytes > DefaultLogLimit {
		return errors.New("task output limit exceeds 64 MiB")
	}
	return nil
}

func taskDirectory(h Handle) string { return filepath.Join(h.WorkDir, ".lester", "tasks", h.TaskID) }
func requestHandle(r Request) Handle {
	return Handle{TaskID: r.TaskID, RunID: r.RunID, ConversationID: r.ConversationID, WorkDir: r.WorkDir, OwnershipToken: r.OwnershipToken}
}

func sameIdentity(a, b Handle) bool {
	return a.TaskID == b.TaskID && a.RunID == b.RunID && a.ConversationID == b.ConversationID && a.WorkDir == b.WorkDir && subtle.ConstantTimeCompare([]byte(a.OwnershipToken), []byte(b.OwnershipToken)) == 1
}

func openTask(h Handle, create bool) (*os.Root, error) {
	if _, err := uuid.Parse(h.TaskID); err != nil {
		return nil, errors.New("invalid task ID")
	}
	if !filepath.IsAbs(h.WorkDir) || filepath.Clean(h.WorkDir) != h.WorkDir {
		return nil, errors.New("invalid task work directory")
	}
	root, err := os.OpenRoot(h.WorkDir)
	if err != nil {
		return nil, err
	}
	for _, part := range []string{".lester", "tasks", h.TaskID} {
		if create {
			if e := root.Mkdir(part, 0700); e != nil && !os.IsExist(e) {
				root.Close()
				return nil, e
			}
		}
		info, e := root.Lstat(part)
		if e != nil {
			root.Close()
			return nil, e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, errors.New("task store cannot traverse a link")
		}
		next, e := root.OpenRoot(part)
		root.Close()
		if e != nil {
			return nil, e
		}
		root = next
	}
	return root, nil
}

func writeJSON(root *os.Root, name string, value any, exclusive bool) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if exclusive {
		f, e := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		defer f.Close()
		if _, e = f.Write(data); e != nil {
			return e
		}
		return f.Sync()
	}
	tmp := ".state-" + uuid.NewString()
	f, e := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	defer root.Remove(tmp)
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = root.Rename(tmp, name); e != nil {
		return e
	}
	dir, e := root.Open(".")
	if e == nil {
		defer dir.Close()
		e = dir.Sync()
	}
	return e
}

func readJSON(root *os.Root, name string, value any) error {
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 2<<20 {
		return errors.New("invalid native task record")
	}
	f, err := openRecord(root, name, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 2<<20))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(value); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("invalid native task JSON")
	}
	return nil
}

func openRecord(root *os.Root, name string, flags int, mode os.FileMode) (*os.File, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 || !singleLink(before) {
		return nil, errors.New("native task file must be regular with one hard link")
	}
	f, err := root.OpenFile(name, flags|noFollowFlag(), mode)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || !atMostOneLink(info) || name == "output.log" && (!singleLink(info) || !os.SameFile(before, info)) {
		f.Close()
		return nil, errors.New("native task file must be an unlinked regular file")
	}
	return f, nil
}

func Inspect(h Handle, read ReadOptions) (*Snapshot, error) {
	root, err := openTask(h, false)
	if err != nil {
		if os.IsNotExist(err) {
			return &Snapshot{Handle: h, Status: "unknown", Shell: "unknown", Error: "task record is unavailable; it was not restarted"}, nil
		}
		return nil, err
	}
	defer root.Close()
	var state Snapshot
	if err = readJSON(root, "state.json", &state); err != nil {
		return nil, err
	}
	if !sameIdentity(h, state.Handle) {
		return nil, errors.New("task ownership does not match")
	}
	if h.PID != 0 && (h.PID != state.Handle.PID || h.StartTime != state.Handle.StartTime) {
		return nil, errors.New("task process ownership changed")
	}
	if h.PID == 0 {
		state.Handle = h
		state.Status = "unknown"
		state.ExitCode = nil
		state.FinishedAt = nil
		state.CleanupConfirmed = nil
		state.Error = "launch response was not confirmed; metadata cannot grant process ownership"
	}
	if h.PID != 0 && !state.Terminal() && !nativeSupervisor(state.Handle) {
		state.Status = "unknown"
		state.ExitCode = nil
		state.Error = "supervisor is unavailable; command was not restarted"
	}
	if h.PID != 0 && state.Status != "unknown" && state.Terminal() && sameProcess(h.PID, h.StartTime) {
		state.Status = "running"
		state.ExitCode = nil
		state.FinishedAt = nil
		state.Error = "completion record is present but supervisor exit is not yet confirmed"
	}
	if read.Offset < 0 || read.Limit < 0 || read.Limit > MaxPageBytes {
		return nil, errors.New("invalid task log page")
	}
	if read.Limit == 0 {
		read.Limit = 64 << 10
	}
	log, err := openRecord(root, "output.log", os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer log.Close()
	info, err := log.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("task log is not a regular file")
	}
	state.StoredBytes = info.Size()
	if read.Offset > info.Size() {
		return nil, fmt.Errorf("log offset exceeds %d stored bytes", info.Size())
	}
	data := make([]byte, min(int64(read.Limit), info.Size()-read.Offset))
	n, e := log.ReadAt(data, read.Offset)
	if e != nil && e != io.EOF {
		return nil, e
	}
	data = data[:n]
	state.Page = Page{Data: data, Offset: read.Offset, NextOffset: read.Offset + int64(n), HasMore: read.Offset+int64(n) < info.Size(), StoredBytes: info.Size(), OmittedBytes: state.OmittedBytes, Truncated: state.Truncated, Stream: "combined"}
	return &state, nil
}
