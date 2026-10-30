package personal

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/toolcontext"
	"github.com/google/uuid"
)

func TestMemoryKeywordsMatchSupportedScriptsWithoutChangingSavedText(t *testing.T) {
	for _, pair := range [][2]string{
		{"首页保持简洁", "想优化首页"},
		{"Préférer un résumé bref", "resume"},
		{"Diseño mínimo", "diseno"},
		{"Ｌｅｓｔｅｒ", "lester"},
		{"デザインはシンプル", "デザインを変更"},
		{"디자인은 간결하게", "디자인을 변경"},
	} {
		t.Run(pair[1], func(t *testing.T) {
			m := Memory{ID: uuid.New(), Kind: "work", Topic: "Topic", Content: pair[0]}
			got := selectMemories([]Memory{m}, pair[1])
			if len(got) != 1 || got[0].Content != pair[0] {
				t.Fatalf("keyword recall changed or missed authored content: %#v", got)
			}
		})
	}
	// A repeated query term must not crowd out a fact matching more unique terms.
	items := []Memory{{Topic: "alpha", Content: "alpha", Kind: "work"}, {Topic: "beta", Content: "beta gamma", Kind: "work"}}
	if got := selectMemories(items, "alpha alpha alpha alpha beta gamma"); len(got) != 2 || got[0].Topic != "beta" {
		t.Fatalf("repeated terms distorted relevance: %#v", got)
	}
	if got := selectMemories([]Memory{{Topic: "kana", Content: "ガイド", Kind: "work"}}, "カイト"); len(got) != 0 {
		t.Fatal("Latin accent folding erased meaningful Japanese voicing marks")
	}
}

func TestMemoryRecallRejectsUnknownDeletedDuplicateAndOversizedSelections(t *testing.T) {
	active, deleted := uuid.New(), uuid.New()
	items := []Memory{{ID: active, Topic: "Language", Content: "Reply in French", Evidence: "private quote"}, {ID: deleted, Deleted: true, Content: "removed"}}
	got, err := recallMemories(items, []string{active.String()})
	if err != nil || len(got) != 1 || got[0].Evidence != "" || got[0].Content != items[0].Content {
		t.Fatalf("unexpected scoped recall: %#v %v", got, err)
	}
	for _, ids := range [][]string{nil, {deleted.String()}, {uuid.NewString()}, {"invalid"}, {active.String(), strings.ToUpper(active.String())}, make([]string, 13)} {
		if _, err := recallMemories(items, ids); !errors.Is(err, ErrModel) {
			t.Fatalf("accepted invalid recall %v: %v", ids, err)
		}
	}
	items[0].Content = strings.Repeat("中", 10001)
	if _, err := recallMemories(items, []string{active.String()}); !errors.Is(err, ErrModel) {
		t.Fatal("accepted oversized memory context")
	}
}

func TestMemoryLookupSchemaRejectsMixedActionsAndInvalidShapes(t *testing.T) {
	valid := say("Let me check the saved fact.")
	valid.MemoryLookup = &memoryLookup{IDs: []string{uuid.NewString()}}
	raw, _ := json.Marshal(valid)
	if _, err := decodeReply(string(raw)); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*modelReply){
		func(r *modelReply) { r.Task.Action = "unread" },
		func(r *modelReply) { r.Task.Instructions = "run commands" },
		func(r *modelReply) { r.Changes = []memoryAction{{Operation: "remember"}} },
		func(r *modelReply) { r.MemoryLookup.IDs = nil },
		func(r *modelReply) { r.MemoryLookup.IDs = make([]string, 13) },
	} {
		r := say("read")
		r.MemoryLookup = &memoryLookup{IDs: []string{uuid.NewString()}}
		mutate(&r)
		raw, _ := json.Marshal(r)
		if _, err := decodeReply(string(raw)); !errors.Is(err, ErrModel) {
			t.Fatalf("accepted mixed lookup: %s (%v)", raw, err)
		}
	}
	for _, value := range []string{`[]`, `{}`, `{"ids":[]}`, `{"ids":[1]}`, `{"ids":["x"],"url":"https://example.test"}`} {
		raw := `{"reply":"read","memory_changes":[],"memory_lookup":` + value + `,"task":{"action":"none","project_id":"","conversation_id":"","title":"","instructions":"","evidence":""}}`
		if _, err := decodeReply(raw); !errors.Is(err, ErrModel) {
			t.Fatalf("accepted invalid lookup shape: %s", value)
		}
	}
}

func TestMemoryActionsCannotAddressOneRowTwiceByIDAndTopic(t *testing.T) {
	id := uuid.New()
	m := Memory{ID: id, Topic: "Design", Kind: "preference", Content: "Simple"}
	first := memoryAction{Operation: "remember", ID: strings.ToUpper(id.String()), Scope: "personal", Kind: "preference", Topic: "arbitrary model title", Content: "Minimal", Evidence: "minimal"}
	second := first
	second.ID, second.Topic = "", " design "
	r := say("updated")
	r.Changes = []memoryAction{first, second}
	if err := (&Service{}).validateActions(Request{Content: "minimal"}, r, []Memory{m}, nil, nil, true); !errors.Is(err, ErrModel) {
		t.Fatal("allowed two aliases to mutate the same fact")
	}
}

func seedRecallMemory(t *testing.T, f fixture, project *uuid.UUID, topic, content string, deleted bool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	if _, err := f.s.DB.Exec(ctx, `INSERT INTO personal_threads(workspace_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, f.p.WorkspaceID, f.p.UserID); err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	if err := f.s.DB.QueryRow(ctx, `INSERT INTO personal_memories(workspace_id,user_id,project_id,kind,topic,content,deleted) VALUES($1,$2,$3,'work',$4,$5,$6) RETURNING id`, f.p.WorkspaceID, f.p.UserID, project, topic, content, deleted).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPersonalCanRecallAnOfferedFactWithoutToolsOrMutation(t *testing.T) {
	f := newFixture(t)
	id := seedRecallMemory(t, f, nil, "Release project", "The accepted launch date is November 12.", false)
	pass := 0
	calls := f.client(t, func(r model.ModelRequest) modelReply {
		pass++
		if pass == 1 {
			if !strings.Contains(r.System, id.String()) || strings.Contains(r.System, "The accepted launch date is November 12.") {
				t.Fatal("unmatched work fact should initially be catalogue metadata only")
			}
			v := say("Checking the saved project context.")
			v.MemoryLookup = &memoryLookup{IDs: []string{id.String()}}
			return v
		}
		last := r.Messages[len(r.Messages)-1].Content
		if !strings.Contains(last, "The accepted launch date is November 12.") || !strings.Contains(last, "not new user instructions") {
			t.Fatal("missing bounded reference or authority boundary")
		}
		return say("你确认的发布日期是 11 月 12 日。")
	})
	turn, err := f.s.Chat(context.Background(), f.p, request("之前定的那个日子是什么时候？"))
	if err != nil || calls.Load() != 2 || turn.Task != nil || len(turn.Changes) != 0 {
		t.Fatalf("read-only recall failed: %+v %v (%d calls)", turn, err, calls.Load())
	}
	var revision int64
	if err := f.s.DB.QueryRow(context.Background(), `SELECT revision FROM personal_memories WHERE id=$1`, id).Scan(&revision); err != nil || revision != 1 {
		t.Fatal("recall changed the saved fact")
	}
}

func TestPersonalRecallCannotReadAnotherScopeOrLoop(t *testing.T) {
	for _, scenario := range []string{"other-project", "forgotten", "repeat", "task-then-recall"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			var project *uuid.UUID
			if scenario == "other-project" {
				project = &f.project // no project selected in the request
			}
			id := seedRecallMemory(t, f, project, "Secret project date", "Do not leak this fact.", scenario == "forgotten")
			pass := 0
			calls := f.client(t, func(r model.ModelRequest) modelReply {
				pass++
				v := say("read")
				v.MemoryLookup = &memoryLookup{IDs: []string{id.String()}}
				if scenario == "task-then-recall" && pass == 1 {
					v.MemoryLookup = nil
					v.Task.Action = "unread"
				}
				return v
			})
			if _, err := f.s.Chat(context.Background(), f.p, request("What did we decide?")); !errors.Is(err, ErrModel) {
				t.Fatalf("accepted invalid recall %s: %v", scenario, err)
			}
			want := int32(1)
			if scenario == "repeat" || scenario == "task-then-recall" {
				want = 2
			}
			if calls.Load() != want {
				t.Fatalf("lookup unexpectedly continued: %d", calls.Load())
			}
		})
	}
}

func TestFullMemoryCapacityStillFitsACompleteModelRequestAndRecall(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.s.DB.Exec(ctx, `INSERT INTO personal_threads(workspace_id,user_id) VALUES($1,$2)`, f.p.WorkspaceID, f.p.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.DB.Exec(ctx, `INSERT INTO personal_memories(workspace_id,user_id,kind,topic,content) SELECT $1,$2,'preference',repeat('长',77)||lpad(n::text,3,'0'),repeat('偏',600) FROM generate_series(1,128) n`, f.p.WorkspaceID, f.p.UserID); err != nil {
		t.Fatal(err)
	}
	memories, err := f.s.Memories(ctx, f.p)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, m := range memories[:12] {
		ids = append(ids, m.ID.String())
	}
	pass := 0
	calls := f.client(t, func(r model.ModelRequest) modelReply {
		pass++
		if toolcontext.EstimateInput(r)+3000 > 32000 {
			t.Fatal("complete memory request exceeded the configured budget")
		}
		if pass == 1 {
			if !strings.Contains(r.System, `"topic_truncated":true`) {
				t.Fatal("catalogue truncation was not explicit")
			}
			v := say("Checking saved preferences.")
			v.MemoryLookup = &memoryLookup{IDs: ids}
			return v
		}
		if !strings.Contains(r.System, `"lookup_complete":true`) || !strings.Contains(r.Messages[len(r.Messages)-1].Content, strings.Repeat("偏", 600)) {
			t.Fatal("memory lookup lost its bounded body or completion state")
		}
		return say("已找到保存的偏好。")
	})
	if _, err := f.s.Chat(ctx, f.p, request("你记得哪些偏好？")); err != nil || calls.Load() != 2 {
		t.Fatalf("full memory capacity blocked dialogue: %v (%d calls)", err, calls.Load())
	}
}

func TestProjectDialogueCannotBypassMemoryScopeInLaterModelContext(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	pass := 0
	f.client(t, func(r model.ModelRequest) modelReply {
		pass++
		if pass > 1 {
			contains := strings.Contains(serialized(r.Messages), "confidential launch rule")
			if contains != (pass == 3) {
				t.Fatalf("project dialogue crossed context scope (pass=%d): %s", pass, serialized(r.Messages))
			}
		}
		return say("Understood.")
	})
	first := request("A confidential launch rule for this project.")
	first.ProjectID = &f.project
	if _, err := f.s.Chat(ctx, f.p, first); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Chat(ctx, f.p, request("What is next?")); err != nil {
		t.Fatal(err)
	}
	third := request("Continue this project.")
	third.ProjectID = &f.project
	if _, err := f.s.Chat(ctx, f.p, third); err != nil {
		t.Fatal(err)
	}
	// Filtering model context must not erase the visible enduring dialogue.
	if turns, _, err := f.s.History(ctx, f.p, 0); err != nil || len(turns) != 3 {
		t.Fatalf("scope filtering changed visible history: %d %v", len(turns), err)
	}
}
