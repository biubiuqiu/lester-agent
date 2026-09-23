package contextlibrary

import (
	"errors"
	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"net/http"
)

type Handler struct{ Service *Service }

func fail(w http.ResponseWriter, err error) {
	var dbError *pgconn.PgError
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, 409, errors.New("词条不存在或已被修改，请刷新后重试"))
	} else if errors.As(err, &dbError) && dbError.Code == "23505" {
		httpapi.Error(w, 409, errors.New("该名称的词条已存在"))
	} else {
		httpapi.Error(w, 500, errors.New("上下文库操作失败，请重试"))
	}
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	items, err := h.Service.List(r.Context(), p.WorkspaceID)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"entries": items})
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("无效的词条 ID"))
		return
	}
	p, _ := auth.FromContext(r.Context())
	item, err := h.Service.Get(r.Context(), p.WorkspaceID, id)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, item)
}
func (h *Handler) Save(w http.ResponseWriter, r *http.Request) {
	id := uuid.Nil
	var err error
	if r.Method == "PATCH" {
		id, err = uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			httpapi.Error(w, 400, errors.New("无效的词条 ID"))
			return
		}
	}
	var in Entry
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if err = Validate(in); err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	p, _ := auth.FromContext(r.Context())
	item, err := h.Service.Save(r.Context(), p.WorkspaceID, id, in)
	if err != nil {
		fail(w, err)
		return
	}
	status := 200
	if id == uuid.Nil {
		status = 201
	}
	httpapi.JSON(w, status, item)
}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("无效的词条 ID"))
		return
	}
	var in struct {
		Version int `json:"version"`
	}
	if !httpapi.Decode(w, r, &in) {
		return
	}
	p, _ := auth.FromContext(r.Context())
	if err = h.Service.Delete(r.Context(), p.WorkspaceID, id, in.Version); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(204)
}
