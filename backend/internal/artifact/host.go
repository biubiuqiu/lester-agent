package artifact

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/google/uuid"
)

type ObjectReader interface {
	Open(context.Context, string) (io.ReadSeekCloser, error)
}

// Host serves only published manifest members. It has no authentication routes,
// cookies, sandbox access, object listing, or management endpoints.
type Host struct {
	Catalog Catalog
	Store   ObjectReader
}

func (h *Host) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	// Opaque document origins also isolate deployments from one another even
	// when the operator uses path-based hosting on a shared dedicated hostname.
	w.Header().Set("Content-Security-Policy", "sandbox allow-scripts allow-forms; default-src 'none'; script-src 'unsafe-inline' 'unsafe-eval' http: https: blob:; style-src 'unsafe-inline' http: https:; img-src http: https: data: blob:; media-src http: https: data: blob:; font-src http: https: data:; connect-src http: https:; frame-src http: https:; object-src 'none'; base-uri 'self'; form-action http: https:")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Range")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges")
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD, OPTIONS")
		w.WriteHeader(405)
		return
	}
	if r.URL.Path == "/healthz" {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "HEAD" {
			_, _ = io.WriteString(w, `{"ok":true}`)
		}
		return
	}
	id, file, err := requestPath(r.URL.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := h.Catalog.Published(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if file == "" {
		target := &url.URL{Path: "/s/" + id.String() + "/" + p.Entry, RawQuery: r.URL.RawQuery}
		http.Redirect(w, r, target.String(), http.StatusTemporaryRedirect)
		return
	}
	if strings.HasSuffix(file, "/") {
		file += "index.html"
	}
	asset, ok := p.Manifest[file]
	if !ok {
		if _, exists := p.Manifest[file+"/index.html"]; exists {
			http.Redirect(w, r, r.URL.EscapedPath()+"/", 307)
			return
		}
		http.NotFound(w, r)
		return
	}
	content, err := h.Store.Open(r.Context(), asset.Key)
	if err != nil {
		http.Error(w, "site content temporarily unavailable", 503)
		return
	}
	defer content.Close()
	w.Header().Set("Content-Type", asset.ContentType)
	// ServeContent implements HEAD, byte ranges (including suffix/multipart),
	// Content-Length and 416 without loading media into API memory.
	http.ServeContent(noStoreWriter{w}, r, path.Base(file), p.UpdatedAt, content)
}

// ServeContent removes cache headers on range errors. Revocable publications
// must not become cacheable on any response path, including 416.
type noStoreWriter struct{ http.ResponseWriter }

func (w noStoreWriter) WriteHeader(status int) {
	w.Header().Set("Cache-Control", "no-store")
	w.ResponseWriter.WriteHeader(status)
}

func requestPath(raw string) (uuid.UUID, string, error) {
	fail := errors.New("invalid site path")
	if !strings.HasPrefix(raw, "/s/") || strings.ContainsAny(raw, "\\\x00") {
		return uuid.Nil, "", fail
	}
	parts := strings.SplitN(strings.TrimPrefix(raw, "/s/"), "/", 2)
	id, err := uuid.Parse(parts[0])
	if err != nil {
		return id, "", fail
	}
	if len(parts) == 1 {
		return id, "", nil
	}
	file := parts[1]
	for _, part := range strings.Split(file, "/") {
		if part == "." || part == ".." || strings.HasPrefix(part, ".") && part != ".agent" {
			return id, "", fail
		}
	}
	if strings.Contains(file, "//") {
		return id, "", fail
	}
	return id, file, nil
}
