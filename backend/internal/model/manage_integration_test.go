package model

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/model/integration"
	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/biubiuqiu/lester-agent/backend/internal/secret"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type modelFixture struct {
	db    *pgxpool.Pool
	store *Store
	auth  *auth.Service
}

func newModelFixture(t *testing.T) modelFixture {
	t.Helper()
	dsn := os.Getenv("LESTER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LESTER_TEST_DATABASE_URL for PostgreSQL model integration tests")
	}
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test database configuration")
	}
	if config.ConnConfig.Host != "127.0.0.1" && config.ConnConfig.Host != "localhost" && !strings.HasPrefix(config.ConnConfig.Host, "/") {
		t.Fatal("model integration tests require a local test database")
	}
	admin, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `BEGIN; SELECT pg_advisory_xact_lock(hashtextextended('lester:test:pgcrypto',0)); CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public; COMMIT;`); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	schema := "model_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	config, _ = pgxpool.ParseConfig(dsn)
	config.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	files, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, string(data)); err != nil {
			t.Fatalf("migration %s: %v", filepath.Base(file), err)
		}
	}
	secrets, err := secret.New(db, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return modelFixture{db: db, store: NewStore(db, secrets, integration.NewDefaultRegistry()), auth: auth.New(db, nil, time.Hour, false)}
}

func (f modelFixture) identity(t *testing.T, role string) (uuid.UUID, uuid.UUID, *http.Cookie) {
	t.Helper()
	user, workspace := uuid.New(), uuid.New()
	ctx := context.Background()
	if _, err := f.db.Exec(ctx, `INSERT INTO users(id,email,display_name,password_hash,role) VALUES($1,$2,'Fixture','fixture-unused-password-hash',$3)`, user, user.String()+"@example.invalid", role); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(ctx, `INSERT INTO workspaces(id,name) VALUES($1,'Fixture')`, workspace); err != nil {
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
	return user, workspace, &http.Cookie{Name: "lester_access_token", Value: base64.RawURLEncoding.EncodeToString(raw[:])}
}

func (f modelFixture) request(t *testing.T, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	h := NewHandler(f.store)
	router := chi.NewRouter()
	router.Use(f.auth.Middleware)
	router.Get("/api/v1/model-connections", h.ListConnections)
	router.Post("/api/v1/model-connections/{id}/test", h.TestConnection)
	router.Get("/api/v1/model-deployments", h.ListDeployments)
	router.Post("/api/v1/model-deployments", h.CreateDeployment)
	router.Patch("/api/v1/model-deployments/{id}", h.UpdateDeployment)
	router.Post("/api/v1/model-deployments/{id}/test", h.TestDeployment)
	router.With(auth.RequireAdmin).Post("/api/v1/admin/model-deployments/{id}/test", h.TestDeployment)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if cookie != nil {
		req.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func TestModelBudgetsPersistWithoutCrossWorkspaceChanges(t *testing.T) {
	f := newModelFixture(t)
	_, owner, cookie := f.identity(t, "member")
	_, other, otherCookie := f.identity(t, "member")
	ctx := context.Background()
	connection, err := f.store.CreateConnection(ctx, owner, "Fixture", "openai", "https://example.invalid", nil, "fixture-private-key")
	if err != nil {
		t.Fatal(err)
	}
	window, reserve, policy := 128000, 8000, 64000
	deployment, err := f.store.CreateDeploymentWithBudget(ctx, owner, connection.ID, "Fixture", "fixture", true, DeploymentBudgetChange{ContextWindow: &window, OutputReserve: &reserve, PolicyWindow: &policy})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.UpdateDeployment(ctx, owner, deployment.ID, DeploymentChange{Name: "Renamed", ModelID: "fixture", ConnectionID: connection.ID, IsDefault: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	response := f.request(t, cookie, http.MethodPatch, "/api/v1/model-deployments/"+deployment.ID.String(), `{"policy_window":32000}`)
	if response.Code != 204 {
		t.Fatalf("budget PATCH=%d %s", response.Code, response.Body.String())
	}
	items, err := f.store.ListDeployments(ctx, owner)
	if err != nil || len(items) != 1 || items[0].ContextWindow != window || items[0].OutputReserve != reserve || items[0].PolicyWindow != 32000 || !items[0].IsDefault || !items[0].Enabled || items[0].Name != "Renamed" {
		t.Fatalf("deployments=%+v error=%v", items, err)
	}
	_, runtimeDeployment, err := f.store.Client(ctx, owner, deployment.ID)
	if err != nil || runtimeDeployment.PolicyWindow != 32000 || runtimeDeployment.ContextWindow != window || !runtimeDeployment.Enabled {
		t.Fatalf("runtime deployment=%+v error=%v", runtimeDeployment, err)
	}
	if err = f.store.SetDeploymentBudget(ctx, other, deployment.ID, DeploymentBudgetChange{PolicyWindow: &policy}); err == nil {
		t.Fatal("another workspace changed a model budget")
	}
	response = f.request(t, otherCookie, http.MethodPatch, "/api/v1/model-deployments/"+deployment.ID.String(), `{"policy_window":64000}`)
	if response.Code != 404 {
		t.Fatalf("foreign budget PATCH=%d", response.Code)
	}
	response = f.request(t, cookie, http.MethodPatch, "/api/v1/model-deployments/"+deployment.ID.String(), `{"output_reserve":128000}`)
	if response.Code != 400 {
		t.Fatalf("impossible budget accepted: %d", response.Code)
	}
	// Invalid create must not demote the existing default or leave a deployment.
	response = f.request(t, cookie, http.MethodPost, "/api/v1/model-deployments", fmt.Sprintf(`{"connection_id":%q,"name":"Invalid","model_id":"fixture","is_default":true,"context_window":-1}`, connection.ID))
	if response.Code != 400 {
		t.Fatalf("invalid create=%d", response.Code)
	}
	items, err = f.store.ListDeployments(ctx, owner)
	if err != nil || len(items) != 1 || !items[0].IsDefault || items[0].OutputReserve != reserve {
		t.Fatalf("failed changes altered the existing default: %+v %v", items, err)
	}
	// Explicit zero resets only that field; it does not claim a known max window.
	response = f.request(t, cookie, http.MethodPatch, "/api/v1/model-deployments/"+deployment.ID.String(), `{"context_window":0}`)
	if response.Code != 204 {
		t.Fatalf("reset=%d", response.Code)
	}
}

func TestReadinessIsAnExplicitTextCallAndSharedSecretsStayPrivate(t *testing.T) {
	f := newModelFixture(t)
	_, owner, cookie := f.identity(t, "member")
	_, other, otherCookie := f.identity(t, "member")
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["max_tokens"] != float64(32) || payload["tools"] != nil || len(payload["messages"].([]any)) != 1 {
			t.Errorf("probe was not a bounded text-only request: %#v", payload)
		}
		if r.Header.Get("Authorization") != "Bearer fixture-private-key" {
			t.Error("fixture credential did not reach its configured provider")
		}
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"private-generated-text\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":3}}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()
	ctx := context.Background()
	connection, err := f.store.CreateConnection(ctx, owner, "Fixture", "openai", provider.URL, map[string]any{"private_config": "owner-only"}, "fixture-private-key")
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := f.store.CreateDeployment(ctx, owner, connection.ID, "Fixture", "fixture", true)
	if err != nil {
		t.Fatal(err)
	}
	response := f.request(t, cookie, http.MethodPost, "/api/v1/model-connections/"+connection.ID.String()+"/test", "")
	var saved map[string]any
	_ = json.Unmarshal(response.Body.Bytes(), &saved)
	if response.Code != 200 || saved["verified"] != false || saved["status"] != "configuration_saved" || requests.Load() != 0 {
		t.Fatalf("saved configuration falsely verified provider access: %s", response.Body.String())
	}
	path := "/api/v1/model-deployments/" + deployment.ID.String() + "/test"
	response = f.request(t, otherCookie, http.MethodPost, path, "")
	if response.Code != 404 || requests.Load() != 0 {
		t.Fatal("foreign deployment was probed")
	}
	response = f.request(t, cookie, http.MethodPost, path, "")
	var checked DeploymentTestResult
	_ = json.Unmarshal(response.Body.Bytes(), &checked)
	if response.Code != 200 || !checked.Verified || !checked.OK || checked.CapabilityScope != "text_call_only" || checked.Usage == nil || checked.Usage.InputTokens != 10 || requests.Load() != 1 {
		t.Fatalf("check=%d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "private-generated-text") || strings.Contains(response.Body.String(), "fixture-private-key") || strings.Contains(response.Body.String(), "private_config") {
		t.Fatal("probe returned provider text or private connection information")
	}
	sharedConnection, err := f.store.CreateConnection(ctx, SystemWorkspaceID, "Shared", "openai", provider.URL, map[string]any{"private_config": "system-only"}, "fixture-private-key")
	if err != nil {
		t.Fatal(err)
	}
	shared, err := f.store.CreateDeployment(ctx, SystemWorkspaceID, sharedConnection.ID, "Shared", "fixture", false)
	if err != nil {
		t.Fatal(err)
	}
	response = f.request(t, otherCookie, http.MethodPost, "/api/v1/model-deployments/"+shared.ID.String()+"/test", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"verified":true`) || requests.Load() != 2 {
		t.Fatalf("shared member probe=%d %s", response.Code, response.Body.String())
	}
	response = f.request(t, otherCookie, http.MethodGet, "/api/v1/model-connections", "")
	if response.Code != 200 || strings.Contains(response.Body.String(), "system-only") || strings.Contains(response.Body.String(), sharedConnection.ID.String()) {
		t.Fatal("member obtained shared connection configuration")
	}
	response = f.request(t, otherCookie, http.MethodPatch, "/api/v1/model-deployments/"+shared.ID.String(), `{"context_window":32000}`)
	if response.Code != 404 {
		t.Fatal("member changed shared model budget")
	}
	response = f.request(t, nil, http.MethodPost, path, "")
	if response.Code != 401 || requests.Load() != 2 {
		t.Fatal("unauthenticated probe reached provider")
	}
	if _, err = f.db.Exec(ctx, `UPDATE model_deployments SET enabled=false WHERE id=$1 AND workspace_id=$2`, shared.ID, SystemWorkspaceID); err != nil {
		t.Fatal(err)
	}
	response = f.request(t, otherCookie, http.MethodPost, "/api/v1/model-deployments/"+shared.ID.String()+"/test", "")
	if response.Code != 404 || requests.Load() != 2 {
		t.Fatal("disabled shared model was probed")
	}
	_ = other // Workspace exists to prove isolation independently of shared access.
}

func TestReadinessReportsSafeProviderFailureAndCancellation(t *testing.T) {
	f := newModelFixture(t)
	_, workspace, cookie := f.identity(t, "member")
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-private-key" {
			t.Error("expected fixture authentication")
		}
		w.WriteHeader(401)
		_, _ = fmt.Fprint(w, `{"error":{"message":"fixture-private-key private-generated-text"}}`)
	}))
	defer provider.Close()
	connection, err := f.store.CreateConnection(context.Background(), workspace, "Fixture", "openai", provider.URL, nil, "fixture-private-key")
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := f.store.CreateDeployment(context.Background(), workspace, connection.ID, "Fixture", "fixture", true)
	if err != nil {
		t.Fatal(err)
	}
	response := f.request(t, cookie, http.MethodPost, "/api/v1/model-deployments/"+deployment.ID.String()+"/test", "")
	var result DeploymentTestResult
	_ = json.Unmarshal(response.Body.Bytes(), &result)
	if response.Code != 200 || result.Verified || result.OK || result.ErrorKind != modelruntime.ErrorAuthentication || strings.Contains(response.Body.String(), "fixture-private-key") || strings.Contains(response.Body.String(), "private-generated-text") {
		t.Fatalf("failure=%d %s", response.Code, response.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, _ = f.store.TestDeployment(ctx, workspace, deployment.ID)
	if result.Verified || result.OK {
		t.Fatal("cancelled probe reported verified")
	}
}
