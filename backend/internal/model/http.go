package model

import (
	"errors"
	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"strings"
)

type Handler struct{ store *Store }

func NewHandler(store *Store) *Handler { return &Handler{store: store} }
func (h *Handler) ListConnections(w http.ResponseWriter, r *http.Request) {
	workspaceID := requestWorkspace(r)
	items, err := h.store.ListConnections(r.Context(), workspaceID)
	if err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"connections": items})
}
func (h *Handler) CreateConnection(w http.ResponseWriter, r *http.Request) {
	workspaceID := requestWorkspace(r)
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
	item, err := h.store.CreateConnection(r.Context(), workspaceID, req.Name, req.Provider, req.Endpoint, req.Config, req.Credential)
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	httpapi.JSON(w, 201, item)
}
func (h *Handler) TestConnection(w http.ResponseWriter, r *http.Request) {
	workspaceID := requestWorkspace(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	connections, err := h.store.ListConnections(r.Context(), workspaceID)
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
	workspaceID := requestWorkspace(r)
	items, err := h.store.ListDeployments(r.Context(), workspaceID)
	if err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"deployments": items})
}
func (h *Handler) CreateDeployment(w http.ResponseWriter, r *http.Request) {
	workspaceID := requestWorkspace(r)
	var req struct {
		ConnectionID uuid.UUID `json:"connection_id"`
		Name         string    `json:"name"`
		ModelID      string    `json:"model_id"`
		IsDefault    bool      `json:"is_default"`
	}
	if !httpapi.Decode(w, r, &req) {
		return
	}
	item, err := h.store.CreateDeployment(r.Context(), workspaceID, req.ConnectionID, req.Name, req.ModelID, req.IsDefault)
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	httpapi.JSON(w, 201, item)
}

func requestWorkspace(r *http.Request) uuid.UUID {
	if strings.HasPrefix(r.URL.Path, "/api/v1/admin/") {
		return SystemWorkspaceID
	}
	p, _ := auth.FromContext(r.Context())
	return p.WorkspaceID
}
func (h *Handler) UpdateDeployment(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid model ID"))
		return
	}
	var in DeploymentChange
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if err = h.store.UpdateDeployment(r.Context(), requestWorkspace(r), id, in); err != nil {
		httpapi.Error(w, 400, errors.New("could not update model; check name, model ID and connection"))
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) UpdateConnection(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid connection ID"))
		return
	}
	var in ConnectionChange
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if err = h.store.UpdateConnection(r.Context(), requestWorkspace(r), id, in); err != nil {
		httpapi.Error(w, 400, errors.New("could not update connection; check configuration"))
		return
	}
	w.WriteHeader(204)
}
