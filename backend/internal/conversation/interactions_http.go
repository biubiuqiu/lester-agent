package conversation

import (
	"errors"
	"net/http"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) RunInteractions(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.service.reconcileInteractionOwner(r.Context(), p.WorkspaceID, id); err != nil {
		httpapi.Error(w, 503, errors.New("interaction status unavailable"))
		return
	}
	items, mode, err := h.service.RunInteractions(r.Context(), p.WorkspaceID, id)
	if err != nil {
		if errors.Is(err, ErrInteractionNotFound) {
			httpapi.Error(w, 404, err)
		} else {
			httpapi.Error(w, 503, errors.New("interaction status unavailable"))
		}
		return
	}
	httpapi.JSON(w, 200, map[string]any{"interactions": items, "execution_mode": mode})
}

func (h *Handler) RunInteraction(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	conversationID, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.service.reconcileInteractionOwner(r.Context(), p.WorkspaceID, conversationID); err != nil {
		httpapi.Error(w, 503, errors.New("interaction status unavailable"))
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "interactionID"))
	if err != nil || id == uuid.Nil {
		httpapi.Error(w, 400, errors.New("invalid interaction ID"))
		return
	}
	item, err := h.service.RunInteraction(r.Context(), p.WorkspaceID, conversationID, id)
	if err != nil {
		if errors.Is(err, ErrInteractionNotFound) {
			httpapi.Error(w, 404, err)
		} else {
			httpapi.Error(w, 503, errors.New("interaction status unavailable"))
		}
		return
	}
	httpapi.JSON(w, 200, item)
}
func (h *Handler) RespondRunInteraction(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	conversationID, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.service.reconcileInteractionOwner(r.Context(), p.WorkspaceID, conversationID); err != nil {
		httpapi.Error(w, 503, errors.New("interaction status unavailable"))
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "interactionID"))
	if err != nil || id == uuid.Nil {
		httpapi.Error(w, 400, errors.New("invalid interaction ID"))
		return
	}
	var response InteractionResponse
	if !httpapi.Decode(w, r, &response) {
		return
	}
	item, err := h.service.RespondRunInteraction(r.Context(), p.WorkspaceID, p.UserID, conversationID, id, response)
	if err != nil {
		switch {
		case errors.Is(err, ErrInteractionNotFound):
			httpapi.Error(w, 404, err)
		case errors.Is(err, ErrInteractionConflict):
			httpapi.Error(w, 409, err)
		default:
			httpapi.Error(w, 400, err)
		}
		return
	}
	httpapi.JSON(w, 200, item)
}
