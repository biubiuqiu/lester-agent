package deliverable

import (
	"errors"
	"net/http"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct{ Service *Service }

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.FromContext(r.Context())
	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid conversation id"))
		return
	}
	items, err := h.Service.List(r.Context(), principal.WorkspaceID, conversationID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpapi.Error(w, 404, err)
		} else {
			httpapi.Error(w, 500, errors.New("成果记录加载失败"))
		}
		return
	}
	httpapi.JSON(w, 200, map[string]any{"deliverables": items})
}
