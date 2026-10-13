package agenttool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
)

type managedBashFixture struct {
	fakeSandbox
	data   []byte
	handle sandbox.CommandTaskHandle
	starts int
	code   int
}

func (f *managedBashFixture) StartTask(_ context.Context, _ string, r sandbox.CommandTaskRequest) (*sandbox.CommandTaskSnapshot, error) {
	f.starts++
	f.handle = sandbox.CommandTaskHandle{TaskID: r.TaskID, RunID: r.RunID, ConversationID: r.ConversationID, WorkDir: r.WorkDir, OwnershipToken: r.OwnershipToken, PID: 123, StartTime: 456}
	return &sandbox.CommandTaskSnapshot{Handle: f.handle, Status: "running", Shell: "bash", Description: r.Description, StartedAt: time.Now(), LogPath: ".lester/tasks/" + r.TaskID + "/output.log"}, nil
}
func (f *managedBashFixture) InspectTask(_ context.Context, _ string, h sandbox.CommandTaskHandle, o sandbox.CommandTaskReadOptions) (*sandbox.CommandTaskSnapshot, error) {
	limit := o.Limit
	if limit == 0 {
		limit = 64 << 10
	}
	end := min(len(f.data), int(o.Offset)+limit)
	now := time.Now()
	return &sandbox.CommandTaskSnapshot{Handle: h, Status: "exited", Shell: "bash", ExitCode: &f.code, StartedAt: now.Add(-time.Second), FinishedAt: &now, LogPath: ".lester/tasks/" + h.TaskID + "/output.log", MaxOutputBytes: 64 << 20, StoredBytes: int64(len(f.data)), Page: sandbox.CommandTaskPage{Data: f.data[o.Offset:end], Offset: o.Offset, NextOffset: int64(end), HasMore: end < len(f.data), StoredBytes: int64(len(f.data)), Stream: "combined"}}, nil
}
func (f *managedBashFixture) StopTask(ctx context.Context, id string, h sandbox.CommandTaskHandle) (*sandbox.CommandTaskSnapshot, error) {
	return f.InspectTask(ctx, id, h, sandbox.CommandTaskReadOptions{})
}

func TestManagedBashPreviewClippingIsSeparateFromRawRetention(t *testing.T) {
	f := &managedBashFixture{data: []byte(strings.Repeat("x", 40000)), code: 7}
	var states []string
	result, err := (Bash{}).Execute(context.Background(), Environment{Sandboxes: f, RunID: uuid.New(), ConversationID: uuid.New(), WorkDir: "/workspace/conversations/test", RecordTask: func(_ context.Context, s *sandbox.CommandTaskSnapshot) error {
		states = append(states, s.Status)
		return nil
	}, Emit: func(string, map[string]any) {}}, json.RawMessage(`{"command":"printf output","description":"Output fixture"}`))
	if err != nil {
		t.Fatal(err)
	}
	view := result.(map[string]any)
	if f.starts != 1 || view["preview_truncated"] != true || view["truncated"] != false || view["full_output_saved"] != true || view["exit_code"] != 7 || view["exit_code_known"] != true {
		t.Fatalf("retention/preview flags: %#v", view)
	}
	if len(states) != 3 || states[0] != "starting" || states[1] != "running" || states[2] != "exited" {
		t.Fatalf("task state ordering: %v", states)
	}
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), f.handle.OwnershipToken) || strings.Contains(string(encoded), `"pid"`) {
		t.Fatal("trusted process ownership leaked into a model-visible result")
	}
}
