package commandtask

import "time"

const (
	ProtocolVersion = 1
	DefaultLogLimit = int64(64 << 20)
	MaxPageBytes    = 256 << 10
)

type Request struct {
	TaskID         string `json:"task_id"`
	RunID          string `json:"run_id"`
	ConversationID string `json:"conversation_id"`
	WorkDir        string `json:"work_dir"`
	Command        string `json:"command"`
	Description    string `json:"description,omitempty"`
	OwnershipToken string `json:"ownership_token"`
	TimeoutMS      int    `json:"timeout_ms,omitempty"`
	MaxOutputBytes int64  `json:"max_output_bytes,omitempty"`
}

// Handle is private platform state. APIs and model-visible results must not
// disclose the token or accept a PID supplied by an end user.
type Handle struct {
	TaskID         string `json:"task_id"`
	RunID          string `json:"run_id"`
	ConversationID string `json:"conversation_id"`
	WorkDir        string `json:"work_dir"`
	OwnershipToken string `json:"ownership_token"`
	PID            int    `json:"pid"`
	StartTime      uint64 `json:"start_time"`
}

type ReadOptions struct {
	Offset int64 `json:"offset,omitempty"`
	Limit  int   `json:"limit,omitempty"`
}

type Page struct {
	Data         []byte `json:"data"`
	Offset       int64  `json:"offset"`
	NextOffset   int64  `json:"next_offset"`
	HasMore      bool   `json:"has_more"`
	StoredBytes  int64  `json:"stored_bytes"`
	OmittedBytes int64  `json:"omitted_bytes"`
	Truncated    bool   `json:"truncated"`
	Stream       string `json:"stream"`
}

type Snapshot struct {
	Handle           Handle     `json:"handle"`
	Status           string     `json:"status"`
	Shell            string     `json:"shell"`
	Description      string     `json:"description,omitempty"`
	ExitCode         *int       `json:"exit_code"`
	CleanupConfirmed *bool      `json:"cleanup_confirmed,omitempty"`
	StartedAt        time.Time  `json:"started_at"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	LogPath          string     `json:"log_path"`
	MaxOutputBytes   int64      `json:"max_output_bytes"`
	StoredBytes      int64      `json:"stored_bytes"`
	OmittedBytes     int64      `json:"omitted_bytes"`
	Truncated        bool       `json:"truncated"`
	Error            string     `json:"error,omitempty"`
	Page             Page       `json:"page"`
}

func (s Snapshot) Terminal() bool {
	return s.Status == "exited" || s.Status == "timed_out" || s.Status == "cancelled" || s.Status == "unknown"
}

type InspectRequest struct {
	Handle Handle      `json:"handle"`
	Read   ReadOptions `json:"read"`
}
