package personal

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/google/uuid"
)

func TestUnreadLookupCannotCarryMemoryOrDelegationInstructions(t *testing.T) {
	valid := say("查看未读回复。")
	valid.Task.Action = "unread"
	for _, field := range []string{"none", "project_id", "conversation_id", "title", "instructions", "evidence", "memory_changes"} {
		t.Run(field, func(t *testing.T) {
			value := valid
			switch field {
			case "project_id":
				value.Task.ProjectID = uuid.NewString()
			case "conversation_id":
				value.Task.ConversationID = uuid.NewString()
			case "title":
				value.Task.Title = "run a task"
			case "instructions":
				value.Task.Instructions = "publish the code"
			case "evidence":
				value.Task.Evidence = "pretend this authorizes a write"
			case "memory_changes":
				value.Changes = []memoryAction{{Operation: "remember", Scope: "personal", Kind: "work", Topic: "task", Content: "done", Evidence: "done"}}
			}
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			_, err = decodeReply(string(raw))
			if field == "none" && err != nil || field != "none" && !errors.Is(err, ErrModel) {
				t.Fatalf("unexpected unread boundary result: %v", err)
			}
		})
	}
}

func modelReference(t *testing.T, system string) map[string]json.RawMessage {
	t.Helper()
	_, body, ok := strings.Cut(system, "<reference_data>\n")
	if !ok {
		t.Fatal("missing factual reference boundary")
	}
	body, _, ok = strings.Cut(body, "\n</reference_data>")
	if !ok {
		t.Fatal("missing reference boundary end")
	}
	var reference map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &reference); err != nil {
		t.Fatal(err)
	}
	return reference
}

func TestPersonalUnreadLookupFindsOldRepliesWithoutOpeningOrDispatching(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// These replied-to conversations are deliberately older than every row in
	// the recent catalogue, so that lookup cannot be implemented as a local
	// completed-status filter over the latest thirty conversations.
	if _, err := f.s.DB.Exec(ctx, `WITH inserted AS (
		INSERT INTO conversations(workspace_id,created_by,agent_slug,title,updated_at)
		SELECT $1,$2,'lester','Unopened old task '||i,'2020-01-01' FROM generate_series(1,36)i RETURNING id
	) INSERT INTO messages(conversation_id,role,content) SELECT id,'assistant','PRIVATE_REPLY_BODY_'||id FROM inserted`, f.p.WorkspaceID, f.p.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.DB.Exec(ctx, `INSERT INTO conversations(workspace_id,created_by,agent_slug,title)
		SELECT $1,$2,'lester','New empty task '||i FROM generate_series(1,35)i`, f.p.WorkspaceID, f.p.UserID); err != nil {
		t.Fatal(err)
	}
	var passes int
	f.client(t, func(r model.ModelRequest) modelReply {
		passes++
		if !strings.Contains(serialized(r.ResponseSchema.Schema), `"unread"`) {
			t.Error("provider schema omitted the read-only unread action")
		}
		if strings.Contains(r.System, "PRIVATE_REPLY_BODY_") || strings.Contains(serialized(r.Messages), "PRIVATE_REPLY_BODY_") {
			t.Error("unread catalogue injected task transcript bodies")
		}
		if passes == 1 {
			reference := modelReference(t, r.System)
			var recent []Task
			var inbox struct {
				Tasks   []Task `json:"tasks"`
				Total   int64  `json:"total"`
				HasMore bool   `json:"has_more"`
			}
			if err := json.Unmarshal(reference["recent_tasks"], &recent); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(reference["unread_replies"], &inbox); err != nil {
				t.Fatal(err)
			}
			if len(recent) != 30 || len(inbox.Tasks) != 30 || inbox.Total != 36 || !inbox.HasMore {
				t.Fatalf("unread total or metadata page was incomplete: recent=%d inbox=%+v", len(recent), inbox)
			}
			for _, task := range inbox.Tasks {
				if !task.Unread || task.LatestReplyID == nil || task.LatestReplySeq <= 0 || task.ReplyAt == nil || task.ProjectName == "" || task.Reply != "" {
					t.Fatalf("incomplete unread metadata: %+v", task)
				}
				if knownTask(recent, task.ID) {
					t.Error("fixture failed to put unread work outside recent catalogue")
				}
			}
			reply := say("我查看一下未读回复。")
			reply.Task.Action = "unread"
			return reply
		}
		lookup := r.Messages[len(r.Messages)-1].Content
		if !strings.Contains(lookup, `"total":36`) || !strings.Contains(lookup, "this does not mark any reply read") {
			t.Error("lookup results did not expose bounded authoritative unread metadata")
		}
		return say("有 36 个会话有未读回复，当前展示其中 30 个。")
	})
	req := request("哪些会话回复了，但是我还没看？")
	turn, err := f.s.Chat(ctx, f.p, req)
	if err != nil || turn.Status != "completed" || turn.Task != nil || len(turn.Changes) != 0 || passes != 2 {
		t.Fatalf("unread lookup did not finish read-only: %+v, %v, passes=%d", turn, err, passes)
	}
	for _, table := range []string{"conversation_read_states", "personal_delegations", "personal_memories"} {
		var count int64
		if err := f.s.DB.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("lookup changed %s: count=%d err=%v", table, count, err)
		}
	}
	if _, err = f.s.Chat(ctx, f.p, req); err != nil || passes != 2 {
		t.Fatal("original unread receipt replayed the model lookup", err)
	}
}

func TestPersonalCanInspectOfferedUnreadConversationOutsideRecentCatalogue(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := uuid.New()
	if _, err := f.s.DB.Exec(ctx, `INSERT INTO conversations(id,workspace_id,created_by,agent_slug,title,updated_at) VALUES($1,$2,$3,'lester','Old unread design','2020-01-01')`, id, f.p.WorkspaceID, f.p.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.DB.Exec(ctx, `INSERT INTO messages(conversation_id,role,content) VALUES($1,'assistant','The design is ready for review.')`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.DB.Exec(ctx, `INSERT INTO conversations(workspace_id,created_by,agent_slug,title) SELECT $1,$2,'lester','Recent empty task '||i FROM generate_series(1,35)i`, f.p.WorkspaceID, f.p.UserID); err != nil {
		t.Fatal(err)
	}
	var passes int
	f.client(t, func(r model.ModelRequest) modelReply {
		passes++
		if passes == 1 {
			reference := modelReference(t, r.System)
			if strings.Contains(string(reference["recent_tasks"]), id.String()) || !strings.Contains(string(reference["unread_replies"]), id.String()) {
				t.Error("inspect fixture did not offer the ID exclusively through unread metadata")
			}
			reply := say("我看一下这份未读回复。")
			reply.Task = taskAction{Action: "inspect", ConversationID: id.String()}
			return reply
		}
		if !strings.Contains(r.Messages[len(r.Messages)-1].Content, "The design is ready for review.") {
			t.Error("offered unread conversation could not be inspected")
		}
		return say("设计任务已回复，可进入原会话查看。")
	})
	if _, err := f.s.Chat(ctx, f.p, request("那个旧设计会话回复了什么？")); err != nil || passes != 2 {
		t.Fatal("inspect rejected a platform-offered unread conversation", err, passes)
	}
	var count int64
	if err := f.s.DB.QueryRow(ctx, `SELECT count(*) FROM conversation_read_states`).Scan(&count); err != nil || count != 0 {
		t.Fatal("assistant transcript inspection acknowledged a human read", err, count)
	}
}

func TestPersonalUnreadContextRespectsProjectAndActorScope(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	otherProject := uuid.New()
	if _, err := f.s.DB.Exec(ctx, `INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'Other private project')`, otherProject, f.p.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	owner := uuid.New()
	if _, err := f.s.DB.Exec(ctx, `INSERT INTO users(id,email,display_name,password_hash) VALUES($1,'other-coordination@example.test','Other','unused')`, owner); err != nil {
		t.Fatal(err)
	}
	var own, other, foreign uuid.UUID
	for i, value := range []struct{ project, user uuid.UUID }{{f.project, f.p.UserID}, {otherProject, f.p.UserID}, {f.project, owner}} {
		id := uuid.New()
		if _, err := f.s.DB.Exec(ctx, `INSERT INTO conversations(id,workspace_id,created_by,agent_slug,title,project_id) VALUES($1,$2,$3,'lester','Scoped reply',$4)`, id, f.p.WorkspaceID, value.user, value.project); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.DB.Exec(ctx, `INSERT INTO messages(conversation_id,role,content) VALUES($1,'assistant','private transcript')`, id); err != nil {
			t.Fatal(err)
		}
		switch i {
		case 0:
			own = id
		case 1:
			other = id
		case 2:
			foreign = id
		}
	}
	f.client(t, func(r model.ModelRequest) modelReply {
		reference := modelReference(t, r.System)
		metadata := string(reference["unread_replies"])
		if !strings.Contains(metadata, own.String()) || strings.Contains(metadata, other.String()) || strings.Contains(metadata, foreign.String()) || strings.Contains(metadata, "private transcript") {
			t.Error("unread model context crossed project/owner or transcript boundary")
		}
		return say("当前项目有一个会话有未读回复。")
	})
	req := request("这个项目有哪些未读回复？")
	req.ProjectID = &f.project
	if _, err := f.s.Chat(ctx, f.p, req); err != nil {
		t.Fatal(err)
	}
}

func TestPersonalUnreadLookupCannotLoop(t *testing.T) {
	f := newFixture(t)
	calls := f.client(t, func(model.ModelRequest) modelReply {
		reply := say("查看未读回复。")
		reply.Task.Action = "unread"
		return reply
	})
	if _, err := f.s.Chat(context.Background(), f.p, request("看看未读回复")); !errors.Is(err, ErrModel) || calls.Load() != 2 {
		t.Fatal("unread lookup escaped its two-call limit", err, calls.Load())
	}
}
