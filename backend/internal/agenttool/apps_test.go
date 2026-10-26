package agenttool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/connector"
	"github.com/google/uuid"
)

type fakeApps struct {
	conversation, run uuid.UUID
	calls             int
}

func (f *fakeApps) ListForTask(_ context.Context, c, r uuid.UUID) (map[string]any, error) {
	f.conversation = c
	f.run = r
	f.calls++
	return map[string]any{}, nil
}
func (f *fakeApps) ReadForTask(_ context.Context, c, r uuid.UUID, _ connector.ReadRequest) (*connector.ReadResult, error) {
	f.conversation = c
	f.run = r
	f.calls++
	return &connector.ReadResult{}, nil
}
func TestAppToolsUseTaskIdentityAndRejectIdentityArguments(t *testing.T) {
	f := &fakeApps{}
	env := Environment{Apps: f, ConversationID: uuid.New(), RunID: uuid.New()}
	for _, tool := range []Handler{ListAppConnections{}, ReadApp{}} {
		if !PlanReadOnlyTool(tool.Definition().Name) {
			t.Fatal("read tool unavailable in Plan")
		}
		if _, err := tool.Execute(context.Background(), env, json.RawMessage(`{"user_id":"other"}`)); err == nil {
			t.Fatal("accepted identity argument")
		}
	}
	if f.calls != 0 {
		t.Fatal("called adapter with invalid arguments")
	}
	if _, err := (ReadApp{}).Execute(context.Background(), env, json.RawMessage(`{"provider":"gmail","action":"search","query":"project"}`)); err != nil {
		t.Fatal(err)
	}
	if f.conversation != env.ConversationID || f.run != env.RunID {
		t.Fatal("lost task identity")
	}
}
