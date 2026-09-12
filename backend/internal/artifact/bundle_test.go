package artifact

import (
	"context"
	"errors"
	"path"
	"strings"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
)

type memoryFiles map[string]string

func (m memoryFiles) ReadFile(_ context.Context, _, _, p string) ([]byte, error) {
	v, ok := m[p]
	if !ok {
		return nil, errors.New("missing")
	}
	return []byte(v), nil
}
func (m memoryFiles) ListFiles(_ context.Context, _, _, dir string) ([]sandbox.FileEntry, error) {
	prefix := ""
	if dir != "." {
		prefix = dir + "/"
	}
	seen := map[string]bool{}
	entries := []sandbox.FileEntry{}
	for p, v := range m {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		suffix := strings.TrimPrefix(p, prefix)
		parts := strings.Split(suffix, "/")
		name := parts[0]
		if seen[name] {
			continue
		}
		seen[name] = true
		entries = append(entries, sandbox.FileEntry{Name: name, Path: path.Join(dir, name), IsDir: len(parts) > 1, Size: int64(len(v))})
	}
	if len(entries) == 0 {
		return nil, errors.New("missing directory")
	}
	return entries, nil
}
func TestSingleHTMLWithSandboxMediaAndCSS(t *testing.T) {
	source := memoryFiles{
		"landing.html":     `<!doctype html><title>Demo</title><link rel="stylesheet" href="styles/main.css"><img src="/workspace/conversations/test/.agent/upload/photo.png"><video controls poster="pic.png"><source src="media/demo.mp4"></video><img srcset="pic.png 1x, pic2.png 2x"><script src="app.js"></script>`,
		"styles/main.css":  `@import "theme.css"; body{background:url('../pic.png')}`,
		"styles/theme.css": "body{color:green}", "pic.png": "PNG", "pic2.png": "PNG2", ".agent/upload/photo.png": "UPLOAD", "media/demo.mp4": "VIDEO",
		"app.js": `fetch("data.json");`, "data.json": `{"ok":true}`, "unrelated.html": "PRIVATE", ".env": "SECRET",
	}
	b, err := Build(context.Background(), source, "sandbox", "/workspace/conversations/test", "landing.html", "", "/s/site/")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Files) != 9 {
		t.Fatalf("files: %v", b.Files)
	}
	for _, needle := range []string{`href="/s/site/"`, `src="/s/site/.agent/upload/photo.png"`, `src="/s/site/media/demo.mp4"`, `/s/site/pic2.png 2x`} {
		if !strings.Contains(string(b.Files["landing.html"]), needle) {
			t.Fatalf("missing %s in %s", needle, b.Files["landing.html"])
		}
	}
	if strings.Contains(string(b.Files["landing.html"]), "/workspace/") {
		t.Fatal("sandbox path leaked")
	}
	if _, ok := b.Files[".env"]; ok {
		t.Fatal("secret included")
	}
	if _, ok := b.Files["unrelated.html"]; ok {
		t.Fatal("unrelated file included")
	}
	if !strings.Contains(string(b.Files["styles/main.css"]), `/s/site/styles/theme.css`) {
		t.Fatal("CSS import was not rewritten")
	}
}
func TestDirectoryDeploymentAndNavigation(t *testing.T) {
	source := memoryFiles{"site/index.html": `<a href="about/">About</a><img src="/images/a.png"><script type="module" src="js/app.js"></script>`, "site/about/index.html": `<a href="../index.html">Home</a>`, "site/images/a.png": "A", "site/js/app.js": `import './part.js';`, "site/js/part.js": "export const x=1;", "site/.env": "secret", "site/node_modules/dependency.js": "skip"}
	b, err := Build(context.Background(), source, "s", "/workspace/conversations/test", "site", "", "/s/demo/")
	if err != nil {
		t.Fatal(err)
	}
	if b.Entry != "site/index.html" || len(b.Files) != 5 {
		t.Fatalf("unexpected bundle: %+v", b)
	}
	if !strings.Contains(string(b.Files[b.Entry]), `/s/demo/site/about/index.html`) {
		t.Fatal("directory navigation missing")
	}
	if !strings.Contains(string(b.Files[b.Entry]), `/s/demo/site/images/a.png`) {
		t.Fatal("root URL not scoped to site directory")
	}
}
func TestUnsafeOrBrokenBundleIsRejected(t *testing.T) {
	for name, html := range map[string]string{"missing": `<img src="absent.png">`, "traversal": `<img src="../../secret.png">`, "other conversation": `<img src="/workspace/conversations/other/a.png">`, "hidden": `<a href=".env">secret</a>`, "base": `<base href="https://elsewhere.example/">`, "encoded traversal": `<img src="%2e%2e/%2e%2e/a.png">`} {
		t.Run(name, func(t *testing.T) {
			if _, err := Build(context.Background(), memoryFiles{"index.html": html}, "s", "/workspace/conversations/test", "index.html", "", "/s/demo/"); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}
func TestEmbeddedAndRemoteMediaAreNotFetched(t *testing.T) {
	b, err := Build(context.Background(), memoryFiles{"index.html": `<img src="data:image/png;base64,YQ=="><video src="https://cdn.example/demo.mp4"></video><a href="#section">Go</a>`}, "s", "/workspace/conversations/test", "index.html", "", "/s/demo/")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Files) != 1 || len(b.Warnings) != 1 {
		t.Fatalf("unexpected bundle %+v", b)
	}
}
func TestBundleLimits(t *testing.T) {
	source := memoryFiles{"index.html": "<title>ok</title>"}
	for i := 0; i < MaxFiles; i++ {
		source[strings.Repeat("a", i+1)+".txt"] = "x"
	}
	if _, err := Build(context.Background(), source, "s", "/workspace/conversations/test", ".", "", "/s/demo/"); err == nil {
		t.Fatal("expected file limit")
	}
	source = memoryFiles{"index.html": `<img src="huge.png">`, "huge.png": strings.Repeat("a", MaxFileBytes+1)}
	if _, err := Build(context.Background(), source, "s", "/workspace/conversations/test", "index.html", "", "/s/demo/"); err == nil {
		t.Fatal("expected size limit")
	}
}
