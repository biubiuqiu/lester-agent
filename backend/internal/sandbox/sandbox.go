package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"
)

var (
	ErrSandboxNotFound           = errors.New("sandbox not found")
	ErrTerminalResizeUnsupported = errors.New("terminal resize is not supported by this provider")
)

type CreateOptions struct {
	ID     string `json:"id"`
	Image  string `json:"image,omitempty"`
	CPUs   string `json:"cpus,omitempty"`
	Memory string `json:"memory,omitempty"`
}
type Sandbox struct {
	ID           string    `json:"id"`
	Provider     string    `json:"provider"`
	ProviderRef  string    `json:"provider_ref"`
	Status       string    `json:"status"`
	LastActiveAt time.Time `json:"last_active_at"`
}
type Command struct {
	Command        string `json:"command"`
	WorkDir        string `json:"work_dir,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}
type CommandResult struct {
	ExitCode           int    `json:"exit_code"`
	Stdout             string `json:"stdout"`
	Stderr             string `json:"stderr"`
	DurationMS         int64  `json:"duration_ms"`
	StdoutTruncated    bool   `json:"stdout_truncated,omitempty"`
	StderrTruncated    bool   `json:"stderr_truncated,omitempty"`
	StdoutOmittedBytes int64  `json:"stdout_omitted_bytes,omitempty"`
	StderrOmittedBytes int64  `json:"stderr_omitted_bytes,omitempty"`
}
type FileEntry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	IsDir      bool      `json:"is_dir"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}
type FileLine struct {
	Text              string `json:"text"`
	OmittedCharacters int    `json:"omitted_characters,omitempty"`
}
type FileLines struct {
	Lines      []FileLine `json:"lines"`
	StartLine  int        `json:"start_line"`
	TotalLines int        `json:"total_lines"`
}
type FileEditRequest struct {
	OldString      string `json:"old_string"`
	NewString      string `json:"new_string"`
	ReplaceAll     bool   `json:"replace_all,omitempty"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
}
type FileEditResult struct {
	OK           bool   `json:"ok"`
	Replacements int    `json:"replacements"`
	SHA256       string `json:"sha256"`
}

// TerminalSession is an interactive shell owned by a Provider. Provider
// implementations may use a local PTY, a remote process stream, or another
// transport without exposing those details to the HTTP service.
type TerminalSession interface {
	io.ReadWriteCloser
	Resize(context.Context, int, int) error
}

type Provider interface {
	Name() string
	Create(context.Context, CreateOptions) (*Sandbox, error)
	Inspect(context.Context, string) (*Sandbox, error)
	Start(context.Context, string) error
	Suspend(context.Context, string) error
	Resume(context.Context, string) error
	Destroy(context.Context, string) error
	Exec(context.Context, string, Command) (*CommandResult, error)
	ReadFile(context.Context, string, string, string) ([]byte, error)
	ReadFileLines(context.Context, string, string, string, int, int) (*FileLines, error)
	WriteFile(context.Context, string, string, string, []byte) error
	EditFile(context.Context, string, string, string, FileEditRequest) (*FileEditResult, error)
	ListFiles(context.Context, string, string, string) ([]FileEntry, error)
	OpenTerminal(context.Context, string, string) (TerminalSession, error)
}

func decodeEntries(data []byte) ([]FileEntry, error) {
	var entries []FileEntry
	err := json.Unmarshal(data, &entries)
	return entries, err
}
