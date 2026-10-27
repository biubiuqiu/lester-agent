package connector

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGitHubSearchReportsIncompleteAndCappedResults(t *testing.T) {
	for _, tc := range []struct {
		name, cursor, next  string
		total, limit        int
		incomplete, partial bool
	}{
		{name: "provider timeout", total: 1, limit: 20, incomplete: true, partial: true},
		{name: "capped first page", total: 1001, limit: 20, next: "2", partial: true},
		{name: "capped last page", total: 1001, limit: 20, cursor: "50", partial: true},
		{name: "exactly available", total: 1000, limit: 20, cursor: "50"},
		{name: "last start below cap", total: 1001, limit: 12, cursor: "84", partial: true},
		{name: "incomplete and capped", total: 1001, limit: 20, incomplete: true, next: "2", partial: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/search/issues" {
					t.Error("unexpected search request", r.Method, r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"total_count": tc.total, "incomplete_results": tc.incomplete, "items": []any{map[string]any{"id": 1, "number": 12, "title": "Source issue", "repository_url": "https://api.github.com/repos/acme/project"}}})
			}))
			defer server.Close()
			s, err := New(nil, Options{MasterKey: []byte(strings.Repeat("k", 32)), HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.readProvider(context.Background(), Provider{Name: "github", APIURL: server.URL}, "fixture-access", ReadRequest{Action: "search", Query: "project", Cursor: tc.cursor, Limit: tc.limit})
			if err != nil {
				t.Fatal(err)
			}
			if result.Truncated != tc.partial || result.NextCursor != tc.next {
				t.Fatalf("search boundaries: %#v", result)
			}
			if tc.incomplete && !strings.Contains(result.Notice, "marked this search incomplete") {
				t.Fatal("lost provider incompleteness explanation")
			}
			if tc.total > 1000 && !strings.Contains(result.Notice, "first 1,000 matches") {
				t.Fatal("lost search-cap explanation")
			}
			if !strings.Contains(result.Notice, "not instructions or authority") {
				t.Fatal("adapter notice replaced the external-source boundary")
			}
		})
	}
}

func TestGitHubSearchRejectsCursorStartingBeyondAvailableResults(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	s, err := New(nil, Options{MasterKey: []byte(strings.Repeat("k", 32)), HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.readProvider(context.Background(), Provider{Name: "github", APIURL: server.URL}, "fixture-access", ReadRequest{Action: "search", Query: "project", Cursor: "51", Limit: 20})
	if !errors.Is(err, ErrArguments) || calls.Load() != 0 {
		t.Fatal("sent an unavailable search page to GitHub", err, calls.Load())
	}
}

func TestForbiddenRateLimitHeadersAreDistinctFromPermissionErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    error
	}{
		{name: "GitHub exhausted", headers: map[string]string{"X-RateLimit-Remaining": "0"}, want: ErrRateLimit},
		{name: "standard remaining", headers: map[string]string{"RateLimit-Remaining": "0"}, want: ErrRateLimit},
		{name: "retry seconds", headers: map[string]string{"Retry-After": "60"}, want: ErrRateLimit},
		{name: "large retry seconds", headers: map[string]string{"Retry-After": "40000000"}, want: ErrRateLimit},
		{name: "retry date", headers: map[string]string{"Retry-After": "Wed, 21 Oct 2037 07:28:00 GMT"}, want: ErrRateLimit},
		{name: "remaining requests", headers: map[string]string{"X-RateLimit-Remaining": "1"}, want: ErrPermission},
		{name: "malformed retry", headers: map[string]string{"Retry-After": "not-a-retry-time"}, want: ErrPermission},
		{name: "permission", want: ErrPermission},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for key, value := range tc.headers {
					w.Header().Set(key, value)
				}
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"provider-body-is-not-user-copy"}`))
			}))
			defer server.Close()
			s, err := New(nil, Options{MasterKey: []byte(strings.Repeat("k", 32)), HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.request(context.Background(), http.MethodGet, server.URL, "fixture-access", nil, "", nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("wrong status classification: %v", err)
			}
		})
	}
}

func TestGitHubRateLimitPreservesTheAuthorizedConnection(t *testing.T) {
	f := newFixture(t)
	f.connect(t, "github")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	p := f.s.providers["github"]
	p.APIURL = server.URL
	f.s.providers["github"] = p
	if _, err := f.s.Read(context.Background(), f.actor, ReadRequest{Provider: "github", Action: "search", Query: "project"}); !errors.Is(err, ErrRateLimit) {
		t.Fatal("limited read did not report retryable rate limiting", err)
	}
	connections, err := f.s.List(context.Background(), f.actor)
	if err != nil {
		t.Fatal(err)
	}
	for _, connection := range connections {
		if connection.Provider == "github" && connection.Status != "connected" {
			t.Fatal("rate limiting revoked a valid grant")
		}
	}
}

func TestSlackThreadTimestampUsesOnlyValidOfficialPermalinks(t *testing.T) {
	const child, parent = "1700000000.000002", "1700000000.000001"
	for _, tc := range []struct{ name, direct, link, want string }{
		{name: "ordinary root", want: child},
		{name: "direct parent", direct: parent, link: "https://example.slack.com/archives/C1234/p1700000000000002?thread_ts=1700000000.000003", want: parent},
		{name: "reply permalink", link: "https://example.slack.com/archives/C1234/p1700000000000002?thread_ts=" + parent, want: parent},
		{name: "invalid direct parent", direct: "invalid", link: "https://example.slack.com/archives/C1234/p1700000000000002?thread_ts=" + parent, want: parent},
		{name: "http", link: "http://example.slack.com/archives/C1234/p1700000000000002?thread_ts=" + parent, want: child},
		{name: "untrusted suffix", link: "https://example.slack.com.evil.test/archives/C1234/p1700000000000002?thread_ts=" + parent, want: child},
		{name: "lookalike", link: "https://evilslack.com/archives/C1234/p1700000000000002?thread_ts=" + parent, want: child},
		{name: "credentials", link: "https://user:pass@example.slack.com/archives/C1234/p1700000000000002?thread_ts=" + parent, want: child},
		{name: "nonstandard port", link: "https://example.slack.com:8443/archives/C1234/p1700000000000002?thread_ts=" + parent, want: child},
		{name: "invalid timestamp", link: "https://example.slack.com/archives/C1234/p1700000000000002?thread_ts=invalid", want: child},
		{name: "ambiguous timestamp", link: "https://example.slack.com/archives/C1234/p1700000000000002?thread_ts=" + parent + "&thread_ts=" + child, want: child},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := slackThreadTimestamp(map[string]any{"ts": child, "thread_ts": tc.direct, "permalink": tc.link}); got != tc.want {
				t.Fatalf("thread timestamp = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestSlackSearchReplyReadsItsParentThreadAndNextPage(t *testing.T) {
	f := newFixture(t)
	f.connect(t, "slack")
	const parent, child = "1700000000.000001", "1700000000.000002"
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Error("Slack adapter attempted a write")
		}
		switch r.URL.Path {
		case "/search.messages":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "messages": map[string]any{"matches": []any{map[string]any{"ts": child, "text": "Matched reply", "channel": map[string]any{"id": "C1234"}, "permalink": "https://example.slack.com/archives/C1234/p1700000000000002?thread_ts=" + parent + "&cid=C1234"}}, "paging": map[string]any{"pages": 1}}})
		case "/conversations.replies":
			reads.Add(1)
			if r.URL.Query().Get("channel") != "C1234" || r.URL.Query().Get("ts") != parent {
				t.Error("read did not use the search result's parent thread", r.URL.RawQuery)
			}
			if r.URL.Query().Get("cursor") == "" {
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "messages": []any{map[string]any{"ts": parent, "text": "Thread root"}}, "response_metadata": map[string]any{"next_cursor": "next-thread-page"}})
			} else {
				if r.URL.Query().Get("cursor") != "next-thread-page" {
					t.Error("changed thread page cursor")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "messages": []any{map[string]any{"ts": child, "text": "Matched reply"}}})
			}
		default:
			t.Error("adapter fetched a permalink or unknown path", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	p := f.s.providers["slack"]
	p.APIURL = server.URL
	f.s.providers["slack"] = p
	search, err := f.s.Read(context.Background(), f.actor, ReadRequest{Provider: "slack", Action: "search", Query: "project"})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Items) != 1 || search.Items[0].ID != child || search.Items[0].Resource != "C1234:"+parent {
		t.Fatal("reply search lost its message identity or parent thread", search)
	}
	first, err := f.s.Read(context.Background(), f.actor, ReadRequest{Provider: "slack", Action: "read", Resource: search.Items[0].Resource})
	if err != nil {
		t.Fatal(err)
	}
	if first.NextCursor != "next-thread-page" || len(first.Items) != 1 || first.Items[0].Text != "Thread root" {
		t.Fatal("first thread page", first)
	}
	second, err := f.s.Read(context.Background(), f.actor, ReadRequest{Provider: "slack", Action: "read", Resource: search.Items[0].Resource, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if second.NextCursor != "" || len(second.Items) != 1 || second.Items[0].Text != "Matched reply" || reads.Load() != 2 {
		t.Fatal("second thread page", second, reads.Load())
	}
}
