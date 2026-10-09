package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
)

func guideRouter(s *Service) http.Handler {
	r := chi.NewRouter()
	r.Use(MutationOrigin(s.options.WebOrigin), s.Middleware)
	r.Get("/me/guides", s.Guides)
	r.Patch("/me/guides/{topic}", s.SaveGuide)
	return r
}

func readGuides(t *testing.T, s *Service, cookie *http.Cookie) []guideProgress {
	t.Helper()
	r := request(t, s, guideRouter(s).ServeHTTP, "GET", "/me/guides", nil, cookie, false)
	expectStatus(t, r, 200)
	var result struct {
		Guides []guideProgress `json:"guides"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Guides
}

func TestGuidesAccountIsolationAndPersistence(t *testing.T) {
	s := accountFixture(t)
	a := registerAccount(t, s, "guide-a@example.test")
	b := registerAccount(t, s, "guide-b@example.test")
	if len(readGuides(t, s, a)) != 0 || len(readGuides(t, s, b)) != 0 {
		t.Fatal("new account must have an unseen welcome guide")
	}
	router := guideRouter(s)
	expectStatus(t, request(t, s, router.ServeHTTP, "GET", "/me/guides", nil, nil, false), 401)
	for _, status := range []string{"in_progress", "skipped", "completed"} {
		expectStatus(t, request(t, s, router.ServeHTTP, "PATCH", "/me/guides/welcome", map[string]any{"step": 2, "status": status}, a, false), 200)
		got := readGuides(t, s, a)
		if len(got) != 1 || got[0].Step != 2 || got[0].Status != status {
			t.Fatalf("progress not persisted: %#v", got)
		}
		if len(readGuides(t, s, b)) != 0 {
			t.Fatal("progress leaked to another account")
		}
	}
	// Topics update independently, instead of replacing the whole account's progress.
	expectStatus(t, request(t, s, router.ServeHTTP, "PATCH", "/me/guides/models", map[string]any{"step": 1, "status": "in_progress"}, a, false), 200)
	if len(readGuides(t, s, a)) != 2 {
		t.Fatal("other topic was overwritten")
	}
	for _, item := range []struct {
		topic string
		body  map[string]any
	}{
		{"unknown", map[string]any{"step": 0, "status": "skipped"}},
		{"welcome", map[string]any{"step": -1, "status": "skipped"}},
		{"welcome", map[string]any{"step": 20, "status": "skipped"}},
		{"welcome", map[string]any{"step": 0, "status": "verified"}},
		{"welcome", map[string]any{"status": "skipped"}},
	} {
		expectStatus(t, request(t, s, router.ServeHTTP, "PATCH", "/me/guides/"+item.topic, item.body, a, false), 400)
	}
	foreign := httptest.NewRequest("PATCH", "/me/guides/welcome", nil)
	foreign.AddCookie(a)
	foreign.Header.Set("Origin", "https://foreign.example")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, foreign)
	expectStatus(t, response, 403)
}

func TestGuidesRecheckRevokedSession(t *testing.T) {
	s := accountFixture(t)
	cookie := registerAccount(t, s, "guide-session@example.test")
	router := chi.NewRouter()
	router.Use(s.Middleware)
	router.Patch("/me/guides/{topic}", func(w http.ResponseWriter, r *http.Request) {
		p, _ := FromContext(r.Context())
		if _, err := s.db.Exec(r.Context(), `DELETE FROM sessions WHERE user_id=$1`, p.UserID); err != nil {
			t.Fatal(err)
		}
		s.SaveGuide(w, r)
	})
	expectStatus(t, request(t, s, router.ServeHTTP, "PATCH", "/me/guides/welcome", map[string]any{"step": 0, "status": "skipped"}, cookie, false), 401)
	var count int
	if err := s.db.QueryRow(context.Background(), `SELECT count(*) FROM user_guides`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("revoked session changed progress: %d, %v", count, err)
	}
}

func TestGuidesMigrationGrandfathersExistingAccounts(t *testing.T) {
	s := accountFixture(t)
	existing := registerAccount(t, s, "guide-existing@example.test")
	for _, name := range []string{"000014_user_guides.down.sql", "000014_user_guides.up.sql"} {
		sql, err := os.ReadFile("../../migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.db.Exec(context.Background(), string(sql)); err != nil {
			t.Fatal(err)
		}
	}
	got := readGuides(t, s, existing)
	if len(got) != 1 || got[0].Topic != "welcome" || got[0].Status != "skipped" {
		t.Fatalf("existing account would be interrupted: %#v", got)
	}
	newAccount := registerAccount(t, s, "guide-new@example.test")
	if len(readGuides(t, s, newAccount)) != 0 {
		t.Fatal("new account cannot start onboarding")
	}
}
