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

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Handler struct {
	service    *Service
	db         *pgxpool.Pool
	redis      *redis.Client
	sandboxes  *sandbox.Client
	sandboxURL string
	upgrader   websocket.Upgrader
}

func NewHandler(service *Service, db *pgxpool.Pool, redisClient *redis.Client, sandboxes *sandbox.Client, sandboxURL string) *Handler {
	return &Handler{service: service, db: db, redis: redisClient, sandboxes: sandboxes, sandboxURL: strings.TrimRight(sandboxURL, "/"), upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}}
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
	var req struct {
		Content       string      `json:"content"`
		AttachmentIDs []uuid.UUID `json:"attachment_ids"`
	}
	if !httpapi.Decode(w, r, &req) {
		return
	}
	runID, err := h.service.Send(r.Context(), p.WorkspaceID, p.UserID, id, req.Content, req.AttachmentIDs)
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	httpapi.JSON(w, 202, map[string]any{"run_id": runID})
}

func (h *Handler) UploadAttachment(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	const maxUpload = 25 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+(1<<20))
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		httpapi.Error(w, http.StatusBadRequest, errors.New("attachment must be a multipart file no larger than 25 MiB"))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, errors.New("file is required"))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxUpload+1))
	if err != nil || len(data) > maxUpload {
		httpapi.Error(w, http.StatusBadRequest, errors.New("attachment exceeds the 25 MiB limit"))
		return
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	item, err := h.service.UploadAttachment(r.Context(), p.WorkspaceID, p.UserID, id, header.Filename, contentType, data)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, item)
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
	state, err := h.service.ComputerStatus(r.Context(), p.WorkspaceID, id)
	if err != nil {
		httpapi.Error(w, 404, err)
		return
	}
	httpapi.JSON(w, 200, state)
}
func (h *Handler) Files(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	computer, err := h.service.ComputerForConversation(r.Context(), p.WorkspaceID, id)
	if err != nil {
		httpapi.Error(w, 404, err)
		return
	}
	items, err := h.sandboxes.ListFiles(r.Context(), computer.SandboxID, computer.WorkDir, r.URL.Query().Get("path"))
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
	computer, err := h.service.ComputerForConversation(r.Context(), p.WorkspaceID, id)
	if err != nil {
		httpapi.Error(w, 404, err)
		return
	}
	data, err := h.sandboxes.ReadFile(r.Context(), computer.SandboxID, computer.WorkDir, r.URL.Query().Get("path"))
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
	computer, err := h.service.ComputerForConversation(r.Context(), p.WorkspaceID, id)
	if err != nil {
		httpapi.Error(w, 404, err)
		return
	}
	var req struct{ Path, Content string }
	if !httpapi.Decode(w, r, &req) {
		return
	}
	if err := h.sandboxes.WriteFile(r.Context(), computer.SandboxID, computer.WorkDir, req.Path, []byte(req.Content)); err != nil {
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
	computer, err := h.service.ComputerForConversation(r.Context(), p.WorkspaceID, id)
	if err != nil {
		httpapi.Error(w, 404, err)
		return
	}
	var command sandbox.Command
	if !httpapi.Decode(w, r, &command) {
		return
	}
	command.WorkDir = computer.WorkDir
	result, err := h.sandboxes.Exec(r.Context(), computer.SandboxID, command)
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
	computer, err := h.service.ComputerForConversation(r.Context(), p.WorkspaceID, id)
	if err != nil {
		httpapi.Error(w, 404, err)
		return
	}
	client, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer client.Close()
	target := strings.Replace(h.sandboxURL, "http://", "ws://", 1)
	target = strings.Replace(target, "https://", "wss://", 1) + "/v1/sandboxes/" + computer.SandboxID + "/terminal?work_dir=" + url.QueryEscape(computer.WorkDir)
	upstream, _, err := websocket.DefaultDialer.DialContext(r.Context(), target, nil)
	if err != nil {
		_ = client.WriteJSON(map[string]string{"type": "error", "data": err.Error()})
		return
	}
	defer upstream.Close()
	done := make(chan struct{}, 2)
	copyMessages := func(destination, source *websocket.Conn) {
		defer func() { done <- struct{}{} }()
		for {
			messageType, data, readErr := source.ReadMessage()
			if readErr != nil || destination.WriteMessage(messageType, data) != nil {
				return
			}
		}
	}
	go copyMessages(upstream, client)
	go copyMessages(client, upstream)
	select {
	case <-done:
	case <-r.Context().Done():
	}
}

var _ = bufio.ErrInvalidUnreadByte
