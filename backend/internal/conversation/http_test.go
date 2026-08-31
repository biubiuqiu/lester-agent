package conversation

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPreviewContentType(t *testing.T) {
	tests := map[string]string{
		"index.html":       "text/html; charset=utf-8",
		"assets/app.css":   "text/css; charset=utf-8",
		"assets/app.js":    "text/javascript; charset=utf-8",
		"data/config.json": "application/json; charset=utf-8",
		"README.md":        "text/plain; charset=utf-8",
		"image.png":        "image/png",
		"archive.unknown":  "application/octet-stream",
	}
	for filePath, expected := range tests {
		t.Run(filePath, func(t *testing.T) {
			if actual := previewContentType(filePath); actual != expected {
				t.Fatalf("previewContentType(%q) = %q, want %q", filePath, actual, expected)
			}
		})
	}
}

func TestPreviewContentSecurityPolicyAllowsOnlyCurrentAssetOrigin(t *testing.T) {
	request := httptest.NewRequest("GET", "http://localhost:18080/api/v1/conversations/id/preview/index.html", nil)
	policy := previewContentSecurityPolicy(request)
	if !strings.Contains(policy, "script-src 'unsafe-inline' http://localhost:18080") {
		t.Fatalf("policy does not allow current preview assets: %q", policy)
	}
	for _, forbidden := range []string{"connect-src http", "form-action http", "frame-src http"} {
		if strings.Contains(policy, forbidden) {
			t.Fatalf("policy unexpectedly allows %q: %q", forbidden, policy)
		}
	}
}
