package sandbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	acsprocess "github.com/openkruise/agents-api/sdk/proto/envd/process"
	"github.com/openkruise/agents-api/sdk/proto/envd/process/processconnect"
	acsruntime "github.com/openkruise/agents-api/sdk/runtime"
)

func TestGitHubTransferServiceConfinesNativeOperationsAndRequiresAuthentication(t *testing.T) {
	id := uuid.NewString()
	dir := "/workspace/conversations/" + uuid.NewString()
	input := GitHubTransferRequest{Operation: "export", Directory: "repo", Manifest: ".lester/github/" + id + "-paths.json", Output: ".lester/github/" + id + ".json"}
	provider := &transferTestProvider{}
	server := httptest.NewServer(NewServiceHandler(provider, "fixture-token").Router())
	defer server.Close()
	client := NewClient(server.URL, "fixture-token")
	got, err := client.GitHubTransfer(context.Background(), "computer", dir, input)
	if err != nil || got.Stdout != "native receipt" || provider.calls != 1 || provider.dir != dir {
		t.Fatalf("native operation did not round trip: %+v %v", got, err)
	}
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/sandboxes/computer/github-transfer?work_dir="+dir, strings.NewReader(`{"operation":"export"}`))
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("native endpoint did not require service authentication")
	}
	for _, body := range []string{
		`{"operation":"bash","directory":"repo"}`,
		`{"operation":"export","directory":"repo","command":"touch /tmp/bypass"}`,
		`{"operation":"export","directory":"../escape","manifest":"` + input.Manifest + `","output":"` + input.Output + `"}`,
		`{"operation":"export","directory":"repo","manifest":".lester/github/other-paths.json","output":"` + input.Output + `"}`,
	} {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/sandboxes/computer/github-transfer?work_dir="+dir, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer fixture-token")
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 400 {
			t.Fatalf("unsafe request accepted: %s status%d", body, response.StatusCode)
		}
	}
	if provider.calls != 1 {
		t.Fatal("unsafe operation reached provider")
	}
	input.Directory = "repo"
	if _, err := client.GitHubTransfer(context.Background(), "computer", "/workspace", input); err == nil {
		t.Fatal("unscoped workspace transfer accepted")
	}
}

type transferTestProvider struct {
	Provider
	calls int
	dir   string
}

func (p *transferTestProvider) GitHubTransfer(_ context.Context, _ string, dir string, _ GitHubTransferRequest) (*CommandResult, error) {
	p.calls++
	p.dir = dir
	return &CommandResult{ExitCodeKnown: true, Status: "exited", Stdout: "native receipt"}, nil
}

func TestACSNativeToolboxStartsFixedExecutableWithoutShellAndKeepsRuntimeHeaders(t *testing.T) {
	fixture := &nativeToolboxProcessFixture{}
	_, handler := processconnect.NewProcessHandler(fixture)
	server := httptest.NewServer(handler)
	defer server.Close()
	client := acsruntime.NewWithConfig("fixture", acsruntime.NewConfig(acsruntime.WithSandboxBaseURL(server.URL), acsruntime.WithRuntimeToken("fixture-runtime-token")))
	client.Commands.Rpc = processconnect.NewProcessClient(server.Client(), server.URL)
	runtime := &openKruiseRuntime{client: client}
	result, err := runtime.RunToolbox(context.Background(), []string{"version"}, nil, nil)
	if err != nil || result.Stdout != "2\n" {
		t.Fatalf("native process result: %+v %v", result, err)
	}
	request := fixture.request
	if request == nil || request.Header().Get("X-Access-Token") != "fixture-runtime-token" {
		t.Fatal("missing native runtime authentication")
	}
	config := request.Msg.GetProcess()
	if config.Cmd != toolboxContainerPath || len(config.Args) != 1 || config.Args[0] != "version" || config.GetCwd() != "/workspace" || config.Envs["HOME"] != "/nonexistent" || config.Envs["PATH"] != "/usr/bin:/bin" {
		t.Fatalf("native call must use fixed argv with no shell/profile: %+v", config)
	}
}

type nativeToolboxProcessFixture struct {
	processconnect.UnimplementedProcessHandler
	request *connect.Request[acsprocess.StartRequest]
}

func (f *nativeToolboxProcessFixture) Start(_ context.Context, request *connect.Request[acsprocess.StartRequest], stream *connect.ServerStream[acsprocess.StartResponse]) error {
	f.request = request
	if err := stream.Send(&acsprocess.StartResponse{Event: &acsprocess.ProcessEvent{Event: &acsprocess.ProcessEvent_Start{Start: &acsprocess.ProcessEvent_StartEvent{Pid: 42}}}}); err != nil {
		return err
	}
	if err := stream.Send(&acsprocess.StartResponse{Event: &acsprocess.ProcessEvent{Event: &acsprocess.ProcessEvent_Data{Data: &acsprocess.ProcessEvent_DataEvent{Output: &acsprocess.ProcessEvent_DataEvent_Stdout{Stdout: []byte("2\n")}}}}}); err != nil {
		return err
	}
	return stream.Send(&acsprocess.StartResponse{Event: &acsprocess.ProcessEvent{Event: &acsprocess.ProcessEvent_End{End: &acsprocess.ProcessEvent_EndEvent{ExitCode: 0}}}})
}
