package personal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func inboxConversation(t *testing.T, f fixture, owner uuid.UUID, project uuid.UUID, title string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := f.s.DB.Exec(context.Background(), `INSERT INTO conversations(id,workspace_id,created_by,project_id,agent_slug,title) VALUES($1,$2,$3,$4,'lester',$5)`, id, f.p.WorkspaceID, owner, project, title); err != nil {
		t.Fatal(err)
	}
	return id
}

func inboxMessage(t *testing.T, f fixture, conversation uuid.UUID, role, content, metadata, calls string) (uuid.UUID, int64) {
	t.Helper()
	var id uuid.UUID
	var seq int64
	if err := f.s.DB.QueryRow(context.Background(), `INSERT INTO messages(conversation_id,role,content,metadata,tool_calls) VALUES($1,$2,$3,$4::jsonb,$5::jsonb) RETURNING id,seq`, conversation, role, content, metadata, calls).Scan(&id, &seq); err != nil {
		t.Fatal(err)
	}
	return id, seq
}

func inboxActor(t *testing.T, f fixture) auth.Principal {
	t.Helper()
	p := auth.Principal{UserID: uuid.New(), WorkspaceID: f.p.WorkspaceID, SessionHash: []byte(uuid.NewString())}
	for _, input := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users(id,email,display_name,password_hash) VALUES($1,$2,'Other member','unused')`, []any{p.UserID, p.UserID.String() + "@example.test"}},
		{`INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2)`, []any{p.WorkspaceID, p.UserID}},
		{`INSERT INTO sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 day')`, []any{p.UserID, p.SessionHash}},
	} {
		if _, err := f.s.DB.Exec(context.Background(), input.sql, input.args...); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func TestInboxAcknowledgesOnlyTheObservedPublicReplyAndNeverRegresses(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	conversation := inboxConversation(t, f, f.p.UserID, f.project, "Review the landing page")
	first, firstSeq := inboxMessage(t, f, conversation, "assistant", "First real reply", `{}`, `[]`)
	before, err := f.s.UnreadTasks(ctx, f.p, nil, "")
	if err != nil || before.Total != 1 || len(before.Tasks) != 1 || !before.Tasks[0].Unread || before.Tasks[0].LatestReplyID == nil || *before.Tasks[0].LatestReplyID != first || before.Tasks[0].LatestReplySeq != firstSeq || before.Tasks[0].ReplyAt == nil {
		t.Fatalf("missing actual unread boundary: %+v %v", before, err)
	}
	// Listing and ordinary task queries are reads. They must not clear attention.
	if _, err = f.s.Tasks(ctx, f.p); err != nil {
		t.Fatal(err)
	}
	var markers int
	if err = f.s.DB.QueryRow(ctx, `SELECT count(*) FROM conversation_read_states`).Scan(&markers); err != nil || markers != 0 {
		t.Fatal("a list read acknowledged human reading", markers, err)
	}
	second, secondSeq := inboxMessage(t, f, conversation, "assistant", "New reply racing with the old acknowledgement", `{}`, `[]`)
	if seq, e := f.s.MarkRead(ctx, f.p, conversation, first); e != nil || seq != firstSeq {
		t.Fatal("first acknowledgement failed", seq, e)
	}
	after, err := f.s.UnreadTasks(ctx, f.p, nil, "")
	if err != nil || after.Total != 1 || after.Tasks[0].LatestReplyID == nil || *after.Tasks[0].LatestReplyID != second {
		t.Fatal("old acknowledgement hid a newer reply", after, err)
	}
	if seq, e := f.s.MarkRead(ctx, f.p, conversation, second); e != nil || seq != secondSeq {
		t.Fatal("new acknowledgement failed", seq, e)
	}
	if seq, e := f.s.MarkRead(ctx, f.p, conversation, first); e != nil || seq != secondSeq {
		t.Fatal("delayed acknowledgement regressed the cursor", seq, e)
	}
	after, err = f.s.UnreadTasks(ctx, f.p, nil, "")
	if err != nil || after.Total != 0 || len(after.Tasks) != 0 || after.NextCursor != "" {
		t.Fatal("acknowledged reply stayed unread", after, err)
	}
	tasks, err := f.s.Tasks(ctx, f.p)
	if err != nil || len(tasks) != 1 || tasks[0].Unread || tasks[0].LatestReplySeq != secondSeq {
		t.Fatal("recent task state disagrees with inbox", tasks, err)
	}
}

func TestInboxIgnoresInternalPartialEmptyAndForeignMessages(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	conversation := inboxConversation(t, f, f.p.UserID, f.project, "One task")
	var notPublic []uuid.UUID
	for _, entry := range []struct{ role, content, metadata, calls string }{
		{"user", "A user instruction", `{}`, `[]`},
		{"tool", "Private tool output", `{}`, `[]`},
		{"assistant", "Calling a tool", `{}`, `[{"id":"call-1"}]`},
		{"assistant", "Partial draft", `{"incomplete":true}`, `[]`},
		{"assistant", "  \n\t ", `{}`, `[]`},
		{"system", "Internal system note", `{}`, `[]`},
	} {
		id, _ := inboxMessage(t, f, conversation, entry.role, entry.content, entry.metadata, entry.calls)
		notPublic = append(notPublic, id)
	}
	state, err := f.s.UnreadTasks(ctx, f.p, nil, "")
	if err != nil || state.Total != 0 {
		t.Fatal("non-public messages counted as unread replies", state, err)
	}
	for _, id := range notPublic {
		if _, err = f.s.MarkRead(ctx, f.p, conversation, id); !errors.Is(err, ErrNotFound) {
			t.Fatal("non-public message accepted as a read boundary", id, err)
		}
	}
	other := inboxActor(t, f)
	foreignConversation := inboxConversation(t, f, other.UserID, f.project, "Other member task")
	foreignReply, _ := inboxMessage(t, f, foreignConversation, "assistant", "Other member response", `{}`, `[]`)
	if _, err = f.s.MarkRead(ctx, f.p, foreignConversation, foreignReply); !errors.Is(err, ErrNotFound) {
		t.Fatal("member could acknowledge another owner's task", err)
	}
	if _, err = f.s.MarkRead(ctx, f.p, conversation, foreignReply); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign message ID could advance an owned task", err)
	}
	if _, err = f.s.MarkRead(ctx, f.p, conversation, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatal("invented message ID accepted", err)
	}
	if _, err = f.s.MarkRead(ctx, f.p, conversation, uuid.Nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("empty message ID accepted", err)
	}
	state, err = f.s.UnreadTasks(ctx, f.p, nil, "")
	if err != nil || state.Total != 0 {
		t.Fatal("foreign reply leaked into inbox", state, err)
	}
}

func TestInboxPaginatesAllUnreadConversationsBeyondRecentTasksAndScopesProjects(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for i := 0; i < 35; i++ {
		id := inboxConversation(t, f, f.p.UserID, f.project, fmt.Sprintf("Archived reply %d", i))
		reply, _ := inboxMessage(t, f, id, "assistant", "Unread original result", `{}`, `[]`)
		if _, err := f.s.DB.Exec(ctx, `UPDATE conversations SET updated_at='2020-01-01' WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.DB.Exec(ctx, `UPDATE messages SET created_at='2026-01-01' WHERE id=$1`, reply); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 35; i++ {
		inboxConversation(t, f, f.p.UserID, f.project, fmt.Sprintf("Recent idle %d", i))
	}
	tasks, err := f.s.Tasks(ctx, f.p)
	if err != nil || len(tasks) != 30 || tasks[0].Unread {
		t.Fatal("recent tasks fixture is not beyond the unread window", len(tasks), err)
	}
	first, err := f.s.UnreadTasks(ctx, f.p, &f.project, "")
	if err != nil || first.Total != 35 || len(first.Tasks) != 30 || first.NextCursor == "" {
		t.Fatal("inbox truncated its total to recent tasks", first.Total, len(first.Tasks), err)
	}
	second, err := f.s.UnreadTasks(ctx, f.p, &f.project, first.NextCursor)
	if err != nil || second.Total != 35 || len(second.Tasks) != 5 || second.NextCursor != "" {
		t.Fatal("inbox keyset page lost older replies", second.Total, len(second.Tasks), err)
	}
	seen := map[uuid.UUID]bool{}
	for _, task := range append(first.Tasks, second.Tasks...) {
		if seen[task.ID] || !task.Unread || task.ProjectID != f.project {
			t.Fatal("duplicate or incorrectly scoped inbox entry", task)
		}
		seen[task.ID] = true
	}
	var project uuid.UUID
	if err = f.s.DB.QueryRow(ctx, `INSERT INTO projects(workspace_id,name) VALUES($1,'Second project') RETURNING id`, f.p.WorkspaceID).Scan(&project); err != nil {
		t.Fatal(err)
	}
	separate := inboxConversation(t, f, f.p.UserID, project, "Separate project result")
	inboxMessage(t, f, separate, "assistant", "Separate result", `{}`, `[]`)
	filtered, err := f.s.UnreadTasks(ctx, f.p, &project, "")
	if err != nil || filtered.Total != 1 || len(filtered.Tasks) != 1 || filtered.Tasks[0].ID != separate {
		t.Fatal("project scope returned another project's reply", filtered, err)
	}
	for _, cursor := range []string{"not-a-cursor", strings.Repeat("x", 257), "e30"} {
		if _, err = f.s.UnreadTasks(ctx, f.p, nil, cursor); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid cursor accepted", cursor, err)
		}
	}
}

func TestInboxConcurrentAcknowledgementsAreMonotonicAndDoNotMutateTaskData(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	conversation := inboxConversation(t, f, f.p.UserID, f.project, "Concurrent acknowledgements")
	first, _ := inboxMessage(t, f, conversation, "assistant", "First", `{}`, `[]`)
	second, lastSeq := inboxMessage(t, f, conversation, "assistant", "Second", `{}`, `[]`)
	var updated time.Time
	if err := f.s.DB.QueryRow(ctx, `SELECT updated_at FROM conversations WHERE id=$1`, conversation).Scan(&updated); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errorsFound := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := first
			if i%2 == 0 {
				id = second
			}
			_, err := f.s.MarkRead(ctx, f.p, conversation, id)
			errorsFound <- err
		}(i)
	}
	wg.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	var actual int64
	var unchanged time.Time
	if err := f.s.DB.QueryRow(ctx, `SELECT last_read_seq,c.updated_at FROM conversation_read_states reads JOIN conversations c ON c.id=reads.conversation_id WHERE c.id=$1`, conversation).Scan(&actual, &unchanged); err != nil || actual != lastSeq || !unchanged.Equal(updated) {
		t.Fatal("concurrent cursor regressed or changed task ordering", actual, lastSeq, err)
	}
	var messages, runs int
	if err := f.s.DB.QueryRow(ctx, `SELECT (SELECT count(*) FROM messages WHERE conversation_id=$1),(SELECT count(*) FROM runs WHERE conversation_id=$1)`, conversation).Scan(&messages, &runs); err != nil || messages != 2 || runs != 0 {
		t.Fatal("read acknowledgement executed a task", messages, runs, err)
	}
}

func TestInboxRejectsRevokedActorsAndCascadesMembershipReadingState(t *testing.T) {
	for _, revoke := range []string{
		`DELETE FROM sessions WHERE user_id=$1`,
		`UPDATE users SET disabled=true WHERE id=$1`,
		`DELETE FROM workspace_members WHERE user_id=$1`,
	} {
		t.Run(revoke, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			conversation := inboxConversation(t, f, f.p.UserID, f.project, "Private response")
			message, _ := inboxMessage(t, f, conversation, "assistant", "Private", `{}`, `[]`)
			if _, err := f.s.MarkRead(ctx, f.p, conversation, message); err != nil {
				t.Fatal(err)
			}
			if _, err := f.s.DB.Exec(ctx, revoke, f.p.UserID); err != nil {
				t.Fatal(err)
			}
			if _, err := f.s.UnreadTasks(ctx, f.p, nil, ""); !errors.Is(err, ErrUnauthorized) {
				t.Fatal("revoked reader retained inbox access", err)
			}
			if _, err := f.s.MarkRead(ctx, f.p, conversation, message); !errors.Is(err, ErrUnauthorized) {
				t.Fatal("revoked reader mutated a read boundary", err)
			}
			if strings.Contains(revoke, "workspace_members") {
				var remaining int
				if err := f.s.DB.QueryRow(ctx, `SELECT count(*) FROM conversation_read_states`).Scan(&remaining); err != nil || remaining != 0 {
					t.Fatal("removed membership retained reading state", remaining, err)
				}
			}
		})
	}
}

func TestInboxMigrationBaselinesHistoryAndRollbackPreservesMessages(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	read := func(name string) string {
		raw, err := os.ReadFile("../../migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	if _, err := f.s.DB.Exec(ctx, read("000036_conversation_reads.down.sql")); err != nil {
		t.Fatal(err)
	}
	conversation := inboxConversation(t, f, f.p.UserID, f.project, "Historical task")
	inboxMessage(t, f, conversation, "assistant", "Historical reply without a known read history", `{}`, `[]`)
	if _, err := f.s.DB.Exec(ctx, read("000036_conversation_reads.up.sql")); err != nil {
		t.Fatal(err)
	}
	inbox, err := f.s.UnreadTasks(ctx, f.p, nil, "")
	if err != nil || inbox.Total != 0 {
		t.Fatal("upgrade fabricated historical unread activity", inbox, err)
	}
	inboxMessage(t, f, conversation, "assistant", "A reply after tracking started", `{}`, `[]`)
	inbox, err = f.s.UnreadTasks(ctx, f.p, nil, "")
	if err != nil || inbox.Total != 1 {
		t.Fatal("new reply after upgrade not tracked", inbox, err)
	}
	if _, err = f.s.DB.Exec(ctx, read("000036_conversation_reads.down.sql")); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = f.s.DB.QueryRow(ctx, `SELECT count(*) FROM messages WHERE conversation_id=$1`, conversation).Scan(&count); err != nil || count != 2 {
		t.Fatal("rollback removed task reply data", count, err)
	}
}

func TestInboxHTTPUsesAuthenticatedReadsAndStrictObservedMessageAcknowledgements(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	conversation := inboxConversation(t, f, f.p.UserID, f.project, "Actual reply")
	message, seq := inboxMessage(t, f, conversation, "assistant", "Ready to review", `{}`, `[]`)
	raw := bytes.Repeat([]byte{17}, 32)
	digest := sha256.Sum256(raw)
	if _, err := f.s.DB.Exec(ctx, `INSERT INTO auth_access_tokens(token_hash,session_id,expires_at) SELECT $1,id,now()+interval '1 hour' FROM sessions WHERE token_hash=$2`, digest[:], f.p.SessionHash); err != nil {
		t.Fatal(err)
	}
	h := &Handler{Service: f.s}
	router := chi.NewRouter()
	router.Use(auth.New(f.s.DB, nil, time.Hour, false).Middleware)
	router.Get("/personal", h.State)
	router.Get("/personal/tasks", h.Tasks)
	router.Get("/personal/unread", h.UnreadTasks)
	router.Post("/conversations/{id}/read", h.MarkRead)
	call := func(method, path, body string, authenticated bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if authenticated {
			req.AddCookie(&http.Cookie{Name: "lester_access_token", Value: base64.RawURLEncoding.EncodeToString(raw)})
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	for _, path := range []string{"/personal", "/personal/tasks", "/personal/unread"} {
		w := call(http.MethodGet, path, "", true)
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("authenticated list failed", path, w.Code, w.Body.String())
		}
	}
	var markers int
	if err := f.s.DB.QueryRow(ctx, `SELECT count(*) FROM conversation_read_states`).Scan(&markers); err != nil || markers != 0 {
		t.Fatal("loading assistant state marked a reply read", markers, err)
	}
	path := "/conversations/" + conversation.String() + "/read"
	valid := `{"message_id":"` + message.String() + `"}`
	if w := call(http.MethodPost, path, valid, false); w.Code != http.StatusUnauthorized {
		t.Fatal("unauthenticated read mutation accepted", w.Code)
	}
	for _, invalid := range []string{`{}`, `{"message_id":"invalid"}`, `{"message_id":"` + message.String() + `","seq":999999}`, `{`} {
		if w := call(http.MethodPost, path, invalid, true); w.Code != http.StatusBadRequest {
			t.Fatal("invalid or forged read boundary accepted", invalid, w.Code, w.Body.String())
		}
	}
	for _, query := range []string{"?cursor=invalid", "?project_id=invalid", "?project_id=00000000-0000-0000-0000-000000000000"} {
		if w := call(http.MethodGet, "/personal/unread"+query, "", true); w.Code != http.StatusBadRequest {
			t.Fatal("invalid inbox query accepted", query, w.Code)
		}
	}
	w := call(http.MethodPost, path, valid, true)
	var result struct {
		Seq int64 `json:"last_read_seq"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Seq != seq || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("valid observed read boundary failed", w.Code, w.Body.String())
	}
	w = call(http.MethodGet, "/personal/unread", "", true)
	var inbox Inbox
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &inbox) != nil || inbox.Total != 0 {
		t.Fatal("confirmed acknowledgement did not reach list", w.Code, w.Body.String())
	}
}
