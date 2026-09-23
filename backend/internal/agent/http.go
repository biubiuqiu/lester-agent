package agent

import (
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Handler struct{ Service *Service }

func fail(w http.ResponseWriter, err error) {
	var dbErr *pgconn.PgError
	switch {
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrInvalidFile), errors.Is(err, ErrFileLimit):
		httpapi.Error(w, 400, err)
	case errors.Is(err, pgx.ErrNoRows):
		httpapi.Error(w, 409, errors.New("Agent 不存在或已更新，请刷新重试"))
	case errors.As(err, &dbErr) && dbErr.Code == "23505":
		httpapi.Error(w, 409, errors.New("该名称的 Agent 已存在"))
	default:
		httpapi.Error(w, 500, errors.New("Agent 操作失败，请重试"))
	}
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	items, err := h.Service.List(r.Context(), p.WorkspaceID)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"agents": items})
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	slug := chi.URLParam(r, "slug")
	a, err := h.Service.Resolve(r.Context(), p.WorkspaceID, slug)
	if err != nil {
		httpapi.Error(w, 404, errors.New("Agent 不存在"))
		return
	}
	httpapi.JSON(w, 200, a)
}
func (h *Handler) Save(w http.ResponseWriter, r *http.Request) {
	var id uuid.UUID
	if r.Method == "PATCH" {
		var err error
		id, err = uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			httpapi.Error(w, 400, errors.New("无效的 Agent ID"))
			return
		}
	}
	var a Agent
	if !httpapi.Decode(w, r, &a) {
		return
	}
	p, _ := auth.FromContext(r.Context())
	saved, err := h.Service.Save(r.Context(), p.WorkspaceID, id, a)
	if err != nil {
		fail(w, err)
		return
	}
	status := 200
	if id == uuid.Nil {
		status = 201
	}
	httpapi.JSON(w, status, saved)
}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("无效的 Agent ID"))
		return
	}
	var body struct {
		Version int `json:"version"`
	}
	if !httpapi.Decode(w, r, &body) {
		return
	}
	p, _ := auth.FromContext(r.Context())
	if err = h.Service.Delete(r.Context(), p.WorkspaceID, id, body.Version); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(204)
}

func fileIDs(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	agentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("无效的 Agent ID"))
		return uuid.Nil, uuid.Nil, false
	}
	fileID := uuid.Nil
	if raw := chi.URLParam(r, "fileID"); raw != "" {
		fileID, err = uuid.Parse(raw)
		if err != nil {
			httpapi.Error(w, 400, errors.New("无效的文件 ID"))
			return uuid.Nil, uuid.Nil, false
		}
	}
	return agentID, fileID, true
}

func (h *Handler) ListFiles(w http.ResponseWriter, r *http.Request) {
	agentID, _, ok := fileIDs(w, r)
	if !ok {
		return
	}
	p, _ := auth.FromContext(r.Context())
	items, err := h.Service.ListFiles(r.Context(), p.WorkspaceID, agentID)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"files": items})
}

func (h *Handler) UploadFile(w http.ResponseWriter, r *http.Request) {
	agentID, _, ok := fileIDs(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxFileBytes+(1<<20))
	if err := r.ParseMultipartForm(MaxFileBytes); err != nil {
		httpapi.Error(w, 400, errors.New("文件不能超过 10 MiB"))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpapi.Error(w, 400, errors.New("请选择文件"))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil || len(data) > MaxFileBytes {
		httpapi.Error(w, 400, errors.New("文件不能超过 10 MiB"))
		return
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	p, _ := auth.FromContext(r.Context())
	item, err := h.Service.UploadFile(r.Context(), p.WorkspaceID, agentID, header.Filename, contentType, data)
	if err != nil {
		fail(w, err)
		return
	}
	httpapi.JSON(w, 201, item)
}

func (h *Handler) DownloadFile(w http.ResponseWriter, r *http.Request) {
	agentID, fileID, ok := fileIDs(w, r)
	if !ok {
		return
	}
	p, _ := auth.FromContext(r.Context())
	item, reader, err := h.Service.OpenFile(r.Context(), p.WorkspaceID, agentID, fileID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpapi.Error(w, 404, errors.New("文件不存在"))
		} else {
			fail(w, err)
		}
		return
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, MaxFileBytes+1))
	if err != nil || len(data) != int(item.SizeBytes) {
		httpapi.Error(w, 500, errors.New("文件读取失败"))
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": item.Name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(data)
}

func (h *Handler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	agentID, fileID, ok := fileIDs(w, r)
	if !ok {
		return
	}
	p, _ := auth.FromContext(r.Context())
	if err := h.Service.DeleteFile(r.Context(), p.WorkspaceID, agentID, fileID); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(204)
}
