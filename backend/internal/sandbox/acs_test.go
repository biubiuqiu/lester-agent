package sandbox

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestACSProviderPersistsGeneratedReference(t *testing.T) {
	backend := &fakeACSBackend{createdID: "acs-generated-123", state: "running", runtime: &fakeACSRuntime{}}
	provider := &ACSProvider{config: normalizeACSConfig(ACSConfig{Template: "lester", Secure: true, AutoPause: true}), backend: backend}

	created, err := provider.Create(context.Background(), CreateOptions{ID: "user-42"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Provider != "acs" || created.ProviderRef != "acs-generated-123" || backend.logicalID != "user-42" {
		t.Fatalf("created sandbox = %#v, logical id = %q", created, backend.logicalID)
	}
}

func TestACSProviderRecoversCreatedSandboxByLogicalID(t *testing.T) {
	backend := &fakeACSBackend{existingID: "acs-existing-9", runtime: &fakeACSRuntime{}}
	provider := &ACSProvider{config: normalizeACSConfig(ACSConfig{}), backend: backend}
	created, err := provider.Create(context.Background(), CreateOptions{ID: "user-42"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ProviderRef != "acs-existing-9" || backend.createCalls != 0 || backend.connectedID != "acs-existing-9" {
		t.Fatalf("created = %#v, backend = %#v", created, backend)
	}
}

func TestACSProviderMapsPausedState(t *testing.T) {
	provider := &ACSProvider{config: normalizeACSConfig(ACSConfig{}), backend: &fakeACSBackend{createdID: "acs-1", state: "paused", runtime: &fakeACSRuntime{}}}
	item, err := provider.Inspect(context.Background(), "acs-1")
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != "suspended" {
		t.Fatalf("status = %q", item.Status)
	}
}

func TestACSProviderScopesFilesAndLimitsLines(t *testing.T) {
	runtimeClient := &fakeACSRuntime{data: []byte("first\n" + strings.Repeat("界", acsMaxLineCharacters+3) + "\nthird")}
	provider := &ACSProvider{config: normalizeACSConfig(ACSConfig{}), backend: &fakeACSBackend{createdID: "acs-1", state: "running", runtime: runtimeClient}}
	lines, err := provider.ReadFileLines(context.Background(), "acs-1", "/workspace/conversations/c1", "notes.txt", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines.Lines) != 1 || lines.Lines[0].OmittedCharacters != 3 || lines.TotalLines != 3 {
		t.Fatalf("lines = %#v", lines)
	}
	if runtimeClient.readPath != "/workspace/conversations/c1/notes.txt" {
		t.Fatalf("read path = %q", runtimeClient.readPath)
	}
	if _, err = provider.ReadFile(context.Background(), "acs-1", "/workspace/conversations/c1", "/workspace/conversations/other/secret"); err == nil {
		t.Fatal("expected cross-conversation path rejection")
	}
}

func TestACSProviderRejectsSymlinkTraversal(t *testing.T) {
	runtimeClient := &fakeACSRuntime{data: []byte("secret"), symlink: "/workspace/conversations/c1/link"}
	provider := &ACSProvider{config: normalizeACSConfig(ACSConfig{}), backend: &fakeACSBackend{createdID: "acs-1", state: "running", runtime: runtimeClient}}
	_, err := provider.ReadFile(context.Background(), "acs-1", "/workspace/conversations/c1", "link/secret.txt")
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("error = %v", err)
	}
}

func TestProviderFactoryDefaultsToDocker(t *testing.T) {
	t.Setenv("SANDBOX_PROVIDER", "")
	provider, err := NewProviderFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if provider.Name() != "docker" {
		t.Fatalf("provider = %q", provider.Name())
	}
}

func TestProviderFactoryBuildsSecureACSDefaults(t *testing.T) {
	t.Setenv("SANDBOX_PROVIDER", "acs")
	t.Setenv("ACS_SANDBOX_DOMAIN", "sandbox.example.com")
	t.Setenv("ACS_SANDBOX_API_KEY", "test-key")
	t.Setenv("ACS_SANDBOX_SECURE", "")
	t.Setenv("ACS_SANDBOX_AUTO_PAUSE", "")
	provider, err := NewProviderFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	acs, ok := provider.(*ACSProvider)
	if !ok || !acs.config.Secure || !acs.config.AutoPause || acs.config.Protocol != "native" {
		t.Fatalf("provider = %#v", provider)
	}
}

type fakeACSBackend struct {
	createdID   string
	existingID  string
	logicalID   string
	connectedID string
	createCalls int
	state       string
	runtime     acsRuntime
}

func (b *fakeACSBackend) Create(_ context.Context, request acsCreateRequest) (*acsConnection, error) {
	b.createCalls++
	b.logicalID = request.Metadata["lester.logical_id"]
	return &acsConnection{ID: b.createdID, Runtime: b.runtime}, nil
}
func (b *fakeACSBackend) FindByLogicalID(context.Context, string) (string, bool, error) {
	return b.existingID, b.existingID != "", nil
}
func (b *fakeACSBackend) Inspect(context.Context, string) (*acsInfo, error) {
	if b.createdID == "" {
		return nil, ErrSandboxNotFound
	}
	return &acsInfo{ID: b.createdID, State: b.state}, nil
}
func (b *fakeACSBackend) Connect(_ context.Context, id string, _ int32) (*acsConnection, error) {
	b.connectedID = id
	return &acsConnection{ID: id, Runtime: b.runtime}, nil
}
func (b *fakeACSBackend) Pause(context.Context, string) error { return nil }
func (b *fakeACSBackend) Kill(context.Context, string) error  { return nil }

type fakeACSRuntime struct {
	data     []byte
	readPath string
	symlink  string
}

func (r *fakeACSRuntime) Run(context.Context, string, string, func(string), func(string)) (*acsCommandResult, error) {
	return &acsCommandResult{}, nil
}
func (r *fakeACSRuntime) StartTerminal(context.Context, string) (acsProcess, error) {
	return nil, errors.New("not implemented")
}
func (r *fakeACSRuntime) Read(_ context.Context, path string) ([]byte, error) {
	r.readPath = path
	return r.data, nil
}
func (r *fakeACSRuntime) Write(context.Context, string, []byte) error { return nil }
func (r *fakeACSRuntime) List(context.Context, string) ([]acsFileEntry, error) {
	return nil, nil
}
func (r *fakeACSRuntime) Stat(_ context.Context, path string) (*acsFileEntry, error) {
	return &acsFileEntry{Path: path, IsSymlink: path == r.symlink}, nil
}
