package reference

import (
	"net/http"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct{ Service Service }

func (h Handler) Search(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	kind := r.URL.Query().Get("type")
	if kind == "" {
		kind = "all"
	}
	exclude := uuid.Nil
	if raw := r.URL.Query().Get("exclude_conversation"); raw != "" {
		var err error
		exclude, err = uuid.Parse(raw)
		if err != nil {
			httpapi.Error(w, 400, err)
			return
		}
	}
	items, err := h.Service.Search(r.Context(), p.WorkspaceID, exclude, kind, r.URL.Query().Get("q"))
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items})
}

func (h Handler) Preview(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	page, err := h.Service.Preview(r.Context(), p.WorkspaceID, id)
	if err != nil {
		httpapi.Error(w, 404, err)
		return
	}
	httpapi.JSON(w, 200, page)
}

func (h Handler) Skill(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	item, err := h.Service.Skill(r.Context(), id)
	if err != nil {
		httpapi.Error(w, 404, err)
		return
	}
	httpapi.JSON(w, 200, item)
}
