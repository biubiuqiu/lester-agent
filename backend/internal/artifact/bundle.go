package artifact

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"golang.org/x/net/html"
)

const MaxFiles = 256
const MaxTotalBytes = 100 << 20
const MaxFileBytes = 25 << 20

type Files interface {
	ReadFile(context.Context, string, string, string) ([]byte, error)
	ListFiles(context.Context, string, string, string) ([]sandbox.FileEntry, error)
}
type Bundle struct {
	Entry    string
	Files    map[string][]byte
	Warnings []string
}
type bundler struct {
	directories                    int
	ctx                            context.Context
	source                         Files
	sandbox, workDir, root, prefix string
	files                          map[string][]byte
	visiting                       map[string]bool
	warnings                       map[string]bool
	total                          int
}

var staticExtensions = map[string]bool{".html": true, ".htm": true, ".css": true, ".js": true, ".mjs": true, ".json": true, ".webmanifest": true, ".txt": true, ".csv": true, ".xml": true, ".svg": true, ".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true, ".avif": true, ".ico": true, ".mp4": true, ".webm": true, ".mov": true, ".mp3": true, ".wav": true, ".ogg": true, ".m4a": true, ".vtt": true, ".pdf": true, ".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".wasm": true}

func cleanPath(p string) (string, error) {
	if p == "" {
		return ".", nil
	}
	if strings.ContainsAny(p, "\\\x00\r\n") || strings.HasPrefix(p, "/") || strings.Contains(p, ":") {
		return "", errors.New("路径必须相对于当前会话目录")
	}
	p = path.Clean(p)
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", errors.New("文件路径不能超出当前会话目录")
	}
	return p, nil
}
func allowedFile(p string, referenced bool) bool {
	for _, part := range strings.Split(p, "/") {
		if strings.HasPrefix(part, ".") && !(referenced && part == ".agent" && strings.HasPrefix(p, ".agent/upload/")) {
			return false
		}
		if part == "node_modules" || part == "vendor" || part == "__pycache__" {
			return false
		}
	}
	return staticExtensions[strings.ToLower(path.Ext(p))]
}

// Build copies only explicit static directories or the dependency closure of an
// HTML entry. References are rewritten to stable site-scoped URLs, never to the
// authenticated API or the live Computer. The source provider enforces symlinks.
func Build(ctx context.Context, source Files, sandboxID, workDir, sourcePath, entry, prefix string) (Bundle, error) {
	p, err := cleanPath(sourcePath)
	if err != nil {
		return Bundle{}, err
	}
	b := &bundler{ctx: ctx, source: source, sandbox: sandboxID, workDir: strings.TrimRight(workDir, "/"), prefix: prefix, files: map[string][]byte{}, visiting: map[string]bool{}, warnings: map[string]bool{}}
	ext := strings.ToLower(path.Ext(p))
	var initial []string
	if ext == ".html" || ext == ".htm" {
		b.root = path.Dir(p)
		entry = p
		initial = []string{p}
	} else {
		b.root = p
		if entry == "" {
			entry = "index.html"
		}
		entry, err = cleanPath(entry)
		if err != nil {
			return Bundle{}, err
		}
		entry = path.Join(p, entry)
		if !strings.HasSuffix(strings.ToLower(entry), ".html") && !strings.HasSuffix(strings.ToLower(entry), ".htm") {
			return Bundle{}, errors.New("入口必须是 HTML 文件")
		}
		initial, err = b.walk(p, 0)
		if err != nil {
			return Bundle{}, err
		}
	}
	for _, file := range initial {
		if err = b.add(file, false); err != nil {
			return Bundle{}, err
		}
	}
	if _, ok := b.files[entry]; !ok {
		return Bundle{}, fmt.Errorf("缺少入口文件 %s", entry)
	}
	warnings := []string{}
	for w := range b.warnings {
		warnings = append(warnings, w)
	}
	sort.Strings(warnings)
	return Bundle{Entry: entry, Files: b.files, Warnings: warnings}, nil
}
func (b *bundler) walk(dir string, depth int) ([]string, error) {
	b.directories++
	if b.directories > 256 {
		return nil, errors.New("站点目录超过 256 个")
	}
	if depth > 12 {
		return nil, errors.New("站点目录最多支持 12 层")
	}
	entries, err := b.source.ListFiles(b.ctx, b.sandbox, b.workDir, dir)
	if err != nil {
		return nil, fmt.Errorf("无法读取站点目录 %s: %w", dir, err)
	}
	result := []string{}
	for _, e := range entries {
		if e.Name == "" || e.Name == "." || e.Name == ".." || strings.ContainsAny(e.Name, "/\\") {
			return nil, errors.New("无效的目录项")
		}
		p := path.Join(dir, e.Name)
		if e.IsDir {
			if strings.HasPrefix(e.Name, ".") || e.Name == "node_modules" || e.Name == "vendor" || e.Name == "__pycache__" {
				continue
			}
			children, err := b.walk(p, depth+1)
			if err != nil {
				return nil, err
			}
			result = append(result, children...)
		} else if allowedFile(p, false) {
			result = append(result, p)
		}
		if len(result) > MaxFiles {
			return nil, errors.New("站点文件超过 256 个")
		}
	}
	return result, nil
}
func (b *bundler) add(p string, referenced bool) error {
	if b.visiting[p] {
		return nil
	}
	if !allowedFile(p, referenced) {
		return fmt.Errorf("不能发布文件 %s：仅支持静态站点文件，隐藏目录仅允许显式引用的 .agent/upload 附件", p)
	}
	if len(b.visiting) >= MaxFiles {
		return errors.New("站点及引用资源超过 256 个文件")
	}
	b.visiting[p] = true
	data, err := b.source.ReadFile(b.ctx, b.sandbox, b.workDir, p)
	if err != nil {
		return fmt.Errorf("无法读取引用文件 %s，请检查路径: %w", p, err)
	}
	if len(data) > MaxFileBytes {
		return fmt.Errorf("文件 %s 超过 25 MiB", p)
	}
	originalSize := len(data)
	b.total += len(data)
	if b.total > MaxTotalBytes {
		return errors.New("站点总大小超过 100 MiB")
	}
	switch strings.ToLower(path.Ext(p)) {
	case ".html", ".htm":
		data, err = b.html(p, data)
	case ".css":
		data, err = b.css(p, data)
	case ".js", ".mjs":
		data, err = b.js(p, data)
	}
	if err != nil {
		return err
	}
	b.total += len(data) - originalSize
	if len(data) > MaxFileBytes || b.total > MaxTotalBytes {
		return errors.New("重写后的站点超过大小限制")
	}
	b.files[p] = data
	return nil
}
func (b *bundler) reference(from, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return raw, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%s 包含无效资源地址", from)
	}
	if u.IsAbs() || u.Host != "" {
		if u.Scheme == "file" {
			raw = u.Path
			u = &url.URL{Path: raw}
		} else {
			if u.Scheme == "http" || u.Scheme == "https" || u.Host != "" {
				b.warnings["外部资源保持原链接，访问时仍依赖外部服务"] = true
			}
			return raw, nil
		}
	}
	var target string
	switch {
	case u.Path == "":
		target = from
	case strings.HasPrefix(u.Path, b.workDir+"/"):
		target = strings.TrimPrefix(u.Path, b.workDir+"/")
	case strings.HasPrefix(u.Path, "/workspace/"):
		return "", errors.New("引用了其他会话或工作区路径")
	case strings.HasPrefix(u.Path, "/"):
		target = path.Join(b.root, strings.TrimPrefix(u.Path, "/"))
	default:
		target = path.Join(path.Dir(from), u.Path)
	}
	target, err = cleanPath(target)
	if err != nil {
		return "", err
	}
	if path.Ext(target) == "" || strings.HasSuffix(u.Path, "/") {
		target = path.Join(target, "index.html")
	}
	if err = b.add(target, true); err != nil {
		return "", err
	}
	u.Path = b.prefix + target
	u.RawPath = ""
	return u.String(), nil
}

var cssURL = regexp.MustCompile(`(?i)url\(\s*(?:"([^"]*)"|'([^']*)'|([^\s)]+))\s*\)`)
var cssImport = regexp.MustCompile(`(?i)@import\s+["']([^"']+)["']`)

func (b *bundler) css(from string, data []byte) ([]byte, error) {
	var failure error
	text := cssURL.ReplaceAllStringFunc(string(data), func(m string) string {
		a := cssURL.FindStringSubmatch(m)
		raw := a[1] + a[2] + a[3]
		v, err := b.reference(from, raw)
		if err != nil {
			failure = err
		}
		return `url("` + v + `")`
	})
	text = cssImport.ReplaceAllStringFunc(text, func(m string) string {
		v, err := b.reference(from, cssImport.FindStringSubmatch(m)[1])
		if err != nil {
			failure = err
		}
		return `@import "` + v + `"`
	})
	return []byte(text), failure
}

var jsString = regexp.MustCompile(`["']([^"'\s\x60]+)["']`)

func (b *bundler) js(from string, data []byte) ([]byte, error) {
	var failure error
	text := jsString.ReplaceAllStringFunc(string(data), func(m string) string {
		value := m[1 : len(m)-1]
		u, err := url.Parse(value)
		if err != nil || u.Path == "" || !staticExtensions[strings.ToLower(path.Ext(u.Path))] {
			return m
		}
		v, err := b.reference(from, value)
		if err != nil {
			failure = err
		}
		return m[:1] + v + m[len(m)-1:]
	})
	if strings.Contains(text, "${") {
		b.warnings["动态拼接的资源路径无法自动发现；请使用完整站点目录并保持相对路径"] = true
	}
	return []byte(text), failure
}
func (b *bundler) html(from string, data []byte) ([]byte, error) {
	node, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var visit func(*html.Node) error
	visit = func(n *html.Node) error {
		if n.Type == html.ElementNode {
			if n.Data == "base" {
				return errors.New("请移除 HTML 的 base 标签并使用相对资源路径后再部署")
			}
			for i, a := range n.Attr {
				var v string
				var err error
				switch a.Key {
				case "src", "poster":
					v, err = b.reference(from, a.Val)
				case "href":
					if n.Data == "a" || n.Data == "link" || n.Data == "use" || n.Data == "image" {
						v, err = b.reference(from, a.Val)
					} else {
						continue
					}
				case "data":
					if n.Data == "object" {
						v, err = b.reference(from, a.Val)
					} else {
						continue
					}
				case "srcset":
					if strings.Contains(a.Val, "data:") {
						return errors.New("请将 srcset 中的内联 data 图片改为独立文件或普通 src")
					}
					parts := strings.Split(a.Val, ",")
					for j, part := range parts {
						fields := strings.Fields(part)
						if len(fields) == 0 {
							continue
						}
						fields[0], err = b.reference(from, fields[0])
						if err != nil {
							return err
						}
						parts[j] = strings.Join(fields, " ")
					}
					v = strings.Join(parts, ", ")
				case "style":
					var value []byte
					value, err = b.css(from, []byte(a.Val))
					v = string(value)
				default:
					continue
				}
				if err != nil {
					return err
				}
				n.Attr[i].Val = v
			}
			if n.Data == "style" || n.Data == "script" {
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type != html.TextNode {
						continue
					}
					var value []byte
					if n.Data == "style" {
						value, err = b.css(from, []byte(c.Data))
					} else {
						value, err = b.js(from, []byte(c.Data))
					}
					if err != nil {
						return err
					}
					c.Data = string(value)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := visit(c); err != nil {
				return err
			}
		}
		if n.Type == html.ElementNode && n.Data == "head" {
			directory := path.Dir(from)
			baseURL := b.prefix
			if directory != "." {
				baseURL += directory + "/"
			}
			base := &html.Node{Type: html.ElementNode, Data: "base", Attr: []html.Attribute{{Key: "href", Val: baseURL}}}
			n.InsertBefore(base, n.FirstChild)
		}
		return nil
	}
	if err = visit(node); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err = html.Render(&out, node); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
func ContentType(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	}
	v := mime.TypeByExtension(strings.ToLower(path.Ext(p)))
	if v == "" {
		v = "application/octet-stream"
	}
	return v
}
