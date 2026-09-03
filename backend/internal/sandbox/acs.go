package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	acsapi "github.com/openkruise/agents-api/sdk/proto/api"
	acsprocess "github.com/openkruise/agents-api/sdk/proto/envd/process"
	acsruntime "github.com/openkruise/agents-api/sdk/runtime"
	acssandbox "github.com/openkruise/agents-api/sdk/sandbox"
)

const (
	acsDefaultTemplate       = "lester-agent"
	acsDefaultTimeoutSeconds = 3600
	acsMaxFileBytes          = 25 << 20
	acsMaxDirectoryItems     = 500
	acsMaxLineCharacters     = 2000
)

var errACSPathNotFound = errors.New("ACS path not found")

// ACSConfig describes an Alibaba Cloud ACS Agent Sandbox E2B endpoint. Native
// protocol is the production default; Private is supported for a single-domain
// internal gateway setup.
type ACSConfig struct {
	Domain         string
	Scheme         string
	Protocol       string
	APIURL         string
	SandboxBaseURL string
	APIKey         string
	Template       string
	TimeoutSeconds int
	RequestTimeout time.Duration
	RuntimePort    int
	Secure         bool
	AutoPause      bool
}

type acsCreateRequest struct {
	Template  string
	Timeout   int32
	Metadata  map[string]string
	Secure    bool
	AutoPause bool
}

type acsInfo struct {
	ID    string
	State string
}

type acsConnection struct {
	ID      string
	Runtime acsRuntime
}

type acsRuntime interface {
	Run(context.Context, string, string, func(string), func(string)) (*acsCommandResult, error)
	StartTerminal(context.Context, string) (acsProcess, error)
	Read(context.Context, string) ([]byte, error)
	Write(context.Context, string, []byte) error
	List(context.Context, string) ([]acsFileEntry, error)
	Stat(context.Context, string) (*acsFileEntry, error)
}

type acsProcess interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Close() error
}

type acsCommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type acsFileEntry struct {
	Name       string
	Path       string
	IsDir      bool
	Size       int64
	ModifiedAt time.Time
	IsSymlink  bool
}

type acsBackend interface {
	FindByLogicalID(context.Context, string) (string, bool, error)
	Create(context.Context, acsCreateRequest) (*acsConnection, error)
	Inspect(context.Context, string) (*acsInfo, error)
	Connect(context.Context, string, int32) (*acsConnection, error)
	Pause(context.Context, string) error
	Kill(context.Context, string) error
}

type ACSProvider struct {
	config  ACSConfig
	backend acsBackend
}

func NewACSProvider(config ACSConfig) (*ACSProvider, error) {
	config = normalizeACSConfig(config)
	if config.Domain == "" && config.APIURL == "" {
		return nil, errors.New("ACS_SANDBOX_DOMAIN or ACS_SANDBOX_API_URL is required for lifecycle management")
	}
	if config.Domain == "" && config.SandboxBaseURL == "" {
		return nil, errors.New("ACS_SANDBOX_DOMAIN or ACS_SANDBOX_BASE_URL is required for runtime access")
	}
	if config.APIKey == "" {
		return nil, errors.New("ACS_SANDBOX_API_KEY is required")
	}
	if config.Protocol != "native" && config.Protocol != "private" {
		return nil, errors.New("ACS_SANDBOX_PROTOCOL must be native or private")
	}
	if config.Scheme != "http" && config.Scheme != "https" {
		return nil, errors.New("ACS_SANDBOX_SCHEME must be http or https")
	}
	if strings.Contains(config.Domain, "://") {
		return nil, errors.New("ACS_SANDBOX_DOMAIN must not include a URL scheme")
	}
	backend, err := newOpenKruiseBackend(config)
	if err != nil {
		return nil, err
	}
	return &ACSProvider{config: config, backend: backend}, nil
}

func normalizeACSConfig(config ACSConfig) ACSConfig {
	config.Domain = strings.TrimSpace(strings.TrimSuffix(config.Domain, "/"))
	config.APIURL = strings.TrimSpace(strings.TrimSuffix(config.APIURL, "/"))
	config.SandboxBaseURL = strings.TrimSpace(strings.TrimSuffix(config.SandboxBaseURL, "/"))
	config.Scheme = strings.ToLower(strings.TrimSpace(config.Scheme))
	if config.Scheme == "" {
		config.Scheme = "https"
	}
	config.Protocol = strings.ToLower(strings.TrimSpace(config.Protocol))
	if config.Protocol == "" {
		config.Protocol = "native"
	}
	if config.Template == "" {
		config.Template = acsDefaultTemplate
	}
	if config.TimeoutSeconds <= 0 {
		config.TimeoutSeconds = acsDefaultTimeoutSeconds
	}
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = 60 * time.Second
	}
	if config.RuntimePort <= 0 {
		config.RuntimePort = 49983
	}
	return config
}

func (p *ACSProvider) Name() string { return "acs" }

func (p *ACSProvider) Create(ctx context.Context, opts CreateOptions) (*Sandbox, error) {
	if strings.TrimSpace(opts.ID) == "" {
		return nil, errors.New("sandbox logical id is required")
	}
	if existingID, found, err := p.backend.FindByLogicalID(ctx, opts.ID); err != nil {
		return nil, fmt.Errorf("recover ACS sandbox by logical id: %w", err)
	} else if found {
		if _, err = p.backend.Connect(ctx, existingID, int32(p.config.TimeoutSeconds)); err != nil {
			return nil, err
		}
		return &Sandbox{ID: opts.ID, Provider: p.Name(), ProviderRef: existingID, Status: "running", LastActiveAt: time.Now()}, nil
	}
	connection, err := p.backend.Create(ctx, acsCreateRequest{
		Template: p.config.Template, Timeout: int32(p.config.TimeoutSeconds),
		Metadata: map[string]string{"lester.logical_id": opts.ID}, Secure: p.config.Secure, AutoPause: p.config.AutoPause,
	})
	if err != nil {
		return nil, err
	}
	return &Sandbox{ID: opts.ID, Provider: p.Name(), ProviderRef: connection.ID, Status: "running", LastActiveAt: time.Now()}, nil
}

func (p *ACSProvider) Inspect(ctx context.Context, providerRef string) (*Sandbox, error) {
	info, err := p.backend.Inspect(ctx, providerRef)
	if err != nil {
		return nil, err
	}
	status := strings.ToLower(info.State)
	if status == "paused" {
		status = "suspended"
	}
	if status == "" {
		status = "unhealthy"
	}
	return &Sandbox{ID: info.ID, Provider: p.Name(), ProviderRef: info.ID, Status: status, LastActiveAt: time.Now()}, nil
}

func (p *ACSProvider) Start(ctx context.Context, providerRef string) error {
	_, err := p.backend.Connect(ctx, providerRef, int32(p.config.TimeoutSeconds))
	return err
}

func (p *ACSProvider) Suspend(ctx context.Context, providerRef string) error {
	return p.backend.Pause(ctx, providerRef)
}

func (p *ACSProvider) Resume(ctx context.Context, providerRef string) error {
	return p.Start(ctx, providerRef)
}

func (p *ACSProvider) Destroy(ctx context.Context, providerRef string) error {
	return p.backend.Kill(ctx, providerRef)
}

func (p *ACSProvider) runtime(ctx context.Context, providerRef string) (acsRuntime, error) {
	connection, err := p.backend.Connect(ctx, providerRef, int32(p.config.TimeoutSeconds))
	if err != nil {
		return nil, err
	}
	return connection.Runtime, nil
}

func (p *ACSProvider) Exec(ctx context.Context, providerRef string, command Command) (*CommandResult, error) {
	workDir, err := safeWorkDir(command.WorkDir)
	if err != nil {
		return nil, err
	}
	runtimeClient, err := p.runtime(ctx, providerRef)
	if err != nil {
		return nil, err
	}
	if _, err = runtimeClient.Run(ctx, "mkdir -p -- "+shellQuote(workDir), "/workspace", nil, nil); err != nil {
		return nil, fmt.Errorf("prepare sandbox work directory: %w", err)
	}
	timeoutSeconds := command.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 120
	}
	if timeoutSeconds > 600 {
		timeoutSeconds = 600
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	stdout, stderr := newBoundedCapture(commandCaptureLimit), newBoundedCapture(commandCaptureLimit)
	startedAt := time.Now()
	result, runErr := runtimeClient.Run(runCtx, command.Command, workDir,
		func(value string) { _, _ = stdout.Write([]byte(value)) },
		func(value string) { _, _ = stderr.Write([]byte(value)) },
	)
	if result == nil {
		return nil, runErr
	}
	if stdout.String() == "" && result.Stdout != "" {
		_, _ = stdout.Write([]byte(result.Stdout))
	}
	if stderr.String() == "" && result.Stderr != "" {
		_, _ = stderr.Write([]byte(result.Stderr))
	}
	// A non-zero program exit is a command result, matching DockerProvider.
	if runErr != nil && result.ExitCode == 0 {
		return nil, runErr
	}
	return &CommandResult{
		ExitCode: result.ExitCode, Stdout: stdout.String(), Stderr: stderr.String(), DurationMS: time.Since(startedAt).Milliseconds(),
		StdoutTruncated: stdout.Truncated(), StderrTruncated: stderr.Truncated(), StdoutOmittedBytes: stdout.OmittedBytes(), StderrOmittedBytes: stderr.OmittedBytes(),
	}, nil
}

func (p *ACSProvider) ReadFile(ctx context.Context, providerRef, workDir, filePath string) ([]byte, error) {
	_, target, err := scopedPath(workDir, filePath)
	if err != nil {
		return nil, err
	}
	runtimeClient, err := p.runtime(ctx, providerRef)
	if err != nil {
		return nil, err
	}
	if err = validateACSPath(ctx, runtimeClient, path.Clean(workDir), target, false); err != nil {
		return nil, err
	}
	data, err := runtimeClient.Read(ctx, target)
	if err == nil && len(data) > acsMaxFileBytes {
		return nil, fmt.Errorf("file exceeds the %d MiB read limit", acsMaxFileBytes>>20)
	}
	return data, err
}

func (p *ACSProvider) ReadFileLines(ctx context.Context, providerRef, workDir, filePath string, offset, limit int) (*FileLines, error) {
	if offset < 1 || limit < 1 || limit > 2000 {
		return nil, errors.New("invalid file line range")
	}
	data, err := p.ReadFile(ctx, providerRef, workDir, filePath)
	if err != nil {
		return nil, err
	}
	return readLinesFromBytes(data, offset, limit), nil
}

func (p *ACSProvider) WriteFile(ctx context.Context, providerRef, workDir, filePath string, data []byte) error {
	if len(data) > acsMaxFileBytes {
		return errors.New("file exceeds the 25 MiB write limit")
	}
	_, target, err := scopedPath(workDir, filePath)
	if err != nil {
		return err
	}
	runtimeClient, err := p.runtime(ctx, providerRef)
	if err != nil {
		return err
	}
	if err = validateACSPath(ctx, runtimeClient, path.Clean(workDir), target, true); err != nil {
		return err
	}
	return runtimeClient.Write(ctx, target, data)
}

func (p *ACSProvider) EditFile(ctx context.Context, providerRef, workDir, filePath string, request FileEditRequest) (*FileEditResult, error) {
	if request.OldString == "" {
		return nil, errors.New("old_string cannot be empty")
	}
	data, err := p.ReadFile(ctx, providerRef, workDir, filePath)
	if err != nil {
		return nil, err
	}
	currentSHA := sha256String(data)
	if request.ExpectedSHA256 != "" && !strings.EqualFold(request.ExpectedSHA256, currentSHA) {
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
	if err = p.WriteFile(ctx, providerRef, workDir, filePath, []byte(updated)); err != nil {
		return nil, err
	}
	return &FileEditResult{OK: true, Replacements: matches, SHA256: sha256String([]byte(updated))}, nil
}

func (p *ACSProvider) ListFiles(ctx context.Context, providerRef, workDir, filePath string) ([]FileEntry, error) {
	base, target, err := scopedPath(workDir, filePath)
	if err != nil {
		return nil, err
	}
	runtimeClient, err := p.runtime(ctx, providerRef)
	if err != nil {
		return nil, err
	}
	if err = validateACSPath(ctx, runtimeClient, base, target, false); err != nil {
		return nil, err
	}
	entries, err := runtimeClient.List(ctx, target)
	if err != nil {
		return nil, err
	}
	if len(entries) > acsMaxDirectoryItems {
		return nil, fmt.Errorf("directory contains more than %d entries; use bash with a narrower path or filter", acsMaxDirectoryItems)
	}
	result := make([]FileEntry, 0, len(entries))
	for _, entry := range entries {
		name := path.Base(entry.Name)
		if name == "." || name == "/" || name != entry.Name {
			continue
		}
		cleanPath := path.Join(target, name)
		relative := strings.TrimPrefix(strings.TrimPrefix(cleanPath, base), "/")
		result = append(result, FileEntry{Name: name, Path: relative, IsDir: entry.IsDir, Size: entry.Size, ModifiedAt: entry.ModifiedAt})
	}
	return result, nil
}

func (p *ACSProvider) OpenTerminal(ctx context.Context, providerRef, requestedWorkDir string) (TerminalSession, error) {
	workDir, err := safeWorkDir(requestedWorkDir)
	if err != nil {
		return nil, err
	}
	runtimeClient, err := p.runtime(ctx, providerRef)
	if err != nil {
		return nil, err
	}
	if _, err = runtimeClient.Run(ctx, "mkdir -p -- "+shellQuote(workDir), "/workspace", nil, nil); err != nil {
		return nil, fmt.Errorf("prepare terminal work directory: %w", err)
	}
	process, err := runtimeClient.StartTerminal(ctx, workDir)
	if err != nil {
		return nil, err
	}
	return &acsTerminal{process: process}, nil
}

type acsTerminal struct{ process acsProcess }

func (t *acsTerminal) Read(data []byte) (int, error)  { return t.process.Read(data) }
func (t *acsTerminal) Write(data []byte) (int, error) { return t.process.Write(data) }
func (t *acsTerminal) Close() error                   { return t.process.Close() }
func (t *acsTerminal) Resize(context.Context, int, int) error {
	return ErrTerminalResizeUnsupported
}

type openKruiseBackend struct {
	config *acssandbox.ConnectionConfig
	api    *acsapi.APIClient
}

func newOpenKruiseBackend(config ACSConfig) (*openKruiseBackend, error) {
	protocol := acssandbox.ProtocolNative
	if config.Protocol == "private" {
		protocol = acssandbox.ProtocolPrivate
	}
	connection := acssandbox.NewConnectionConfig(
		acssandbox.WithAPIKey(config.APIKey), acssandbox.WithDomain(config.Domain), acssandbox.WithScheme(config.Scheme),
		acssandbox.WithProtocol(protocol), acssandbox.WithRequestTimeout(config.RequestTimeout),
	)
	connection.RuntimePort = config.RuntimePort
	if config.APIURL != "" {
		acssandbox.WithAPIURL(config.APIURL)(connection)
	}
	if config.SandboxBaseURL != "" {
		acssandbox.WithSandboxBaseURL(config.SandboxBaseURL)(connection)
	}
	return &openKruiseBackend{config: connection, api: connection.NewAPIClient()}, nil
}

func (b *openKruiseBackend) Create(ctx context.Context, request acsCreateRequest) (*acsConnection, error) {
	body := acsapi.NewCreateSandboxRequest(request.Template)
	body.SetTimeout(request.Timeout)
	body.SetMetadata(request.Metadata)
	body.SetAutoPause(request.AutoPause)
	body.SetSecure(request.Secure)
	response, httpResponse, err := b.api.SandboxesApi.SandboxesPost(ctx).CreateSandboxRequest(*body).Execute()
	if err != nil {
		return nil, fmt.Errorf("create ACS sandbox: %w", err)
	}
	if httpResponse != nil && httpResponse.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("create ACS sandbox: HTTP %d", httpResponse.StatusCode)
	}
	return b.connection(response.GetSandboxID(), response.GetEnvdAccessToken()), nil
}

func (b *openKruiseBackend) FindByLogicalID(ctx context.Context, logicalID string) (string, bool, error) {
	items, httpResponse, err := b.api.SandboxesApi.V2SandboxesGet(ctx).State([]acsapi.SandboxState{
		acsapi.SANDBOXSTATE_RUNNING, acsapi.SANDBOXSTATE_PAUSED,
	}).Execute()
	if err != nil {
		return "", false, fmt.Errorf("list ACS sandboxes: %w", err)
	}
	if httpResponse != nil && httpResponse.StatusCode >= http.StatusMultipleChoices {
		return "", false, fmt.Errorf("list ACS sandboxes: HTTP %d", httpResponse.StatusCode)
	}
	var newestID string
	var newestStartedAt time.Time
	for _, item := range items {
		metadata, ok := item.GetMetadataOk()
		if !ok || metadata == nil || (*metadata)["lester.logical_id"] != logicalID {
			continue
		}
		if newestID == "" || item.GetStartedAt().After(newestStartedAt) {
			newestID, newestStartedAt = item.GetSandboxID(), item.GetStartedAt()
		}
	}
	return newestID, newestID != "", nil
}

func (b *openKruiseBackend) Inspect(ctx context.Context, providerRef string) (*acsInfo, error) {
	response, httpResponse, err := b.api.SandboxesApi.SandboxesSandboxIDGet(ctx, providerRef).Execute()
	if err != nil {
		if httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
			return nil, ErrSandboxNotFound
		}
		return nil, fmt.Errorf("inspect ACS sandbox: %w", err)
	}
	return &acsInfo{ID: response.GetSandboxID(), State: string(response.GetState())}, nil
}

func (b *openKruiseBackend) Connect(ctx context.Context, providerRef string, timeout int32) (*acsConnection, error) {
	body := acsapi.NewConnectSandbox(timeout)
	response, httpResponse, err := b.api.SandboxesApi.SandboxesSandboxIDConnectPost(ctx, providerRef).ConnectSandbox(*body).Execute()
	if err != nil {
		if httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
			return nil, ErrSandboxNotFound
		}
		return nil, fmt.Errorf("connect ACS sandbox: %w", err)
	}
	return b.connection(response.GetSandboxID(), response.GetEnvdAccessToken()), nil
}

func (b *openKruiseBackend) Pause(ctx context.Context, providerRef string) error {
	httpResponse, err := b.api.SandboxesApi.SandboxesSandboxIDPausePost(ctx, providerRef).Execute()
	if err != nil && httpResponse != nil && httpResponse.StatusCode == http.StatusConflict {
		return nil
	}
	if err != nil {
		if httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
			return ErrSandboxNotFound
		}
		return fmt.Errorf("pause ACS sandbox: %w", err)
	}
	return nil
}

func (b *openKruiseBackend) Kill(ctx context.Context, providerRef string) error {
	httpResponse, err := b.api.SandboxesApi.SandboxesSandboxIDDelete(ctx, providerRef).Execute()
	if err != nil && httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
		return nil
	}
	if err != nil {
		return fmt.Errorf("destroy ACS sandbox: %w", err)
	}
	return nil
}

func (b *openKruiseBackend) connection(id, runtimeToken string) *acsConnection {
	config := acsruntime.NewConfig(
		acsruntime.WithScheme(b.config.Scheme), acsruntime.WithRuntimePort(b.config.RuntimePort),
		acsruntime.WithSandboxBaseURL(b.config.GetSandboxURL(id)), acsruntime.WithRuntimeToken(runtimeToken),
		acsruntime.WithAPIKey(b.config.APIKey), acsruntime.WithRequestTimeout(b.config.RequestTimeout),
	)
	return &acsConnection{ID: id, Runtime: &openKruiseRuntime{client: acsruntime.NewWithConfig(id, config)}}
}

type openKruiseRuntime struct{ client *acsruntime.Client }

func (r *openKruiseRuntime) Run(ctx context.Context, command, workDir string, stdout, stderr func(string)) (*acsCommandResult, error) {
	result, err := r.client.Commands.Run(ctx, command, acsruntime.RunOpts{Cwd: workDir, OnStdout: stdout, OnStderr: stderr})
	if result == nil {
		return nil, err
	}
	return &acsCommandResult{Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: int(result.ExitCode)}, err
}

func (r *openKruiseRuntime) StartTerminal(ctx context.Context, workDir string) (acsProcess, error) {
	cwd, stdin := workDir, true
	request := connect.NewRequest(&acsprocess.StartRequest{
		Process: &acsprocess.ProcessConfig{Cmd: "/bin/bash", Args: []string{"-l", "-c", "exec /bin/bash -li"}, Cwd: &cwd},
		Stdin:   &stdin,
	})
	for key, value := range r.client.Config().SandboxHeaders(r.client.SandboxID()) {
		request.Header().Set(key, value)
	}
	stream, err := r.client.Commands.Rpc.Start(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("start ACS terminal: %w", err)
	}
	if !stream.Receive() {
		if streamErr := stream.Err(); streamErr != nil {
			return nil, fmt.Errorf("receive ACS terminal start event: %w", streamErr)
		}
		return nil, errors.New("ACS terminal closed without a start event")
	}
	start := stream.Msg().GetEvent().GetStart()
	if start == nil {
		return nil, errors.New("ACS terminal did not return a process ID")
	}
	reader, writer := io.Pipe()
	process := &openKruiseProcess{reader: reader, writer: writer, commands: r.client.Commands, pid: start.GetPid(), stream: stream}
	go process.readStream()
	return process, nil
}

func (r *openKruiseRuntime) Read(ctx context.Context, target string) ([]byte, error) {
	return r.client.Files.Read(ctx, target)
}

func (r *openKruiseRuntime) Write(ctx context.Context, target string, data []byte) error {
	_, err := r.client.Files.Write(ctx, target, data)
	return err
}

func (r *openKruiseRuntime) List(ctx context.Context, target string) ([]acsFileEntry, error) {
	entries, err := r.client.Files.List(ctx, target, 1)
	if err != nil {
		return nil, err
	}
	result := make([]acsFileEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, acsFileEntry{Name: entry.Name, Path: entry.Path, IsDir: entry.Type == acsruntime.EntryTypeDir, IsSymlink: entry.Type == acsruntime.EntryTypeSymlink, Size: entry.Size, ModifiedAt: entry.ModifiedTime})
	}
	return result, nil
}

func (r *openKruiseRuntime) Stat(ctx context.Context, target string) (*acsFileEntry, error) {
	entry, err := r.client.Files.GetInfo(ctx, target)
	if err != nil {
		var connectErr *connect.Error
		if errors.As(err, &connectErr) && connectErr.Code() == connect.CodeNotFound {
			return nil, errACSPathNotFound
		}
		return nil, err
	}
	return &acsFileEntry{Name: entry.Name, Path: entry.Path, IsDir: entry.Type == acsruntime.EntryTypeDir, IsSymlink: entry.Type == acsruntime.EntryTypeSymlink, Size: entry.Size, ModifiedAt: entry.ModifiedTime}, nil
}

type openKruiseProcess struct {
	reader   *io.PipeReader
	writer   *io.PipeWriter
	commands *acsruntime.Commands
	pid      uint32
	stream   *connect.ServerStreamForClient[acsprocess.StartResponse]
	once     sync.Once
}

func (p *openKruiseProcess) Read(data []byte) (int, error) { return p.reader.Read(data) }
func (p *openKruiseProcess) Write(data []byte) (int, error) {
	writeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.commands.SendStdin(writeCtx, p.pid, string(data)); err != nil {
		return 0, err
	}
	return len(data), nil
}
func (p *openKruiseProcess) Close() error {
	p.once.Do(func() {
		killCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = p.commands.Kill(killCtx, p.pid)
		cancel()
		p.stream.Close()
		_ = p.reader.Close()
		_ = p.writer.Close()
	})
	return nil
}

func (p *openKruiseProcess) readStream() {
	defer p.stream.Close()
	for p.stream.Receive() {
		event := p.stream.Msg().GetEvent()
		if event == nil {
			continue
		}
		if data := event.GetData(); data != nil {
			if len(data.GetStdout()) > 0 {
				_, _ = p.writer.Write(data.GetStdout())
			}
			if len(data.GetStderr()) > 0 {
				_, _ = p.writer.Write(data.GetStderr())
			}
		}
		if event.GetEnd() != nil {
			_ = p.writer.Close()
			return
		}
	}
	_ = p.writer.CloseWithError(p.stream.Err())
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func validateACSPath(ctx context.Context, runtimeClient acsRuntime, base, target string, allowMissing bool) error {
	base, target = path.Clean(base), path.Clean(target)
	if target != base && !strings.HasPrefix(target, base+"/") {
		return errors.New("path escapes conversation")
	}
	current := "/"
	for _, component := range strings.Split(strings.TrimPrefix(target, "/"), "/") {
		current = path.Join(current, component)
		entry, err := runtimeClient.Stat(ctx, current)
		if errors.Is(err, errACSPathNotFound) && allowMissing {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsSymlink {
			return fmt.Errorf("refusing to access symbolic link %q", current)
		}
	}
	return nil
}

func sha256String(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readLinesFromBytes(data []byte, offset, limit int) *FileLines {
	result := &FileLines{StartLine: offset}
	reader := bufio.NewReader(bytes.NewReader(data))
	lineNumber := 1
	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			result.TotalLines++
			if lineNumber >= offset && lineNumber < offset+limit {
				kept, omitted := truncateRunes(line, acsMaxLineCharacters)
				result.Lines = append(result.Lines, FileLine{Text: kept, OmittedCharacters: omitted})
			}
			lineNumber++
		}
		if readErr != nil {
			break
		}
	}
	return result
}

func truncateRunes(value string, limit int) (string, int) {
	count := utf8.RuneCountInString(value)
	if count <= limit {
		return value, 0
	}
	runes := []rune(value)
	return string(runes[:limit]), count - limit
}

func parsePositiveInt(value string, fallback int) (int, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("expected a positive integer, got %q", value)
	}
	return parsed, nil
}
