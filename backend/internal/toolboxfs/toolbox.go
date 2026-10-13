package toolboxfs

import (
	"bufio"
	"crypto/rand"
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
	"unicode/utf8"
)

const (
	MaxFileBytes        = 25 << 20
	MaxDirectoryItems   = 500
	MaxReadLines        = 2000
	MaxCharactersInLine = 2000
	ProtocolVersion     = "2"
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
	Lines           []FileLine `json:"lines"`
	StartLine       int        `json:"start_line"`
	TotalLines      int        `json:"total_lines"`
	SHA256          string     `json:"sha256"`
	ByteSize        int64      `json:"byte_size"`
	EndsWithNewline bool       `json:"ends_with_newline"`
}

type CheckedWriteRequest struct {
	Content        []byte `json:"content"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
	CreateOnly     bool   `json:"create_only,omitempty"`
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
		_, err := io.WriteString(stdout, ProtocolVersion+"\n")
		return err
	}
	if args[0] == "prepare-input" {
		return prepareInput(args[1:], stdout)
	}
	args, staged, err := stagedInput(args)
	if err != nil {
		return err
	}
	if staged != nil {
		defer staged.Close()
		stdin = staged
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
	case "write-checked", "write-request":
		if len(remaining) != 0 {
			return errors.New("write-checked does not accept positional arguments")
		}
		var request CheckedWriteRequest
		if err = decodeOneJSON(io.LimitReader(stdin, (MaxFileBytes*4/3)+(1<<20)), &request); err != nil {
			return fmt.Errorf("decode checked write: %w", err)
		}
		if request.Content == nil {
			return errors.New("content must be supplied, including an explicit empty value")
		}
		var result *WriteResult
		var writeErr error
		if args[0] == "write-request" {
			if request.CreateOnly || request.ExpectedSHA256 != "" {
				return errors.New("write-request does not accept conditional fields")
			}
			result, writeErr = Write(root, target, strings.NewReader(string(request.Content)), "")
		} else {
			result, writeErr = WriteChecked(root, target, request)
		}
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

func prepareInput(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("prepare-input", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	name := flags.String("name", "", "temporary request basename")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || filepath.Base(*name) != *name || !strings.HasPrefix(*name, ".lester-input-") {
		return errors.New("invalid temporary request name")
	}
	root, err := os.OpenRoot(os.TempDir())
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := root.OpenFile(*name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, filepath.Join(os.TempDir(), *name))
	return err
}

// Remote runtimes with no stdin API stage a single native protocol request.
// Open and unlink it before execution so contents do not linger on failure.
func stagedInput(args []string) ([]string, *os.File, error) {
	filtered := []string{args[0]}
	var filename string
	for index := 1; index < len(args); index++ {
		if args[index] != "--input-file" {
			filtered = append(filtered, args[index])
			continue
		}
		if filename != "" || index+1 >= len(args) {
			return nil, nil, errors.New("invalid input-file option")
		}
		index++
		filename = args[index]
	}
	if filename == "" {
		return filtered, nil, nil
	}
	if args[0] != "edit" && args[0] != "write-checked" && args[0] != "write-request" {
		return nil, nil, errors.New("input-file is only supported for checked write and edit")
	}
	if filepath.Dir(filename) != os.TempDir() || !strings.HasPrefix(filepath.Base(filename), ".lester-input-") {
		return nil, nil, errors.New("input-file must be a Lester temporary request")
	}
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > (MaxFileBytes*4/3)+(1<<20) {
		return nil, nil, errors.New("invalid staged request file")
	}
	root, err := os.OpenRoot(os.TempDir())
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	file, err := root.OpenFile(filepath.Base(filename), os.O_RDONLY|nonBlockingOpenFlag|noFollowOpenFlag, 0)
	if err != nil {
		return nil, nil, err
	}
	if info, statErr := file.Stat(); statErr != nil || !info.Mode().IsRegular() || info.Size() > (MaxFileBytes*4/3)+(1<<20) || info.Mode().Perm()&0o077 != 0 {
		file.Close()
		return nil, nil, errors.New("invalid opened staged request")
	}
	if err = root.Remove(filepath.Base(filename)); err != nil {
		file.Close()
		return nil, nil, fmt.Errorf("remove staged request: %w", err)
	}
	return filtered, file, nil
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
	rootReal, resolved, err := resolveExisting(root, target)
	if err != nil {
		return err
	}
	file, err := openRegularFile(rootReal, resolved)
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
	rootReal, resolved, err := resolveExisting(root, target)
	if err != nil {
		return nil, err
	}
	file, err := openRegularFile(rootReal, resolved)
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
	if info, statErr := file.Stat(); statErr != nil || info.Size() > MaxFileBytes {
		return nil, errors.New("file exceeds the 25 MiB read limit or could not be inspected")
	}
	hash := sha256.New()
	counter := &countingReader{reader: io.TeeReader(io.LimitReader(file, MaxFileBytes+1), hash)}
	reader := bufio.NewReader(counter)
	lineNumber, characters, kept, omitted := 1, 0, 0, 0
	pageCharacters := 0
	pageFull := false
	var line strings.Builder
	finishLine := func() {
		result.TotalLines++
		if !pageFull && lineNumber >= offset && lineNumber < offset+limit {
			// Reserve line-prefix and explicit truncation-notice room. This is a
			// protocol page cap, not a file-size or token-window claim.
			needed := kept + 9
			if omitted > 0 {
				needed += len(fmt.Sprintf(" [line truncated: %d characters omitted]", omitted))
			}
			if pageCharacters+needed <= 30000 {
				result.Lines = append(result.Lines, FileLine{Text: line.String(), OmittedCharacters: omitted})
				pageCharacters += needed
			} else {
				pageFull = true
			}
		}
		lineNumber++
		characters, kept, omitted = 0, 0, 0
		line.Reset()
	}
	for {
		value, runeBytes, readErr := reader.ReadRune()
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if characters > 0 {
					finishLine()
				}
				break
			}
			return nil, fmt.Errorf("read file lines: %w", readErr)
		}
		if value == 0 || value == utf8.RuneError && runeBytes == 1 {
			return nil, errors.New("file contains binary or invalid UTF-8 data; inspect it with a format-aware command instead of text read")
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
	if counter.count > MaxFileBytes {
		return nil, errors.New("file exceeds the 25 MiB read limit")
	}
	result.SHA256 = hex.EncodeToString(hash.Sum(nil))
	result.ByteSize = counter.count
	result.EndsWithNewline = counter.last == '\n' && counter.count > 0
	return result, nil
}

type countingReader struct {
	reader io.Reader
	count  int64
	last   byte
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	r.count += int64(n)
	if n > 0 {
		r.last = buffer[n-1]
	}
	return n, err
}

func Write(root, target string, input io.Reader, expectedSHA256 string) (*WriteResult, error) {
	bytesWritten, sum, err := atomicWrite(root, target, input, expectedSHA256, false)
	if err != nil {
		return nil, err
	}
	return &WriteResult{OK: true, BytesWritten: bytesWritten, SHA256: sum}, nil
}

func WriteChecked(root, target string, request CheckedWriteRequest) (*WriteResult, error) {
	if request.CreateOnly == (request.ExpectedSHA256 != "") {
		return nil, errors.New("choose create_only or an expected_sha256")
	}
	if request.ExpectedSHA256 != "" && !validDigest(request.ExpectedSHA256) {
		return nil, errors.New("expected_sha256 must be a SHA256 digest")
	}
	count, sum, err := atomicWrite(root, target, strings.NewReader(string(request.Content)), request.ExpectedSHA256, request.CreateOnly)
	if err != nil {
		return nil, err
	}
	return &WriteResult{OK: true, SHA256: sum, BytesWritten: count}, nil
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func Edit(root, target string, request EditRequest) (*EditResult, error) {
	if request.OldString == "" {
		return nil, errors.New("old_string cannot be empty")
	}
	rootReal, resolved, err := resolveExisting(root, target)
	if err != nil {
		return nil, err
	}
	data, err := readRegularFile(rootReal, resolved)
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

func atomicWrite(root, target string, input io.Reader, expectedSHA256 string, createOnly bool) (int64, string, error) {
	rootReal, parent, destination, err := resolveWriteTarget(root, target)
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
	scoped, err := os.OpenRoot(rootReal)
	if err != nil {
		return 0, "", err
	}
	defer scoped.Close()
	parentRelative, err := filepath.Rel(rootReal, parent)
	if err != nil {
		return 0, "", err
	}
	parentRoot, err := scoped.OpenRoot(parentRelative)
	if err != nil {
		return 0, "", err
	}
	defer parentRoot.Close()
	temporaryName := ".lester-write-" + rand.Text()
	temporary, err := parentRoot.OpenFile(temporaryName, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return 0, "", fmt.Errorf("create temporary file: %w", err)
	}
	temporaryPath := filepath.Join(parent, temporaryName)
	cleanup := func() {
		_ = temporary.Close()
		_ = parentRoot.Remove(temporaryName)
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
		_ = parentRoot.Remove(temporaryName)
		return 0, "", fmt.Errorf("close temporary file: %w", err)
	}
	// All native-helper writers acquire the same stable per-path lock, including
	// legacy writes. Prepare bytes before taking it, then check and commit under
	// the lock so two conditional writers cannot both accept the same version.
	unlock, err := lockFile(root, destination)
	if err != nil {
		_ = parentRoot.Remove(temporaryName)
		return 0, "", err
	}
	defer unlock()
	defer parentRoot.Remove(temporaryName)
	_, currentParent, currentDestination, err := resolveWriteTarget(root, target)
	if err != nil || currentParent != parent || currentDestination != destination {
		return 0, "", errors.New("file path changed while preparing the write")
	}
	if info, statErr := os.Lstat(destination); statErr == nil && !info.Mode().IsRegular() {
		return 0, "", errors.New("refusing to replace a non-regular file or symbolic link")
	}
	if info, statErr := os.Lstat(destination); statErr == nil {
		// Preserve the mode at commit time, including permission-only changes
		// while bytes were being prepared; content hashes do not encode mode.
		if err = parentRoot.Chmod(temporaryName, info.Mode().Perm()); err != nil {
			return 0, "", fmt.Errorf("preserve file permissions: %w", err)
		}
	}
	if expectedSHA256 != "" {
		current, readErr := readRegularFile(rootReal, destination)
		if readErr != nil {
			return 0, "", fmt.Errorf("file changed before replacement: %w", readErr)
		}
		if !strings.EqualFold(expectedSHA256, sha256Hex(current)) {
			return 0, "", errors.New("file changed since it was read; read it again before modifying it")
		}
	}
	if createOnly {
		// A link publishes the prepared inode only if the destination is absent.
		// Unlike stat+rename this cannot overwrite a concurrent external creator.
		temporaryRelative, _ := filepath.Rel(rootReal, temporaryPath)
		destinationRelative, _ := filepath.Rel(rootReal, destination)
		if err = scoped.Link(temporaryRelative, destinationRelative); err != nil {
			return 0, "", fmt.Errorf("create-only write refused; the file may already exist, read it before overwriting: %w", err)
		}
		_ = parentRoot.Remove(temporaryName)
	} else {
		temporaryRelative, _ := filepath.Rel(rootReal, temporaryPath)
		destinationRelative, _ := filepath.Rel(rootReal, destination)
		err = scoped.Rename(temporaryRelative, destinationRelative)
		if err != nil && runtime.GOOS == "windows" {
			err = replaceFile(temporaryPath, destination)
		}
		if err != nil {
			_ = parentRoot.Remove(temporaryName)
			return 0, "", fmt.Errorf("replace file atomically: %w", err)
		}
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

func openRegularFile(root, target string) (*os.File, error) {
	scoped, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer scoped.Close()
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return nil, err
	}
	// Root prevents links swapped after resolveExisting from escaping the root;
	// NONBLOCK lets us reject a swapped named pipe without hanging while opening.
	file, err := scoped.OpenFile(relative, os.O_RDONLY|nonBlockingOpenFlag, 0)
	if err != nil {
		return nil, fmt.Errorf("open scoped file: %w", err)
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

func readRegularFile(root, target string) ([]byte, error) {
	file, err := openRegularFile(root, target)
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
