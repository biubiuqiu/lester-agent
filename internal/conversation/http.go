package conversation

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/biubiuqiu/lester-agent/internal/auth"
	"github.com/biubiuqiu/lester-agent/internal/httpapi"
	"github.com/biubiuqiu/lester-agent/internal/sandbox"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Handler struct {
	service    *Service
	db         *pgxpool.Pool
	redis      *redis.Client
	sandboxes  *sandbox.Client
	sandboxURL string
}

func NewHandler(service *Service, db *pgxpool.Pool, redisClient *redis.Client, sandboxes *sandbox.Client, sandboxURL string) *Handler {
	return &Handler{service: service, db: db, redis: redisClient, sandboxes: sandboxes, sandboxURL: strings.TrimRight(sandboxURL, "/")}
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	items, err := h.service.List(r.Context(), p.WorkspaceID)
	if err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"conversations": items})
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	var req struct {
		AgentSlug         string    `json:"agent_slug"`
		Title             string    `json:"title"`
		ModelDeploymentID uuid.UUID `json:"model_deployment_id"`
	}
	if !httpapi.Decode(w, r, &req) {
		return
	}
	item, err := h.service.Create(r.Context(), p.WorkspaceID, p.UserID, req.AgentSlug, req.Title, req.ModelDeploymentID)
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	httpapi.JSON(w, 201, item)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	item, messages, err := h.service.Get(r.Context(), p.WorkspaceID, id)
	if err != nil {
		httpapi.Error(w, 404, errors.New("conversation not found"))
		return
	}
	httpapi.JSON(w, 200, map[string]any{"conversation": item, "messages": messages})
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		ModelDeploymentID uuid.UUID `json:"model_deployment_id"`
	}
	if !httpapi.Decode(w, r, &req) {
		return
	}
	if err := h.service.UpdateModel(r.Context(), p.WorkspaceID, id, req.ModelDeploymentID); err != nil {
		httpapi.Error(w, 404, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) Send(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct{ Content string }
	if !httpapi.Decode(w, r, &req) {
		return
	}
	runID, err := h.service.Send(r.Context(), p.WorkspaceID, id, req.Content)
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	httpapi.JSON(w, 202, map[string]any{"run_id": runID})
}
func (h *Handler) Events(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, _, err := h.service.Get(r.Context(), p.WorkspaceID, id); err != nil {
		httpapi.Error(w, 404, errors.New("conversation not found"))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpapi.Error(w, 500, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	rows, err := h.db.Query(r.Context(), `SELECT id,run_id,conversation_id,type,payload,created_at FROM run_events WHERE conversation_id=$1 ORDER BY id`, id)
	if err == nil {
		for rows.Next() {
			var event RunEvent
			var raw []byte
			if rows.Scan(&event.ID, &event.RunID, &event.ConversationID, &event.Type, &raw, &event.CreatedAt) == nil {
				_ = json.Unmarshal(raw, &event.Payload)
				writeSSE(w, event)
			}
		}
		rows.Close()
		flusher.Flush()
	}
	subscription := h.redis.Subscribe(r.Context(), "conversation:"+id.String())
	defer subscription.Close()
	for {
		select {
		case <-r.Context().Done():
			return
		case message := <-subscription.Channel():
			fmt.Fprintf(w, "data: %s\n\n", message.Payload)
			flusher.Flush()
		}
	}
}
func writeSSE(w io.Writer, event RunEvent) {
	data, _ := json.Marshal(event)
	fmt.Fprintf(w, "id: %d\ndata: %s\n\n", event.ID, data)
}
func pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, err)
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) Computer(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, _, err := h.service.Get(r.Context(), p.WorkspaceID, id); err != nil {
		httpapi.Error(w, 404, err)
		return
	}
	var status, ref string
	err := h.db.QueryRow(r.Context(), `SELECT status,provider_ref FROM sandboxes WHERE conversation_id=$1`, id).Scan(&status, &ref)
	if err != nil {
		httpapi.JSON(w, 200, map[string]any{"conversation_id": id, "status": "not_created"})
		return
	}
	httpapi.JSON(w, 200, map[string]any{"conversation_id": id, "provider": "docker", "provider_ref": ref, "status": status})
}
func (h *Handler) Files(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, _, err := h.service.Get(r.Context(), p.WorkspaceID, id); err != nil {
		httpapi.Error(w, 404, err)
		return
	}
	items, err := h.sandboxes.ListFiles(r.Context(), id.String(), r.URL.Query().Get("path"))
	if err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"files": items})
}
func (h *Handler) ReadFile(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, _, err := h.service.Get(r.Context(), p.WorkspaceID, id); err != nil {
		httpapi.Error(w, 404, err)
		return
	}
	data, err := h.sandboxes.ReadFile(r.Context(), id.String(), r.URL.Query().Get("path"))
	if err != nil {
		httpapi.Error(w, 404, err)
		return
	}
	_, _ = w.Write(data)
}
func (h *Handler) WriteFile(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, _, err := h.service.Get(r.Context(), p.WorkspaceID, id); err != nil {
		httpapi.Error(w, 404, err)
		return
	}
	var req struct{ Path, Content string }
	if !httpapi.Decode(w, r, &req) {
		return
	}
	if err := h.sandboxes.WriteFile(r.Context(), id.String(), req.Path, []byte(req.Content)); err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) Exec(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, _, err := h.service.Get(r.Context(), p.WorkspaceID, id); err != nil {
		httpapi.Error(w, 404, err)
		return
	}
	var command sandbox.Command
	if !httpapi.Decode(w, r, &command) {
		return
	}
	result, err := h.sandboxes.Exec(r.Context(), id.String(), command)
	if err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	httpapi.JSON(w, 200, result)
}
func (h *Handler) Terminal(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, _, err := h.service.Get(r.Context(), p.WorkspaceID, id); err != nil {
		httpapi.Error(w, 404, err)
		return
	}
	target := h.sandboxURL + "/v1/sandboxes/" + id.String() + "/terminal"
	parsed, _ := url.Parse(target)
	if parsed.Scheme == "http" {
		parsed.Scheme = "ws"
	} else {
		parsed.Scheme = "wss"
	}
	http.Redirect(w, r, parsed.String(), http.StatusTemporaryRedirect)
}

var _ = bufio.ErrInvalidUnreadByte
