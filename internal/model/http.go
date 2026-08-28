package model

import (
	"errors"
	"github.com/biubiuqiu/lester-agent/internal/auth"
	"github.com/biubiuqiu/lester-agent/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
)

type Handler struct{ store *Store }

func NewHandler(store *Store) *Handler { return &Handler{store: store} }
func (h *Handler) ListConnections(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	items, err := h.store.ListConnections(r.Context(), p.WorkspaceID)
	if err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"connections": items})
}
func (h *Handler) CreateConnection(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	var req struct {
		Name       string         `json:"name"`
		Provider   string         `json:"provider"`
		Endpoint   string         `json:"endpoint"`
		Credential string         `json:"credential"`
		Config     map[string]any `json:"config"`
	}
	if !httpapi.Decode(w, r, &req) {
		return
	}
	item, err := h.store.CreateConnection(r.Context(), p.WorkspaceID, req.Name, req.Provider, req.Endpoint, req.Config, req.Credential)
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	httpapi.JSON(w, 201, item)
}
func (h *Handler) TestConnection(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	connections, err := h.store.ListConnections(r.Context(), p.WorkspaceID)
	if err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	for _, item := range connections {
		if item.ID == id {
			httpapi.JSON(w, 200, map[string]bool{"ok": true})
			return
		}
	}
	httpapi.Error(w, 404, errors.New("connection not found"))
}
func (h *Handler) ListDeployments(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	items, err := h.store.ListDeployments(r.Context(), p.WorkspaceID)
	if err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"deployments": items})
}
func (h *Handler) CreateDeployment(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	var req struct {
		ConnectionID uuid.UUID `json:"connection_id"`
		Name         string    `json:"name"`
		ModelID      string    `json:"model_id"`
		IsDefault    bool      `json:"is_default"`
	}
	if !httpapi.Decode(w, r, &req) {
		return
	}
	item, err := h.store.CreateDeployment(r.Context(), p.WorkspaceID, req.ConnectionID, req.Name, req.ModelID, req.IsDefault)
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	httpapi.JSON(w, 201, item)
}
