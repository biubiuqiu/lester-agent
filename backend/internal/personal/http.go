package personal

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/conversation"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"strconv"
	"time"
)

type Handler struct{ Service *Service }

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	w.Header().Set("Cache-Control", "no-store")
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64000))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		fail(w, ErrInvalid)
		return false
	}
	return true
}
func fail(w http.ResponseWriter, err error) {
	status := 503
	switch {
	case errors.Is(err, ErrUnauthorized):
		status = 401
	case errors.Is(err, ErrInvalid):
		status = 400
	case errors.Is(err, ErrConflict), errors.Is(err, ErrBusy), errors.Is(err, ErrMemoryFull):
		status = 409
	case errors.Is(err, ErrNotFound):
		status = 404
	case errors.Is(err, ErrModel):
		status = 502
	default:
		err = errors.New("个人助理加载失败，请稍后检查状态")
	}
	httpapi.Error(w, status, err)
}
func (h *Handler) State(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, _ := auth.FromContext(r.Context())
	before := int64(0)
	if value := r.URL.Query().Get("before"); value != "" {
		var err error
		before, err = strconv.ParseInt(value, 10, 64)
		if err != nil || before <= 0 {
			fail(w, ErrInvalid)
			return
		}
	}
	turns, more, err := h.Service.History(r.Context(), p, before)
	if err != nil {
		fail(w, err)
		return
	}
	for i := range turns {
		if turns[i].Status == "pending" {
			if err = h.Service.recoverTurn(r.Context(), p, turns[i].ID); err != nil {
				fail(w, err)
				return
			}
			turns[i], err = h.Service.Turn(r.Context(), p, turns[i].ID)
			if err != nil {
				fail(w, err)
				return
			}
		}
	}
	memories, err := h.Service.Memories(r.Context(), p)
	if err != nil {
		fail(w, err)
		return
	}
	tasks, err := h.Service.Tasks(r.Context(), p)
	if err != nil {
		fail(w, err)
		return
	}
	inbox, err := h.Service.UnreadTasks(r.Context(), p, nil, "")
	if err != nil {
		fail(w, err)
		return
	}
	auto := true
	err = h.Service.DB.QueryRow(r.Context(), `SELECT auto_memory FROM personal_threads WHERE workspace_id=$1 AND user_id=$2`, p.WorkspaceID, p.UserID).Scan(&auto)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"turns": turns, "has_more": more, "memories": memories, "tasks": tasks, "inbox": inbox, "auto_memory": auto})
}
func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	var input Request
	if !decode(w, r, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	ctx = conversation.WithClientTimezone(ctx, r.Header.Get("X-Lester-Timezone"))
	item, err := h.Service.Chat(ctx, p, input)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, item)
}
func pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || id == uuid.Nil {
		fail(w, ErrInvalid)
		return id, false
	}
	return id, true
}
func (h *Handler) Turn(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.Service.recoverTurn(r.Context(), p, id); err != nil {
		fail(w, err)
		return
	}
	if err := h.Service.reconcileDelegation(r.Context(), p, id); err != nil {
		fail(w, err)
		return
	}
	item, err := h.Service.Turn(r.Context(), p, id)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, item)
}
func (h *Handler) EditMemory(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var input struct {
		Revision int64          `json:"revision"`
		Content  string         `json:"content"`
		Forget   bool           `json:"forget"`
		Details  *MemoryDetails `json:"details"`
	}
	if !decode(w, r, &input) {
		return
	}
	item, err := h.Service.UpdateMemory(r.Context(), p, id, input.Revision, input.Content, input.Forget, input.Details)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, item)
}

func (h *Handler) CreateMemory(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var input ManualMemoryInput
	if !decode(w, r, &input) {
		return
	}
	item, err := h.Service.CreateMemory(r.Context(), p, id, input)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, item)
}

func (h *Handler) Memory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	item, err := h.Service.Memory(r.Context(), p, id)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, item)
}
func (h *Handler) Undo(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.Service.Undo(r.Context(), p, id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) Settings(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	var input struct {
		Auto *bool `json:"auto_memory"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Auto == nil {
		fail(w, ErrInvalid)
		return
	}
	if err := h.Service.SetAutoMemory(r.Context(), p, *input.Auto); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) Tasks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, _ := auth.FromContext(r.Context())
	items, err := h.Service.Tasks(r.Context(), p)
	if err != nil {
		fail(w, err)
		return
	}
	inbox, err := h.Service.UnreadTasks(r.Context(), p, nil, "")
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"tasks": items, "inbox": inbox})
}

func (h *Handler) UnreadTasks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, _ := auth.FromContext(r.Context())
	var project *uuid.UUID
	if value := r.URL.Query().Get("project_id"); value != "" {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil {
			fail(w, ErrInvalid)
			return
		}
		project = &id
	}
	inbox, err := h.Service.UnreadTasks(r.Context(), p, project, r.URL.Query().Get("cursor"))
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, inbox)
}

func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var input struct {
		MessageID uuid.UUID `json:"message_id"`
	}
	if !decode(w, r, &input) {
		return
	}
	seq, err := h.Service.MarkRead(r.Context(), p, id, input.MessageID)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"last_read_seq": seq})
}
