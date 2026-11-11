package desktopcloud

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func (s *Service) DesktopDeviceOnline(id uuid.UUID) bool {
	s.mu.Lock()
	p := s.peers[id]
	s.mu.Unlock()
	if p == nil {
		return false
	}
	select {
	case <-p.closed:
		return false
	default:
		return true
	}
}
func (s *Service) disconnect(id uuid.UUID) {
	s.mu.Lock()
	p := s.peers[id]
	delete(s.peers, id)
	s.mu.Unlock()
	if p != nil {
		p.close()
	}
}
func (h Handler) Connect(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		httpapi.Error(w, 401, errDenied)
		return
	}
	raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	d, _, hash, err := h.Service.deviceFromToken(r.Context(), raw)
	if err != nil {
		httpapi.Error(w, 401, errDenied)
		return
	}
	id, ok := deviceID(w, r)
	if !ok {
		return
	}
	if id != d.ID {
		httpapi.Error(w, 403, errDenied)
		return
	}
	upgrade := websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" || origin == h.Service.WebOrigin
	}}
	conn, err := upgrade.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	p := &peer{device: d, conn: conn, tokenHash: hash, calls: map[string]*pendingCall{}, closed: make(chan struct{})}
	h.Service.mu.Lock()
	old := h.Service.peers[id]
	if old != nil && old.device.Generation > d.Generation {
		h.Service.mu.Unlock()
		p.close()
		return
	}
	h.Service.peers[id] = p
	h.Service.mu.Unlock()
	if old != nil {
		old.close()
	}
	defer func() {
		p.close()
		h.Service.mu.Lock()
		if h.Service.peers[id] == p {
			delete(h.Service.peers, id)
		}
		h.Service.mu.Unlock()
	}()
	// Recheck after publishing this generation, covering rotation/revocation
	// racing the initial token lookup and socket upgrade.
	check, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	live, _, currentHash, err := h.Service.deviceFromToken(check, raw)
	cancel()
	if err != nil || live.Generation != d.Generation || !bytes.Equal(currentHash, hash) {
		return
	}
	conn.SetReadLimit(maxResponseBytes + 4096)
	_ = conn.SetReadDeadline(time.Now().Add(75 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(75 * time.Second)) })
	heartbeatCtx, stop := context.WithCancel(r.Context())
	defer stop()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-p.closed:
				return
			case <-ticker.C:
				ctx, done := context.WithTimeout(heartbeatCtx, 5*time.Second)
				current, _, _, err := h.Service.deviceFromToken(ctx, raw)
				if err == nil && current.Generation == d.Generation {
					_, err = h.Service.DB.Exec(ctx, `UPDATE desktop_devices SET last_seen_at=now() WHERE id=$1 AND generation=$2 AND revoked_at IS NULL`, id, d.Generation)
				}
				done()
				if err != nil || current.Generation != d.Generation {
					p.close()
					return
				}
				p.writeMu.Lock()
				err = conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second))
				p.writeMu.Unlock()
				if err != nil {
					p.close()
					return
				}
			}
		}
	}()
	_, _ = h.Service.DB.Exec(r.Context(), `UPDATE desktop_devices SET last_seen_at=now() WHERE id=$1 AND generation=$2`, id, d.Generation)
	for {
		var frame Frame
		if err = conn.ReadJSON(&frame); err != nil {
			return
		}
		if frame.ID == "" || len(frame.ID) > 64 || (frame.Type != "model.event" && frame.Type != "model.done") {
			return
		}
		if frame.Request != nil || frame.ModelKey != "" || frame.Stream {
			return
		}
		if frame.Type == "model.event" && (frame.Response != nil || frame.ErrorKind != "" || frame.Event == nil || !validEvent(*frame.Event)) {
			return
		}
		if frame.Type == "model.done" {
			if frame.Event != nil {
				return
			}
			if frame.ErrorKind != "" && safeKind(frame.ErrorKind) != frame.ErrorKind {
				return
			}
			if frame.Response != nil {
				response := frame.Response
				if !validResponseWire(response) {
					return
				}
			}
		}
		raw, err := json.Marshal(frame)
		if err != nil {
			return
		}
		p.mu.Lock()
		pending := p.calls[frame.ID]
		if pending == nil || pending.done {
			p.mu.Unlock()
			continue
		}
		pending.bytes += len(raw)
		if pending.bytes > maxResponseBytes {
			p.mu.Unlock()
			return
		}
		if frame.Type == "model.done" {
			pending.done = true
		}
		p.mu.Unlock()
		select {
		case pending.frames <- frame:
		case <-p.closed:
			return
		default:
			return
		}
	}
}
func validResponseWire(r *WireResponse) bool {
	return validResponse(&modelruntime.Response{Content: r.Content, ToolCalls: r.ToolCalls, Usage: r.Usage, NormalizedUsage: r.NormalizedUsage})
}
