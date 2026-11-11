package desktopcloud

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/model/integration"
	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/biubiuqiu/lester-agent/backend/internal/secret"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixture struct {
	db      *pgxpool.Pool
	service *Service
	auth    *auth.Service
	server  *httptest.Server
}
type identity struct {
	p      auth.Principal
	cookie *http.Cookie
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("LESTER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LESTER_TEST_DATABASE_URL for real PostgreSQL desktop tests")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost" && !strings.HasPrefix(cfg.ConnConfig.Host, "/") {
		t.Fatal("desktop tests require local PostgreSQL")
	}
	ctx := context.Background()
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `BEGIN; SELECT pg_advisory_xact_lock(hashtextextended('lester:test:pgcrypto',0)); CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public; COMMIT;`); err != nil {
		t.Fatal(err)
	}
	schema := "desktop_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	files, _ := filepath.Glob("../../migrations/*.up.sql")
	for _, file := range files {
		raw, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(ctx, string(raw)); e != nil {
			t.Fatalf("migration %s: %v", file, e)
		}
	}
	secrets, err := secret.New(db, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	models := model.NewStore(db, secrets, integration.NewDefaultRegistry())
	s := New(db, models, "https://lester.example")
	models.SetDesktopProvider(s)
	f := &fixture{db: db, service: s, auth: auth.New(db, nil, time.Hour, false)}
	h := Handler{Service: s}
	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(f.auth.Middleware)
		r.Post("/api/v1/desktop/devices", h.RegisterDevice)
		r.Get("/api/v1/desktop/devices", h.ListDevices)
		r.Delete("/api/v1/desktop/devices/{id}", h.RevokeDevice)
		r.Post("/api/v1/desktop/devices/{id}/token", h.RotateToken)
	})
	router.Get("/api/v1/desktop/devices/{id}/connect", h.Connect)
	router.Group(func(r chi.Router) {
		r.Use(h.Access(f.auth.Middleware))
		r.Get("/api/v1/desktop/models", h.ListModels)
		r.Post("/api/v1/desktop/models/{id}/generate", h.Generate)
		r.Post("/api/v1/desktop/models/{id}/stream", h.Stream)
		r.Get("/api/v1/desktop/conversations", h.ListConversations)
		r.Put("/api/v1/desktop/devices/{id}/models", h.SyncModels)
		r.Put("/api/v1/desktop/devices/{id}/conversations", h.SyncConversations)
		r.Get("/api/v1/ordinary", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	})
	f.server = httptest.NewServer(router)
	t.Cleanup(f.server.Close)
	t.Cleanup(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, p := range s.peers {
			p.close()
		}
	})
	return f
}
func (f *fixture) identity(t *testing.T, workspace uuid.UUID) identity {
	t.Helper()
	ctx := context.Background()
	user := uuid.New()
	if workspace == uuid.Nil {
		workspace = uuid.New()
		if _, err := f.db.Exec(ctx, `INSERT INTO workspaces(id,name) VALUES($1,'Desktop fixture')`, workspace); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec(ctx, `INSERT INTO users(id,email,display_name,password_hash) VALUES($1,$2,'Desktop owner','unused-test-hash')`, user, user.String()+"@example.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2)`, workspace, user); err != nil {
		t.Fatal(err)
	}
	raw := sha256.Sum256([]byte(uuid.NewString()))
	hash := sha256.Sum256(raw[:])
	if _, err := f.db.Exec(ctx, `WITH family AS (INSERT INTO sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '30 days') RETURNING id) INSERT INTO auth_access_tokens(token_hash,session_id,expires_at) SELECT $2,id,now()+interval '2 hours' FROM family`, user, hash[:]); err != nil {
		t.Fatal(err)
	}
	return identity{p: auth.Principal{UserID: user, WorkspaceID: workspace, SessionHash: hash[:]}, cookie: &http.Cookie{Name: "lester_access_token", Value: base64.RawURLEncoding.EncodeToString(raw[:])}}
}
func (f *fixture) request(t *testing.T, who identity, bearer, method, path string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = strings.NewReader(string(raw))
	}
	req, err := http.NewRequest(method, f.server.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	} else if who.cookie != nil {
		req.AddCookie(who.cookie)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	return response
}
func (f *fixture) register(t *testing.T, who identity) (Device, string) {
	t.Helper()
	d, raw, err := f.service.Register(context.Background(), who.p, uuid.New(), "Fixture desktop", "linux", false)
	if err != nil {
		t.Fatal(err)
	}
	return d, raw
}
func (f *fixture) socket(t *testing.T, d Device, raw string) *websocket.Conn {
	t.Helper()
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(f.server.URL, "http")+"/api/v1/desktop/devices/"+d.ID.String()+"/connect", http.Header{"Authorization": []string{"Bearer " + raw}})
	if err != nil {
		if response != nil {
			t.Fatalf("socket: %v (%d)", err, response.StatusCode)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}
func (f *fixture) localModel(t *testing.T, who identity, d Device) uuid.UUID {
	t.Helper()
	if err := f.service.SyncModels(context.Background(), who.p, d.ID, []ModelMetadata{{ID: "local-fixture", Name: "Local fixture", Provider: "openai", ModelID: "fixture", ContextWindow: 32768}}); err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	if err := f.db.QueryRow(context.Background(), `SELECT deployment_id FROM desktop_device_models WHERE device_id=$1`, d.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func actor(who identity) context.Context {
	return modelruntime.WithActor(context.Background(), who.p.UserID)
}
func status(t *testing.T, response *http.Response, want int) {
	t.Helper()
	if response.StatusCode != want {
		raw, _ := io.ReadAll(response.Body)
		t.Fatalf("status %d want %d: %s", response.StatusCode, want, raw)
	}
}

func TestDeviceCredentialsOwnerReadOnlyAndRevocation(t *testing.T) {
	f := newFixture(t)
	owner := f.identity(t, uuid.Nil)
	other := f.identity(t, owner.p.WorkspaceID)
	d, raw := f.register(t, owner)
	id := f.localModel(t, owner, d)
	// The opaque credential is hashed, short-scoped, and independently expires.
	var hash []byte
	var expires time.Time
	if err := f.db.QueryRow(context.Background(), `SELECT token_hash,token_expires_at FROM desktop_devices WHERE id=$1`, d.ID).Scan(&hash, &expires); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(raw))
	if string(hash) != string(sum[:]) || time.Until(expires) > 30*24*time.Hour || time.Until(expires) < 29*24*time.Hour {
		t.Fatal("device token hash/expiry")
	}
	_, again, err := f.service.Register(context.Background(), owner.p, d.ID, d.Name, d.Platform, false)
	if err != nil || again != "" {
		t.Fatal("registration replay rotated or returned credential")
	}
	status(t, f.request(t, identity{}, raw, "GET", "/api/v1/ordinary", nil), 401)
	status(t, f.request(t, identity{}, raw, "POST", "/api/v1/desktop/devices", map[string]any{"id": uuid.New(), "name": "bad", "platform": "linux"}), 401)
	devices, _ := f.service.ListDevices(context.Background(), other.p)
	if len(devices) != 0 {
		t.Fatal("same-workspace owner leak")
	}
	models, err := f.service.Models.ListDeployments(actor(other), other.p.WorkspaceID)
	if err != nil || len(models) != 0 {
		t.Fatalf("cross-owner model leak: %v", err)
	}
	if _, _, err = f.service.Models.Client(actor(other), other.p.WorkspaceID, id); err == nil {
		t.Fatal("another workspace member used private model")
	}
	if _, _, err = f.service.Models.Client(context.Background(), owner.p.WorkspaceID, id); err == nil {
		t.Fatal("missing actor accepted")
	}
	models, err = f.service.Models.ListDeployments(actor(owner), owner.p.WorkspaceID)
	if err != nil || len(models) != 1 || models[0].Source != "device" || !models[0].ReadOnly || models[0].Online {
		t.Fatalf("model metadata: %+v %v", models, err)
	}
	value := 1234
	if err = f.service.Models.SetDeploymentBudget(actor(owner), owner.p.WorkspaceID, id, model.DeploymentBudgetChange{ContextWindow: &value}); !errors.Is(err, model.ErrDeploymentNotFound) {
		t.Fatalf("desktop budget writable: %v", err)
	}
	if err = f.service.SyncModels(context.Background(), other.p, d.ID, nil); !errors.Is(err, errDenied) {
		t.Fatal("cross-owner model mutation")
	}
	conn := f.socket(t, d, raw)
	if err = f.service.Revoke(context.Background(), owner.p, d.ID); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err = conn.ReadMessage(); err == nil {
		t.Fatal("revoked socket alive")
	}
	status(t, f.request(t, identity{}, raw, "GET", "/api/v1/desktop/models", nil), 401)
	if _, _, _, err = f.service.deviceFromToken(context.Background(), raw); err == nil {
		t.Fatal("revoked bearer accepted")
	}
}

func TestCloudToLocalInferenceCancellationGenerationAndNoReplay(t *testing.T) {
	f := newFixture(t)
	owner := f.identity(t, uuid.Nil)
	d, raw := f.register(t, owner)
	id := f.localModel(t, owner, d)
	conn := f.socket(t, d, raw)
	client, _, err := f.service.Models.Client(actor(owner), owner.p.WorkspaceID, id)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		response, e := client.Generate(actor(owner), modelruntime.Request{Messages: []modelruntime.Message{{Role: "user", Content: "hello"}}})
		if e == nil && (response == nil || response.Content != "from local") {
			e = errors.New("unexpected response")
		}
		result <- e
	}()
	var request Frame
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err = conn.ReadJSON(&request); err != nil {
		t.Fatal(err)
	}
	if request.Type != "model.request" || request.ModelKey != "local-fixture" || request.Stream || request.Request == nil {
		t.Fatalf("request: %+v", request)
	}
	if err = conn.WriteJSON(Frame{Type: "model.done", ID: request.ID, Response: &WireResponse{Content: "from local"}}); err != nil {
		t.Fatal(err)
	}
	if err = <-result; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(actor(owner))
	events, err := client.Stream(ctx, modelruntime.Request{Messages: []modelruntime.Message{{Role: "user", Content: "stream"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = conn.ReadJSON(&request); err != nil {
		t.Fatal(err)
	}
	if err = conn.WriteJSON(Frame{Type: "model.event", ID: request.ID, Event: &WireEvent{Type: "MODEL_DELTA", ToolCall: &WireToolCall{ID: "call-1", Name: "read", Arguments: `{"pa`, Index: 0}}}); err != nil {
		t.Fatal(err)
	}
	ev := <-events
	if ev.ToolCall == nil || string(ev.ToolCall.Arguments) != `{"pa` {
		t.Fatal("tool fragments lost")
	}
	cancel()
	var cancelled Frame
	if err = conn.ReadJSON(&cancelled); err != nil || cancelled.Type != "model.cancel" || cancelled.ID != request.ID {
		t.Fatalf("cancel: %+v %v", cancelled, err)
	}
	for range events {
	}
	go func() {
		_, e := client.Generate(actor(owner), modelruntime.Request{Messages: []modelruntime.Message{{Role: "user", Content: "unknown"}}})
		result <- e
	}()
	if err = conn.ReadJSON(&request); err != nil {
		t.Fatal(err)
	}
	replacement := f.socket(t, d, raw)
	if err = <-result; !errors.Is(err, ErrUnavailable) {
		t.Fatalf("replacement did not interrupt: %v", err)
	}
	// A fresh explicit request must be first on the replacement connection.
	go func() {
		_, e := client.Generate(actor(owner), modelruntime.Request{Messages: []modelruntime.Message{{Role: "user", Content: "fresh"}}})
		result <- e
	}()
	var fresh Frame
	_ = replacement.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err = replacement.ReadJSON(&fresh); err != nil {
		t.Fatal(err)
	}
	if fresh.ID == request.ID || fresh.Request.Messages[0].Content != "fresh" {
		t.Fatal("in-flight request replayed")
	}
	next, err := f.service.Rotate(context.Background(), owner.p, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = <-result; !errors.Is(err, ErrUnavailable) {
		t.Fatalf("rotation did not interrupt: %v", err)
	}
	if _, _, _, err = f.service.deviceFromToken(context.Background(), raw); err == nil {
		t.Fatal("old token accepted")
	}
	newPeer := f.socket(t, d, next)
	// Closure of both stale peers must leave the replacement generation online.
	f.service.mu.Lock()
	live := f.service.peers[d.ID]
	f.service.mu.Unlock()
	if live == nil || live.device.Generation <= d.Generation {
		t.Fatal("new generation missing")
	}
	_ = newPeer
}

func TestConversationMetadataRevisionAndOfflineState(t *testing.T) {
	f := newFixture(t)
	owner := f.identity(t, uuid.Nil)
	other := f.identity(t, owner.p.WorkspaceID)
	d, raw := f.register(t, owner)
	updated := time.Now().UTC().Truncate(time.Microsecond)
	item := Conversation{ID: uuid.New(), Title: "Local private title", ProjectName: "Local project", Status: "running", Revision: 2, UpdatedAt: updated}
	if err := f.service.SyncConversations(context.Background(), owner.p, d.ID, []Conversation{item}); err != nil {
		t.Fatal(err)
	}
	if err := f.service.SyncConversations(context.Background(), owner.p, d.ID, []Conversation{item}); err != nil {
		t.Fatal("exact revision retry", err)
	}
	older := item
	older.Revision = 1
	if err := f.service.SyncConversations(context.Background(), owner.p, d.ID, []Conversation{older}); !errors.Is(err, errConflict) {
		t.Fatal("stale revision accepted")
	}
	changed := item
	changed.Status = "failed"
	if err := f.service.SyncConversations(context.Background(), owner.p, d.ID, []Conversation{changed}); !errors.Is(err, errConflict) {
		t.Fatal("conflicting revision accepted")
	}
	status(t, f.request(t, identity{}, raw, "PUT", "/api/v1/desktop/devices/"+d.ID.String()+"/conversations", map[string]any{"conversations": []map[string]any{{"id": uuid.New(), "title": "secret", "path": "/private", "revision": 1, "status": "idle", "updated_at": updated}}}), 400)
	list, err := f.service.ListConversations(context.Background(), owner.p)
	if err != nil || len(list) != 1 || list[0].Online || list[0].Status != "running" || list[0].Source != "local" {
		t.Fatalf("offline status mutated: %+v %v", list, err)
	}

	cloudID := uuid.New()
	if _, err = f.db.Exec(context.Background(), `INSERT INTO conversations(id,workspace_id,created_by,agent_slug,title) VALUES($1,$2,$3,'lester','Owned cloud task')`, cloudID, owner.p.WorkspaceID, owner.p.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(context.Background(), `INSERT INTO runs(conversation_id,status) VALUES($1,'completed')`, cloudID); err != nil {
		t.Fatal(err)
	}
	merged, err := f.service.ListConversations(context.Background(), owner.p)
	if err != nil || len(merged) != 2 {
		t.Fatalf("unified metadata: %+v %v", merged, err)
	}
	cloudFound := false
	for _, entry := range merged {
		if entry.ID == cloudID {
			cloudFound = entry.Source == "cloud" && entry.Online && entry.Status == "completed" && entry.ProjectID != nil
		}
	}
	if !cloudFound {
		t.Fatal("owned cloud task metadata missing")
	}
	list, err = f.service.ListConversations(context.Background(), other.p)
	if err != nil || len(list) != 0 {
		t.Fatal("cross-owner conversation leak")
	}
	if _, err = f.db.Exec(context.Background(), `DELETE FROM sessions WHERE user_id=$1`, owner.p.UserID); err != nil {
		t.Fatal(err)
	}
	status(t, f.request(t, identity{}, raw, "GET", "/api/v1/desktop/conversations", nil), 401)
	if err = f.service.SyncConversations(context.Background(), owner.p, d.ID, []Conversation{item}); !errors.Is(err, errDenied) {
		t.Fatalf("revoked family metadata accepted: %v", err)
	}
}

func TestLocalToCloudGatewayAndSafeStream(t *testing.T) {
	f := newFixture(t)
	owner := f.identity(t, uuid.Nil)
	d, raw := f.register(t, owner)
	_ = d
	var calls atomic.Int32
	var reject atomic.Bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if reject.Load() {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, "provider private echo fixture-cloud-secret")
			return
		}
		if r.Header.Get("Authorization") != "Bearer fixture-cloud-secret" {
			t.Error("server did not retain cloud secret")
		}
		var req struct {
			Stream bool   `json:"stream"`
			Model  string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "fixture" {
			t.Errorf("gateway did not resolve provider model ID: %q", req.Model)
		}
		if !req.Stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"from cloud"}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"cloud stream\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()
	connection, err := f.service.Models.CreateConnection(context.Background(), owner.p.WorkspaceID, "Cloud fixture", "openai", provider.URL, nil, "fixture-cloud-secret")
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := f.service.Models.CreateDeployment(context.Background(), owner.p.WorkspaceID, connection.ID, "Cloud fixture", "fixture", false)
	if err != nil {
		t.Fatal(err)
	}
	req := modelruntime.Request{Model: "caller-must-not-choose-this-model", Messages: []modelruntime.Message{{Role: "user", Content: "hello"}}}
	response := f.request(t, identity{}, raw, "POST", "/api/v1/desktop/models/"+deployment.ID.String()+"/generate", req)
	status(t, response, 200)
	body, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(body), "cloud stream") || strings.Contains(string(body), "fixture-cloud-secret") {
		t.Fatalf("generate body: %s", body)
	}
	response = f.request(t, identity{}, raw, "POST", "/api/v1/desktop/models/"+deployment.ID.String()+"/stream", req)
	status(t, response, 200)
	scan := bufio.NewScanner(response.Body)
	delta, done := false, false
	for scan.Scan() {
		var event WireEvent
		if err = json.Unmarshal(scan.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		delta = delta || event.Delta == "cloud stream"
		done = done || event.Type == "MODEL_COMPLETED"
		if strings.Contains(scan.Text(), "fixture-cloud-secret") {
			t.Fatal("secret leaked")
		}
	}
	if err = scan.Err(); err != nil || !delta || !done {
		t.Fatalf("incomplete stream %v %v %v", delta, done, err)
	}
	reject.Store(true)
	for _, mode := range []string{"generate", "stream"} {
		response = f.request(t, identity{}, raw, "POST", "/api/v1/desktop/models/"+deployment.ID.String()+"/"+mode, req)
		status(t, response, 502)
		body, _ = io.ReadAll(response.Body)
		if strings.Contains(string(body), "fixture-cloud-secret") || strings.Contains(string(body), "private echo") || !strings.Contains(string(body), "authentication") {
			t.Fatalf("unsafe provider error %s", body)
		}
	}
	// Invalid limits must be rejected before any provider invocation.
	before := calls.Load()
	req.Tools = make([]modelruntime.Tool, 65)
	status(t, f.request(t, identity{}, raw, "POST", "/api/v1/desktop/models/"+deployment.ID.String()+"/generate", req), 400)
	if calls.Load() != before {
		t.Fatal("invalid request reached provider")
	}
	for i := 0; i < cap(f.service.gateway); i++ {
		f.service.gateway <- struct{}{}
	}
	status(t, f.request(t, identity{}, raw, "POST", "/api/v1/desktop/models/"+deployment.ID.String()+"/generate", modelruntime.Request{}), 429)
	for len(f.service.gateway) > 0 {
		<-f.service.gateway
	}
}

func TestPeerConcurrencyAndTokenExpiry(t *testing.T) {
	f := newFixture(t)
	owner := f.identity(t, uuid.Nil)
	d, raw := f.register(t, owner)
	id := f.localModel(t, owner, d)
	conn := f.socket(t, d, raw)
	client, _, err := f.service.Models.Client(actor(owner), owner.p.WorkspaceID, id)
	if err != nil {
		t.Fatal(err)
	}
	contexts := make([]context.CancelFunc, 0, maxPeerCalls)
	streams := make([]<-chan modelruntime.Event, 0, maxPeerCalls)
	for i := 0; i < maxPeerCalls; i++ {
		ctx, cancel := context.WithCancel(actor(owner))
		contexts = append(contexts, cancel)
		events, err := client.Stream(ctx, modelruntime.Request{})
		if err != nil {
			t.Fatal(err)
		}
		streams = append(streams, events)
		var frame Frame
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err = conn.ReadJSON(&frame); err != nil || frame.Type != "model.request" {
			t.Fatalf("request %d: %+v %v", i, frame, err)
		}
	}
	if _, err = client.Stream(actor(owner), modelruntime.Request{}); modelruntime.ErrorKindOf(err) != modelruntime.ErrorRateLimit {
		t.Fatalf("missing peer concurrency bound: %v", err)
	}
	// Cancelling all pending calls releases both peer and global capacity.
	for _, cancel := range contexts {
		cancel()
	}
	for range contexts {
		var frame Frame
		if err = conn.ReadJSON(&frame); err != nil || frame.Type != "model.cancel" {
			t.Fatalf("cancel: %+v %v", frame, err)
		}
	}
	for _, events := range streams {
		for range events {
		}
	}
	if len(f.service.gateway) != 0 {
		t.Fatal("cancelled requests leaked aggregate capacity")
	}
	if _, err = f.db.Exec(context.Background(), `UPDATE desktop_devices SET token_expires_at=now()-interval '1 second' WHERE id=$1`, d.ID); err != nil {
		t.Fatal(err)
	}
	status(t, f.request(t, identity{}, raw, "GET", "/api/v1/desktop/models", nil), 401)
	if _, err = client.Generate(actor(owner), modelruntime.Request{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expired desktop inference accepted: %v", err)
	}
}

func TestLongStreamRechecksDeviceSession(t *testing.T) {
	if testing.Short() {
		t.Skip("30-second production session guard test")
	}
	f := newFixture(t)
	owner := f.identity(t, uuid.Nil)
	_, raw := f.register(t, owner)
	providerCancelled := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"first\"},\"finish_reason\":null}]}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(providerCancelled)
	}))
	defer provider.Close()
	c, err := f.service.Models.CreateConnection(context.Background(), owner.p.WorkspaceID, "Guard fixture", "openai", provider.URL, nil, "fixture-secret")
	if err != nil {
		t.Fatal(err)
	}
	d, err := f.service.Models.CreateDeployment(context.Background(), owner.p.WorkspaceID, c.ID, "Guard fixture", "fixture", false)
	if err != nil {
		t.Fatal(err)
	}
	response := f.request(t, identity{}, raw, "POST", "/api/v1/desktop/models/"+d.ID.String()+"/stream", modelruntime.Request{})
	status(t, response, 200)
	reader := bufio.NewReader(response.Body)
	if _, err = reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(context.Background(), `DELETE FROM sessions WHERE user_id=$1`, owner.p.UserID); err != nil {
		t.Fatal(err)
	}
	ended := make(chan struct{})
	var terminal []byte
	go func() { terminal, _ = io.ReadAll(reader); close(ended) }()
	select {
	case <-ended:
	case <-time.After(35 * time.Second):
		t.Fatal("long stream survived revoked session beyond 30-second recheck")
	}
	if !strings.Contains(string(terminal), "error_kind") {
		t.Fatal("revoked stream ended without a failure frame")
	}
	select {
	case <-providerCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not receive session revocation cancellation")
	}
}

func TestDisconnectedBufferedStreamCannotAppearCompleted(t *testing.T) {
	f := newFixture(t)
	owner := f.identity(t, uuid.Nil)
	d, raw := f.register(t, owner)
	id := f.localModel(t, owner, d)
	conn := f.socket(t, d, raw)
	client, _, err := f.service.Models.Client(actor(owner), owner.p.WorkspaceID, id)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(actor(owner))
	defer cancel()
	events, err := client.Stream(ctx, modelruntime.Request{})
	if err != nil {
		t.Fatal(err)
	}
	var request Frame
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err = conn.ReadJSON(&request); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 24; i++ {
		if err = conn.WriteJSON(Frame{Type: "model.event", ID: request.ID, Event: &WireEvent{Type: "MODEL_DELTA", Delta: "partial"}}); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(events) < cap(events) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(events) != cap(events) {
		t.Fatal("fixture did not fill consumer buffer")
	}
	_ = conn.Close()
	failed := false
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		for event := range events {
			if event.Err != nil {
				failed = true
			}
		}
	}()
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("disconnected stream did not terminate")
	}
	if !failed {
		t.Fatal("buffered partial stream closed without a failure event")
	}
}
