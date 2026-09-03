package conversation

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

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
	service      *Service
	db           *pgxpool.Pool
	redis        *redis.Client
	sandboxes    *sandbox.Client
	sandboxURL   string
	sandboxToken string
	upgrader     websocket.Upgrader
}

func NewHandler(service *Service, db *pgxpool.Pool, redisClient *redis.Client, sandboxes *sandbox.Client, sandboxURL, sandboxToken, webOrigin string) *Handler {
	return &Handler{service: service, db: db, redis: redisClient, sandboxes: sandboxes, sandboxURL: strings.TrimRight(sandboxURL, "/"), sandboxToken: sandboxToken, upgrader: websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == webOrigin }}}
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
	if r.URL.Query().Get("include_internal") != "true" {
		messages = visibleMessages(messages)
	}
	activeRun, err := h.service.ActiveRun(r.Context(), p.WorkspaceID, id)
	if err != nil {
		httpapi.Error(w, http.StatusInternalServerError, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"conversation": item, "messages": messages, "active_run": activeRun})
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
		if errors.Is(err, ErrRunInProgress) {
			httpapi.Error(w, http.StatusConflict, err)
			return
		}
		httpapi.Error(w, 400, err)
		return
	}
	httpapi.JSON(w, 202, map[string]any{"run_id": runID})
}

func (h *Handler) CancelRun(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	conversationID, ok := pathID(w, r)
	if !ok {
		return
	}
	runID, err := uuid.Parse(chi.URLParam(r, "runID"))
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, errors.New("invalid run id"))
		return
	}
	if err = h.service.CancelRun(r.Context(), p.WorkspaceID, conversationID, runID); err != nil {
		switch {
		case errors.Is(err, ErrRunNotFound):
			httpapi.Error(w, http.StatusNotFound, err)
		case errors.Is(err, ErrRunNotActive):
			httpapi.Error(w, http.StatusConflict, err)
		default:
			httpapi.Error(w, http.StatusInternalServerError, err)
		}
		return
	}
	httpapi.JSON(w, http.StatusAccepted, map[string]any{"run_id": runID, "status": "cancelling"})
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
	if h.redis == nil {
		httpapi.Error(w, http.StatusServiceUnavailable, errors.New("event stream unavailable"))
		return
	}
	subscription := h.redis.Subscribe(r.Context(), "conversation:"+id.String())
	defer subscription.Close()
	if _, err := subscription.Receive(r.Context()); err != nil {
		httpapi.Error(w, http.StatusServiceUnavailable, errors.New("event stream unavailable"))
		return
	}
	lastID := lastEventID(r)
	query := `SELECT id,run_id,conversation_id,type,payload,created_at FROM (
		SELECT id,run_id,conversation_id,type,payload,created_at FROM run_events
		WHERE conversation_id=$1 AND id>$2 ORDER BY id DESC LIMIT 1200
	) recent ORDER BY id`
	args := []any{id, lastID}
	if lastID == 0 {
		query = `SELECT id,run_id,conversation_id,type,payload,created_at FROM (
			SELECT id,run_id,conversation_id,type,payload,created_at FROM run_events
			WHERE conversation_id=$1 ORDER BY id DESC LIMIT 1200
		) recent ORDER BY id`
		args = []any{id}
	}
	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		httpapi.Error(w, http.StatusServiceUnavailable, errors.New("event history unavailable"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	sentID := lastID
	for rows.Next() {
		var event RunEvent
		var raw []byte
		if err = rows.Scan(&event.ID, &event.RunID, &event.ConversationID, &event.Type, &raw, &event.CreatedAt); err != nil {
			rows.Close()
			return
		}
		if json.Unmarshal(raw, &event.Payload) != nil {
			continue
		}
		writeSSE(w, event)
		sentID = event.ID
	}
	if rows.Err() != nil {
		rows.Close()
		return
	}
	rows.Close()
	flusher.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	channel := subscription.Channel()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = io.WriteString(w, ": keepalive\n\n")
			flusher.Flush()
		case message, ok := <-channel:
			if !ok {
				return
			}
			var event RunEvent
			if message == nil || json.Unmarshal([]byte(message.Payload), &event) != nil || event.ID <= sentID {
				continue
			}
			writeSSE(w, event)
			sentID = event.ID
			flusher.Flush()
		}
	}
}

func lastEventID(r *http.Request) int64 {
	value := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	if value == "" {
		value = strings.TrimSpace(r.URL.Query().Get("last_event_id"))
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 0 {
		return 0
	}
	return id
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
func (h *Handler) PreviewFile(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	filePath := strings.TrimPrefix(chi.URLParam(r, "*"), "/")
	if filePath == "" {
		httpapi.Error(w, http.StatusBadRequest, errors.New("preview path is required"))
		return
	}
	computer, err := h.service.ComputerForConversation(r.Context(), p.WorkspaceID, id)
	if err != nil {
		httpapi.Error(w, http.StatusNotFound, err)
		return
	}
	data, err := h.sandboxes.ReadFile(r.Context(), computer.SandboxID, computer.WorkDir, filePath)
	if err != nil {
		httpapi.Error(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", previewContentType(filePath))
	w.Header().Set("Content-Security-Policy", previewContentSecurityPolicy(r))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

func previewContentSecurityPolicy(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Host
	if host == "" || strings.ContainsAny(host, " \t\r\n;'\"") {
		return "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; connect-src 'none'; object-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'"
	}
	origin := scheme + "://" + host
	return fmt.Sprintf("default-src 'none'; script-src 'unsafe-inline' %s; style-src 'unsafe-inline' %s; img-src %s data: blob:; font-src %s data:; media-src %s data: blob:; connect-src 'none'; object-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'", origin, origin, origin, origin, origin)
}

func previewContentType(filePath string) string {
	extension := strings.ToLower(path.Ext(filePath))
	switch extension {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs", ".ts", ".tsx", ".jsx":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".md", ".txt", ".log", ".csv", ".yaml", ".yml", ".toml", ".xml", ".py", ".go", ".rs", ".java", ".sh", ".sql":
		return "text/plain; charset=utf-8"
	}
	if contentType := mime.TypeByExtension(extension); contentType != "" {
		return contentType
	}
	return "application/octet-stream"
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
	target = strings.Replace(target, "https://", "wss://", 1) + "/v1/sandboxes/" + url.PathEscape(computer.SandboxID) + "/terminal?work_dir=" + url.QueryEscape(computer.WorkDir)
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+h.sandboxToken)
	upstream, _, err := websocket.DefaultDialer.DialContext(r.Context(), target, headers)
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
