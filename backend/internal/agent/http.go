package agent

import (
	"errors"
	"net/http"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Handler struct{ Service *Service }

func fail(w http.ResponseWriter, err error) {
	var dbErr *pgconn.PgError
	switch {
	case errors.Is(err, ErrInvalid):
		httpapi.Error(w, 400, err)
	case errors.Is(err, pgx.ErrNoRows):
		httpapi.Error(w, 409, errors.New("Agent 不存在或已更新，请刷新重试"))
	case errors.As(err, &dbErr) && dbErr.Code == "23505":
		httpapi.Error(w, 409, errors.New("该名称的 Agent 已存在"))
	default:
		httpapi.Error(w, 500, errors.New("Agent 操作失败，请重试"))
	}
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	items, err := h.Service.List(r.Context(), p.WorkspaceID)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"agents": items})
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	slug := chi.URLParam(r, "slug")
	a, err := h.Service.Resolve(r.Context(), p.WorkspaceID, slug)
	if err != nil {
		httpapi.Error(w, 404, errors.New("Agent 不存在"))
		return
	}
	httpapi.JSON(w, 200, a)
}
func (h *Handler) Save(w http.ResponseWriter, r *http.Request) {
	var id uuid.UUID
	if r.Method == "PATCH" {
		var err error
		id, err = uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			httpapi.Error(w, 400, errors.New("无效的 Agent ID"))
			return
		}
	}
	var a Agent
	if !httpapi.Decode(w, r, &a) {
		return
	}
	p, _ := auth.FromContext(r.Context())
	saved, err := h.Service.Save(r.Context(), p.WorkspaceID, id, a)
	if err != nil {
		fail(w, err)
		return
	}
	status := 200
	if id == uuid.Nil {
		status = 201
	}
	httpapi.JSON(w, status, saved)
}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("无效的 Agent ID"))
		return
	}
	var body struct {
		Version int `json:"version"`
	}
	if !httpapi.Decode(w, r, &body) {
		return
	}
	p, _ := auth.FromContext(r.Context())
	if err = h.Service.Delete(r.Context(), p.WorkspaceID, id, body.Version); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(204)
}
