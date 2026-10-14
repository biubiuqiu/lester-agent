package conversation

import (
	"errors"
	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
)

func (h *Handler) RunPermissions(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.service.reconcileInteractionOwner(r.Context(), p.WorkspaceID, id); err != nil {
		httpapi.Error(w, 503, errors.New("permission status unavailable"))
		return
	}
	if value := chi.URLParam(r, "permissionID"); value != "" {
		target, err := uuid.Parse(value)
		if err != nil {
			httpapi.Error(w, 400, err)
			return
		}
		item, err := h.service.runPermission(r.Context(), p.WorkspaceID, id, target)
		if err != nil {
			permissionHTTPError(w, err)
			return
		}
		httpapi.JSON(w, 200, item)
		return
	}
	items, err := h.service.runPermissions(r.Context(), p.WorkspaceID, id)
	if err != nil {
		permissionHTTPError(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"permissions": items})
}
func (h *Handler) RespondRunPermission(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	conversationID, ok := pathID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "permissionID"))
	if err != nil || id == uuid.Nil {
		httpapi.Error(w, 400, errors.New("invalid permission ID"))
		return
	}
	if err = h.service.reconcileInteractionOwner(r.Context(), p.WorkspaceID, conversationID); err != nil {
		httpapi.Error(w, 503, errors.New("permission status unavailable"))
		return
	}
	var response struct {
		RequestDigest string `json:"request_digest"`
		Decision      string `json:"decision"`
	}
	if !httpapi.Decode(w, r, &response) {
		return
	}
	item, err := h.service.respondPermission(r.Context(), p.WorkspaceID, p.UserID, conversationID, id, response.RequestDigest, response.Decision)
	if err != nil {
		permissionHTTPError(w, err)
		return
	}
	httpapi.JSON(w, 200, item)
}
func permissionHTTPError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInteractionNotFound):
		httpapi.Error(w, 404, err)
	case errors.Is(err, ErrInteractionConflict):
		httpapi.Error(w, 409, err)
	default:
		httpapi.Error(w, 400, err)
	}
}
