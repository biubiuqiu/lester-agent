package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/conversation"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/model/integration"
	"github.com/biubiuqiu/lester-agent/backend/internal/secret"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAdministration(t *testing.T) {
	url := os.Getenv("LESTER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set LESTER_TEST_DATABASE_URL for administration integration test")
	}
	ctx := context.Background()
	root, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	schema := "admin_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = root.Exec(ctx, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer root.Exec(ctx, `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` CASCADE`)
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	files, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		b, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(ctx, string(b)); e != nil {
			t.Fatalf("%s: %v", file, e)
		}
	}
	service := auth.New(db, nil, time.Hour, false)
	admin, err := service.CreateManagedUser(ctx, auth.UserInput{Email: "admin@example.test", DisplayName: "Admin", Role: "admin", Password: "test-password-123"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := service.CreateManagedUser(ctx, auth.UserInput{Email: "member@example.test", DisplayName: "Member", Role: "member", Password: "test-password-123"})
	if err != nil {
		t.Fatal(err)
	}
	var memberWorkspace uuid.UUID
	var projects int
	if err = db.QueryRow(ctx, `SELECT workspace_id FROM workspace_members WHERE user_id=$1`, member.ID).Scan(&memberWorkspace); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `SELECT count(*) FROM projects WHERE workspace_id=$1 AND is_default`, memberWorkspace).Scan(&projects); err != nil || projects != 1 {
		t.Fatalf("default project: %d %v", projects, err)
	}
	access := func(id uuid.UUID, adminOnly bool) int {
		raw := []byte(uuid.NewString())
		sum := sha256.Sum256(raw)
		if _, err = db.Exec(ctx, `INSERT INTO sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 hour')`, id, sum[:]); err != nil {
			t.Fatal(err)
		}
		handler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
		if adminOnly {
			handler = auth.RequireAdmin(handler)
		}
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{Name: "lester_session", Value: base64.RawURLEncoding.EncodeToString(raw)})
		result := httptest.NewRecorder()
		service.Middleware(handler).ServeHTTP(result, req)
		return result.Code
	}
	if got := access(member.ID, true); got != 403 {
		t.Fatalf("member admin access = %d", got)
	}
	if got := access(admin.ID, true); got != 204 {
		t.Fatalf("admin access = %d", got)
	}
	if err = service.ChangeManagedUser(ctx, admin.ID, admin.ID, auth.UserChange{DisplayName: "Admin", Role: "member"}); err == nil {
		t.Fatal("self-demotion allowed")
	}
	if err = service.ChangeManagedUser(ctx, admin.ID, member.ID, auth.UserChange{DisplayName: "Member", Role: "member", Disabled: true}); err != nil {
		t.Fatal(err)
	}
	var sessions int
	db.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, member.ID).Scan(&sessions)
	if sessions != 0 {
		t.Fatal("disabled member retained sessions")
	}
	if got := access(member.ID, false); got != 401 {
		t.Fatalf("disabled member access = %d", got)
	}
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"Email":"member@example.test","Password":"test-password-123"}`))
	response := httptest.NewRecorder()
	service.Login(response, req)
	if response.Code != 401 {
		t.Fatalf("disabled login = %d", response.Code)
	}
	if err = service.ChangeManagedUser(ctx, admin.ID, member.ID, auth.UserChange{DisplayName: "Member", Role: "member", Password: "new-password-123"}); err != nil {
		t.Fatal(err)
	}
	secrets, err := secret.New(db, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	models := model.NewStore(db, secrets, integration.NewDefaultRegistry())
	shared, err := models.CreateConnection(ctx, model.SystemWorkspaceID, "Shared", "openai", "", nil, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := models.CreateDeployment(ctx, model.SystemWorkspaceID, shared.ID, "Shared model", "test-model", true)
	if err != nil {
		t.Fatal(err)
	}
	conversations := conversation.New(db, nil, models, nil, nil)
	chat, err := conversations.Create(ctx, memberWorkspace, member.ID, "lester", "Shared default", uuid.Nil)
	if err != nil || chat.ModelDeploymentID != deployment.ID {
		t.Fatalf("conversation system default: %v %v", chat.ModelDeploymentID, err)
	}
	if err = conversations.UpdateModel(ctx, memberWorkspace, chat.ID, deployment.ID); err != nil {
		t.Fatalf("select shared model: %v", err)
	}
	list, err := models.ListDeployments(ctx, memberWorkspace)
	if err != nil || len(list) != 1 || !list[0].Shared {
		t.Fatalf("shared visibility: %#v %v", list, err)
	}
	if _, _, err = models.Client(ctx, memberWorkspace, deployment.ID); err != nil {
		t.Fatalf("shared credential scope: %v", err)
	}
	connections, err := models.ListConnections(ctx, memberWorkspace)
	if err != nil || len(connections) != 0 {
		t.Fatal("shared connection exposed to member")
	}
	private, err := models.CreateConnection(ctx, memberWorkspace, "Private", "openai", "", nil, "private-secret")
	if err != nil {
		t.Fatal(err)
	}
	privateModel, err := models.CreateDeployment(ctx, memberWorkspace, private.ID, "Private model", "private", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = models.Client(ctx, uuid.New(), privateModel.ID); err == nil {
		t.Fatal("cross-workspace model access allowed")
	}
	if _, err = models.CreateDeployment(ctx, model.SystemWorkspaceID, private.ID, "Invalid", "test", true); err == nil {
		t.Fatal("cross-scope connection allowed")
	}
	list, err = models.ListDeployments(ctx, model.SystemWorkspaceID)
	if err != nil || len(list) != 1 || !list[0].IsDefault {
		t.Fatal("failed creation cleared default")
	}
	if err = models.UpdateDeployment(ctx, model.SystemWorkspaceID, deployment.ID, model.DeploymentChange{Name: "Shared model", ModelID: "test-model", ConnectionID: shared.ID, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = models.Client(ctx, memberWorkspace, deployment.ID); err == nil {
		t.Fatal("disabled model usable")
	}
	if _, err = conversations.Create(ctx, memberWorkspace, member.ID, "lester", "Disabled model", deployment.ID); err == nil {
		t.Fatal("new conversation accepted disabled model")
	}
	if err = conversations.UpdateModel(ctx, memberWorkspace, chat.ID, deployment.ID); err == nil {
		t.Fatal("conversation accepted disabled model switch")
	}
	list, err = models.ListDeployments(ctx, memberWorkspace)
	if err != nil || len(list) != 1 || list[0].ID != privateModel.ID {
		t.Fatal("disabled model visible to member")
	}
	rollback, err := os.ReadFile("../../migrations/000007_administration.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, string(rollback)); err != nil {
		t.Fatalf("rollback: %v", err)
	}
}
