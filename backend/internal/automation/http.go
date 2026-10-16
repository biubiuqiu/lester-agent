package automation

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct{ Service *Service }

func fail(w http.ResponseWriter, err error) {
	status := http.StatusServiceUnavailable
	message := errors.New("自动任务暂不可用，请稍后重试")
	switch {
	case errors.Is(err, ErrInvalid):
		status, message = 400, ErrInvalid
	case errors.Is(err, ErrNotFound):
		status, message = 404, ErrNotFound
	case errors.Is(err, ErrConflict):
		status, message = 409, ErrConflict
	case errors.Is(err, ErrPaused):
		status, message = 409, ErrPaused
	case errors.Is(err, ErrUnauthorized):
		status, message = 401, ErrUnauthorized
	case errors.Is(err, ErrLimit):
		status, message = 429, ErrLimit
		w.Header().Set("Retry-After", "60")
	}
	httpapi.Error(w, status, message)
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		fail(w, ErrInvalid)
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		fail(w, ErrInvalid)
		return false
	}
	return true
}
func idParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || id == uuid.Nil {
		fail(w, ErrInvalid)
		return uuid.Nil, false
	}
	return id, true
}
func noStore(w http.ResponseWriter) { w.Header().Set("Cache-Control", "no-store") }
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	p, _ := auth.FromContext(r.Context())
	items, err := h.Service.List(r.Context(), p.WorkspaceID, p.UserID)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"automations": items})
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	p, _ := auth.FromContext(r.Context())
	item, err := h.Service.Get(r.Context(), p.WorkspaceID, p.UserID, id)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, item)
}
func (h *Handler) Save(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var req struct {
		Config
		Revision int64 `json:"revision"`
	}
	if !decode(w, r, &req) {
		return
	}
	p, _ := auth.FromContext(r.Context())
	item, secret, err := h.Service.Save(r.Context(), p.WorkspaceID, p.UserID, id, req.Config, req.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"automation": item, "secret": secret})
}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	revision, err := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
	if err != nil || revision < 1 {
		fail(w, ErrInvalid)
		return
	}
	p, _ := auth.FromContext(r.Context())
	if err = h.Service.Delete(r.Context(), p.WorkspaceID, p.UserID, id, revision); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) Rotate(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var req struct {
		Revision int64 `json:"revision"`
	}
	if !decode(w, r, &req) {
		return
	}
	p, _ := auth.FromContext(r.Context())
	item, secret, err := h.Service.Rotate(r.Context(), p.WorkspaceID, p.UserID, id, req.Revision)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"automation": item, "secret": secret})
}
func (h *Handler) Trigger(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var req struct {
		ID uuid.UUID `json:"id"`
	}
	if !decode(w, r, &req) {
		return
	}
	p, _ := auth.FromContext(r.Context())
	item, err := h.Service.Trigger(r.Context(), p.WorkspaceID, p.UserID, id, req.ID)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 202, item)
}
func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	p, _ := auth.FromContext(r.Context())
	items, err := h.Service.History(r.Context(), p.WorkspaceID, p.UserID, id)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"deliveries": items})
}
func (h *Handler) Delivery(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	delivery, err := uuid.Parse(chi.URLParam(r, "deliveryID"))
	if err != nil {
		fail(w, ErrInvalid)
		return
	}
	p, _ := auth.FromContext(r.Context())
	item, err := h.Service.Delivery(r.Context(), p.WorkspaceID, p.UserID, id, delivery)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, item)
}
func (h *Handler) Webhook(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	authorization := r.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") || len(authorization) > 107 {
		fail(w, ErrUnauthorized)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32768))
	if err != nil {
		fail(w, ErrInvalid)
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var data map[string]any
	if err = decoder.Decode(&data); err != nil || data == nil {
		fail(w, ErrInvalid)
		return
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		fail(w, ErrInvalid)
		return
	}
	payload, err := json.Marshal(data)
	if err != nil {
		fail(w, ErrInvalid)
		return
	}
	item, err := h.Service.Webhook(r.Context(), id, strings.TrimPrefix(authorization, "Bearer "), r.Header.Get("X-Lester-Event-ID"), payload)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 202, item)
}
