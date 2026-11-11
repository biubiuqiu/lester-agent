package localrun

import (
	"container/heap"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	MaxLocalFileBytes      = 1 << 20
	MaxLocalDirectoryItems = 500
)

var (
	ErrPathDenied    = errors.New("path is outside the project or is protected")
	ErrSensitivePath = errors.New("sensitive file requires explicit approval")
	ErrConflict      = errors.New("file changed or the requested replacement is ambiguous")
	ErrTooLarge      = errors.New("file exceeds the local text size limit")
	ErrBinary        = errors.New("file is not UTF-8 text")
	ErrUnsupported   = errors.New("operation is unsupported on this platform")
	projectFileLocks sync.Map
)

type FileEntry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	IsDir      bool      `json:"is_dir"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

type FileContent struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

type FilePage struct {
	Entries    []FileEntry `json:"entries"`
	NextCursor string      `json:"next_cursor,omitempty"`
	Truncated  bool        `json:"truncated"`
}

// ProjectFS confines file operations to an opened, authorized directory. This
// boundary applies to file APIs; executing a local command is not an OS sandbox.
type ProjectFS struct {
	root   *os.Root
	name   string
	mu     *sync.Mutex
	closed bool
}

func NewProjectFS(root string) (*ProjectFS, error) {
	name, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve project root: %w", err)
	}
	name, err = filepath.EvalSymlinks(name)
	if err != nil {
		return nil, fmt.Errorf("resolve project root: %w", err)
	}
	directory, err := os.OpenRoot(name)
	if err != nil {
		return nil, fmt.Errorf("open project root: %w", err)
	}
	lock, _ := projectFileLocks.LoadOrStore(name, &sync.Mutex{})
	return &ProjectFS{root: directory, name: name, mu: lock.(*sync.Mutex)}, nil
}

func (p *ProjectFS) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	return p.root.Close()
}

func protectedComponent(component string) bool {
	switch strings.ToLower(component) {
	case ".git", ".ssh", ".gnupg", ".aws", ".azure", ".kube", ".docker", ".config", ".gcloud":
		return true
	}
	return strings.HasPrefix(strings.ToLower(component), ".lester-write-")
}

func sensitiveComponent(component string) bool {
	component = strings.ToLower(component)
	if component == ".env" || strings.HasPrefix(component, ".env.") {
		return true
	}
	switch component {
	case ".netrc", ".npmrc", ".pypirc", ".git-credentials", "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519":
		return true
	}
	return false
}

func localPath(name string, allowSensitive bool) (string, error) {
	if name == "" || name == "." {
		return ".", nil
	}
	// Treat backslashes and drive prefixes as invalid on every host so a path
	// cannot change meaning when an approved request crosses platforms.
	if strings.ContainsAny(name, "\\\x00:") || filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return "", ErrPathDenied
	}
	for _, component := range strings.Split(name, "/") {
		if component == ".." || protectedComponent(component) {
			return "", ErrPathDenied
		}
		if sensitiveComponent(component) && !allowSensitive {
			return "", ErrSensitivePath
		}
	}
	return path.Clean(name), nil
}

// parent opens each directory separately and rejects symlinks, including aliases
// to protected directories within the project. Root itself prevents escape even
// if an external program changes the directory tree while it is being opened.
func (p *ProjectFS) parent(name string) (*os.Root, string, error) {
	if p.closed {
		return nil, "", os.ErrClosed
	}
	directory, err := p.root.OpenRoot(".")
	if err != nil {
		return nil, "", err
	}
	for _, component := range strings.Split(path.Dir(name), "/") {
		if component == "." {
			continue
		}
		before, statErr := directory.Lstat(component)
		if statErr != nil {
			directory.Close()
			return nil, "", statErr
		}
		if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			directory.Close()
			return nil, "", ErrPathDenied
		}
		next, openErr := directory.OpenRoot(component)
		directory.Close()
		if openErr != nil {
			return nil, "", openErr
		}
		after, statErr := next.Stat(".")
		if statErr != nil || !os.SameFile(before, after) {
			next.Close()
			return nil, "", ErrPathDenied
		}
		directory = next
	}
	return directory, path.Base(name), nil
}

func openLocalRegular(directory *os.Root, name string) (*os.File, os.FileInfo, error) {
	before, err := directory.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, nil, ErrPathDenied
	}
	file, err := directory.OpenFile(name, os.O_RDONLY|localFileReadFlags, 0)
	if err != nil {
		return nil, nil, err
	}
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		file.Close()
		return nil, nil, ErrPathDenied
	}
	return file, after, nil
}

func localText(content []byte) error {
	if len(content) > MaxLocalFileBytes {
		return ErrTooLarge
	}
	if !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0 {
		return ErrBinary
	}
	return nil
}

func fileContent(name string, content []byte) FileContent {
	digest := sha256.Sum256(content)
	return FileContent{Path: name, Content: string(content), SHA256: hex.EncodeToString(digest[:]), Size: int64(len(content))}
}

func readLocal(directory *os.Root, base, name string) (FileContent, os.FileInfo, error) {
	file, info, err := openLocalRegular(directory, base)
	if err != nil {
		return FileContent{}, nil, err
	}
	defer file.Close()
	if info.Size() > MaxLocalFileBytes {
		return FileContent{}, nil, ErrTooLarge
	}
	content, err := io.ReadAll(io.LimitReader(file, MaxLocalFileBytes+1))
	if err != nil {
		return FileContent{}, nil, err
	}
	if err := localText(content); err != nil {
		return FileContent{}, nil, err
	}
	return fileContent(name, content), info, nil
}

func (p *ProjectFS) Read(name string) (FileContent, error) {
	return p.read(name, false)
}

// ReadApproved is only for an upper layer that has obtained explicit approval
// for this exact sensitive file. It does not grant access to protected directories.
func (p *ProjectFS) ReadApproved(name string) (FileContent, error) {
	return p.read(name, true)
}

func (p *ProjectFS) read(name string, allowSensitive bool) (FileContent, error) {
	name, err := localPath(name, allowSensitive)
	if err != nil {
		return FileContent{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	directory, base, err := p.parent(name)
	if err != nil {
		return FileContent{}, err
	}
	defer directory.Close()
	content, _, err := readLocal(directory, base, name)
	return content, err
}

type entryHeap []FileEntry

func (h entryHeap) Len() int           { return len(h) }
func (h entryHeap) Less(i, j int) bool { return h[i].Name > h[j].Name }
func (h entryHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *entryHeap) Push(v any)        { *h = append(*h, v.(FileEntry)) }
func (h *entryHeap) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]
	return last
}

func (p *ProjectFS) List(name string) ([]FileEntry, error) {
	page, err := p.ListPage(name, "", MaxLocalDirectoryItems)
	return page.Entries, err
}

// ListPage uses a lexical name cursor. A page retains at most limit+1 entries in
// memory; changes made between requests are reflected in subsequent pages.
func (p *ProjectFS) ListPage(name, after string, limit int) (FilePage, error) {
	if limit < 1 || limit > MaxLocalDirectoryItems {
		return FilePage{}, errors.New("directory page limit must be between 1 and 500")
	}
	if strings.ContainsAny(after, "/\\\x00") {
		return FilePage{}, ErrPathDenied
	}
	name, err := localPath(name, false)
	if err != nil {
		return FilePage{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	parent, base, err := p.parent(name)
	if err != nil {
		return FilePage{}, err
	}
	defer parent.Close()
	before, err := parent.Lstat(base)
	if err != nil {
		return FilePage{}, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return FilePage{}, ErrPathDenied
	}
	directory, err := parent.OpenFile(base, os.O_RDONLY|localFileReadFlags, 0)
	if err != nil {
		return FilePage{}, err
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil || !info.IsDir() || !os.SameFile(before, info) {
		return FilePage{}, ErrPathDenied
	}
	entries := &entryHeap{}
	heap.Init(entries)
	for {
		batch, readErr := directory.ReadDir(128)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return FilePage{}, readErr
		}
		for _, entry := range batch {
			if entry.Name() <= after || protectedComponent(entry.Name()) || sensitiveComponent(entry.Name()) || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			if entries.Len() == limit+1 && entry.Name() >= (*entries)[0].Name {
				continue
			}
			entryInfo, statErr := entry.Info()
			if statErr != nil {
				if errors.Is(statErr, os.ErrNotExist) {
					continue
				}
				return FilePage{}, statErr
			}
			if !entryInfo.IsDir() && !entryInfo.Mode().IsRegular() {
				continue
			}
			if entries.Len() == limit+1 {
				heap.Pop(entries)
			}
			heap.Push(entries, FileEntry{Name: entry.Name(), Path: path.Join(name, entry.Name()), IsDir: entryInfo.IsDir(), Size: entryInfo.Size(), ModifiedAt: entryInfo.ModTime()})
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	result := FilePage{Entries: []FileEntry{}}
	if entries.Len() > limit {
		heap.Pop(entries)
		result.Truncated = true
	}
	result.Entries = append(result.Entries, (*entries)...)
	sort.Slice(result.Entries, func(i, j int) bool { return result.Entries[i].Name < result.Entries[j].Name })
	if result.Truncated {
		result.NextCursor = result.Entries[len(result.Entries)-1].Name
	}
	return result, nil
}

func validLocalSHA(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (p *ProjectFS) Write(name, content string, expectedSHA *string, createOnly bool) (FileContent, error) {
	name, err := localPath(name, false)
	if err != nil {
		return FileContent{}, err
	}
	if err := localText([]byte(content)); err != nil {
		return FileContent{}, err
	}
	if expectedSHA != nil && !validLocalSHA(*expectedSHA) {
		return FileContent{}, ErrConflict
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.write(name, content, expectedSHA, createOnly)
}

func (p *ProjectFS) write(name, content string, expectedSHA *string, createOnly bool) (FileContent, error) {
	directory, base, err := p.parent(name)
	if err != nil {
		return FileContent{}, err
	}
	defer directory.Close()
	mode := os.FileMode(0o644)
	old, info, err := readLocal(directory, base, name)
	if err == nil {
		if createOnly || expectedSHA == nil || !strings.EqualFold(old.SHA256, *expectedSHA) {
			return FileContent{}, ErrConflict
		}
		mode = info.Mode().Perm()
	} else if errors.Is(err, os.ErrNotExist) {
		if expectedSHA != nil {
			return FileContent{}, ErrConflict
		}
	} else {
		return FileContent{}, err
	}
	temporary := ".lester-write-" + uuid.NewString()
	file, err := directory.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return FileContent{}, err
	}
	defer directory.Remove(temporary)
	if _, err = io.WriteString(file, content); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return FileContent{}, err
	}
	if closeErr != nil {
		return FileContent{}, closeErr
	}
	if expectedSHA == nil {
		// Hard linking implements no-replace creation even if another process
		// creates the destination after the initial existence check.
		if err := directory.Link(temporary, base); err != nil {
			if errors.Is(err, os.ErrExist) {
				return FileContent{}, ErrConflict
			}
			return FileContent{}, err
		}
	} else {
		current, _, err := readLocal(directory, base, name)
		if err != nil || !strings.EqualFold(current.SHA256, *expectedSHA) {
			return FileContent{}, ErrConflict
		}
		if err := directory.Rename(temporary, base); err != nil {
			return FileContent{}, err
		}
	}
	return fileContent(name, []byte(content)), nil
}

func (p *ProjectFS) Edit(name, old, replacement, expectedSHA string, all bool) (FileContent, error) {
	name, err := localPath(name, false)
	if err != nil {
		return FileContent{}, err
	}
	if old == "" || !validLocalSHA(expectedSHA) {
		return FileContent{}, ErrConflict
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	directory, base, err := p.parent(name)
	if err != nil {
		return FileContent{}, err
	}
	current, _, err := readLocal(directory, base, name)
	directory.Close()
	if err != nil {
		return FileContent{}, err
	}
	if !strings.EqualFold(current.SHA256, expectedSHA) {
		return FileContent{}, ErrConflict
	}
	count := strings.Count(current.Content, old)
	if count == 0 || (count != 1 && !all) {
		return FileContent{}, ErrConflict
	}
	content := strings.ReplaceAll(current.Content, old, replacement)
	if err := localText([]byte(content)); err != nil {
		return FileContent{}, err
	}
	return p.write(name, content, &expectedSHA, false)
}

// GitDiff invokes a fixed, read-only argv against a temporary index and no
// repository configuration. In particular, clean filters in .git/config must
// never turn an unapproved read into arbitrary code execution.
func (p *ProjectFS) GitDiff(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return "", os.ErrClosed
	}
	info, err := p.root.Lstat(".git")
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrPathDenied
	}
	actual, err := os.Stat(p.name)
	pinned, pinErr := p.root.Stat(".")
	if err != nil || pinErr != nil || !os.SameFile(actual, pinned) {
		return "", ErrPathDenied
	}
	git := ""
	for _, candidate := range []string{"/usr/bin/git", "/usr/local/bin/git", "/bin/git"} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			git = candidate
			break
		}
	}
	if git == "" {
		return "", ErrUnsupported
	}
	metadata, err := p.diffMetadata()
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(metadata)
	args := []string{git, "--no-pager", "--git-dir=" + metadata, "--work-tree=" + p.name,
		"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.attributesFile=/dev/null",
		"-c", "core.pager=cat", "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--ignore-submodules=all", "--no-color", "--", "."}
	for _, pattern := range []string{".git", ".ssh", ".gnupg", ".aws", ".azure", ".kube", ".docker", ".config", ".gcloud"} {
		args = append(args, ":(exclude,glob,icase)**/"+pattern+"/**", ":(exclude,glob,icase)**/"+pattern)
	}
	for _, pattern := range []string{".env", ".env.*", ".netrc", ".npmrc", ".pypirc", ".git-credentials", "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", ".lester-write-*"} {
		args = append(args, ":(exclude,glob,icase)**/"+pattern)
	}
	result, err := executeArgv(ctx, p.name, args, 10*time.Second)
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", errors.New("git diff failed")
	}
	if result.StdoutTruncated {
		return result.Stdout + "\n[diff truncated]\n", nil
	}
	return result.Stdout, nil
}

func (p *ProjectFS) diffMetadata() (_ string, resultErr error) {
	directory, base, err := p.parent(".git/index")
	if err != nil {
		return "", err
	}
	defer directory.Close()
	objects, err := directory.Lstat("objects")
	if err != nil || !objects.IsDir() || objects.Mode()&os.ModeSymlink != 0 {
		return "", ErrPathDenied
	}
	metadata, err := os.MkdirTemp("", "lester-diff-")
	if err != nil {
		return "", err
	}
	defer func() {
		if resultErr != nil {
			os.RemoveAll(metadata)
		}
	}()
	if err := os.Mkdir(filepath.Join(metadata, "refs"), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(metadata, "HEAD"), []byte("ref: refs/heads/local-diff\n"), 0o600); err != nil {
		return "", err
	}
	if err := os.Symlink(filepath.Join(p.name, ".git", "objects"), filepath.Join(metadata, "objects")); err != nil {
		return "", err
	}
	index, info, err := openLocalRegular(directory, base)
	if errors.Is(err, os.ErrNotExist) {
		return metadata, nil
	}
	if err != nil {
		return "", err
	}
	defer index.Close()
	const maxIndexBytes = 32 << 20
	if info.Size() > maxIndexBytes {
		return "", ErrTooLarge
	}
	content, err := io.ReadAll(io.LimitReader(index, maxIndexBytes+1))
	if err != nil {
		return "", err
	}
	if len(content) > maxIndexBytes {
		return "", ErrTooLarge
	}
	if err := os.WriteFile(filepath.Join(metadata, "index"), content, 0o600); err != nil {
		return "", err
	}
	return metadata, nil
}
