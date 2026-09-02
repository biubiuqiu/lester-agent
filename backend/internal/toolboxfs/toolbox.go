package toolboxfs

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	MaxFileBytes        = 25 << 20
	MaxDirectoryItems   = 500
	MaxReadLines        = 2000
	MaxCharactersInLine = 2000
)

type Entry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	IsDir      bool      `json:"is_dir"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

type FileLine struct {
	Text              string `json:"text"`
	OmittedCharacters int    `json:"omitted_characters,omitempty"`
}

type FileLines struct {
	Lines      []FileLine `json:"lines"`
	StartLine  int        `json:"start_line"`
	TotalLines int        `json:"total_lines"`
}

type EditRequest struct {
	OldString      string `json:"old_string"`
	NewString      string `json:"new_string"`
	ReplaceAll     bool   `json:"replace_all,omitempty"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
}

type EditResult struct {
	OK           bool   `json:"ok"`
	Replacements int    `json:"replacements"`
	SHA256       string `json:"sha256"`
}

type WriteResult struct {
	OK           bool   `json:"ok"`
	BytesWritten int64  `json:"bytes_written"`
	SHA256       string `json:"sha256"`
}

func Run(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("missing operation")
	}
	if args[0] == "version" {
		_, err := io.WriteString(stdout, "1\n")
		return err
	}
	root, target, offset, limit, remaining, err := parsePathFlags(args[0], args[1:])
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		if len(remaining) != 0 {
			return errors.New("list does not accept positional arguments")
		}
		entries, listErr := List(root, target)
		if listErr != nil {
			return listErr
		}
		return json.NewEncoder(stdout).Encode(entries)
	case "read":
		if len(remaining) != 0 {
			return errors.New("read does not accept positional arguments")
		}
		return Read(root, target, stdout)
	case "read-lines":
		if len(remaining) != 0 {
			return errors.New("read-lines does not accept positional arguments")
		}
		result, readErr := ReadLines(root, target, offset, limit)
		if readErr != nil {
			return readErr
		}
		return json.NewEncoder(stdout).Encode(result)
	case "write":
		if len(remaining) != 0 {
			return errors.New("write does not accept positional arguments")
		}
		result, writeErr := Write(root, target, stdin, "")
		if writeErr != nil {
			return writeErr
		}
		return json.NewEncoder(stdout).Encode(result)
	case "edit":
		if len(remaining) != 0 {
			return errors.New("edit does not accept positional arguments")
		}
		var request EditRequest
		if decodeErr := decodeOneJSON(stdin, &request); decodeErr != nil {
			return fmt.Errorf("decode edit request: %w", decodeErr)
		}
		result, editErr := Edit(root, target, request)
		if editErr != nil {
			return editErr
		}
		return json.NewEncoder(stdout).Encode(result)
	default:
		return fmt.Errorf("unknown operation %q", args[0])
	}
}

func parsePathFlags(operation string, args []string) (string, string, int, int, []string, error) {
	flags := flag.NewFlagSet(operation, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", "", "workspace root")
	target := flags.String("path", ".", "path inside the workspace root")
	offset := flags.Int("offset", 0, "one-based first line")
	limit := flags.Int("limit", 0, "maximum number of lines")
	if err := flags.Parse(args); err != nil {
		return "", "", 0, 0, nil, err
	}
	if strings.TrimSpace(*root) == "" {
		return "", "", 0, 0, nil, errors.New("root is required")
	}
	if operation != "read-lines" && (*offset != 0 || *limit != 0) {
		return "", "", 0, 0, nil, fmt.Errorf("%s does not accept line range flags", operation)
	}
	return *root, *target, *offset, *limit, flags.Args(), nil
}

func List(root, target string) ([]Entry, error) {
	rootPath, resolved, err := resolveExisting(root, target)
	if err != nil {
		return nil, err
	}
	items, err := os.ReadDir(resolved)
	if err != nil {
		return nil, fmt.Errorf("list directory: %w", err)
	}
	if len(items) > MaxDirectoryItems {
		return nil, fmt.Errorf("directory contains more than %d entries; use bash with a narrower path or filter", MaxDirectoryItems)
	}
	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		info, infoErr := item.Info()
		if infoErr != nil {
			return nil, fmt.Errorf("stat %q: %w", item.Name(), infoErr)
		}
		itemPath := filepath.Join(resolved, item.Name())
		relative, relErr := filepath.Rel(rootPath, itemPath)
		if relErr != nil {
			return nil, fmt.Errorf("resolve relative path: %w", relErr)
		}
		entries = append(entries, Entry{
			Name: item.Name(), Path: filepath.ToSlash(relative), IsDir: item.IsDir(),
			Size: info.Size(), ModifiedAt: info.ModTime().UTC(),
		})
	}
	return entries, nil
}

func Read(root, target string, output io.Writer) error {
	_, resolved, err := resolveExisting(root, target)
	if err != nil {
		return err
	}
	file, err := openRegularFile(resolved)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}
	if info.Size() > MaxFileBytes {
		return fmt.Errorf("file exceeds the %d MiB read limit", MaxFileBytes>>20)
	}
	written, err := io.Copy(output, io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	if written > MaxFileBytes {
		return fmt.Errorf("file exceeds the %d MiB read limit", MaxFileBytes>>20)
	}
	return nil
}

func ReadLines(root, target string, offset, limit int) (*FileLines, error) {
	if offset < 1 || limit < 1 || limit > MaxReadLines {
		return nil, errors.New("invalid file line range")
	}
	_, resolved, err := resolveExisting(root, target)
	if err != nil {
		return nil, err
	}
	file, err := openRegularFile(resolved)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}
	if info.Size() > MaxFileBytes {
		return nil, fmt.Errorf("file exceeds the %d MiB read limit", MaxFileBytes>>20)
	}

	result := &FileLines{StartLine: offset}
	reader := bufio.NewReader(file)
	lineNumber, characters, kept, omitted := 1, 0, 0, 0
	var line strings.Builder
	finishLine := func() {
		result.TotalLines++
		if lineNumber >= offset && lineNumber < offset+limit {
			result.Lines = append(result.Lines, FileLine{Text: line.String(), OmittedCharacters: omitted})
		}
		lineNumber++
		characters, kept, omitted = 0, 0, 0
		line.Reset()
	}
	for {
		value, _, readErr := reader.ReadRune()
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if characters > 0 {
					finishLine()
				}
				break
			}
			return nil, fmt.Errorf("read file lines: %w", readErr)
		}
		if value == '\n' {
			finishLine()
			continue
		}
		characters++
		if lineNumber < offset || lineNumber >= offset+limit {
			continue
		}
		if kept < MaxCharactersInLine {
			line.WriteRune(value)
			kept++
		} else {
			omitted++
		}
	}
	return result, nil
}

func Write(root, target string, input io.Reader, expectedSHA256 string) (*WriteResult, error) {
	bytesWritten, sum, err := atomicWrite(root, target, input, expectedSHA256)
	if err != nil {
		return nil, err
	}
	return &WriteResult{OK: true, BytesWritten: bytesWritten, SHA256: sum}, nil
}

func Edit(root, target string, request EditRequest) (*EditResult, error) {
	if request.OldString == "" {
		return nil, errors.New("old_string cannot be empty")
	}
	_, resolved, err := resolveExisting(root, target)
	if err != nil {
		return nil, err
	}
	data, err := readRegularFile(resolved)
	if err != nil {
		return nil, err
	}
	currentSum := sha256Hex(data)
	if request.ExpectedSHA256 != "" && !strings.EqualFold(request.ExpectedSHA256, currentSum) {
		return nil, errors.New("file changed since it was read")
	}
	matches := strings.Count(string(data), request.OldString)
	if matches == 0 {
		return nil, errors.New("old_string was not found in the file")
	}
	if matches > 1 && !request.ReplaceAll {
		return nil, fmt.Errorf("old_string matched %d locations; include more context or set replace_all", matches)
	}
	updated := strings.Replace(string(data), request.OldString, request.NewString, 1)
	if request.ReplaceAll {
		updated = strings.ReplaceAll(string(data), request.OldString, request.NewString)
	}
	writeResult, err := Write(root, target, strings.NewReader(updated), currentSum)
	if err != nil {
		return nil, err
	}
	return &EditResult{OK: true, Replacements: matches, SHA256: writeResult.SHA256}, nil
}

func atomicWrite(root, target string, input io.Reader, expectedSHA256 string) (int64, string, error) {
	_, parent, destination, err := resolveWriteTarget(root, target)
	if err != nil {
		return 0, "", err
	}
	mode := os.FileMode(0o644)
	if info, statErr := os.Lstat(destination); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return 0, "", errors.New("refusing to write through a symbolic link")
		}
		if !info.Mode().IsRegular() {
			return 0, "", errors.New("target is not a regular file")
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return 0, "", fmt.Errorf("stat write target: %w", statErr)
	}
	if expectedSHA256 != "" {
		current, readErr := readRegularFile(destination)
		if readErr != nil {
			return 0, "", readErr
		}
		if !strings.EqualFold(expectedSHA256, sha256Hex(current)) {
			return 0, "", errors.New("file changed during edit")
		}
	}

	temporary, err := os.CreateTemp(parent, ".lester-write-*")
	if err != nil {
		return 0, "", fmt.Errorf("create temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	if err = temporary.Chmod(mode); err != nil {
		cleanup()
		return 0, "", fmt.Errorf("set file mode: %w", err)
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(input, MaxFileBytes+1))
	if err != nil {
		cleanup()
		return 0, "", fmt.Errorf("write temporary file: %w", err)
	}
	if written > MaxFileBytes {
		cleanup()
		return 0, "", fmt.Errorf("file exceeds the %d MiB write limit", MaxFileBytes>>20)
	}
	if err = temporary.Sync(); err != nil {
		cleanup()
		return 0, "", fmt.Errorf("sync temporary file: %w", err)
	}
	if err = temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return 0, "", fmt.Errorf("close temporary file: %w", err)
	}
	if err = replaceFile(temporaryPath, destination); err != nil {
		_ = os.Remove(temporaryPath)
		return 0, "", fmt.Errorf("replace file atomically: %w", err)
	}
	if directory, openErr := os.Open(parent); openErr == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return written, hex.EncodeToString(hash.Sum(nil)), nil
}

func replaceFile(source, destination string) error {
	if err := os.Rename(source, destination); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		return err
	}
	// Windows does not replace an existing destination with Rename. The helper
	// runs on Linux in production; this fallback keeps local Windows tests useful.
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, destination)
}

func resolveExisting(root, target string) (string, string, error) {
	rootPath, rootReal, err := prepareRoot(root, false)
	if err != nil {
		return "", "", err
	}
	candidate, err := lexicalTarget(rootPath, target)
	if err != nil {
		return "", "", err
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", "", fmt.Errorf("resolve path: %w", err)
	}
	if !within(rootReal, resolved) {
		return "", "", errors.New("path escapes workspace root")
	}
	return rootReal, resolved, nil
}

func resolveWriteTarget(root, target string) (string, string, string, error) {
	rootPath, rootReal, err := prepareRoot(root, true)
	if err != nil {
		return "", "", "", err
	}
	candidate, err := lexicalTarget(rootPath, target)
	if err != nil {
		return "", "", "", err
	}
	if filepath.Clean(candidate) == filepath.Clean(rootPath) {
		return "", "", "", errors.New("file path must not be the workspace root")
	}
	parent := filepath.Dir(candidate)
	probe := parent
	for {
		if _, statErr := os.Lstat(probe); statErr == nil {
			break
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", "", "", fmt.Errorf("inspect write parent: %w", statErr)
		}
		next := filepath.Dir(probe)
		if next == probe {
			return "", "", "", errors.New("unable to resolve write parent")
		}
		probe = next
	}
	probeReal, err := filepath.EvalSymlinks(probe)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve write parent: %w", err)
	}
	if !within(rootReal, probeReal) {
		return "", "", "", errors.New("path escapes workspace root through a symbolic link")
	}
	suffix, err := filepath.Rel(probe, parent)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve write parent suffix: %w", err)
	}
	parent = filepath.Join(probeReal, suffix)
	if err = os.MkdirAll(parent, 0o755); err != nil {
		return "", "", "", fmt.Errorf("create write parent: %w", err)
	}
	parentReal, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve created write parent: %w", err)
	}
	if !within(rootReal, parentReal) {
		return "", "", "", errors.New("path escapes workspace root through a symbolic link")
	}
	return rootReal, parentReal, filepath.Join(parentReal, filepath.Base(candidate)), nil
}

func prepareRoot(root string, create bool) (string, string, error) {
	rootPath, err := filepath.Abs(filepath.Clean(strings.TrimSpace(root)))
	if err != nil || strings.TrimSpace(root) == "" {
		return "", "", errors.New("invalid workspace root")
	}
	if create {
		if err = os.MkdirAll(rootPath, 0o755); err != nil {
			return "", "", fmt.Errorf("create workspace root: %w", err)
		}
	}
	rootReal, err := filepath.EvalSymlinks(rootPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve workspace root: %w", err)
	}
	if filepath.Clean(rootReal) != filepath.Clean(rootPath) {
		return "", "", errors.New("workspace root must not contain symbolic links")
	}
	return rootPath, rootReal, nil
}

func lexicalTarget(root, target string) (string, error) {
	value := strings.TrimSpace(target)
	if value == "" {
		value = "."
	}
	candidate := value
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate = filepath.Clean(candidate)
	if !within(root, candidate) {
		return "", errors.New("path escapes workspace root")
	}
	return candidate, nil
}

func within(root, target string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func openRegularFile(path string) (*os.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("stat file: %w", err)
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("target is not a regular file")
	}
	return file, nil
}

func readRegularFile(path string) ([]byte, error) {
	file, err := openRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("file exceeds the %d MiB read limit", MaxFileBytes>>20)
	}
	return data, nil
}

func decodeOneJSON(input io.Reader, target any) error {
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
