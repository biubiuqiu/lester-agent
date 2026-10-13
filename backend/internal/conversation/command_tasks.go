package conversation

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/eventlog"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// commandTaskView deliberately has no process ownership handle. A caller can
// select a scoped task UUID, never supply a process ID or ownership token.
func commandTaskView(snapshot *sandbox.CommandTaskSnapshot) map[string]any {
	return map[string]any{
		"task_id": snapshot.Handle.TaskID, "run_id": snapshot.Handle.RunID,
		"conversation_id": snapshot.Handle.ConversationID,
		"status":          snapshot.Status, "shell": snapshot.Shell,
		"description": snapshot.Description, "exit_code": snapshot.ExitCode,
		"cleanup_confirmed": snapshot.CleanupConfirmed,
		"started_at":        snapshot.StartedAt, "finished_at": snapshot.FinishedAt,
		"log_path": snapshot.LogPath, "stored_bytes": snapshot.StoredBytes,
		"omitted_bytes": snapshot.OmittedBytes, "truncated": snapshot.Truncated,
		"page": snapshot.Page,
	}
}

func taskFinished(status string) bool {
	return status == "exited" || status == "cancelled" || status == "timed_out"
}

func sameTaskIdentity(a, b sandbox.CommandTaskHandle) bool {
	return a.TaskID == b.TaskID && a.RunID == b.RunID && a.ConversationID == b.ConversationID && a.WorkDir == b.WorkDir &&
		a.OwnershipToken != "" && subtle.ConstantTimeCompare([]byte(a.OwnershipToken), []byte(b.OwnershipToken)) == 1 &&
		(a.PID == 0 || a.PID == b.PID && a.StartTime == b.StartTime)
}

func (s *Service) recordCommandTask(ctx context.Context, runID, conversationID uuid.UUID, computer *Computer, snapshot *sandbox.CommandTaskSnapshot) error {
	if snapshot == nil || computer == nil {
		return errors.New("missing command task state")
	}
	handle := snapshot.Handle
	id, err := uuid.Parse(handle.TaskID)
	if err != nil || handle.RunID != runID.String() || handle.ConversationID != conversationID.String() || handle.WorkDir != computer.WorkDir || handle.OwnershipToken == "" {
		return errors.New("invalid command task identity")
	}
	switch snapshot.Status {
	case "starting", "running", "stopping", "exited", "timed_out", "cancelled", "unknown":
	default:
		return errors.New("invalid command task status")
	}
	// Cancellation may already have revoked the run context. Persist only the
	// observed command facts on this bounded cleanup context, not new work.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Follow the transcript/event lock order: conversation, then run, then the
	// workspace event sequence. A task callback never inverts cancellation locks.
	if _, err = tx.Exec(ctx, `SELECT id FROM conversations WHERE id=$1 FOR UPDATE`, conversationID); err != nil {
		return err
	}
	var workspaceID uuid.UUID
	var runStatus string
	if err = tx.QueryRow(ctx, `SELECT c.workspace_id,r.status FROM conversations c JOIN runs r ON r.conversation_id=c.id WHERE c.id=$1 AND r.id=$2 FOR SHARE OF r`, conversationID, runID).Scan(&workspaceID, &runStatus); err != nil {
		return err
	}
	var existing sandbox.CommandTaskHandle
	var existingRaw []byte
	var oldStatus string
	err = tx.QueryRow(ctx, `SELECT handle,status FROM command_tasks WHERE id=$1 AND workspace_id=$2 AND conversation_id=$3 AND run_id=$4 FOR UPDATE`, id, workspaceID, conversationID, runID).Scan(&existingRaw, &oldStatus)
	creating := errors.Is(err, pgx.ErrNoRows)
	if !creating && err != nil {
		return err
	}
	if creating {
		if runStatus != "running" || snapshot.Status != "starting" || handle.PID != 0 {
			return errors.New("command launch must be recorded before execution")
		}
	} else {
		if json.Unmarshal(existingRaw, &existing) != nil || !sameTaskIdentity(existing, handle) {
			return errors.New("command ownership changed")
		}
		if taskFinished(oldStatus) {
			return tx.Commit(ctx)
		}
	}
	private, err := json.Marshal(handle)
	if err != nil {
		return err
	}
	view := commandTaskView(snapshot)
	// Logs live in the Computer. Never copy a large page into state/events.
	delete(view, "page")
	public, err := json.Marshal(view)
	if err != nil {
		return err
	}
	if creating {
		_, err = tx.Exec(ctx, `INSERT INTO command_tasks(id,workspace_id,conversation_id,run_id,sandbox_ref,handle,snapshot,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, workspaceID, conversationID, runID, computer.SandboxID, private, public, snapshot.Status)
	} else {
		_, err = tx.Exec(ctx, `UPDATE command_tasks SET handle=$2,snapshot=$3,status=$4,revision=revision+1,updated_at=now() WHERE id=$1`, id, private, public, snapshot.Status)
	}
	if err != nil {
		return err
	}
	if creating || oldStatus != snapshot.Status {
		if err = eventlog.Append(ctx, tx, runID, conversationID, "TASK_UPDATED", view); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Service) commandTask(ctx context.Context, workspaceID, conversationID, taskID uuid.UUID, read sandbox.CommandTaskReadOptions, stop bool) (map[string]any, error) {
	var handle sandbox.CommandTaskHandle
	var raw, saved []byte
	var ref string
	var runID uuid.UUID
	var rowRevision int64
	if err := s.db.QueryRow(ctx, `SELECT t.handle,t.snapshot,t.sandbox_ref,t.run_id,t.revision FROM command_tasks t JOIN conversations c ON c.id=t.conversation_id AND c.workspace_id=t.workspace_id WHERE t.id=$1 AND t.workspace_id=$2 AND t.conversation_id=$3`, taskID, workspaceID, conversationID).Scan(&raw, &saved, &ref, &runID, &rowRevision); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &handle); err != nil {
		return nil, errors.New("invalid stored task handle")
	}
	var snapshot *sandbox.CommandTaskSnapshot
	var err error
	if stop {
		snapshot, err = s.sandboxes.StopTask(ctx, ref, handle)
	} else {
		snapshot, err = s.sandboxes.InspectTask(ctx, ref, handle, read)
	}
	if err != nil {
		var view map[string]any
		if json.Unmarshal(saved, &view) != nil {
			return nil, errors.New("task state unavailable")
		}
		view["last_known_status"] = view["status"]
		view["status"] = "unknown"
		view["sync_error"] = true
		view["stop_confirmed"] = false
		view["revision"] = rowRevision
		if !taskFinished(fmtTaskStatus(view["last_known_status"])) {
			var uncertain sandbox.CommandTaskSnapshot
			if json.Unmarshal(saved, &uncertain) == nil {
				uncertain.Handle = handle
				uncertain.Status = "unknown"
				if s.recordCommandTask(ctx, runID, conversationID, &Computer{SandboxID: ref, WorkDir: handle.WorkDir}, &uncertain) == nil {
					if s.db.QueryRow(ctx, `SELECT revision FROM command_tasks WHERE id=$1 AND workspace_id=$2`, taskID, workspaceID).Scan(&rowRevision) == nil {
						view["revision"] = rowRevision
					}
				}
			}
		}
		return view, nil
	}
	if snapshot == nil || !sameTaskIdentity(handle, snapshot.Handle) {
		return nil, errors.New("task ownership check failed")
	}
	computer := &Computer{SandboxID: ref, WorkDir: handle.WorkDir}
	if err = s.recordCommandTask(ctx, runID, conversationID, computer, snapshot); err != nil {
		return nil, err
	}
	// A late inspect must not overwrite an already committed terminal state.
	var canonical []byte
	var revision int64
	if err = s.db.QueryRow(ctx, `SELECT snapshot,revision FROM command_tasks WHERE id=$1 AND workspace_id=$2 AND conversation_id=$3`, taskID, workspaceID, conversationID).Scan(&canonical, &revision); err != nil {
		return nil, err
	}
	var view map[string]any
	if err = json.Unmarshal(canonical, &view); err != nil {
		return nil, err
	}
	view["revision"] = revision
	view["page"] = snapshot.Page
	return view, nil
}

func fmtTaskStatus(value any) string { status, _ := value.(string); return status }

// Refresh persisted task facts independently of Agent completion. No command is
// launched here: an abandoned launch is inspected with its original identity.
func (s *Service) reconcileCommandTasks(ctx context.Context) {
	ctx, cancelBatch := context.WithTimeout(ctx, 20*time.Second)
	defer cancelBatch()
	rows, err := s.db.Query(ctx, `SELECT workspace_id,conversation_id,id FROM command_tasks WHERE status IN ('starting','running','stopping','unknown') ORDER BY updated_at LIMIT 25`)
	if err != nil {
		return
	}
	type item struct{ workspace, conversation, task uuid.UUID }
	var items []item
	for rows.Next() {
		var entry item
		if rows.Scan(&entry.workspace, &entry.conversation, &entry.task) == nil {
			items = append(items, entry)
		}
	}
	rows.Close()
	for _, entry := range items {
		if ctx.Err() != nil {
			return
		}
		checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, _ = s.commandTask(checkCtx, entry.workspace, entry.conversation, entry.task, sandbox.CommandTaskReadOptions{Limit: 1}, false)
		cancel()
	}
}

// The download is a finite snapshot of currently retained bytes, even while a
// process is still writing. Binary bytes are preserved; model/UI pages are text.
func (h *Handler) CommandTaskLog(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid conversation"))
		return
	}
	taskID, err := uuid.Parse(chi.URLParam(r, "taskID"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid task"))
		return
	}
	view, err := h.service.commandTask(r.Context(), p.WorkspaceID, conversationID, taskID, sandbox.CommandTaskReadOptions{Limit: 256 << 10}, false)
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, 404, errors.New("task not found"))
		return
	}
	page, ok := view["page"].(sandbox.CommandTaskPage)
	if err != nil || !ok || view["sync_error"] == true {
		httpapi.Error(w, 503, errors.New("task log unavailable"))
		return
	}
	size := page.StoredBytes
	if size < 0 || size > 64<<20 {
		httpapi.Error(w, 500, errors.New("invalid log size"))
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+taskID.String()+`.log"`)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	var offset int64
	for offset < size {
		data := page.Data
		if int64(len(data)) > size-offset {
			data = data[:size-offset]
		}
		if len(data) == 0 {
			return
		}
		if _, err = w.Write(data); err != nil {
			return
		}
		offset += int64(len(data))
		if offset >= size {
			return
		}
		view, err = h.service.commandTask(r.Context(), p.WorkspaceID, conversationID, taskID, sandbox.CommandTaskReadOptions{Offset: offset, Limit: 256 << 10}, false)
		if err != nil {
			return
		}
		page, ok = view["page"].(sandbox.CommandTaskPage)
		if !ok || page.Offset != offset {
			return
		}
	}
}

func (s *Service) commandTasks(ctx context.Context, workspaceID, conversationID uuid.UUID) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `SELECT snapshot,revision FROM command_tasks WHERE workspace_id=$1 AND conversation_id=$2 ORDER BY created_at DESC,id DESC LIMIT 100`, workspaceID, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var raw []byte
		var revision int64
		if err = rows.Scan(&raw, &revision); err != nil {
			return nil, err
		}
		var view map[string]any
		if err = json.Unmarshal(raw, &view); err != nil {
			return nil, err
		}
		view["revision"] = revision
		items = append(items, view)
	}
	return items, rows.Err()
}

func (h *Handler) CommandTasks(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid conversation"))
		return
	}
	// An empty result must not leak the existence of a foreign conversation.
	var exists bool
	if err = h.service.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM conversations WHERE id=$1 AND workspace_id=$2)`, id, p.WorkspaceID).Scan(&exists); err != nil {
		httpapi.Error(w, 500, errors.New("task list unavailable"))
		return
	}
	if !exists {
		httpapi.Error(w, 404, errors.New("conversation not found"))
		return
	}
	items, err := h.service.commandTasks(r.Context(), p.WorkspaceID, id)
	if err != nil {
		httpapi.Error(w, 500, errors.New("task list unavailable"))
		return
	}
	httpapi.JSON(w, 200, map[string]any{"tasks": items})
}

func (h *Handler) CommandTask(w http.ResponseWriter, r *http.Request)     { h.commandTask(w, r, false) }
func (h *Handler) StopCommandTask(w http.ResponseWriter, r *http.Request) { h.commandTask(w, r, true) }
func (h *Handler) commandTask(w http.ResponseWriter, r *http.Request, stop bool) {
	p, _ := auth.FromContext(r.Context())
	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid conversation"))
		return
	}
	taskID, err := uuid.Parse(chi.URLParam(r, "taskID"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid task"))
		return
	}
	read := sandbox.CommandTaskReadOptions{Limit: 64 << 10}
	if value := r.URL.Query().Get("offset"); value != "" {
		read.Offset, err = strconv.ParseInt(value, 10, 64)
		if err != nil || read.Offset < 0 {
			httpapi.Error(w, 400, errors.New("invalid log offset"))
			return
		}
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		read.Limit, err = strconv.Atoi(value)
		if err != nil || read.Limit < 1 || read.Limit > 256<<10 {
			httpapi.Error(w, 400, errors.New("invalid log limit"))
			return
		}
	}
	view, err := h.service.commandTask(r.Context(), p.WorkspaceID, conversationID, taskID, read, stop)
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, 404, errors.New("task not found"))
		return
	}
	if err != nil {
		httpapi.Error(w, 500, errors.New("task state unavailable"))
		return
	}
	httpapi.JSON(w, 200, view)
}
