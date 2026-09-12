package artifact

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type testCatalog struct {
	published bool
	id        uuid.UUID
}

func (c *testCatalog) Published(_ context.Context, id uuid.UUID) (Published, error) {
	if !c.published || id != c.id {
		return Published{}, errors.New("not found")
	}
	return Published{Entry: "index.html", UpdatedAt: time.Unix(100, 0), Manifest: map[string]Asset{"index.html": {Key: "html", ContentType: "text/html"}, "movie.mp4": {Key: "video", ContentType: "video/mp4"}}}, nil
}

type readCloser struct{ *bytes.Reader }

func (readCloser) Close() error { return nil }

type testObjects map[string][]byte

func (o testObjects) Open(_ context.Context, key string) (io.ReadSeekCloser, error) {
	v, ok := o[key]
	if !ok {
		return nil, errors.New("missing")
	}
	return readCloser{bytes.NewReader(v)}, nil
}
func TestHostRangesHeadAndRevocation(t *testing.T) {
	id := uuid.New()
	catalog := &testCatalog{true, id}
	host := &Host{Catalog: catalog, Store: testObjects{"html": []byte("<h1>Hi</h1>"), "video": []byte("0123456789")}}
	base := "/s/" + id.String() + "/"
	for _, tc := range []struct {
		method, path, rng string
		status            int
		body              string
	}{{"GET", base, "", 307, ""}, {"GET", base + "movie.mp4", "bytes=2-5", 206, "2345"}, {"GET", base + "movie.mp4", "bytes=-3", 206, "789"}, {"GET", base + "movie.mp4", "bytes=20-", 416, ""}, {"HEAD", base + "movie.mp4", "", 200, ""}, {"GET", base + "secret.env", "", 404, ""}, {"GET", base + "../index.html", "", 404, ""}, {"POST", base + "index.html", "", 405, ""}} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.rng != "" {
			req.Header.Set("Range", tc.rng)
		}
		w := httptest.NewRecorder()
		host.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
		if tc.body != "" && w.Body.String() != tc.body {
			t.Fatalf("bad range: %s", w.Body.String())
		}
		if tc.method == "HEAD" && (w.Body.Len() != 0 || w.Header().Get("Content-Length") != "10") {
			t.Fatal("invalid HEAD")
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox allow-scripts") || strings.Contains(w.Header().Get("Content-Security-Policy"), "allow-same-origin") {
			t.Fatal("missing document isolation")
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Set-Cookie") != "" {
			t.Fatal("unsafe cache/cookie policy")
		}
	}
	catalog.published = false
	w := httptest.NewRecorder()
	host.ServeHTTP(w, httptest.NewRequest(http.MethodGet, base+"movie.mp4", nil))
	if w.Code != 404 {
		t.Fatal("unpublished asset is still available")
	}
}
func TestHostingOriginMustBeSeparate(t *testing.T) {
	for _, bad := range []string{"http://localhost:13181", "javascript:foo", "https://user:pass@sites.example", "https://sites.example/path"} {
		if err := ValidateOrigin(bad, "http://localhost:13180"); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	if err := ValidateOrigin("http://127.0.0.1:13181", "http://localhost:13180"); err != nil {
		t.Fatal(err)
	}
}
