package personal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestMemoryCollectionAvailabilityAndUndoReceipts(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	collection, err := f.s.MemoryCollection(ctx, f.p)
	if err != nil || len(collection.Memories) != 0 || !collection.AutoMemory || collection.UserID != f.p.UserID || collection.WorkspaceID != f.p.WorkspaceID {
		t.Fatal("model-independent empty collection failed", collection, err)
	}
	var count int
	if err = f.s.DB.QueryRow(ctx, `SELECT count(*) FROM personal_threads`).Scan(&count); err != nil || count != 0 {
		t.Fatal("opening the manager created dialogue state", count, err)
	}
	created, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Design", Content: "Quiet interfaces", Kind: "preference"})
	if err != nil {
		t.Fatal(err)
	}
	paused, err := f.s.SetMemoryPaused(ctx, f.p, created.Memory.ID, 1, true)
	if err != nil || !paused.Memory.Paused || paused.Memory.Revision != 2 || paused.Memory.Content != created.Memory.Content || paused.ChangeID == uuid.Nil {
		t.Fatal("pause changed the fact or lost its receipt", paused, err)
	}
	// A lost response is reconciled by reading. Repeating the original payload
	// with its old revision is a conflict, never an additional accepted change.
	current, err := f.s.Memory(ctx, f.p, created.Memory.ID)
	if err != nil || !current.Paused || current.Revision != 2 {
		t.Fatal("read-only pause reconciliation failed", current, err)
	}
	if _, err = f.s.SetMemoryPaused(ctx, f.p, current.ID, 1, true); !errors.Is(err, ErrConflict) {
		t.Fatal("stale pause was replayed", err)
	}
	if _, err = f.s.SetMemoryPaused(ctx, f.p, current.ID, 0, false); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid availability revision accepted", err)
	}
	if err = f.s.SetAutoMemory(ctx, f.p, false); err != nil {
		t.Fatal(err)
	}
	collection, err = f.s.MemoryCollection(ctx, f.p)
	if err != nil || collection.AutoMemory || len(collection.Memories) != 1 || !collection.Memories[0].Paused {
		t.Fatal("paused fact disappeared from its manager", collection, err)
	}
	edited, err := f.s.UpdateMemory(ctx, f.p, current.ID, 2, "Warm interfaces", false, nil)
	if err != nil || !edited.Memory.Paused || edited.Memory.Revision != 3 {
		t.Fatal("manual editing silently resumed a fact", edited, err)
	}
	resumed, err := f.s.SetMemoryPaused(ctx, f.p, current.ID, 3, false)
	if err != nil || resumed.Memory.Paused || resumed.Memory.Revision != 4 {
		t.Fatal("resume failed", resumed, err)
	}
	for range 2 {
		if err = f.s.Undo(ctx, f.p, resumed.ChangeID); err != nil {
			t.Fatal("availability undo lost idempotency", err)
		}
	}
	history, err := f.s.MemoryHistory(ctx, f.p, current.ID, 0)
	if err != nil || len(history.Changes) != 5 || history.HasMore || history.Memory.Revision != 5 || !history.Memory.Paused {
		t.Fatal("availability history is incomplete", history, err)
	}
	undo, original := history.Changes[0], history.Changes[1]
	if undo.UndoOf == nil || *undo.UndoOf != resumed.ChangeID || undo.Before == nil || undo.Before.Paused || !undo.After.Paused || undo.Revision != 5 || !original.Undone || original.ID != resumed.ChangeID {
		t.Fatal("undo result was not journaled", undo, original)
	}
	if history.Changes[4].Before != nil || history.Changes[4].After.Paused {
		t.Fatal("creation history fabricated a prior fact", history.Changes[4])
	}
	if err = f.s.Undo(ctx, f.p, undo.ID); err != nil {
		t.Fatal("latest undo could not itself be undone", err)
	}
	if _, err = f.s.SetMemoryPaused(ctx, f.p, current.ID, 5, true); !errors.Is(err, ErrConflict) {
		t.Fatal("undo did not advance the revision", err)
	}
}

func TestMemoryHistoryPagesTombstonesAndHistoricalGaps(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Release", Content: "Original", Kind: "project", ProjectID: &f.project})
	if err != nil {
		t.Fatal(err)
	}
	for rev := int64(1); rev < 35; rev++ {
		if _, err = f.s.UpdateMemory(ctx, f.p, created.Memory.ID, rev, fmt.Sprintf("Revision %d", rev+1), false, nil); err != nil {
			t.Fatal(err)
		}
	}
	forgotten, err := f.s.UpdateMemory(ctx, f.p, created.Memory.ID, 35, "", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a journal produced before 038: old snapshots lack paused, and
	// an old undo revision has no corresponding saved change.
	if _, err = f.s.DB.Exec(ctx, `UPDATE personal_memory_changes SET before_value=before_value-'paused',after_value=after_value-'paused' WHERE memory_id=$1`, created.Memory.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.DB.Exec(ctx, `DELETE FROM personal_memory_changes WHERE memory_id=$1 AND revision=10`, created.Memory.ID); err != nil {
		t.Fatal(err)
	}
	first, err := f.s.MemoryHistory(ctx, f.p, created.Memory.ID, 0)
	if err != nil || !first.HasMore || len(first.Changes) != 30 || !first.Memory.Deleted || first.Changes[0].Revision != 36 {
		t.Fatal("first history page lost tombstone or bound", first, err)
	}
	second, err := f.s.MemoryHistory(ctx, f.p, created.Memory.ID, first.Changes[29].Revision)
	if err != nil || second.HasMore || len(second.Changes) != 5 || second.Changes[4].Revision != 1 {
		t.Fatal("history cursor skipped or overlapped changes", second, err)
	}
	all := append(first.Changes, second.Changes...)
	previous := int64(37)
	for _, change := range all {
		if change.Revision >= previous || change.Revision == 10 || change.After.Paused || change.MemoryID != created.Memory.ID {
			t.Fatal("history fabricated or reordered old revisions", change)
		}
		previous = change.Revision
	}
	if collection, err := f.s.MemoryCollection(ctx, f.p); err != nil || len(collection.Memories) != 0 {
		t.Fatal("forgotten fact remained in active collection", collection, err)
	}
	if _, err = f.s.SetMemoryPaused(ctx, f.p, created.Memory.ID, 36, true); !errors.Is(err, ErrConflict) {
		t.Fatal("availability revived a tombstone", err)
	}
	if err = f.s.Undo(ctx, f.p, forgotten.ChangeID); err != nil {
		t.Fatal(err)
	}
	latest, err := f.s.MemoryHistory(ctx, f.p, created.Memory.ID, 0)
	if err != nil || latest.Memory.Deleted || latest.Changes[0].Revision != 37 || latest.Changes[0].Before == nil || !latest.Changes[0].Before.Deleted || latest.Changes[0].UndoOf == nil || *latest.Changes[0].UndoOf != forgotten.ChangeID {
		t.Fatal("forget undo lost its result snapshot", latest, err)
	}
}

func TestMemoryUndoJournalPreservesScopeAndSource(t *testing.T) {
	for _, field := range []string{"project", "source"} {
		t.Run(field, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			var latest Memory
			var changeID uuid.UUID
			if field == "project" {
				otherProject := uuid.New()
				if _, err := f.s.DB.Exec(ctx, `INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'Other project')`, otherProject, f.p.WorkspaceID); err != nil {
					t.Fatal(err)
				}
				created, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Release", Content: "Friday", Kind: "project", ProjectID: &f.project})
				if err != nil {
					t.Fatal(err)
				}
				moved, err := f.s.UpdateMemory(ctx, f.p, created.Memory.ID, 1, created.Memory.Content, false, &MemoryDetails{Topic: created.Memory.Topic, Kind: "project", ProjectID: &otherProject})
				if err != nil {
					t.Fatal(err)
				}
				latest, changeID = moved.Memory, moved.ChangeID
			} else {
				for _, content := range []string{"I prefer blue", "I prefer green"} {
					req := request(content)
					f.client(t, func(model.ModelRequest) modelReply {
						v := say("Saved")
						v.Changes = []memoryAction{{Operation: "remember", Scope: "personal", Kind: "preference", Topic: "Color", Content: req.Content, Evidence: req.Content}}
						return v
					})
					turn, err := f.s.Chat(ctx, f.p, req)
					if err != nil || len(turn.Changes) != 1 {
						t.Fatal("source update failed", turn, err)
					}
					latest, err = f.s.Memory(ctx, f.p, turn.Changes[0].MemoryID)
					if err != nil {
						t.Fatal(err)
					}
					changeID = turn.Changes[0].ID
				}
			}
			if err := f.s.Undo(ctx, f.p, changeID); err != nil {
				t.Fatal(err)
			}
			history, err := f.s.MemoryHistory(ctx, f.p, latest.ID, 0)
			if err != nil || len(history.Changes) == 0 || history.Changes[0].Before == nil {
				t.Fatal("undo journal missing", history, err)
			}
			matchesLatest := func(m Memory) bool {
				return m.Content == latest.Content && m.Evidence == latest.Evidence &&
					(m.ProjectID == nil && latest.ProjectID == nil || m.ProjectID != nil && latest.ProjectID != nil && *m.ProjectID == *latest.ProjectID) &&
					(m.SourceTurnID == nil && latest.SourceTurnID == nil || m.SourceTurnID != nil && latest.SourceTurnID != nil && *m.SourceTurnID == *latest.SourceTurnID)
			}
			undo := history.Changes[0]
			if !matchesLatest(*undo.Before) {
				t.Error("undo rewrote the saved before scope or source", *undo.Before, latest)
			}
			if err = f.s.Undo(ctx, f.p, undo.ID); err != nil {
				t.Fatal(err)
			}
			restored, err := f.s.Memory(ctx, f.p, latest.ID)
			if err != nil || !matchesLatest(restored) {
				t.Fatal("undoing the undo lost the original scope or source", restored, latest, err)
			}
		})
	}
}

func TestMemoryManagementActorAndWorkspaceIsolation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Private", Content: "Only my fact", Kind: "work"})
	if err != nil {
		t.Fatal(err)
	}
	other := auth.Principal{UserID: uuid.New(), WorkspaceID: f.p.WorkspaceID, SessionHash: []byte("management-other")}
	if _, err = f.s.DB.Exec(ctx, `INSERT INTO users(id,email,display_name,password_hash) VALUES($1,'management-other@example.test','Other','unused')`, other.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.DB.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2)`, other.WorkspaceID, other.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.DB.Exec(ctx, `INSERT INTO sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 day')`, other.UserID, other.SessionHash); err != nil {
		t.Fatal(err)
	}
	foreign := f.p
	foreign.WorkspaceID = uuid.New()
	if _, err = f.s.DB.Exec(ctx, `INSERT INTO workspaces(id,name) VALUES($1,'Other workspace')`, foreign.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.DB.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2)`, foreign.WorkspaceID, foreign.UserID); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []auth.Principal{other, foreign} {
		if collection, err := f.s.MemoryCollection(ctx, actor); err != nil || len(collection.Memories) != 0 {
			t.Fatal("collection crossed actor/workspace scope", collection, err)
		}
		if _, err = f.s.MemoryHistory(ctx, actor, created.Memory.ID, 0); !errors.Is(err, ErrNotFound) {
			t.Fatal("history crossed actor/workspace scope", err)
		}
		if _, err = f.s.SetMemoryPaused(ctx, actor, created.Memory.ID, 1, true); !errors.Is(err, ErrNotFound) {
			t.Fatal("availability crossed actor/workspace scope", err)
		}
	}
	if _, err = f.s.DB.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, f.p.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.MemoryCollection(ctx, f.p); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("revoked actor opened collection", err)
	}
	if _, err = f.s.MemoryHistory(ctx, f.p, created.Memory.ID, 0); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("revoked actor opened history", err)
	}
	if _, err = f.s.SetMemoryPaused(ctx, f.p, created.Memory.ID, 1, true); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("revoked actor paused memory", err)
	}
}

func TestPausedMemoryNeverEntersModelRecallOrDelegation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Hidden preference", Content: "Keep this paused fact private", Kind: "preference"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.SetMemoryPaused(ctx, f.p, created.Memory.ID, 1, true); err != nil {
		t.Fatal(err)
	}
	active, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Language", Content: "Use French", Kind: "preference"})
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"reply", "lookup", "old-id", "old-topic", "automatic-old-topic", "delegate"} {
		t.Run(scenario, func(t *testing.T) {
			req := request("Continue the requested task")
			f.client(t, func(r model.ModelRequest) modelReply {
				for _, hidden := range []string{created.Memory.ID.String(), created.Memory.Topic, created.Memory.Content} {
					if strings.Contains(r.System+serialized(r.Messages), hidden) {
						t.Fatal("paused fact reached the model", hidden)
					}
				}
				v := say("Understood")
				switch scenario {
				case "lookup":
					v.MemoryLookup = &memoryLookup{IDs: []string{created.Memory.ID.String()}}
				case "old-id", "old-topic", "automatic-old-topic":
					a := memoryAction{Operation: "remember", Scope: "personal", Kind: "preference", Topic: created.Memory.Topic, Content: "Unexpected replacement", Evidence: req.Content, Automatic: scenario == "automatic-old-topic"}
					if scenario == "old-id" {
						a.ID = created.Memory.ID.String()
					}
					v.Changes = []memoryAction{a}
				case "delegate":
					v.Task = taskAction{Action: "create", ProjectID: f.project.String(), Title: "Task", Instructions: req.Content, Evidence: req.Content}
				}
				return v
			})
			turn, err := f.s.Chat(ctx, f.p, req)
			if scenario == "reply" || scenario == "delegate" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrModel) {
				t.Fatal("model bypassed paused availability", scenario, err)
			}
			if scenario == "delegate" {
				var prompt string
				if err = f.s.DB.QueryRow(ctx, `SELECT prompt FROM personal_delegations WHERE turn_id=$1`, turn.ID).Scan(&prompt); err != nil || strings.Contains(prompt, created.Memory.Content) || !strings.Contains(prompt, active.Memory.Content) {
					t.Fatal("delegation snapshot crossed paused availability", prompt, err)
				}
			}
		})
	}
	current, err := f.s.Memory(ctx, f.p, created.Memory.ID)
	if err != nil || current.Revision != 2 || !current.Paused || current.Content != created.Memory.Content {
		t.Fatal("model changed or resumed a paused fact", current, err)
	}
	if _, err = f.s.DB.Exec(ctx, `INSERT INTO personal_memories(workspace_id,user_id,kind,topic,content,paused) SELECT $1,$2,'work','Reserved '||i,'Fact',true FROM generate_series(1,126)i`, f.p.WorkspaceID, f.p.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Overflow", Content: "Fact", Kind: "work"}); !errors.Is(err, ErrMemoryFull) {
		t.Fatal("paused facts silently released capacity", err)
	}
}

func TestPauseInvalidatesPendingModelOutputAndPreservesSource(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	req := request("I prefer quiet interfaces")
	f.client(t, func(model.ModelRequest) modelReply {
		v := say("Saved")
		v.Changes = []memoryAction{{Operation: "remember", Scope: "personal", Kind: "preference", Topic: "Design", Content: req.Content, Evidence: req.Content, Automatic: true}}
		return v
	})
	if _, err := f.s.Chat(ctx, f.p, req); err != nil {
		t.Fatal(err)
	}
	memories, err := f.s.Memories(ctx, f.p)
	if err != nil || len(memories) != 1 {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	f.client(t, func(model.ModelRequest) modelReply { close(entered); <-release; return say("Old response") })
	done := make(chan error, 1)
	go func() { _, err := f.s.Chat(ctx, f.p, request("Hello")); done <- err }()
	<-entered
	paused, err := f.s.SetMemoryPaused(ctx, f.p, memories[0].ID, 1, true)
	close(release)
	if err != nil || paused.Memory.Evidence != req.Content || paused.Memory.SourceTurnID == nil || *paused.Memory.SourceTurnID != req.ID || !paused.Memory.Automatic {
		t.Fatal("availability rewrote source attribution", paused, err)
	}
	if err = <-done; !errors.Is(err, ErrConflict) {
		t.Fatal("stale model response survived pause", err)
	}
}

func TestMemoryManagementHTTPStrictInputsAndReadOnlyPages(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	raw := bytes.Repeat([]byte{29}, 32)
	digest := sha256.Sum256(raw)
	if _, err := f.s.DB.Exec(ctx, `INSERT INTO auth_access_tokens(token_hash,session_id,expires_at) SELECT $1,id,now()+interval '1 hour' FROM sessions WHERE token_hash=$2`, digest[:], f.p.SessionHash); err != nil {
		t.Fatal(err)
	}
	created, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Source", Content: "Fact", Kind: "work"})
	if err != nil {
		t.Fatal(err)
	}
	h := Handler{Service: f.s}
	router := chi.NewRouter()
	router.Use(auth.New(f.s.DB, nil, time.Hour, false).Middleware)
	router.Get("/memories", h.Memories)
	router.Get("/memories/{id}/history", h.MemoryHistory)
	router.Patch("/memories/{id}/availability", h.MemoryAvailability)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "lester_access_token", Value: base64.RawURLEncoding.EncodeToString(raw)})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	path := "/memories/" + created.Memory.ID.String()
	for _, body := range []string{`{}`, `{"revision":1}`, `{"revision":1,"paused":null}`, `{"revision":1,"paused":"true"}`, `{"revision":0,"paused":true}`, `{"revision":1,"paused":true,"user_id":"x"}`, `{"revision":1,"paused":true} {}`} {
		if w := call(http.MethodPatch, path+"/availability", body); w.Code != http.StatusBadRequest {
			t.Fatal("invalid availability payload accepted", body, w.Code)
		}
	}
	if w := call(http.MethodPatch, path+"/availability", `{"revision":1,"paused":true}`); w.Code != http.StatusOK {
		t.Fatal("HTTP pause failed", w.Code, w.Body.String())
	}
	for _, suffix := range []string{"?before_revision=0", "?before_revision=-1", "?before_revision=", "?before_revision=x", "?before_revision=1&before_revision=2"} {
		if w := call(http.MethodGet, path+"/history"+suffix, ""); w.Code != http.StatusBadRequest {
			t.Fatal("invalid history cursor accepted", suffix, w.Code)
		}
	}
	for _, endpoint := range []string{"/memories", path + "/history", path + "/history?before_revision=2"} {
		w := call(http.MethodGet, endpoint, "")
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("HTTP manager read failed or was cacheable", endpoint, w.Code, w.Body.String())
		}
		if endpoint == "/memories" {
			var collection MemoryCollection
			if err := json.Unmarshal(w.Body.Bytes(), &collection); err != nil || collection.UserID != f.p.UserID || collection.WorkspaceID != f.p.WorkspaceID {
				t.Fatal("collection identity did not come from authenticated actor", collection, err)
			}
		}
	}
	history, err := f.s.MemoryHistory(ctx, f.p, created.Memory.ID, 0)
	if err != nil || len(history.Changes) != 2 || history.Memory.Revision != 2 {
		t.Fatal("read-only manager recovery repeated a mutation", history, err)
	}
}

func TestMemoryManagementRollbackPreservesFactsAndRequiresExplicitResume(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Keep", Content: "Preserve me", Kind: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.SetMemoryPaused(ctx, f.p, created.Memory.ID, 1, true); err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/000038_memory_management.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.DB.Exec(ctx, string(down)); err == nil {
		t.Fatal("rollback silently resumed a paused fact")
	}
	if _, err = f.s.SetMemoryPaused(ctx, f.p, created.Memory.ID, 2, false); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.DB.Exec(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	var content string
	var count int
	if err = f.s.DB.QueryRow(ctx, `SELECT content FROM personal_memories WHERE id=$1`, created.Memory.ID).Scan(&content); err != nil || content != "Preserve me" {
		t.Fatal("rollback deleted authored content", content, err)
	}
	if err = f.s.DB.QueryRow(ctx, `SELECT count(*) FROM personal_memory_changes WHERE memory_id=$1`, created.Memory.ID).Scan(&count); err != nil || count != 3 {
		t.Fatal("rollback deleted saved history", count, err)
	}
}
