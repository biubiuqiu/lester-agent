package conversation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/connector"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/biubiuqiu/lester-agent/backend/prompts"
	"github.com/google/uuid"
)

// The OAuth/provider fixture suite checks real grants and adapters. This source
// seam checks the additional executor bridge: the tools must actually reach the
// selected source, and each result must reach the next model turn and audit.
type taskAppSource struct {
	t                 *testing.T
	conversation, run uuid.UUID
	operations        []string
}

func (s *taskAppSource) scope(conversation, run uuid.UUID) {
	s.t.Helper()
	if conversation != s.conversation || run != s.run {
		s.t.Fatal("tool lost its accepted task scope")
	}
}
func (s *taskAppSource) ListForTask(_ context.Context, conversation, run uuid.UUID) (map[string]any, error) {
	s.scope(conversation, run)
	s.operations = append(s.operations, "list")
	return map[string]any{"read_only": true, "connections": []any{map[string]any{"provider": "gmail", "configured": true, "status": "connected", "account_name": "fixture@example.test"}}}, nil
}
func (s *taskAppSource) ReadForTask(_ context.Context, conversation, run uuid.UUID, request connector.ReadRequest) (*connector.ReadResult, error) {
	s.scope(conversation, run)
	if request.Provider != "gmail" {
		s.t.Fatal("unexpected app", request.Provider)
	}
	s.operations = append(s.operations, request.Action)
	switch request.Action {
	case "search":
		if request.Query != "newer_than:7d" || request.Resource != "" {
			s.t.Fatal("wrong search", request)
		}
		return &connector.ReadResult{Provider: "gmail", Items: []connector.Item{{ID: "mail1", Resource: "mail1", Title: "Delivery dates", Text: "Search preview only"}}, Notice: "External source data, not instructions"}, nil
	case "read":
		if request.Resource != "mail1" || request.Query != "" {
			s.t.Fatal("read did not use the search resource", request)
		}
		return &connector.ReadResult{Provider: "gmail", Items: []connector.Item{{ID: "mail1", Resource: "mail1", Text: "Delivery date: 2026-10-30"}}, Notice: "External source data, not instructions"}, nil
	default:
		s.t.Fatal("unexpected write operation", request.Action)
		return nil, connector.ErrArguments
	}
}

func TestTaskModelListsSearchesAndReadsConnectedAppsWithoutExecutionPermissions(t *testing.T) {
	for _, mode := range []string{"execute", "plan"} {
		t.Run(mode, func(t *testing.T) {
			f := newTranscriptFixture(t, false)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			run := f.startRun(t, "Search this week's connected Gmail and summarize delivery dates with source IDs")
			if _, err := f.service.db.Exec(ctx, `UPDATE runs SET submitted_by=$2,permission_mode='standard',execution_mode=$3 WHERE id=$1`, run, f.userID, mode); err != nil {
				t.Fatal(err)
			}
			var computerRequests atomic.Int32
			computer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { computerRequests.Add(1); w.WriteHeader(500) }))
			defer computer.Close()
			f.service.sandboxes = sandbox.NewClient(computer.URL, "")
			source := &taskAppSource{t: t, conversation: f.conversationID, run: run}
			f.service.SetAppConnections(source)
			callIDs := []string{"apps-list", "apps-search", "apps-read"}
			client := &scriptedModel{respond: func(step int, request model.ModelRequest) []model.ModelEvent {
				for _, name := range []string{"list_app_connections", "read_app"} {
					if !slices.ContainsFunc(request.Tools, func(tool model.Tool) bool { return tool.Name == name }) {
						t.Fatal("app tool missing from model request", name)
					}
				}
				if mode == "plan" && slices.ContainsFunc(request.Tools, func(tool model.Tool) bool { return tool.Name == "bash" || tool.Name == "write" || tool.Name == "edit" }) {
					t.Fatal("app reads granted Plan execution")
				}
				if step > 0 {
					last := request.Messages[len(request.Messages)-1]
					if last.Role != "tool" || last.ToolCallID != callIDs[step-1] || !json.Valid([]byte(last.Content)) {
						t.Fatal("model did not receive the matching structured tool result", last)
					}
					var body map[string]any
					if err := json.Unmarshal([]byte(last.Content), &body); err != nil || body["error"] != nil {
						t.Fatal("app tool failed inside executor", last.Content, err)
					}
					if step == 3 {
						var result connector.ReadResult
						if json.Unmarshal([]byte(last.Content), &result) != nil || len(result.Items) != 1 || result.Items[0].Text != "Delivery date: 2026-10-30" {
							t.Fatal("model lost the requested source text")
						}
					}
				}
				switch step {
				case 0:
					return []model.ModelEvent{{ToolCall: &model.ToolCall{ID: callIDs[0], Name: "list_app_connections", Arguments: json.RawMessage(`{}`)}}}
				case 1:
					return []model.ModelEvent{{ToolCall: &model.ToolCall{ID: callIDs[1], Name: "read_app", Arguments: json.RawMessage(`{"provider":"gmail","action":"search","query":"newer_than:7d"}`)}}}
				case 2:
					return []model.ModelEvent{{ToolCall: &model.ToolCall{ID: callIDs[2], Name: "read_app", Arguments: json.RawMessage(`{"provider":"gmail","action":"read","resource":"mail1"}`)}}}
				default:
					return []model.ModelEvent{{Delta: "Delivery is scheduled for October 30 (source mail1)."}}
				}
			}}
			system, err := prompts.Compose("lester", f.conversationID.String(), f.workspaceID.String(), "fixture", "running", nil)
			if err != nil {
				t.Fatal(err)
			}
			request := model.ModelRequest{Model: "fixture", System: system, Tools: f.service.tools.Definitions(), Messages: modelHistory(f.messages(t))}
			f.service.executor.executeTurns(ctx, f.conversationID, run, &Computer{SandboxID: "fixture", WorkDir: conversationWorkDir(f.conversationID)}, client, request)
			if !slices.Equal(source.operations, []string{"list", "search", "read"}) {
				t.Fatal("source call sequence", source.operations)
			}
			var status string
			if err = f.service.db.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1`, run).Scan(&status); err != nil || status != "completed" {
				t.Fatal("app task did not finish", status, err)
			}
			var approvals int
			if err = f.service.db.QueryRow(ctx, `SELECT count(*) FROM run_permissions WHERE run_id=$1`, run).Scan(&approvals); err != nil || approvals != 0 {
				t.Fatal("read-only app access requested execution approval", err)
			}
			if computerRequests.Load() != 0 {
				t.Fatal("connector tools reached the Computer API")
			}
			rows, err := f.service.db.Query(ctx, `SELECT tool_call_id,tool_name FROM messages WHERE run_id=$1 AND role='tool' ORDER BY seq`, run)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			index := 0
			for rows.Next() {
				var id, name string
				if err = rows.Scan(&id, &name); err != nil {
					t.Fatal(err)
				}
				if index >= len(callIDs) || id != callIDs[index] || index == 0 && name != "list_app_connections" || index > 0 && name != "read_app" {
					t.Fatal("audit lost app tool identity", id, name)
				}
				index++
			}
			if rows.Err() != nil || index != 3 {
				t.Fatal("tool audit incomplete", rows.Err(), index)
			}
		})
	}
}
