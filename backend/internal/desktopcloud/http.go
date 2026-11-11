package desktopcloud

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct{ Service *Service }
type deviceKey struct{}

// Access accepts device credentials only on the explicitly scoped desktop API.
// It never makes a device credential an ordinary application session.
func (h Handler) Access(cookieMiddleware func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		cookie := cookieMiddleware(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				cookie.ServeHTTP(w, r)
				return
			}
			if !desktopScope(r) || !strings.HasPrefix(header, "Bearer ") {
				httpapi.Error(w, 401, errDenied)
				return
			}
			raw := strings.TrimPrefix(header, "Bearer ")
			d, p, _, err := h.Service.deviceFromToken(r.Context(), raw)
			if err != nil {
				httpapi.Error(w, 401, errDenied)
				return
			}
			ctx := context.WithValue(r.Context(), principalKey{}, p)
			ctx = context.WithValue(ctx, deviceKey{}, d)
			ctx, cancel := h.Service.guard(modelruntime.WithActor(ctx, p.UserID), raw, d.Generation)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
func desktopScope(r *http.Request) bool {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/desktop/")
	if path == r.URL.Path {
		return false
	}
	if r.Method == http.MethodGet && (path == "models" || path == "conversations") {
		return true
	}
	parts := strings.Split(path, "/")
	if len(parts) == 3 && parts[0] == "models" && r.Method == http.MethodPost && (parts[2] == "generate" || parts[2] == "stream") {
		_, err := uuid.Parse(parts[1])
		return err == nil
	}
	if len(parts) == 3 && parts[0] == "devices" && r.Method == http.MethodPut && (parts[2] == "models" || parts[2] == "conversations") {
		_, err := uuid.Parse(parts[1])
		return err == nil
	}
	return false
}
func (s *Service) guard(ctx context.Context, raw string, generation int64) (context.Context, context.CancelFunc) {
	guarded, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-guarded.Done():
				return
			case <-ticker.C:
				check, done := context.WithTimeout(guarded, 5*time.Second)
				d, _, _, err := s.deviceFromToken(check, raw)
				done()
				if err != nil || d.Generation != generation {
					cancel()
					return
				}
			}
		}
	}()
	return guarded, cancel
}
func safeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errDenied):
		httpapi.Error(w, 403, errDenied)
	case errors.Is(err, errConflict):
		httpapi.Error(w, 409, errConflict)
	case errors.Is(err, ErrUnavailable):
		httpapi.Error(w, 503, ErrUnavailable)
	default:
		httpapi.Error(w, 400, errors.New("desktop request could not be completed"))
	}
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		httpapi.Error(w, 400, errors.New("invalid desktop request"))
		return false
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		httpapi.Error(w, 400, errors.New("invalid desktop request"))
		return false
	}
	return true
}
func cookiePrincipal(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	p, ok := auth.FromContext(r.Context())
	if !ok || r.Header.Get("Authorization") != "" {
		httpapi.Error(w, 401, errDenied)
		return auth.Principal{}, false
	}
	return p, true
}
func requestPrincipal(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	p, ok := principal(r.Context())
	if !ok {
		httpapi.Error(w, 401, errDenied)
	}
	return p, ok
}
func deviceID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid desktop device"))
		return uuid.Nil, false
	}
	if d, ok := r.Context().Value(deviceKey{}).(Device); ok && d.ID != id {
		httpapi.Error(w, 403, errDenied)
		return uuid.Nil, false
	}
	return id, true
}
func (h Handler) ListDevices(w http.ResponseWriter, r *http.Request) {
	p, ok := cookiePrincipal(w, r)
	if !ok {
		return
	}
	items, err := h.Service.ListDevices(r.Context(), p)
	if err != nil {
		safeError(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"devices": items})
}
func (h Handler) RegisterDevice(w http.ResponseWriter, r *http.Request) {
	p, ok := cookiePrincipal(w, r)
	if !ok {
		return
	}
	var in struct {
		ID          uuid.UUID `json:"id"`
		Name        string    `json:"name"`
		Platform    string    `json:"platform"`
		Reauthorize bool      `json:"reauthorize,omitempty"`
	}
	if !decode(w, r, &in) {
		return
	}
	d, raw, err := h.Service.Register(r.Context(), p, in.ID, in.Name, in.Platform, in.Reauthorize)
	if err != nil {
		safeError(w, err)
		return
	}
	out := map[string]any{"device": d}
	if raw != "" {
		out["token"] = raw
	}
	w.Header().Set("Cache-Control", "no-store")
	httpapi.JSON(w, 200, out)
}
func (h Handler) RevokeDevice(w http.ResponseWriter, r *http.Request) {
	p, ok := cookiePrincipal(w, r)
	if !ok {
		return
	}
	id, ok := deviceID(w, r)
	if !ok {
		return
	}
	if err := h.Service.Revoke(r.Context(), p, id); err != nil {
		safeError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h Handler) RotateToken(w http.ResponseWriter, r *http.Request) {
	p, ok := cookiePrincipal(w, r)
	if !ok {
		return
	}
	id, ok := deviceID(w, r)
	if !ok {
		return
	}
	raw, err := h.Service.Rotate(r.Context(), p, id)
	if err != nil {
		safeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpapi.JSON(w, 200, map[string]string{"token": raw})
}
func (h Handler) SyncModels(w http.ResponseWriter, r *http.Request) {
	p, ok := requestPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := deviceID(w, r)
	if !ok {
		return
	}
	var in struct {
		Models []ModelMetadata `json:"models"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := h.Service.SyncModels(r.Context(), p, id, in.Models); err != nil {
		safeError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h Handler) SyncConversations(w http.ResponseWriter, r *http.Request) {
	p, ok := requestPrincipal(w, r)
	if !ok {
		return
	}
	id, ok := deviceID(w, r)
	if !ok {
		return
	}
	// Transport excludes source/device/online fields as well as transcript bodies
	// and paths, so a device can synchronize only its owned metadata.
	var in struct {
		Conversations []struct {
			ID          uuid.UUID  `json:"id"`
			ProjectID   *uuid.UUID `json:"project_id"`
			ProjectName string     `json:"project_name"`
			Title       string     `json:"title"`
			Status      string     `json:"status"`
			Revision    int64      `json:"revision"`
			UpdatedAt   time.Time  `json:"updated_at"`
		} `json:"conversations"`
	}
	if !decode(w, r, &in) {
		return
	}
	items := make([]Conversation, 0, len(in.Conversations))
	for _, c := range in.Conversations {
		items = append(items, Conversation{ID: c.ID, ProjectID: c.ProjectID, ProjectName: c.ProjectName, Title: c.Title, Status: c.Status, Revision: c.Revision, UpdatedAt: c.UpdatedAt})
	}
	if err := h.Service.SyncConversations(r.Context(), p, id, items); err != nil {
		safeError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h Handler) ListConversations(w http.ResponseWriter, r *http.Request) {
	p, ok := requestPrincipal(w, r)
	if !ok {
		return
	}
	items, err := h.Service.ListConversations(r.Context(), p)
	if err != nil {
		safeError(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"conversations": items})
}
func (h Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	p, ok := requestPrincipal(w, r)
	if !ok {
		return
	}
	items, err := h.Service.Models.ListDeployments(r.Context(), p.WorkspaceID)
	if err != nil {
		safeError(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"models": items})
}
func (h Handler) Generate(w http.ResponseWriter, r *http.Request) { h.infer(w, r, false) }
func (h Handler) Stream(w http.ResponseWriter, r *http.Request)   { h.infer(w, r, true) }
func (h Handler) infer(w http.ResponseWriter, r *http.Request, stream bool) {
	p, ok := requestPrincipal(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid model"))
		return
	}
	var req modelruntime.Request
	if !decode(w, r, &req) {
		return
	}
	if err = validateRequest(req); err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	release, err := h.Service.acquire()
	if err != nil {
		httpapi.Error(w, 429, errors.New("desktop model concurrency limit reached"))
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), maxInferenceTime)
	defer cancel()
	client, deployment, err := h.Service.Models.Client(ctx, p.WorkspaceID, id)
	if err != nil {
		httpapi.Error(w, 404, errors.New("model unavailable"))
		return
	}
	req.Model = deployment.ModelID
	if !stream {
		response, err := client.Generate(ctx, req)
		if err != nil {
			httpapi.JSON(w, 502, map[string]any{"error_kind": safeKind(modelruntime.ErrorKindOf(err))})
			return
		}
		if !validResponse(response) {
			httpapi.JSON(w, 502, map[string]any{"error_kind": modelruntime.ErrorProtocol})
			return
		}
		httpapi.JSON(w, 200, toWireResponse(response))
		return
	}
	events, err := client.Stream(ctx, req)
	if err != nil {
		httpapi.JSON(w, 502, map[string]any{"error_kind": safeKind(modelruntime.ErrorKindOf(err))})
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	encoder := json.NewEncoder(w)
	size := 0
	completed := false
	writeFailure := func(kind modelruntime.ErrorKind) {
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Second))
		_ = encoder.Encode(WireEvent{Type: "RUN_FAILED", ErrorKind: kind})
		_ = http.NewResponseController(w).Flush()
	}
	for {
		select {
		case <-ctx.Done():
			kind := modelruntime.ErrorCancelled
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				kind = modelruntime.ErrorTransport
			}
			writeFailure(kind)
			return
		case ev, open := <-events:
			if !open {
				if !completed {
					writeFailure(modelruntime.ErrorTransport)
				}
				return
			}
			wire := toWireEvent(ev)
			raw, err := json.Marshal(wire)
			size += len(raw)
			if err != nil || size > maxResponseBytes || !validEvent(wire) {
				_ = encoder.Encode(WireEvent{Type: "RUN_FAILED", ErrorKind: modelruntime.ErrorProtocol})
				return
			}
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err = encoder.Encode(wire); err != nil {
				return
			}
			if err = http.NewResponseController(w).Flush(); err != nil {
				return
			}
			if wire.Type == "MODEL_COMPLETED" {
				completed = true
			}
			if wire.ErrorKind != "" {
				return
			}
		}
	}
}
