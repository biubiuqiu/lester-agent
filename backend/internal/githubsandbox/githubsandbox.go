// Package githubsandbox implements the credential-free native transfer boundary
// between a GitHub task's server-owned source snapshot and its Computer files.
package githubsandbox

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	MaxFiles         = 5000
	MaxImportBytes   = 25 << 20
	MaxChanges       = 100
	MaxExportBytes   = 1 << 20
	maxManifestBytes = 128 << 10
)

var (
	errPath    = errors.New("invalid GitHub task path")
	errFiles   = errors.New("GitHub task contains unsafe, unsupported or conflicting files")
	shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type ImportRequest struct{ Root, Archive, Directory, CheckoutID, BaseSHA string }
type ImportResult struct {
	Directory string `json:"directory"`
	LocalHead string `json:"local_head"`
	FileCount int    `json:"file_count"`
}
type Manifest struct {
	Files       []string `json:"files"`
	DeleteFiles []string `json:"delete_files"`
}
type ExportFile struct {
	Path    string `json:"path"`
	Content []byte `json:"content"`
	Mode    string `json:"mode"`
}
type ExportResult struct {
	Files       []ExportFile `json:"files"`
	DeleteFiles []string     `json:"delete_files"`
}
type ExportReceipt struct {
	Output      string `json:"output"`
	FileCount   int    `json:"file_count"`
	DeleteCount int    `json:"delete_count"`
}

func Run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("missing GitHub task operation")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", "", "conversation root")
	directory := flags.String("directory", "", "repository directory")
	switch args[0] {
	case "github-import":
		archive := flags.String("archive", "", "source archive")
		id := flags.String("checkout-id", "", "checkout identity")
		base := flags.String("base-sha", "", "source commit")
		if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
			return errors.New("invalid GitHub import options")
		}
		result, err := Import(context.Background(), ImportRequest{Root: *root, Archive: *archive, Directory: *directory, CheckoutID: *id, BaseSHA: *base})
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(result)
	case "github-export":
		manifest := flags.String("manifest", "", "selection manifest")
		output := flags.String("output", "", "private export receipt")
		if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
			return errors.New("invalid GitHub export options")
		}
		result, err := Export(*root, *directory, *manifest, *output)
		if err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(result)
	default:
		return errors.New("unknown GitHub task operation")
	}
}

func safeRelative(value string) bool {
	if value == "" || len(value) > 1024 || !utf8.ValidString(value) || strings.ContainsAny(value, "\\\x00\r\n:") || path.IsAbs(value) || path.Clean(value) != value || strings.Count(value, "/") > 32 {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		for _, r := range part {
			if r < 32 || r == 127 {
				return false
			}
		}
	}
	return true
}
func sourcePath(value string) bool {
	if !safeRelative(value) {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		switch strings.ToLower(part) {
		case ".git", ".agent", ".lester", "node_modules", ".venv", "venv", "__pycache__":
			return false
		}
	}
	return true
}
func privatePath(value, suffix string) (string, bool) {
	if !safeRelative(value) || !strings.HasPrefix(value, ".lester/github/") {
		return "", false
	}
	name := strings.TrimPrefix(value, ".lester/github/")
	if strings.Contains(name, "/") || !strings.HasSuffix(name, suffix) {
		return "", false
	}
	id := strings.TrimSuffix(name, suffix)
	parsed, err := uuid.Parse(id)
	return id, err == nil && parsed.String() == id
}

// Open every ancestor independently and refuse symbolic links before and after
// opening it. os.Root additionally confines race-time resolution to its scope.
func openDirectory(parent *os.Root, name string, create bool) (*os.Root, error) {
	before, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) && create {
		err = parent.Mkdir(name, 0o700)
		if err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		before, err = parent.Lstat(name)
	}
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, errPath
	}
	opened, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := parent.Lstat(name)
	if err != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, after) {
		opened.Close()
		return nil, errPath
	}
	fd, err := opened.Open(".")
	if err != nil {
		opened.Close()
		return nil, err
	}
	actual, err := fd.Stat()
	fd.Close()
	if err != nil || !os.SameFile(before, actual) {
		opened.Close()
		return nil, errPath
	}
	return opened, nil
}
func openRoot(value string) (*os.Root, error) {
	if !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return nil, errPath
	}
	volume := filepath.VolumeName(value)
	base := volume + string(filepath.Separator)
	current, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(value, volume), string(filepath.Separator))
	if rest == "" {
		current.Close()
		return nil, errPath
	}
	for _, part := range strings.Split(rest, string(filepath.Separator)) {
		next, e := openDirectory(current, part, false)
		current.Close()
		if e != nil {
			return nil, errPath
		}
		current = next
	}
	return current, nil
}
func walkDirectory(root *os.Root, value string, create bool) (*os.Root, error) {
	current, err := root.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	if value == "." || value == "" {
		return current, nil
	}
	for _, part := range strings.Split(value, "/") {
		next, e := openDirectory(current, part, create)
		current.Close()
		if e != nil {
			return nil, e
		}
		current = next
	}
	return current, nil
}
func openRegular(root *os.Root, value string) (*os.File, error) {
	parent, err := walkDirectory(root, path.Dir(value), false)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	name := path.Base(value)
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errFiles
	}
	file, err := parent.OpenFile(name, os.O_RDONLY|noFollowFlag|nonBlockingFlag, 0)
	if err != nil {
		return nil, err
	}
	actual, err := file.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(before, actual) {
		file.Close()
		return nil, errFiles
	}
	return file, nil
}
func boundedRead(file *os.File, limit int64) ([]byte, error) {
	before, err := file.Stat()
	if err != nil || before.Size() > limit {
		return nil, errors.New("GitHub task file exceeds transfer limits")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("GitHub task file exceeds transfer limits")
	}
	after, err := file.Stat()
	if err != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return nil, errors.New("GitHub task file changed during transfer")
	}
	return data, nil
}
func newPrivateFile(parent *os.Root, prefix string) (*os.File, string, error) {
	for range 8 {
		name := prefix + uuid.NewString()
		file, err := parent.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|noFollowFlag, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return file, name, err
	}
	return nil, "", errors.New("unable to create GitHub task transfer file")
}
func writeNew(parent *os.Root, name string, data []byte, mode os.FileMode) error {
	file, err := parent.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|noFollowFlag, mode)
	if err != nil {
		return err
	}
	// Use the opened FD: umask must not silently remove source executable bits,
	// and path-based chmod would introduce a symbolic-link race.
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

func Import(ctx context.Context, request ImportRequest) (*ImportResult, error) {
	id, err := uuid.Parse(request.CheckoutID)
	if err != nil || id.String() != request.CheckoutID || !shaPattern.MatchString(request.BaseSHA) || !sourcePath(request.Directory) {
		return nil, errPath
	}
	archiveID, valid := privatePath(request.Archive, ".zip")
	if !valid || archiveID != request.CheckoutID {
		return nil, errPath
	}
	root, err := openRoot(request.Root)
	if err != nil {
		return nil, errPath
	}
	defer root.Close()
	source, err := openRegular(root, request.Archive)
	if err != nil {
		return nil, errors.New("GitHub source archive is unavailable")
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil || info.Size() > MaxImportBytes {
		return nil, errors.New("GitHub source archive exceeds transfer limits")
	}
	archive, err := zip.NewReader(source, info.Size())
	if err != nil || len(archive.File) > MaxFiles {
		return nil, errFiles
	}
	seen := map[string]bool{}
	var total uint64
	count := 0
	for _, entry := range archive.File {
		name := strings.TrimSuffix(entry.Name, "/")
		if _, duplicate := seen[name]; !sourcePath(name) || duplicate {
			return nil, errFiles
		}
		seen[name] = entry.FileInfo().IsDir()
		if entry.FileInfo().IsDir() {
			continue
		}
		if !entry.Mode().IsRegular() || entry.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || entry.UncompressedSize64 > MaxImportBytes {
			return nil, errFiles
		}
		total += entry.UncompressedSize64
		if total > MaxImportBytes {
			return nil, errors.New("GitHub source archive exceeds transfer limits")
		}
		count++
	}
	if count == 0 {
		return nil, errors.New("GitHub source archive contains no supported files")
	}
	for name := range seen {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if directory, exists := seen[parent]; exists && !directory {
				return nil, errFiles
			}
		}
	}

	parent, err := walkDirectory(root, path.Dir(request.Directory), true)
	if err != nil {
		return nil, errors.New("GitHub destination parent is unavailable")
	}
	defer parent.Close()
	target := path.Base(request.Directory)
	if _, err = parent.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("GitHub destination already exists")
	}
	stageName := ".lester-github-import-" + uuid.NewString()
	if err = parent.Mkdir(stageName, 0o700); err != nil {
		return nil, errors.New("unable to stage GitHub source")
	}
	defer parent.RemoveAll(stageName)
	stage, err := openDirectory(parent, stageName, false)
	if err != nil {
		return nil, errPath
	}
	defer stage.Close()
	for _, entry := range archive.File {
		name := strings.TrimSuffix(entry.Name, "/")
		if entry.FileInfo().IsDir() {
			directory, e := walkDirectory(stage, name, true)
			if e != nil {
				return nil, errFiles
			}
			directory.Close()
			continue
		}
		directory, e := walkDirectory(stage, path.Dir(name), true)
		if e != nil {
			return nil, errFiles
		}
		dataReader, e := entry.Open()
		if e != nil {
			directory.Close()
			return nil, errFiles
		}
		data, e := io.ReadAll(io.LimitReader(dataReader, MaxImportBytes+1))
		dataReader.Close()
		if e != nil || uint64(len(data)) != entry.UncompressedSize64 {
			directory.Close()
			return nil, errFiles
		}
		mode := os.FileMode(0o644)
		if entry.Mode().Perm()&0o111 != 0 {
			mode = 0o755
		}
		e = writeNew(directory, path.Base(name), data, mode)
		directory.Close()
		if e != nil {
			return nil, errFiles
		}
	}
	stageFD, err := stage.Open(".")
	if err != nil {
		return nil, errPath
	}
	defer stageFD.Close()
	head, err := initializeGit(ctx, stageFD)
	if err != nil {
		return nil, err
	}
	marker, _ := json.Marshal(map[string]string{"checkout_id": request.CheckoutID, "base_sha": request.BaseSHA})
	git, err := openDirectory(stage, ".git", false)
	if err != nil {
		return nil, errors.New("GitHub local repository initialization failed")
	}
	err = writeNew(git, "lester-checkout.json", marker, 0o600)
	git.Close()
	if err != nil {
		return nil, errors.New("GitHub local repository receipt could not be saved")
	}
	if err = renameNew(parent, stageName, target); err != nil {
		return nil, errors.New("GitHub destination exists or atomic import is unavailable")
	}
	return &ImportResult{Directory: request.Directory, LocalHead: head, FileCount: count}, nil
}

func initializeGit(ctx context.Context, directory *os.File) (string, error) {
	git := ""
	for _, candidate := range []string{"/usr/bin/git", "/bin/git"} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			git = candidate
			break
		}
	}
	if git == "" {
		return "", errors.New("Git is not installed in this Computer image")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	run := func(arguments ...string) (string, error) {
		args := append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "protocol.allow=never", "-c", "commit.gpgsign=false", "-c", "user.name=Lester Agent", "-c", "user.email=lester-agent@users.noreply.github.com"}, arguments...)
		cmd := exec.CommandContext(ctx, git, args...)
		cmd.ExtraFiles = []*os.File{directory}
		cmd.Dir = directoryPath(directory)
		cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "HOME=/nonexistent", "XDG_CONFIG_HOME=/nonexistent", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=", "GIT_NO_REPLACE_OBJECTS=1"}
		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err != nil {
			return "", errors.New("GitHub local repository initialization failed")
		}
		return strings.TrimSpace(output.String()), nil
	}
	if _, err := run("init", "--template=", "--initial-branch=lester"); err != nil {
		return "", err
	}
	if _, err := run("add", "--force", "--all", "--", "."); err != nil {
		return "", err
	}
	if _, err := run("commit", "--no-verify", "-m", "GitHub source snapshot"); err != nil {
		return "", err
	}
	head, err := run("rev-parse", "--verify", "HEAD")
	if err != nil || !shaPattern.MatchString(head) {
		return "", errors.New("GitHub local repository baseline could not be verified")
	}
	return head, nil
}

func Export(rootName, directoryName, manifestName, outputName string) (*ExportReceipt, error) {
	id, valid := privatePath(manifestName, "-paths.json")
	outputID, outputValid := privatePath(outputName, ".json")
	if !valid || !outputValid || id != outputID || !sourcePath(directoryName) {
		return nil, errPath
	}
	root, err := openRoot(rootName)
	if err != nil {
		return nil, errPath
	}
	defer root.Close()
	manifestFile, err := openRegular(root, manifestName)
	if err != nil {
		return nil, errors.New("GitHub change manifest is unavailable")
	}
	data, err := boundedRead(manifestFile, maxManifestBytes)
	manifestFile.Close()
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if decoder.Decode(&manifest) != nil || decoder.Decode(new(any)) != io.EOF || len(manifest.Files)+len(manifest.DeleteFiles) == 0 || len(manifest.Files)+len(manifest.DeleteFiles) > MaxChanges {
		return nil, errors.New("invalid GitHub change manifest")
	}
	seen := map[string]bool{}
	for _, name := range append(append([]string{}, manifest.Files...), manifest.DeleteFiles...) {
		if _, duplicate := seen[name]; !sourcePath(name) || duplicate {
			return nil, errFiles
		}
		seen[name] = true
	}
	repo, err := walkDirectory(root, directoryName, false)
	if err != nil {
		return nil, errors.New("GitHub repository directory is unavailable")
	}
	defer repo.Close()
	result := ExportResult{Files: []ExportFile{}, DeleteFiles: append([]string{}, manifest.DeleteFiles...)}
	remaining := int64(MaxExportBytes)
	for _, name := range manifest.Files {
		file, e := openRegular(repo, name)
		if e != nil {
			return nil, errFiles
		}
		content, e := boundedRead(file, remaining)
		info, statErr := file.Stat()
		file.Close()
		if e != nil {
			return nil, e
		}
		if statErr != nil {
			return nil, errFiles
		}
		remaining -= int64(len(content))
		mode := "100644"
		if info.Mode().Perm()&0o111 != 0 {
			mode = "100755"
		}
		result.Files = append(result.Files, ExportFile{Path: name, Content: content, Mode: mode})
	}
	for _, name := range manifest.DeleteFiles {
		parent, e := walkDirectory(repo, path.Dir(name), false)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return nil, errFiles
		}
		_, e = parent.Lstat(path.Base(name))
		parent.Close()
		if !errors.Is(e, os.ErrNotExist) {
			return nil, errors.New("a requested GitHub deletion still exists or could not be verified")
		}
	}
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Path < result.Files[j].Path })
	sort.Strings(result.DeleteFiles)
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, errors.New("unable to encode GitHub changes")
	}
	parent, err := walkDirectory(root, ".lester/github", false)
	if err != nil {
		return nil, errPath
	}
	defer parent.Close()
	file, temp, err := newPrivateFile(parent, ".export-")
	if err != nil {
		return nil, errors.New("unable to stage GitHub changes")
	}
	defer parent.Remove(temp)
	if _, err = file.Write(encoded); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, errors.New("unable to save GitHub changes")
	}
	if err = renameNew(parent, temp, path.Base(outputName)); err != nil {
		return nil, errors.New("GitHub export receipt already exists or atomic export is unavailable")
	}
	return &ExportReceipt{Output: outputName, FileCount: len(result.Files), DeleteCount: len(result.DeleteFiles)}, nil
}
