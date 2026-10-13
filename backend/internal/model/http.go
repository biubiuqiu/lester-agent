package model

import (
	"errors"
	"net/http"
	"strings"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
			httpapi.JSON(w, 200, map[string]any{"ok": true, "verified": false, "status": "configuration_saved"})
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
		DeploymentBudgetChange
	}
	if !httpapi.Decode(w, r, &req) {
		return
	}
	item, err := h.store.CreateDeploymentWithBudget(r.Context(), workspaceID, req.ConnectionID, req.Name, req.ModelID, req.IsDefault, req.DeploymentBudgetChange)
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
	var req struct {
		Name         *string    `json:"name"`
		ModelID      *string    `json:"model_id"`
		ConnectionID *uuid.UUID `json:"connection_id"`
		IsDefault    *bool      `json:"is_default"`
		Enabled      *bool      `json:"enabled"`
		DeploymentBudgetChange
	}
	if !httpapi.Decode(w, r, &req) {
		return
	}
	if req.Name == nil && req.ModelID == nil && req.ConnectionID == nil && req.IsDefault == nil && req.Enabled == nil {
		err = h.store.SetDeploymentBudget(r.Context(), requestWorkspace(r), id, req.DeploymentBudgetChange)
	} else {
		var in DeploymentChange
		in.DeploymentBudgetChange = req.DeploymentBudgetChange
		if req.Name != nil {
			in.Name = *req.Name
		}
		if req.ModelID != nil {
			in.ModelID = *req.ModelID
		}
		if req.ConnectionID != nil {
			in.ConnectionID = *req.ConnectionID
		}
		if req.IsDefault != nil {
			in.IsDefault = *req.IsDefault
		}
		if req.Enabled != nil {
			in.Enabled = *req.Enabled
		}
		err = h.store.UpdateDeployment(r.Context(), requestWorkspace(r), id, in)
	}
	if errors.Is(err, ErrDeploymentNotFound) {
		httpapi.Error(w, 404, ErrDeploymentNotFound)
		return
	}
	if err != nil {
		httpapi.Error(w, 400, errors.New("could not update model; check name, model ID and connection"))
		return
	}
	w.WriteHeader(204)
}

func (h *Handler) TestDeployment(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid model ID"))
		return
	}
	result, err := h.store.TestDeployment(r.Context(), requestWorkspace(r), id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, 404, ErrDeploymentNotFound)
		return
	}
	// Probe failures are completed checks with safe structured recovery details.
	// Never surface initialization errors or raw provider text/credentials.
	httpapi.JSON(w, 200, result)
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
