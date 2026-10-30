package personal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestManualMemoryCreationReceiptNeverReplaysLaterChanges(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := uuid.New()
	input := ManualMemoryInput{Topic: "  Design  ", Content: "  Prefer quiet interfaces  ", Kind: "preference"}
	created, err := f.s.CreateMemory(ctx, f.p, id, input)
	if err != nil || created.Memory.ID != id || created.Memory.Topic != "Design" || created.Memory.Content != "Prefer quiet interfaces" || created.Memory.SourceTurnID != nil || created.Memory.Automatic || created.ChangeID == uuid.Nil {
		t.Fatal("manual creation failed without a configured model", created, err)
	}
	var calls, changes int
	if err = f.s.DB.QueryRow(ctx, `SELECT count(*) FROM personal_turns`).Scan(&calls); err != nil || calls != 0 {
		t.Fatal("manual creation generated a model turn", calls, err)
	}
	for range 2 {
		repeated, err := f.s.CreateMemory(ctx, f.p, id, input)
		if err != nil || repeated.ChangeID != created.ChangeID || repeated.Memory.Revision != 1 {
			t.Fatal("creation receipt was not stable", repeated, err)
		}
	}
	if err = f.s.DB.QueryRow(ctx, `SELECT count(*) FROM personal_memory_changes WHERE memory_id=$1`, id).Scan(&changes); err != nil || changes != 1 {
		t.Fatal("duplicate creation made another change", changes, err)
	}
	updated, err := f.s.UpdateMemory(ctx, f.p, id, 1, "Use warm colors", false, &MemoryDetails{Topic: "Visual direction", Kind: "project", ProjectID: &f.project})
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := f.s.CreateMemory(ctx, f.p, id, input)
	if err != nil || repeated.ChangeID != created.ChangeID || repeated.Memory.Topic != updated.Memory.Topic || repeated.Memory.Revision != 2 || repeated.Memory.ProjectID == nil || *repeated.Memory.ProjectID != f.project {
		t.Fatal("old creation replay replaced edited metadata", repeated, err)
	}
	changed := input
	changed.Content = "different creation payload"
	if _, err = f.s.CreateMemory(ctx, f.p, id, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("stable UUID accepted different payload", err)
	}
	forgotten, err := f.s.UpdateMemory(ctx, f.p, id, 2, "Must not be retained", true, nil)
	if err != nil || !forgotten.Memory.Deleted || forgotten.Memory.Content != "" || forgotten.Memory.Evidence != "" || forgotten.Memory.SourceTurnID != nil {
		t.Fatal("forget did not clear active content", forgotten, err)
	}
	current, err := f.s.Memory(ctx, f.p, id)
	if err != nil || !current.Deleted || current.Revision != 3 {
		t.Fatal("read recovery lost the tombstone", current, err)
	}
	repeated, err = f.s.CreateMemory(ctx, f.p, id, input)
	if err != nil || !repeated.Memory.Deleted || repeated.Memory.Revision != 3 {
		t.Fatal("creation replay resurrected a forgotten memory", repeated, err)
	}
	if _, err = f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "visual DIRECTION", Content: "new", Kind: "project", ProjectID: &f.project}); !errors.Is(err, ErrConflict) {
		t.Fatal("new UUID silently revived a forgotten topic", err)
	}
	if err = f.s.Undo(ctx, f.p, forgotten.ChangeID); err != nil {
		t.Fatal("forget audit cannot restore user-requested content", err)
	}
	current, err = f.s.Memory(ctx, f.p, id)
	if err != nil || current.Deleted || current.Content != "Use warm colors" {
		t.Fatal("undo did not restore the audited prior value", current, err)
	}
}

func TestManualMemoryMetadataUndoAndTopicConflictsAreAtomic(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Direction", Content: "Simple", Kind: "preference"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Occupied", Content: "Other", Kind: "project", ProjectID: &f.project})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.UpdateMemory(ctx, f.p, first.Memory.ID, 1, "Changed", false, &MemoryDetails{Topic: "occupied", Kind: "work", ProjectID: &f.project}); !errors.Is(err, ErrConflict) {
		t.Fatal("rename/move collision was not a conflict", err)
	}
	current, err := f.s.Memory(ctx, f.p, first.Memory.ID)
	if err != nil || current.Revision != 1 || current.Content != "Simple" || current.ProjectID != nil {
		t.Fatal("failed rename partially changed the memory", current, err)
	}
	moved, err := f.s.UpdateMemory(ctx, f.p, first.Memory.ID, 1, "Warm", false, &MemoryDetails{Topic: "Direction", Kind: "project", ProjectID: &f.project})
	if err != nil {
		t.Fatal(err)
	}
	blocking, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "direction", Content: "A new personal fact", Kind: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.Undo(ctx, f.p, moved.ChangeID); !errors.Is(err, ErrConflict) {
		t.Fatal("undo overwrote a later topic occupant", err)
	}
	current, err = f.s.Memory(ctx, f.p, first.Memory.ID)
	if err != nil || current.Revision != 2 || current.Kind != "project" || current.ProjectID == nil {
		t.Fatal("conflicted undo partially restored metadata", current, err)
	}
	if _, err = f.s.UpdateMemory(ctx, f.p, blocking.Memory.ID, 1, blocking.Memory.Content, false, &MemoryDetails{Topic: "Another topic", Kind: "work"}); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Undo(ctx, f.p, moved.ChangeID); err != nil {
		t.Fatal("resolved undo failed", err)
	}
	current, err = f.s.Memory(ctx, f.p, first.Memory.ID)
	if err != nil || current.Topic != "Direction" || current.Kind != "preference" || current.ProjectID != nil || current.Content != "Simple" || current.Revision != 3 {
		t.Fatal("undo did not restore the complete prior metadata", current, err)
	}
	if _, err = f.s.UpdateMemory(ctx, f.p, second.Memory.ID, 0, "bad revision", false, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid revision accepted", err)
	}
	if _, err = f.s.UpdateMemory(ctx, f.p, first.Memory.ID, 1, "stale", false, nil); !errors.Is(err, ErrConflict) {
		t.Fatal("stale metadata edit accepted", err)
	}
	if err = f.s.Undo(ctx, f.p, first.ChangeID); !errors.Is(err, ErrConflict) {
		t.Fatal("old creation undo removed newer edits", err)
	}
}

func TestManualMemoryCreationUndoIsIdempotentAndDoesNotResurrect(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := uuid.New()
	input := ManualMemoryInput{Topic: "Schedule", Content: "Prefer mornings", Kind: "work"}
	created, err := f.s.CreateMemory(ctx, f.p, id, input)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = f.s.Undo(ctx, f.p, created.ChangeID); err != nil {
			t.Fatal("creation undo was not idempotent", err)
		}
	}
	repeated, err := f.s.CreateMemory(ctx, f.p, id, input)
	if err != nil || !repeated.Memory.Deleted || repeated.Memory.Content != "" || repeated.Memory.Revision != 2 {
		t.Fatal("replayed creation restored undone memory", repeated, err)
	}
}

func TestManualMemoryActorAndProjectBoundaries(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Private", Content: "My fact", Kind: "work"})
	if err != nil {
		t.Fatal(err)
	}
	other := auth.Principal{UserID: uuid.New(), WorkspaceID: f.p.WorkspaceID, SessionHash: []byte("other-memory-session")}
	if _, err = f.s.DB.Exec(ctx, `INSERT INTO users(id,email,display_name,password_hash) VALUES($1,'other-memory@example.test','Other','unused')`, other.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.DB.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2)`, other.WorkspaceID, other.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.DB.Exec(ctx, `INSERT INTO sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 day')`, other.UserID, other.SessionHash); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Memory(ctx, other, created.Memory.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("another workspace member read private memory", err)
	}
	if _, err = f.s.UpdateMemory(ctx, other, created.Memory.ID, 1, "stolen", false, nil); !errors.Is(err, ErrNotFound) {
		t.Fatal("another member edited private memory", err)
	}
	if err = f.s.Undo(ctx, other, created.ChangeID); !errors.Is(err, ErrNotFound) {
		t.Fatal("another member undid private memory", err)
	}
	if _, err = f.s.CreateMemory(ctx, other, created.Memory.ID, ManualMemoryInput{Topic: "Private", Content: "My fact", Kind: "work"}); !errors.Is(err, ErrConflict) {
		t.Fatal("another member claimed existing UUID", err)
	}
	foreignWorkspace := uuid.New()
	var foreignProject uuid.UUID
	if _, err = f.s.DB.Exec(ctx, `INSERT INTO workspaces(id,name) VALUES($1,'Foreign')`, foreignWorkspace); err != nil {
		t.Fatal(err)
	}
	if err = f.s.DB.QueryRow(ctx, `SELECT id FROM projects WHERE workspace_id=$1 AND is_default`, foreignWorkspace).Scan(&foreignProject); err != nil {
		t.Fatal(err)
	}
	for _, project := range []*uuid.UUID{&foreignProject, &uuid.Nil} {
		want := ErrNotFound
		if *project == uuid.Nil {
			want = ErrInvalid
		}
		if _, err = f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Wrong project", Content: "fact", Kind: "project", ProjectID: project}); !errors.Is(err, want) {
			t.Fatal("invalid create project accepted", err)
		}
		if _, err = f.s.UpdateMemory(ctx, f.p, created.Memory.ID, 1, "fact", false, &MemoryDetails{Topic: "Wrong project", Kind: "project", ProjectID: project}); !errors.Is(err, want) {
			t.Fatal("invalid edit project accepted", err)
		}
	}
	if _, err = f.s.DB.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, f.p.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Revoked", Content: "fact", Kind: "work"}); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("revoked actor created memory", err)
	}
	if _, err = f.s.UpdateMemory(ctx, f.p, created.Memory.ID, 1, "fact", false, nil); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("revoked actor edited memory", err)
	}
	if _, err = f.s.Memory(ctx, f.p, created.Memory.ID); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("revoked actor read recovery record", err)
	}
}

func TestManualMemoryCapacityAndUndoCannotExceedLimit(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "First", Content: "Fact", Kind: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.DB.Exec(ctx, `INSERT INTO personal_memories(workspace_id,user_id,kind,topic,content) SELECT $1,$2,'work','Topic '||i,'Fact '||i FROM generate_series(1,127)i`, f.p.WorkspaceID, f.p.UserID); err != nil {
		t.Fatal(err)
	}
	input := ManualMemoryInput{Topic: "Next", Content: "Fact", Kind: "work"}
	if _, err = f.s.CreateMemory(ctx, f.p, uuid.New(), input); !errors.Is(err, ErrMemoryFull) {
		t.Fatal("manual creation exceeded memory capacity", err)
	}
	forgotten, err := f.s.UpdateMemory(ctx, f.p, created.Memory.ID, 1, "", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.CreateMemory(ctx, f.p, uuid.New(), input); err != nil {
		t.Fatal("forget did not release capacity", err)
	}
	if err = f.s.Undo(ctx, f.p, forgotten.ChangeID); !errors.Is(err, ErrMemoryFull) {
		t.Fatal("undo exceeded the memory capacity", err)
	}
}

func TestManualMemoryConcurrentCreationHasOneOriginalReceipt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := uuid.New()
	input := ManualMemoryInput{Topic: "One topic", Content: "One fact", Kind: "preference"}
	var start sync.WaitGroup
	start.Add(1)
	type outcome struct {
		receipt MemoryReceipt
		err     error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			start.Wait()
			receipt, err := f.s.CreateMemory(ctx, f.p, id, input)
			results <- outcome{receipt, err}
		}()
	}
	start.Done()
	a, b := <-results, <-results
	if a.err != nil || b.err != nil || a.receipt.ChangeID != b.receipt.ChangeID {
		t.Fatal("concurrent duplicate creation disagreed", a, b)
	}
	input.Topic = "Another topic"
	start.Add(1)
	for range 2 {
		go func() {
			start.Wait()
			receipt, err := f.s.CreateMemory(ctx, f.p, uuid.New(), input)
			results <- outcome{receipt, err}
		}()
	}
	start.Done()
	a, b = <-results, <-results
	if !(a.err == nil && errors.Is(b.err, ErrConflict) || b.err == nil && errors.Is(a.err, ErrConflict)) {
		t.Fatal("same-topic competing creation did not converge", a, b)
	}
}

func TestManualMemoryCreateAndMetadataEditsInvalidatePendingModelOutput(t *testing.T) {
	for _, mutate := range []string{"create", "edit"} {
		t.Run(mutate, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			created, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Design", Content: "Quiet", Kind: "preference"})
			if err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			f.client(t, func(model.ModelRequest) modelReply { close(entered); <-release; return say("Stale output") })
			done := make(chan error, 1)
			go func() { _, err := f.s.Chat(ctx, f.p, request("Hello")); done <- err }()
			<-entered
			if mutate == "create" {
				_, err = f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "New fact", Content: "Latest", Kind: "work"})
			} else {
				_, err = f.s.UpdateMemory(ctx, f.p, created.Memory.ID, 1, "Latest", false, &MemoryDetails{Topic: "Project design", Kind: "project", ProjectID: &f.project})
			}
			close(release)
			if err != nil {
				t.Fatal(err)
			}
			if err = <-done; !errors.Is(err, ErrConflict) {
				t.Fatal("pending model survived a manual memory barrier", err)
			}
		})
	}
}

func TestManualMemoryHTTPStrictPayloadAndReadOnlyRecovery(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	raw := bytes.Repeat([]byte{23}, 32)
	digest := sha256.Sum256(raw)
	if _, err := f.s.DB.Exec(ctx, `INSERT INTO auth_access_tokens(token_hash,session_id,expires_at) SELECT $1,id,now()+interval '1 hour' FROM sessions WHERE token_hash=$2`, digest[:], f.p.SessionHash); err != nil {
		t.Fatal(err)
	}
	h := &Handler{Service: f.s}
	router := chi.NewRouter()
	router.Use(auth.New(f.s.DB, nil, time.Hour, false).Middleware)
	router.Put("/memories/{id}", h.CreateMemory)
	router.Get("/memories/{id}", h.Memory)
	router.Patch("/memories/{id}", h.EditMemory)
	call := func(method, path, body string, authenticated bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if authenticated {
			req.AddCookie(&http.Cookie{Name: "lester_access_token", Value: base64.RawURLEncoding.EncodeToString(raw)})
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	id := uuid.New()
	path := "/memories/" + id.String()
	valid := `{"topic":"Design","content":"Quiet","kind":"preference","project_id":null}`
	if w := call(http.MethodPut, path, valid, false); w.Code != http.StatusUnauthorized {
		t.Fatal("unauthenticated manual creation accepted", w.Code)
	}
	for _, body := range []string{valid + `{}`, `{"topic":"X","content":"Fact","kind":"work","user_id":"` + f.p.UserID.String() + `"}`, `{"topic":"X","content":"Fact","kind":"project","project_id":null}`, `{"topic":" ","content":"Fact","kind":"work"}`} {
		if w := call(http.MethodPut, path, body, true); w.Code != http.StatusBadRequest {
			t.Fatal("invalid create payload accepted", body, w.Code, w.Body.String())
		}
	}
	w := call(http.MethodPut, path, valid, true)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("manual create failed", w.Code, w.Body.String())
	}
	var receipt MemoryReceipt
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil || receipt.ChangeID == uuid.Nil || receipt.Memory.ID != id {
		t.Fatal("invalid creation response", receipt, err)
	}
	body := `{"revision":1,"content":"Warm","details":{"topic":"Visual","kind":"project","project_id":"` + f.project.String() + `"}}`
	w = call(http.MethodPatch, path, body, true)
	if w.Code != http.StatusOK {
		t.Fatal("metadata patch failed", w.Code, w.Body.String())
	}
	for _, details := range []string{`{"topic":"Renamed","kind":"work"}`, `{"topic":"Renamed","kind":"work","project_id":null,"extra":true}`, `{"topic":null,"kind":"work","project_id":null}`} {
		if w = call(http.MethodPatch, path, `{"revision":2,"content":"Warm","details":`+details+`}`, true); w.Code != http.StatusBadRequest {
			t.Fatal("partial metadata silently widened project scope", details, w.Code, w.Body.String())
		}
	}
	w = call(http.MethodGet, path, "", true)
	var current Memory
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(w.Body.Bytes(), &current) != nil || current.Revision != 2 || current.Topic != "Visual" || current.ProjectID == nil || *current.ProjectID != f.project {
		t.Fatal("read-only metadata recovery failed", w.Code, w.Body.String())
	}
	if w = call(http.MethodPatch, path, `{"revision":1,"content":"stale"}`, true); w.Code != http.StatusConflict {
		t.Fatal("HTTP stale update was not a conflict", w.Code, w.Body.String())
	}
	if w = call(http.MethodPatch, path, `{"revision":2,"content":"","forget":true}`, true); w.Code != http.StatusOK {
		t.Fatal("HTTP forget failed", w.Code, w.Body.String())
	}
	if w = call(http.MethodGet, path, "", true); w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &current) != nil || !current.Deleted || current.Content != "" {
		t.Fatal("forgotten read recovery failed", w.Code, w.Body.String())
	}
	if w = call(http.MethodGet, "/memories/"+uuid.NewString(), "", true); w.Code != http.StatusNotFound {
		t.Fatal("missing memory was not distinct from empty list", w.Code)
	}
}
