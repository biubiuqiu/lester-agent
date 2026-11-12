package localrun

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/harness"
	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/biubiuqiu/lester-agent/backend/internal/toolcontext"
	"github.com/google/uuid"
)

func TestContextJSONLIsReadSourceAndSerializesWriters(t *testing.T) {
	s, dir, _, c := openTestStore(t)
	defer s.Close()
	ctx := context.Background()
	run, _, _, err := s.Accept(ctx, c.ID, uuid.NewString(), "question\n第二行")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := s.AppendMessage(ctx, c.ID, run.ID, modelruntime.Message{Role: "assistant", Content: "reply"}, false); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	messages, err := s.Messages(ctx, c.ID)
	if err != nil || len(messages) != 25 {
		t.Fatalf("messages: %d %v", len(messages), err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "contexts", c.ID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != 25 {
		t.Fatalf("expected one JSON line per message, got %d", len(lines))
	}
	for i, line := range lines {
		var r contextRecord
		if json.Unmarshal([]byte(line), &r) != nil || r.Message.Seq != int64(i+1) || r.Message.ID != messages[i].ID {
			t.Fatalf("record %d: %s", i, line)
		}
	}
	for _, table := range []string{"messages", "context_outbox"} {
		var count int
		if err = s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s retained transcript: %d %v", table, count, err)
		}
	}
	// A second process/store must not interrupt an active runner.
	if other, err := OpenStore(dir, "profile-a", "device-a"); err == nil {
		other.Close()
		t.Fatal("second runner opened active profile")
	}
}

func TestContextRecoversCommittedOutboxAtAppendBoundaries(t *testing.T) {
	for _, boundary := range []string{"before-write", "torn-write", "after-sync"} {
		t.Run(boundary, func(t *testing.T) {
			s, dir, _, c := openTestStore(t)
			ctx := context.Background()
			requestID := uuid.NewString()
			run, _, _, err := s.Accept(ctx, c.ID, requestID, "once")
			if err != nil {
				t.Fatal(err)
			}
			tx, err := s.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			message, err := s.appendMessage(ctx, tx, c.ID, run.ID, modelruntime.Message{Role: "assistant", ToolCalls: []modelruntime.ToolCall{{ID: "one", Name: "bash", Arguments: json.RawMessage(`{"command":"touch once"}`)}}}, false)
			if err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			var raw string
			if err = s.db.QueryRow("SELECT record FROM context_outbox WHERE conversation_id=?", c.ID).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "contexts", c.ID+".jsonl")
			if boundary != "before-write" {
				f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if boundary == "torn-write" {
					_, err = f.WriteString(raw[:len(raw)/2])
				} else {
					_, err = f.WriteString(raw + "\n")
				}
				if err != nil {
					t.Fatal(err)
				}
				if err = f.Sync(); err != nil {
					t.Fatal(err)
				}
				f.Close()
			}
			s.Close()
			s, err = OpenStore(dir, "profile-a", "device-a")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			messages, err := s.Messages(ctx, c.ID)
			if err != nil || len(messages) != 3 || messages[1].ID != message.ID || messages[2].ToolCallID != "one" || !strings.Contains(messages[2].Content, `"status":"unknown"`) {
				t.Fatalf("recovered: %+v %v", messages, err)
			}
			receipt, accepted, _, err := s.Accept(ctx, c.ID, requestID, "once")
			if err != nil || accepted || receipt.Status != "interrupted" {
				t.Fatalf("replayed: %+v %v %v", receipt, accepted, err)
			}
		})
	}
}

func TestContextCorruptionIsNotTreatedAsEmptyHistory(t *testing.T) {
	for _, kind := range []string{"missing", "truncated", "interior", "identity", "version", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			s, dir, _, c := openTestStore(t)
			ctx := context.Background()
			_, _, _, err := s.Accept(ctx, c.ID, uuid.NewString(), "keep me")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "contexts", c.ID+".jsonl")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			s.Close()
			switch kind {
			case "missing":
				err = os.Remove(path)
			case "truncated":
				err = os.WriteFile(path, raw[:len(raw)/2], 0600)
			case "interior":
				err = os.WriteFile(path, append([]byte("{}\n"), raw...), 0600)
			case "identity", "version":
				var r contextRecord
				json.Unmarshal(raw, &r)
				if kind == "identity" {
					r.ProfileID = "foreign"
				} else {
					r.Version++
				}
				r.Digest = recordDigest(r)
				raw, _ = json.Marshal(r)
				err = os.WriteFile(path, append(raw, '\n'), 0600)
			case "symlink":
				target := filepath.Join(t.TempDir(), "outside")
				if err = os.WriteFile(target, raw, 0600); err != nil {
					t.Fatal(err)
				}
				os.Remove(path)
				err = os.Symlink(target, path)
			}
			if err != nil {
				t.Fatal(err)
			}
			if other, err := OpenStore(dir, "profile-a", "device-a"); err == nil {
				other.Close()
				t.Fatal("corrupt history opened")
			}
		})
	}
}

func TestContextMigratesLegacySQLiteWithoutChangingIDs(t *testing.T) {
	s, dir, _, c := openTestStore(t)
	ctx := context.Background()
	requestID := uuid.NewString()
	run, _, _, err := s.Accept(ctx, c.ID, requestID, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.AppendMessage(ctx, c.ID, run.ID, modelruntime.Message{Role: "assistant", Content: "partial"}, true); err != nil {
		t.Fatal(err)
	}
	old, err := s.Messages(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range old {
		calls, _ := json.Marshal(m.ToolCalls)
		if _, err = tx.Exec("INSERT INTO messages VALUES(?,?,?,?,?,?,?,?,?,?)", m.ID, c.ID, m.RunID, m.Seq, m.Role, m.Content, string(calls), m.ToolCallID, m.Partial, timestamp(m.CreatedAt)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec("DELETE FROM context_heads; DELETE FROM identity WHERE key='context_storage'"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	s.Close()
	os.Remove(filepath.Join(dir, "contexts", c.ID+".jsonl"))
	s, err = OpenStore(dir, "profile-a", "device-a")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Messages(ctx, c.ID)
	a, _ := json.Marshal(old)
	b, _ := json.Marshal(got)
	if err != nil || string(a) != string(b) {
		t.Fatalf("migration changed history: %s %v", b, err)
	}
	receipt, err := s.Receipt(ctx, requestID)
	if err != nil || receipt.ID != run.ID || receipt.Status != "interrupted" {
		t.Fatalf("migration lost receipt: %+v %v", receipt, err)
	}
}

const localSummary = `{"intent":"Continue the local task","decisions":[],"completed":[],"pending":["answer the current question"],"risks":[]}`

func TestLocalContextCompactionPersistsAndRestoresWithoutLosingHistory(t *testing.T) {
	s, dir, _, c := openTestStore(t)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		run, _, _, err := s.Accept(ctx, c.ID, uuid.NewString(), strings.Repeat("u", 25000))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = s.AppendMessage(ctx, c.ID, run.ID, modelruntime.Message{Role: "assistant", Content: strings.Repeat("a", 25000)}, false); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Finish(ctx, run.ID, "completed", ""); err != nil {
			t.Fatal(err)
		}
	}
	run, _, _, err := s.Accept(ctx, c.ID, uuid.NewString(), "current literal question")
	if err != nil {
		t.Fatal(err)
	}
	history, err := s.Messages(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	state := harness.State{Request: modelruntime.Request{Model: "fixture", Tools: localTools()}}
	for _, m := range history {
		state.Request.Messages = append(state.Request.Messages, m.Runtime())
	}
	calls := 0
	client := &fixtureClient{generate: func(_ context.Context, r modelruntime.Request) (*modelruntime.Response, error) {
		calls++
		if len(r.Tools) != 0 || r.System != toolcontext.CompactionSystem {
			t.Fatal("compaction gained tools")
		}
		return &modelruntime.Response{Content: localSummary}, nil
	}}
	runner := NewRunner(s, nil, nil)
	prepare, err := runner.prepareContext(ctx, run, client)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := prepare(ctx, &state)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(projected.Messages) >= len(history) || !strings.Contains(projected.Messages[0].Content, localSummary) || projected.Messages[len(projected.Messages)-1].Content != "current literal question" {
		t.Fatalf("bad projection: %d calls, %d messages", calls, len(projected.Messages))
	}
	if len(state.Request.Messages) != len(history) {
		t.Fatal("compaction rewrote transcript")
	}
	s.Finish(ctx, run.ID, "completed", "")
	s.Close()
	s, err = OpenStore(dir, "profile-a", "device-a")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	checkpoint, err := s.ContextCheckpoint(ctx, c.ID)
	if err != nil || checkpoint == nil || checkpoint.Summary != localSummary {
		t.Fatalf("missing checkpoint: %+v %v", checkpoint, err)
	}
	messages, err := s.Messages(ctx, c.ID)
	if err != nil || len(messages) != len(history) {
		t.Fatal("full history lost")
	}
	// A stale writer cannot replace the saved checkpoint or invent sources.
	_, err = s.SaveCheckpoint(ctx, c.ID, run.ID, nil, toolcontext.CompactionPlan{}, localSummary, "fixture")
	if !errors.Is(err, ErrReceiptConflict) {
		t.Fatalf("stale checkpoint: %v", err)
	}
}

func TestLocalContextFailedCompactionPreservesPriorHistory(t *testing.T) {
	for _, kind := range []string{"invalid-summary", "tool-call", "provider-error", "cancelled", "ineffective"} {
		t.Run(kind, func(t *testing.T) {
			s, _, _, c := openTestStore(t)
			defer s.Close()
			ctx := context.Background()
			old, _, _, err := s.Accept(ctx, c.ID, uuid.NewString(), strings.Repeat("old", 2000))
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = s.AppendMessage(ctx, c.ID, old.ID, modelruntime.Message{Role: "assistant", Content: "old answer"}, false); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Finish(ctx, old.ID, "completed", ""); err != nil {
				t.Fatal(err)
			}
			run, _, _, err := s.Accept(ctx, c.ID, uuid.NewString(), "current")
			if err != nil {
				t.Fatal(err)
			}
			history, err := s.Messages(ctx, c.ID)
			if err != nil {
				t.Fatal(err)
			}
			request := modelruntime.Request{Model: "fixture"}
			for _, m := range history {
				request.Messages = append(request.Messages, m.Runtime())
			}
			calls := 0
			callCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			client := &fixtureClient{generate: func(context.Context, modelruntime.Request) (*modelruntime.Response, error) {
				calls++
				switch kind {
				case "invalid-summary":
					return &modelruntime.Response{Content: "invalid"}, nil
				case "tool-call":
					return &modelruntime.Response{Content: localSummary, ToolCalls: []modelruntime.ToolCall{{ID: "bad", Name: "bash", Arguments: json.RawMessage(`{}`)}}}, nil
				case "provider-error":
					return nil, errors.New("provider failed")
				case "cancelled":
					cancel()
				}
				return &modelruntime.Response{Content: localSummary}, nil
			}}
			runner := NewRunner(s, nil, nil)
			estimator := &toolcontext.Estimator{}
			before := toolcontext.Budget{OutputReserve: 2048, EstimatedInput: 1} // the summary cannot improve this estimate
			_, err = runner.compactContext(callCtx, run, client, request, nil, estimator, before)
			if err == nil || calls != 1 {
				t.Fatalf("unsafe compaction: %d calls, %v", calls, err)
			}
			if cp, err := s.ContextCheckpoint(ctx, c.ID); err != nil || cp != nil {
				t.Fatalf("failed checkpoint saved: %+v %v", cp, err)
			}
			got, err := s.Messages(ctx, c.ID)
			a, _ := json.Marshal(history)
			b, _ := json.Marshal(got)
			if err != nil || string(a) != string(b) {
				t.Fatal("failed compaction changed history", err)
			}
		})
	}
}

func TestLocalContextSupportsLargeToolResults(t *testing.T) {
	s, _, _, c := openTestStore(t)
	defer s.Close()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Minute))
	defer cancel()
	run, _, _, err := s.Accept(ctx, c.ID, uuid.NewString(), "read")
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("多行\n", 40000)
	if _, _, err = s.AppendMessage(ctx, c.ID, run.ID, modelruntime.Message{Role: "tool", ToolCallID: "large", Content: content}, false); err != nil {
		t.Fatal(err)
	}
	messages, err := s.Messages(ctx, c.ID)
	if err != nil || messages[1].Content != content {
		t.Fatal("large result was truncated", err)
	}
}

func TestLocalContextCacheDoesNotHideChangesOrShareMutableMessages(t *testing.T) {
	s, dir, _, c := openTestStore(t)
	defer s.Close()
	ctx := context.Background()
	run, _, _, err := s.Accept(ctx, c.ID, uuid.NewString(), "original")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.AppendMessage(ctx, c.ID, run.ID, modelruntime.Message{Role: "assistant", ToolCalls: []modelruntime.ToolCall{{ID: "read", Name: "read", Arguments: json.RawMessage(`{"file_path":"x"}`)}}}, false); err != nil {
		t.Fatal(err)
	}
	messages, err := s.Messages(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	messages[0].Content = "mutated"
	messages[1].ToolCalls[0].Arguments[0] = '!'
	got, err := s.Messages(ctx, c.ID)
	if err != nil || got[0].Content != "original" || !json.Valid(got[1].ToolCalls[0].Arguments) {
		t.Fatal("cached history shared caller memory", err)
	}
	if err = os.Truncate(filepath.Join(dir, "contexts", c.ID+".jsonl"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Messages(ctx, c.ID); err == nil {
		t.Fatal("cache hid truncation")
	}
}
