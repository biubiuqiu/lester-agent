package deliverable

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func projectTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("LESTER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set LESTER_TEST_DATABASE_URL for PostgreSQL project catalogue checks")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := "project_results_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, `BEGIN; SELECT pg_advisory_xact_lock(hashtextextended('lester:test:pgcrypto',0)); CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public; COMMIT;`); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
	})
	files, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, string(raw)); err != nil {
			t.Fatalf("migration %s: %v", name, err)
		}
	}
	return db
}

func TestProjectCatalogueScopePaginationSearchAndMoves(t *testing.T) {
	db := projectTestDB(t)
	ctx := context.Background()
	s := Service{DB: db}
	user, workspace, foreign := uuid.New(), uuid.New(), uuid.New()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users(id,email,display_name,password_hash) VALUES($1,'catalogue@example.invalid','Catalogue','unused')`, []any{user}},
		{`INSERT INTO workspaces(id,name) VALUES($1,'Mine'),($2,'Foreign')`, []any{workspace, foreign}},
	} {
		if _, err := db.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	var project, otherProject, foreignProject uuid.UUID
	if err := db.QueryRow(ctx, `SELECT id FROM projects WHERE workspace_id=$1 AND is_default`, workspace).Scan(&project); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT id FROM projects WHERE workspace_id=$1 AND is_default`, foreign).Scan(&foreignProject); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `INSERT INTO projects(workspace_id,name) VALUES($1,'Other') RETURNING id`, workspace).Scan(&otherProject); err != nil {
		t.Fatal(err)
	}
	var source uuid.UUID
	for i := 0; i < 32; i++ {
		ws, pr := workspace, project
		if i == 30 {
			pr = otherProject
		} else if i == 31 {
			ws, pr = foreign, foreignProject
		}
		conversation, run := uuid.New(), uuid.New()
		if i == 0 {
			source = conversation
		}
		if _, err := db.Exec(ctx, `INSERT INTO conversations(id,workspace_id,created_by,project_id,agent_slug,title) VALUES($1,$2,$3,$4,'lester',$5)`, conversation, ws, user, pr, fmt.Sprintf("Source %02d", i)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO runs(id,conversation_id,status) VALUES($1,$2,'failed')`, run, conversation); err != nil {
			t.Fatal(err)
		}
		// Identical paths and timestamps across conversations must remain distinct.
		if _, err := db.Exec(ctx, `INSERT INTO deliverables(workspace_id,conversation_id,run_id,title,summary,entry_path,kind,content_sha256,updated_at) VALUES($1,$2,$3,$4,'','index.html','html',$5,'2026-10-17T09:00:00Z')`, ws, conversation, run, fmt.Sprintf("Report 100%% %02d", i), strings.Repeat("a", 64)); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ProjectList(ctx, workspace, project, ProjectQuery{})
	if err != nil || len(page.Deliverables) != 24 || page.NextCursor == "" {
		t.Fatalf("first page: %+v %v", page, err)
	}
	second, err := s.ProjectList(ctx, workspace, project, ProjectQuery{Cursor: page.NextCursor})
	if err != nil || len(second.Deliverables) != 6 || second.NextCursor != "" {
		t.Fatalf("second page: %+v %v", second, err)
	}
	seen := map[uuid.UUID]bool{}
	for _, item := range append(page.Deliverables, second.Deliverables...) {
		if seen[item.ID] || item.RunStatus != "failed" || item.ConversationTitle == "" {
			t.Fatalf("missing provenance/status or repeated entry: %+v", item)
		}
		seen[item.ID] = true
	}
	for _, query := range []ProjectQuery{{Search: "Source 00"}, {Search: "100% 00"}, {Search: "index.html", Kind: "html"}} {
		filtered, err := s.ProjectList(ctx, workspace, project, query)
		if err != nil || len(filtered.Deliverables) == 0 {
			t.Fatalf("literal search %+v: %+v %v", query, filtered, err)
		}
	}
	for _, query := range []ProjectQuery{{Search: "%_"}, {Kind: "markdown"}} {
		filtered, err := s.ProjectList(ctx, workspace, project, query)
		if err != nil || len(filtered.Deliverables) != 0 {
			t.Fatalf("unexpected filtered entries: %+v %v", filtered, err)
		}
	}
	if _, err = s.ProjectList(ctx, workspace, foreignProject, ProjectQuery{}); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("foreign project visible: %v", err)
	}
	for _, query := range []ProjectQuery{{Kind: "pdf"}, {Cursor: "invalid"}, {Search: strings.Repeat("中", 121)}, {Cursor: page.NextCursor, Search: "changed"}} {
		if _, err = s.ProjectList(ctx, workspace, project, query); !errors.Is(err, ErrProjectQuery) {
			t.Fatalf("invalid query accepted: %+v %v", query, err)
		}
	}
	if _, err = s.ProjectList(ctx, workspace, otherProject, ProjectQuery{Cursor: page.NextCursor}); !errors.Is(err, ErrProjectQuery) {
		t.Fatalf("cursor crossed project: %v", err)
	}
	if _, err = db.Exec(ctx, `UPDATE conversations SET project_id=$1 WHERE id=$2`, otherProject, source); err != nil {
		t.Fatal(err)
	}
	filtered, err := s.ProjectList(ctx, workspace, project, ProjectQuery{Search: "Source 00"})
	if err != nil || len(filtered.Deliverables) != 0 {
		t.Fatalf("moved source remains in old project: %+v %v", filtered, err)
	}
	filtered, err = s.ProjectList(ctx, workspace, otherProject, ProjectQuery{Search: "Source 00"})
	if err != nil || len(filtered.Deliverables) != 1 || filtered.Deliverables[0].ConversationID != source {
		t.Fatalf("moved result missing: %+v %v", filtered, err)
	}
	var empty uuid.UUID
	if err = db.QueryRow(ctx, `INSERT INTO projects(workspace_id,name) VALUES($1,'Empty') RETURNING id`, workspace).Scan(&empty); err != nil {
		t.Fatal(err)
	}
	filtered, err = s.ProjectList(ctx, workspace, empty, ProjectQuery{})
	if err != nil || filtered.Deliverables == nil || len(filtered.Deliverables) != 0 {
		t.Fatalf("empty is not a successful empty list: %+v %v", filtered, err)
	}
}
