package sandbox

import (
	"context"
	"errors"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/creack/pty"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"os/exec"
	"time"
)

type ServiceHandler struct {
	provider Provider
	upgrader websocket.Upgrader
}

func NewServiceHandler(provider Provider) *ServiceHandler {
	return &ServiceHandler{provider: provider, upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}}
}
func (h *ServiceHandler) Router() http.Handler {
	r := chi.NewRouter()
	r.Post("/v1/sandboxes", h.create)
	r.Get("/v1/sandboxes/{id}", h.inspect)
	r.Post("/v1/sandboxes/{id}/start", h.action(h.provider.Start))
	r.Post("/v1/sandboxes/{id}/suspend", h.action(h.provider.Suspend))
	r.Post("/v1/sandboxes/{id}/resume", h.action(h.provider.Resume))
	r.Delete("/v1/sandboxes/{id}", h.action(h.provider.Destroy))
	r.Post("/v1/sandboxes/{id}/exec", h.exec)
	r.Get("/v1/sandboxes/{id}/files", h.list)
	r.Get("/v1/sandboxes/{id}/files/content", h.read)
	r.Put("/v1/sandboxes/{id}/files/content", h.write)
	r.Get("/v1/sandboxes/{id}/terminal", h.terminal)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { httpapi.JSON(w, 200, map[string]bool{"ok": true}) })
	return r
}
func (h *ServiceHandler) inspect(w http.ResponseWriter, r *http.Request) {
	item, err := h.provider.Inspect(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, ErrSandboxNotFound) {
			httpapi.Error(w, 404, err)
			return
		}
		httpapi.Error(w, 500, err)
		return
	}
	httpapi.JSON(w, 200, item)
}
func (h *ServiceHandler) create(w http.ResponseWriter, r *http.Request) {
	var req CreateOptions
	if !httpapi.Decode(w, r, &req) {
		return
	}
	item, err := h.provider.Create(r.Context(), req)
	if err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	httpapi.JSON(w, 201, item)
}
func (h *ServiceHandler) action(fn func(context.Context, string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(r.Context(), chi.URLParam(r, "id")); err != nil {
			httpapi.Error(w, 500, err)
			return
		}
		w.WriteHeader(204)
	}
}
func (h *ServiceHandler) exec(w http.ResponseWriter, r *http.Request) {
	var req Command
	if !httpapi.Decode(w, r, &req) {
		return
	}
	item, err := h.provider.Exec(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	httpapi.JSON(w, 200, item)
}
func (h *ServiceHandler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.provider.ListFiles(r.Context(), chi.URLParam(r, "id"), r.URL.Query().Get("work_dir"), r.URL.Query().Get("path"))
	if err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"files": items})
}
func (h *ServiceHandler) read(w http.ResponseWriter, r *http.Request) {
	data, err := h.provider.ReadFile(r.Context(), chi.URLParam(r, "id"), r.URL.Query().Get("work_dir"), r.URL.Query().Get("path"))
	if err != nil {
		httpapi.Error(w, 404, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(data)
}
func (h *ServiceHandler) write(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 20<<20))
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	if err = h.provider.WriteFile(r.Context(), chi.URLParam(r, "id"), r.URL.Query().Get("work_dir"), r.URL.Query().Get("path"), data); err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	w.WriteHeader(204)
}

type terminalMessage struct {
	Type, Data string
	Cols, Rows int
}

func (h *ServiceHandler) terminal(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workDir, err := safeWorkDir(r.URL.Query().Get("work_dir"))
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	name, err := containerName(id)
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	if err = exec.CommandContext(r.Context(), "docker", "exec", name, "mkdir", "-p", workDir).Run(); err != nil {
		_ = conn.WriteJSON(terminalMessage{Type: "error", Data: err.Error()})
		return
	}
	command := exec.CommandContext(r.Context(), "docker", "exec", "-it", "-w", workDir, name, "sh")
	terminal, err := pty.Start(command)
	if err != nil {
		_ = conn.WriteJSON(terminalMessage{Type: "error", Data: err.Error()})
		return
	}
	defer terminal.Close()
	_ = pty.Setsize(terminal, &pty.Winsize{Cols: 120, Rows: 32})
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 4096)
		for {
			n, e := terminal.Read(buffer)
			if n > 0 {
				_ = conn.WriteJSON(terminalMessage{Type: "output", Data: string(buffer[:n])})
			}
			if e != nil {
				return
			}
		}
	}()
	for {
		var message terminalMessage
		if conn.ReadJSON(&message) != nil {
			break
		}
		if message.Type == "input" {
			_, _ = terminal.Write([]byte(message.Data))
		}
		if message.Type == "resize" {
			_ = pty.Setsize(terminal, &pty.Winsize{Cols: uint16(max(message.Cols, 1)), Rows: uint16(max(message.Rows, 1))})
		}
	}
	_ = command.Process.Kill()
	select {
	case <-done:
	case <-time.After(time.Second):
	}
}
