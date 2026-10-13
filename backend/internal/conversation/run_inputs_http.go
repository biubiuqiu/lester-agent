package conversation

import (
	"errors"
	"net/http"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func runInputHTTPError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrRunInputNotFound):
		httpapi.Error(w, 404, err)
	case errors.Is(err, ErrRunInputConflict) || errors.Is(err, ErrRunNotActive) || errors.Is(err, ErrRunInProgress):
		httpapi.Error(w, 409, err)
	case errors.Is(err, ErrRunInputRejected):
		httpapi.Error(w, 400, err)
	default:
		httpapi.Error(w, 503, errors.New("run input status unavailable"))
	}
}
func (h *Handler) AcceptRunInput(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.service.reconcileInteractionOwner(r.Context(), p.WorkspaceID, id); err != nil {
		httpapi.Error(w, 503, errors.New("run input status unavailable"))
		return
	}
	var request RunInputRequest
	if !httpapi.Decode(w, r, &request) {
		return
	}
	item, err := h.service.AcceptRunInput(r.Context(), p.WorkspaceID, p.UserID, id, request)
	if err != nil {
		runInputHTTPError(w, err)
		return
	}
	httpapi.JSON(w, 202, item)
}
func (h *Handler) RunInputs(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.service.reconcileInteractionOwner(r.Context(), p.WorkspaceID, id); err != nil {
		httpapi.Error(w, 503, errors.New("run input status unavailable"))
		return
	}
	items, err := h.service.RunInputs(r.Context(), p.WorkspaceID, p.UserID, id)
	if err != nil {
		runInputHTTPError(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"inputs": items})
}
func (h *Handler) RunInput(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	conversationID, ok := pathID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "inputID"))
	if err != nil || id == uuid.Nil {
		httpapi.Error(w, 400, errors.New("invalid input ID"))
		return
	}
	item, err := h.service.RunInput(r.Context(), p.WorkspaceID, p.UserID, conversationID, id)
	if err != nil {
		runInputHTTPError(w, err)
		return
	}
	httpapi.JSON(w, 200, item)
}
func (h *Handler) ChangeRunInput(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	conversationID, ok := pathID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "inputID"))
	if err != nil || id == uuid.Nil {
		httpapi.Error(w, 400, errors.New("invalid input ID"))
		return
	}
	var request struct {
		Action string `json:"action"`
	}
	if !httpapi.Decode(w, r, &request) {
		return
	}
	item, err := h.service.ChangeRunInput(r.Context(), p.WorkspaceID, p.UserID, conversationID, id, request.Action)
	if err != nil {
		runInputHTTPError(w, err)
		return
	}
	httpapi.JSON(w, 200, item)
}
