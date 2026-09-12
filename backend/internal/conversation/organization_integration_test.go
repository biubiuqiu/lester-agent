package conversation

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/artifact"
	"github.com/biubiuqiu/lester-agent/backend/internal/project"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
)

func TestProjectMigrationAndOrganization(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	p := project.Service{DB: f.service.db}
	list, err := p.List(ctx, f.workspaceID)
	if err != nil || len(list) != 1 || !list[0].IsDefault || list[0].ConversationCount != 1 {
		t.Fatalf("backfill: %+v %v", list, err)
	}
	original := list[0].ID
	other := uuid.New()
	if _, err = f.service.db.Exec(ctx, `INSERT INTO workspaces(id,name) VALUES($1,'other')`, other); err != nil {
		t.Fatal(err)
	}
	foreign, err := p.List(ctx, other)
	if err != nil || len(foreign) != 1 || !foreign[0].IsDefault {
		t.Fatalf("new workspace default: %+v %v", foreign, err)
	}
	target, err := p.Create(ctx, f.workspaceID, "Demo project")
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	if err = p.Update(ctx, f.workspaceID, target.ID, nil, &yes); err != nil {
		t.Fatal(err)
	}
	if err = f.service.Organize(ctx, f.workspaceID, f.conversationID, OrganizationUpdate{ProjectID: &target.ID, Pinned: &yes}); err != nil {
		t.Fatal(err)
	}
	c, _, err := f.service.Get(ctx, f.workspaceID, f.conversationID)
	if err != nil || c.ProjectID != target.ID || !c.Pinned {
		t.Fatalf("move/pin: %+v %v", c, err)
	}
	list, err = p.List(ctx, f.workspaceID)
	if err != nil || list[0].ID != target.ID || !list[0].Pinned || list[0].ConversationCount != 1 {
		t.Fatalf("order/count: %+v %v", list, err)
	}
	if err = f.service.Organize(ctx, f.workspaceID, f.conversationID, OrganizationUpdate{ProjectID: &foreign[0].ID}); err == nil {
		t.Fatal("cross workspace move accepted")
	}
	if err = f.service.Organize(ctx, other, f.conversationID, OrganizationUpdate{Pinned: &yes}); err == nil {
		t.Fatal("cross workspace pin accepted")
	}
	if err = p.Update(ctx, other, target.ID, nil, &yes); err == nil {
		t.Fatal("cross workspace project update accepted")
	}
	var assigned uuid.UUID
	if err = f.service.db.QueryRow(ctx, `INSERT INTO conversations(workspace_id,created_by,agent_slug) VALUES($1,$2,'lester') RETURNING project_id`, f.workspaceID, f.userID).Scan(&assigned); err != nil || assigned != original {
		t.Fatalf("implicit default: %s %v", assigned, err)
	}
	if _, err = f.service.db.Exec(ctx, `INSERT INTO conversations(workspace_id,created_by,agent_slug,project_id) VALUES($1,$2,'lester',$3)`, f.workspaceID, f.userID, foreign[0].ID); err == nil {
		t.Fatal("cross workspace insert accepted")
	}
	applyTestMigration(t, f.service.db, "000006_projects_artifacts.down.sql")
	applyTestMigration(t, f.service.db, "000006_projects_artifacts.up.sql")
	list, err = p.List(ctx, f.workspaceID)
	if err != nil || len(list) != 1 || list[0].ConversationCount != 2 {
		t.Fatalf("rollback/reapply: %+v %v", list, err)
	}
}

type publicationFiles map[string]string

func (f publicationFiles) ReadFile(_ context.Context, _, _, p string) ([]byte, error) {
	v, ok := f[p]
	if !ok {
		return nil, errors.New("missing")
	}
	return []byte(v), nil
}
func (f publicationFiles) ListFiles(context.Context, string, string, string) ([]sandbox.FileEntry, error) {
	return nil, errors.New("not used")
}

type publicationStore struct {
	objects map[string]string
	fail    bool
}

func (s *publicationStore) Ensure(context.Context) error { return nil }
func (s *publicationStore) Put(_ context.Context, k string, r io.Reader, _ int64, _ string) error {
	if s.fail {
		return errors.New("simulated storage outage")
	}
	b, e := io.ReadAll(r)
	s.objects[k] = string(b)
	return e
}
func (s *publicationStore) Get(_ context.Context, k string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(s.objects[k])), nil
}
func (s *publicationStore) Delete(_ context.Context, k string) error {
	delete(s.objects, k)
	return nil
}
func TestPublicationAtomicityIsolationAndRevocation(t *testing.T) {
	f := newTranscriptFixture(t, false)
	ctx := context.Background()
	store := &publicationStore{objects: map[string]string{}}
	files := publicationFiles{"index.html": "<h1>Version one</h1>"}
	s := artifact.Service{DB: f.service.db, Store: store, Files: files, BaseURL: "https://sites.example.net", Prepare: func(context.Context, uuid.UUID, uuid.UUID) (string, string, error) {
		return "sandbox", "/workspace/conversations/" + f.conversationID.String(), nil
	}}
	in := artifact.PublishInput{Name: "Demo", SourcePath: "index.html"}
	a, err := s.Publish(ctx, f.workspaceID, f.conversationID, in)
	if err != nil {
		t.Fatal(err)
	}
	if a.FileCount != 1 || a.Status != "published" || a.ArtifactID != a.ID || !strings.HasSuffix(a.URL, a.ID.String()+"/") {
		t.Fatalf("publication: %+v", a)
	}
	pub, err := s.Published(ctx, a.ID)
	if err != nil || !strings.Contains(store.objects[pub.Manifest["index.html"].Key], "Version one") {
		t.Fatalf("read published: %v", err)
	}
	old := pub.Manifest["index.html"].Key
	files["index.html"] = "<h1>Version two</h1>"
	in.ArtifactID = a.ID
	store.fail = true
	if _, err = s.Publish(ctx, f.workspaceID, f.conversationID, in); err == nil {
		t.Fatal("upload failure ignored")
	}
	pub, err = s.Published(ctx, a.ID)
	if err != nil || pub.Manifest["index.html"].Key != old || len(store.objects) != 1 {
		t.Fatalf("failed publish changed manifest or leaked staging: %v", err)
	}
	store.fail = false
	files["index.html"] = "<img src='missing.png'>"
	if _, err = s.Publish(ctx, f.workspaceID, f.conversationID, in); err == nil {
		t.Fatal("missing asset accepted")
	}
	if len(store.objects) != 1 {
		t.Fatal("bundling failure uploaded objects")
	}
	files["index.html"] = "<h1>Version two</h1>"
	b, err := s.Publish(ctx, f.workspaceID, f.conversationID, in)
	if err != nil || b.ID != a.ID || b.Version == a.Version || b.URL != a.URL {
		t.Fatalf("redeploy: %+v %v", b, err)
	}
	other := uuid.New()
	if _, err = f.service.db.Exec(ctx, `INSERT INTO workspaces(id,name) VALUES($1,'other')`, other); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Publish(ctx, other, f.conversationID, in); err == nil {
		t.Fatal("foreign publication accepted")
	}
	if err = s.Unpublish(ctx, other, a.ID); err == nil {
		t.Fatal("foreign revocation accepted")
	}
	list, err := s.List(ctx, other)
	if err != nil || len(list) != 0 {
		t.Fatalf("foreign list leaked: %+v %v", list, err)
	}
	if err = s.Unpublish(ctx, f.workspaceID, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Published(ctx, a.ID); err == nil {
		t.Fatal("revoked publication still readable")
	}
	list, err = s.List(ctx, f.workspaceID)
	if err != nil || len(list) != 1 || list[0].Status != "unpublished" {
		t.Fatalf("management history: %+v %v", list, err)
	}
	if _, err = s.Publish(ctx, f.workspaceID, f.conversationID, in); err != nil {
		t.Fatalf("republish: %v", err)
	}
}
