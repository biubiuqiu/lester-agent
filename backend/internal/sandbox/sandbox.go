package sandbox

import (
	"context"
	"encoding/json"
	"time"
)

type CreateOptions struct {
	ID     string `json:"id"`
	Image  string `json:"image,omitempty"`
	CPUs   string `json:"cpus,omitempty"`
	Memory string `json:"memory,omitempty"`
}
type Sandbox struct {
	ID           string    `json:"id"`
	ProviderRef  string    `json:"provider_ref"`
	Status       string    `json:"status"`
	LastActiveAt time.Time `json:"last_active_at"`
}
type Command struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}
type CommandResult struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMS int64  `json:"duration_ms"`
}
type FileEntry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	IsDir      bool      `json:"is_dir"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}
type Provider interface {
	Create(context.Context, CreateOptions) (*Sandbox, error)
	Start(context.Context, string) error
	Suspend(context.Context, string) error
	Resume(context.Context, string) error
	Destroy(context.Context, string) error
	Exec(context.Context, string, Command) (*CommandResult, error)
	ReadFile(context.Context, string, string) ([]byte, error)
	WriteFile(context.Context, string, string, []byte) error
	ListFiles(context.Context, string, string) ([]FileEntry, error)
}

func decodeEntries(data []byte) ([]FileEntry, error) {
	var entries []FileEntry
	err := json.Unmarshal(data, &entries)
	return entries, err
}
