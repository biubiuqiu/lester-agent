package desktopcloud

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrUnavailable = errors.New("desktop device is offline or unavailable")
var errDenied = errors.New("desktop access is unavailable")
var errConflict = errors.New("desktop metadata changed; reconnect or load the latest state")

type Device struct {
	ID          uuid.UUID  `json:"id"`
	WorkspaceID uuid.UUID  `json:"-"`
	UserID      uuid.UUID  `json:"-"`
	SessionID   *uuid.UUID `json:"-"`
	Name        string     `json:"name"`
	Platform    string     `json:"platform"`
	Generation  int64      `json:"generation"`
	Revoked     bool       `json:"revoked"`
	Online      bool       `json:"online"`
	LastSeenAt  *time.Time `json:"last_seen_at,omitempty"`
}

type ModelMetadata struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Source        string `json:"source,omitempty"`
	Provider      string `json:"provider"`
	ModelID       string `json:"model_id"`
	ContextWindow int    `json:"context_window,omitempty"`
	OutputReserve int    `json:"output_reserve,omitempty"`
}

type Conversation struct {
	ID          uuid.UUID  `json:"id"`
	ProjectID   *uuid.UUID `json:"project_id,omitempty"`
	ProjectName string     `json:"project_name"`
	Title       string     `json:"title"`
	Status      string     `json:"status"`
	Revision    int64      `json:"revision"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeviceID    uuid.UUID  `json:"device_id,omitempty"`
	DeviceName  string     `json:"device_name,omitempty"`
	Source      string     `json:"source"`
	Online      bool       `json:"online"`
	LastSeenAt  *time.Time `json:"last_seen_at,omitempty"`
}

// WireEvent has no error object, credentials, endpoint or provider error text.
type WireToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Index     int    `json:"index"`
}
type WireEvent struct {
	Type            string                        `json:"type"`
	Delta           string                        `json:"delta,omitempty"`
	ToolCall        *WireToolCall                 `json:"tool_call,omitempty"`
	ToolCalls       []*WireToolCall               `json:"tool_calls,omitempty"`
	Usage           map[string]int                `json:"usage,omitempty"`
	NormalizedUsage *modelruntime.NormalizedUsage `json:"normalized_usage,omitempty"`
	ErrorKind       modelruntime.ErrorKind        `json:"error_kind,omitempty"`
}
type WireResponse struct {
	Content         string                        `json:"content"`
	ToolCalls       []modelruntime.ToolCall       `json:"tool_calls,omitempty"`
	Usage           map[string]int                `json:"usage,omitempty"`
	NormalizedUsage *modelruntime.NormalizedUsage `json:"normalized_usage,omitempty"`
}
type Frame struct {
	Type      string                 `json:"type"`
	ID        string                 `json:"id,omitempty"`
	ModelKey  string                 `json:"model_key,omitempty"`
	Stream    bool                   `json:"stream,omitempty"`
	Request   *modelruntime.Request  `json:"request,omitempty"`
	Event     *WireEvent             `json:"event,omitempty"`
	Response  *WireResponse          `json:"response,omitempty"`
	ErrorKind modelruntime.ErrorKind `json:"error_kind,omitempty"`
}

type Service struct {
	DB        *pgxpool.Pool
	Models    *model.Store
	WebOrigin string
	mu        sync.Mutex
	peers     map[uuid.UUID]*peer
	// Gate bounds aggregate requests in addition to each connection's pending map.
	gateway chan struct{}
}

func New(db *pgxpool.Pool, models *model.Store, webOrigin string) *Service {
	return &Service{DB: db, Models: models, WebOrigin: webOrigin, peers: map[uuid.UUID]*peer{}, gateway: make(chan struct{}, 32)}
}

type pendingCall struct {
	frames chan Frame
	bytes  int
	done   bool
}

type peer struct {
	device    Device
	conn      *websocket.Conn
	tokenHash []byte
	writeMu   sync.Mutex
	mu        sync.Mutex
	calls     map[string]*pendingCall
	closed    chan struct{}
	once      sync.Once
}

func (p *peer) send(frame Frame) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return p.conn.WriteJSON(frame)
}
func (p *peer) close() { p.once.Do(func() { close(p.closed); _ = p.conn.Close() }) }

type principalKey struct{}

func principal(ctx context.Context) (auth.Principal, bool) {
	if p, ok := auth.FromContext(ctx); ok {
		return p, true
	}
	p, ok := ctx.Value(principalKey{}).(auth.Principal)
	return p, ok
}
