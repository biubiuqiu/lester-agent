package personal

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/google/uuid"
)

func TestPausedMemoryExcludedByEveryRetrievalProjection(t *testing.T) {
	paused := Memory{ID: uuid.New(), Topic: "Hidden", Content: "Secret", Kind: "preference", Paused: true}
	if selected := selectMemories([]Memory{paused}, "Secret"); len(selected) != 0 {
		t.Fatal("paused memory entered initial retrieval", selected)
	}
	if directory := memoryDirectory([]Memory{paused}); len(directory) != 0 {
		t.Fatal("paused memory entered directory", directory)
	}
	if _, err := recallMemories([]Memory{paused}, []string{paused.ID.String()}); !errors.Is(err, ErrModel) {
		t.Fatal("paused memory entered explicit lookup", err)
	}
}

func TestMemoryContextProjectionIsBoundedAndDeduplicatesExactRevisions(t *testing.T) {
	first := MemoryReference{ID: uuid.New(), Revision: 1, Topic: "One"}
	updated := first
	updated.Revision = 2
	refs := []MemoryReference{first, first, updated, {ID: uuid.Nil, Revision: 1}, {ID: uuid.New(), Revision: 0}}
	for range 40 {
		refs = append(refs, MemoryReference{ID: uuid.New(), Revision: 1})
	}
	raw, _ := json.Marshal([]any{map[string]any{"usage": map[string]any{"private": "accounting"}, "memory_context": refs}, map[string]any{"memory_context": refs}})
	got := projectMemoryContext(raw)
	if len(got) != 36 || got[0] != first || got[1] != updated {
		t.Fatal("reference receipt lost its bound or revision identity", got)
	}
	if got := projectMemoryContext([]byte(`[{"usage":{"tokens":1}}]`)); len(got) != 0 {
		t.Fatal("legacy usage fabricated memory references", got)
	}
}

func injectedModelMemories(t *testing.T, r model.ModelRequest) []Memory {
	t.Helper()
	const begin, end = "\n<reference_data>\n", "\n</reference_data>"
	start := strings.LastIndex(r.System, begin)
	finish := strings.LastIndex(r.System, end)
	if start < 0 || finish < start {
		t.Fatal("missing model reference data")
	}
	var data struct {
		Memories []Memory `json:"memories"`
	}
	if err := json.Unmarshal([]byte(r.System[start+len(begin):finish]), &data); err != nil {
		t.Fatal(err)
	}
	return data.Memories
}

func TestMemoryContextDoesNotCountDirectoryOnlyFacts(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := seedRecallMemory(t, f, nil, "Release", "November 12", false)
	f.client(t, func(r model.ModelRequest) modelReply {
		if !strings.Contains(r.System, id.String()) || len(injectedModelMemories(t, r)) != 0 {
			t.Fatal("expected an offered directory without supplied bodies")
		}
		return say("Hello")
	})
	turn, err := f.s.Chat(ctx, f.p, request("Hello"))
	if err != nil || len(turn.MemoryContext) != 0 {
		t.Fatal("directory-only memory was counted as supplied", turn, err)
	}
}

func TestMemoryContextTracksSuppliedBodiesAndReadReceipts(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	preference, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Language", Content: "Use French", Kind: "preference"})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Release", Content: "November 12", Kind: "work"})
	if err != nil {
		t.Fatal(err)
	}
	pass := 0
	f.client(t, func(r model.ModelRequest) modelReply {
		pass++
		v := say("Here is the saved detail")
		if pass == 1 {
			if !strings.Contains(r.System, detail.Memory.ID.String()) || strings.Contains(r.System, detail.Memory.Content) {
				t.Fatal("directory-only fact unexpectedly supplied a body")
			}
			v.MemoryLookup = &memoryLookup{IDs: []string{preference.Memory.ID.String(), detail.Memory.ID.String()}}
		} else if !strings.Contains(r.Messages[len(r.Messages)-1].Content, detail.Memory.Content) {
			t.Fatal("lookup did not supply the detailed fact")
		}
		return v
	})
	req := request("Hello")
	turn, err := f.s.Chat(ctx, f.p, req)
	if err != nil || len(turn.MemoryContext) != 2 {
		t.Fatal("immediate chat response lost supplied memory receipts", turn, err)
	}
	want := memoryReferences([]Memory{preference.Memory, detail.Memory})
	if !reflect.DeepEqual(turn.MemoryContext, want) {
		t.Fatal("directory IDs counted or repeat lookup duplicated a fact", turn.MemoryContext, want)
	}
	read, err := f.s.Turn(ctx, f.p, req.ID)
	if err != nil || !reflect.DeepEqual(read.MemoryContext, want) {
		t.Fatal("turn read disagrees with immediate reference receipt", read, err)
	}
	history, _, err := f.s.History(ctx, f.p, 0)
	if err != nil || len(history) != 1 || !reflect.DeepEqual(history[0].MemoryContext, want) {
		t.Fatal("history read disagrees with reference receipt", history, err)
	}
	raw, _ := json.Marshal(turn)
	if strings.Contains(string(raw), `"usage"`) || strings.Contains(string(raw), preference.Memory.Content) || strings.Contains(string(raw), detail.Memory.Content) {
		t.Fatal("public reference projection exposed bodies or provider accounting", string(raw))
	}
}

func TestMemoryContextRequiresAConfirmedProviderReturn(t *testing.T) {
	for _, scenario := range []string{"invalid-output", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			created, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Language", Content: "Use French", Kind: "preference"})
			if err != nil {
				t.Fatal(err)
			}
			f.s.Client = func(context.Context, uuid.UUID, uuid.UUID) (model.ModelClient, model.Deployment, error) {
				return fakeClient{generate: func(context.Context, model.ModelRequest) (*model.ModelResponse, error) {
					if scenario == "timeout" {
						return nil, context.DeadlineExceeded
					}
					return &model.ModelResponse{Content: "invalid JSON"}, nil
				}}, model.Deployment{ModelID: "fixture", ContextWindow: 32000}, nil
			}
			req := request("Hello")
			if _, err = f.s.Chat(ctx, f.p, req); !errors.Is(err, ErrModel) {
				t.Fatal("failed model unexpectedly accepted a reply", err)
			}
			turn, err := f.s.Turn(ctx, f.p, req.ID)
			if err != nil || turn.Status != "failed" {
				t.Fatal("failed turn receipt unavailable", turn, err)
			}
			if scenario == "timeout" && len(turn.MemoryContext) != 0 {
				t.Fatal("uncertain provider result fabricated a supplied receipt", turn.MemoryContext)
			}
			if scenario == "invalid-output" && (len(turn.MemoryContext) != 1 || turn.MemoryContext[0].ID != created.Memory.ID) {
				t.Fatal("successful provider return lost reference despite rejected output", turn.MemoryContext)
			}
		})
	}
}

func TestMemoryContextIsRecheckedBeforeEachProviderCall(t *testing.T) {
	for _, lookup := range []string{"memory", "search", "unread", "inspect", "revoked-session"} {
		t.Run(lookup, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			kind := "preference"
			if lookup == "memory" || lookup == "revoked-session" {
				kind = "work"
			}
			created, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Language", Content: "Use French", Kind: kind})
			if err != nil {
				t.Fatal(err)
			}
			conversationID := uuid.New()
			if _, err = f.s.DB.Exec(ctx, `INSERT INTO conversations(id,workspace_id,created_by,agent_slug,title) VALUES($1,$2,$3,'lester','Existing task')`, conversationID, f.p.WorkspaceID, f.p.UserID); err != nil {
				t.Fatal(err)
			}
			if _, err = f.s.DB.Exec(ctx, `INSERT INTO messages(conversation_id,role,content) VALUES($1,'assistant','Task result')`, conversationID); err != nil {
				t.Fatal(err)
			}
			passes := 0
			calls := f.client(t, func(r model.ModelRequest) modelReply {
				passes++
				if passes > 1 {
					return say("Unexpected second call")
				}
				if !strings.Contains(r.System, created.Memory.ID.String()) || strings.Contains(r.System, created.Memory.Content) != (kind == "preference") {
					t.Fatal("fixture did not initially offer the expected memory reference")
				}
				if lookup == "revoked-session" {
					if _, err := f.s.DB.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, f.p.UserID); err != nil {
						t.Fatal(err)
					}
				} else if _, err := f.s.SetMemoryPaused(ctx, f.p, created.Memory.ID, 1, true); err != nil {
					t.Fatal(err)
				}
				v := say("Checking the saved context")
				switch lookup {
				case "memory", "revoked-session":
					v.MemoryLookup = &memoryLookup{IDs: []string{created.Memory.ID.String()}}
				case "search":
					v.Task = taskAction{Action: "search", Title: "Existing"}
				case "unread":
					v.Task.Action = "unread"
				case "inspect":
					v.Task = taskAction{Action: "inspect", ConversationID: conversationID.String()}
				}
				return v
			})
			_, err = f.s.Chat(ctx, f.p, request("Hello"))
			want := ErrConflict
			if lookup == "revoked-session" {
				want = ErrUnauthorized
			}
			if !errors.Is(err, want) || calls.Load() != 1 {
				t.Fatalf("invalidated context started another provider call: err=%v calls=%d", err, calls.Load())
			}
		})
	}
}
