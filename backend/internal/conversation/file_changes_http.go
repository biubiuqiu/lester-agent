package conversation

import (
	"errors"
	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
)

func fileChangeHTTPError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrFileChangeNotFound):
		httpapi.Error(w, 404, err)
	case errors.Is(err, ErrFileRollbackConflict) || errors.Is(err, ErrRunInProgress):
		httpapi.Error(w, 409, err)
	default:
		httpapi.Error(w, 503, errors.New("file review unavailable"))
	}
}
func (h *Handler) FileChanges(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	items, err := h.service.FileChanges(r.Context(), p.WorkspaceID, id)
	if err != nil {
		fileChangeHTTPError(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"changes": items})
}
func changeIDs(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	conversationID, ok := pathID(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "changeID"))
	if err != nil || id == uuid.Nil {
		httpapi.Error(w, 400, errors.New("invalid file change id"))
		return uuid.Nil, uuid.Nil, false
	}
	return conversationID, id, true
}
func (h *Handler) FileChange(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	conversationID, id, ok := changeIDs(w, r)
	if !ok {
		return
	}
	item, err := h.service.FileChange(r.Context(), p.WorkspaceID, conversationID, id)
	if err != nil {
		fileChangeHTTPError(w, err)
		return
	}
	httpapi.JSON(w, 200, item)
}
func (h *Handler) RollbackFileChange(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	conversationID, id, ok := changeIDs(w, r)
	if !ok {
		return
	}
	var request struct {
		ID uuid.UUID `json:"id"`
	}
	if !httpapi.Decode(w, r, &request) {
		return
	}
	item, err := h.service.RollbackFileChange(r.Context(), p.WorkspaceID, p.UserID, conversationID, id, request.ID)
	if err != nil {
		fileChangeHTTPError(w, err)
		return
	}
	httpapi.JSON(w, 200, item)
}
func (h *Handler) FileRollback(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	conversationID, id, ok := changeIDs(w, r)
	if !ok {
		return
	}
	receiptID, err := uuid.Parse(chi.URLParam(r, "rollbackID"))
	if err != nil || receiptID == uuid.Nil {
		httpapi.Error(w, 400, errors.New("invalid restore id"))
		return
	}
	item, err := h.service.FileRollback(r.Context(), p.WorkspaceID, p.UserID, conversationID, id, receiptID)
	if err != nil {
		fileChangeHTTPError(w, err)
		return
	}
	httpapi.JSON(w, 200, item)
}
