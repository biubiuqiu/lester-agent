package sandbox

import (
	"context"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	acsprocess "github.com/openkruise/agents-api/sdk/proto/envd/process"
	"github.com/openkruise/agents-api/sdk/proto/envd/process/processconnect"
	acsruntime "github.com/openkruise/agents-api/sdk/runtime"
)

func TestACSTerminalPTYProtocol(t *testing.T) {
	fixture := &ptyProcessFixture{started: make(chan *connect.Request[acsprocess.StartRequest], 1), inputs: make(chan *connect.Request[acsprocess.SendInputRequest], 1), resizes: make(chan *connect.Request[acsprocess.UpdateRequest], 1)}
	_, handler := processconnect.NewProcessHandler(fixture)
	server := httptest.NewServer(handler)
	defer server.Close()
	client := acsruntime.NewWithConfig("fixture-sandbox", acsruntime.NewConfig(acsruntime.WithSandboxBaseURL(server.URL), acsruntime.WithRuntimeToken("fixture-runtime-token")))
	client.Commands.Rpc = processconnect.NewProcessClient(server.Client(), server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runtime := &openKruiseRuntime{client: client}
	terminal, err := runtime.StartTerminal(ctx, "/workspace/conversations/fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	start := <-fixture.started
	if start.Msg.GetPty().GetSize().GetCols() != 120 || start.Msg.GetPty().GetSize().GetRows() != 32 || !start.Msg.GetStdin() || start.Msg.GetProcess().GetCwd() != "/workspace/conversations/fixture" {
		t.Fatalf("interactive start = %v", start.Msg)
	}
	if start.Header().Get("X-Access-Token") != "fixture-runtime-token" {
		t.Fatal("PTY start missing runtime credentials")
	}
	output := make([]byte, len("真实 PTY\r\n"))
	if _, err := io.ReadFull(terminal, output); err != nil || string(output) != "真实 PTY\r\n" {
		t.Fatalf("PTY output = %q, %v", output, err)
	}
	if _, err := terminal.Write([]byte("\t\x03")); err != nil {
		t.Fatal(err)
	}
	input := <-fixture.inputs
	if string(input.Msg.GetInput().GetPty()) != "\t\x03" || input.Msg.GetProcess().GetPid() != 42 || input.Header().Get("X-Access-Token") != "fixture-runtime-token" {
		t.Fatalf("PTY input = %v", input.Msg)
	}
	if err := terminal.Resize(ctx, 93, 37); err != nil {
		t.Fatal(err)
	}
	resize := <-fixture.resizes
	if resize.Msg.GetPty().GetSize().GetCols() != 93 || resize.Msg.GetPty().GetSize().GetRows() != 37 || resize.Msg.GetProcess().GetPid() != 42 || resize.Header().Get("X-Access-Token") != "fixture-runtime-token" {
		t.Fatalf("PTY resize = %v", resize.Msg)
	}
}

type ptyProcessFixture struct {
	processconnect.UnimplementedProcessHandler
	started chan *connect.Request[acsprocess.StartRequest]
	inputs  chan *connect.Request[acsprocess.SendInputRequest]
	resizes chan *connect.Request[acsprocess.UpdateRequest]
}

func (f *ptyProcessFixture) Start(ctx context.Context, request *connect.Request[acsprocess.StartRequest], stream *connect.ServerStream[acsprocess.StartResponse]) error {
	f.started <- request
	if err := stream.Send(&acsprocess.StartResponse{Event: &acsprocess.ProcessEvent{Event: &acsprocess.ProcessEvent_Start{Start: &acsprocess.ProcessEvent_StartEvent{Pid: 42}}}}); err != nil {
		return err
	}
	if err := stream.Send(&acsprocess.StartResponse{Event: &acsprocess.ProcessEvent{Event: &acsprocess.ProcessEvent_Data{Data: &acsprocess.ProcessEvent_DataEvent{Output: &acsprocess.ProcessEvent_DataEvent_Pty{Pty: []byte("真实 PTY\r\n")}}}}}); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}
func (f *ptyProcessFixture) SendInput(_ context.Context, request *connect.Request[acsprocess.SendInputRequest]) (*connect.Response[acsprocess.SendInputResponse], error) {
	f.inputs <- request
	return connect.NewResponse(&acsprocess.SendInputResponse{}), nil
}
func (f *ptyProcessFixture) Update(_ context.Context, request *connect.Request[acsprocess.UpdateRequest]) (*connect.Response[acsprocess.UpdateResponse], error) {
	f.resizes <- request
	return connect.NewResponse(&acsprocess.UpdateResponse{}), nil
}
func (*ptyProcessFixture) SendSignal(context.Context, *connect.Request[acsprocess.SendSignalRequest]) (*connect.Response[acsprocess.SendSignalResponse], error) {
	return connect.NewResponse(&acsprocess.SendSignalResponse{}), nil
}
