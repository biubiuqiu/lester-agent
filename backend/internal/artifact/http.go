package artifact

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct{ Service *Service }

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	items, err := h.Service.List(r.Context(), p.WorkspaceID)
	if err != nil {
		httpapi.Error(w, 500, errors.New("产物加载失败"))
		return
	}
	httpapi.JSON(w, 200, map[string]any{"artifacts": items})
}
func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid conversation id"))
		return
	}
	var in PublishInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	a, err := h.Service.Publish(ctx, p.WorkspaceID, id, in)
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	httpapi.JSON(w, 201, a)
}
func (h *Handler) Unpublish(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid artifact id"))
		return
	}
	if err = h.Service.Unpublish(r.Context(), p.WorkspaceID, id); err != nil {
		httpapi.Error(w, 404, err)
		return
	}
	w.WriteHeader(204)
}
