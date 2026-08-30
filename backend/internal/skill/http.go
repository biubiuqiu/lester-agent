package skill

import (
	"errors"
	"net/http"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/conversation"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct {
	service       *Service
	conversations *conversation.Service
}

func NewHandler(service *Service, conversations *conversation.Service) *Handler {
	return &Handler{service: service, conversations: conversations}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.List(r.Context())
	if err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"skills": items})
}

func (h *Handler) Installed(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := conversationID(w, r)
	if !ok {
		return
	}
	items, err := h.service.Installed(r.Context(), p.WorkspaceID, id)
	if err != nil {
		httpapi.Error(w, 404, errors.New("conversation not found"))
		return
	}
	httpapi.JSON(w, 200, map[string]any{"skills": items})
}

func (h *Handler) Install(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := conversationID(w, r)
	if !ok {
		return
	}
	computer, err := h.conversations.ComputerForConversation(r.Context(), p.WorkspaceID, id)
	if err != nil {
		httpapi.Error(w, 404, errors.New("conversation not found"))
		return
	}
	item, err := h.service.Install(r.Context(), p.WorkspaceID, p.UserID, id, computer.SandboxID, computer.WorkDir, chi.URLParam(r, "slug"))
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	httpapi.JSON(w, 201, item)
}

func (h *Handler) Uninstall(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := conversationID(w, r)
	if !ok {
		return
	}
	computer, err := h.conversations.ComputerForConversation(r.Context(), p.WorkspaceID, id)
	if err != nil {
		httpapi.Error(w, 404, errors.New("conversation not found"))
		return
	}
	if err = h.service.Uninstall(r.Context(), p.WorkspaceID, id, computer.SandboxID, computer.WorkDir, chi.URLParam(r, "slug")); err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func conversationID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, err)
		return uuid.Nil, false
	}
	return id, true
}
