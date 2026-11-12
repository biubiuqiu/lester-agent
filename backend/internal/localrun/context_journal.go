package localrun

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/biubiuqiu/lester-agent/backend/internal/toolcontext"
	"github.com/google/uuid"
)

const journalVersion = 1
const maxJournalRecord = 16 << 20

// SQLite commits acceptance, receipts and this outbox atomically. A successful
// message write is acknowledged only after its JSONL record has been fsynced.
// The outbox is then removed; reads use JSONL, not a second transcript in SQL.
const journalSchema = `
CREATE TABLE IF NOT EXISTS context_heads(conversation_id TEXT PRIMARY KEY REFERENCES conversations(id),records INTEGER NOT NULL,message_seq INTEGER NOT NULL,digest TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS context_outbox(conversation_id TEXT NOT NULL REFERENCES conversations(id),seq INTEGER NOT NULL,record TEXT NOT NULL,PRIMARY KEY(conversation_id,seq));
`

type contextRecord struct {
	Version        int                     `json:"version"`
	ProfileID      string                  `json:"profile_id"`
	DeviceID       string                  `json:"device_id"`
	ConversationID string                  `json:"conversation_id"`
	Seq            int64                   `json:"seq"`
	Previous       string                  `json:"previous"`
	Message        *Message                `json:"message,omitempty"`
	Checkpoint     *toolcontext.Checkpoint `json:"checkpoint,omitempty"`
	Digest         string                  `json:"digest"`
}

type localContext struct {
	Messages   []Message
	Checkpoint *toolcontext.Checkpoint
	records    int64
	digest     string
}

type cachedContext struct {
	id    string
	info  os.FileInfo
	state localContext
}

func cloneLocalContext(state localContext) localContext {
	state.Messages = slices.Clone(state.Messages)
	for i := range state.Messages {
		state.Messages[i].ToolCalls = slices.Clone(state.Messages[i].ToolCalls)
		for j := range state.Messages[i].ToolCalls {
			state.Messages[i].ToolCalls[j].Arguments = slices.Clone(state.Messages[i].ToolCalls[j].Arguments)
		}
	}
	if state.Checkpoint != nil {
		cp := *state.Checkpoint
		cp.RetainedIDs = slices.Clone(cp.RetainedIDs)
		state.Checkpoint = &cp
	}
	return state
}

func recordDigest(record contextRecord) string {
	record.Digest = ""
	raw, _ := json.Marshal(record)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Store) stageRecord(ctx context.Context, tx *sql.Tx, id string, message *Message, checkpoint *toolcontext.Checkpoint) error {
	var n, seq int64
	var digest string
	err := tx.QueryRowContext(ctx, "SELECT records,message_seq,digest FROM context_heads WHERE conversation_id=?", id).Scan(&n, &seq, &digest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if message != nil {
		seq++
		if seq > 10000 {
			return errors.New("local message history exceeds the supported bound")
		}
		message.Seq = seq
	}
	record := contextRecord{Version: journalVersion, ProfileID: s.profileID, DeviceID: s.deviceID, ConversationID: id, Seq: n + 1, Previous: digest, Message: message, Checkpoint: checkpoint}
	record.Digest = recordDigest(record)
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(raw)+1 > maxJournalRecord {
		return errors.New("local context record exceeds the supported bound")
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO context_outbox VALUES(?,?,?)", id, record.Seq, string(raw)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO context_heads VALUES(?,?,?,?) ON CONFLICT(conversation_id) DO UPDATE SET records=excluded.records,message_seq=excluded.message_seq,digest=excluded.digest`, id, record.Seq, seq, record.Digest)
	return err
}

// migrateContext stages existing SQLite history in one transaction. An interrupted
// migration resumes from the committed outbox, retaining IDs/order/receipts.
func (s *Store) migrateContext(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version string
	err = tx.QueryRowContext(ctx, "SELECT value FROM identity WHERE key='context_storage'").Scan(&version)
	if err == nil {
		if version != "jsonl-v1" {
			return errors.New("unsupported local context storage version")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,conversation_id,run_id,seq,role,content,tool_calls,tool_call_id,partial,created_at FROM messages ORDER BY conversation_id,seq")
	if err != nil {
		return err
	}
	var messages []Message
	for rows.Next() {
		var m Message
		var calls, created string
		if err = rows.Scan(&m.ID, &m.ConversationID, &m.RunID, &m.Seq, &m.Role, &m.Content, &calls, &m.ToolCallID, &m.Partial, &created); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal([]byte(calls), &m.ToolCalls); err != nil {
			rows.Close()
			return err
		}
		m.CreatedAt = parseTimestamp(created)
		messages = append(messages, m)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, m := range messages {
		oldSeq := m.Seq
		if err = s.stageRecord(ctx, tx, m.ConversationID, &m, nil); err != nil {
			return err
		}
		if oldSeq != m.Seq {
			return errors.New("legacy local context sequence is invalid")
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM messages"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO identity VALUES('context_storage','jsonl-v1')"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) flushContexts(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, "SELECT conversation_id FROM context_heads ORDER BY conversation_id")
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, id := range ids {
		if _, err = s.loadContext(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) validateRecord(id string, record contextRecord, state *localContext) error {
	if record.Version != journalVersion || record.ProfileID != s.profileID || record.DeviceID != s.deviceID || record.ConversationID != id || record.Seq != state.records+1 || record.Previous != state.digest || record.Digest != recordDigest(record) || (record.Message == nil) == (record.Checkpoint == nil) {
		return errors.New("invalid local context journal record")
	}
	if m := record.Message; m != nil {
		if !validID(m.ID) || !validID(m.RunID) || m.ConversationID != id || m.Seq != int64(len(state.Messages)+1) || m.CreatedAt.IsZero() {
			return errors.New("invalid local context message")
		}
		state.Messages = append(state.Messages, *m)
		if len(state.Messages) > 10000 {
			return errors.New("local message history exceeds the supported bound")
		}
	} else {
		if err := validateLocalCheckpoint(state.Messages, state.Checkpoint, record.Checkpoint); err != nil {
			return err
		}
		state.Checkpoint = record.Checkpoint
	}
	state.records, state.digest = record.Seq, record.Digest
	return nil
}

// loadContext runs under the Store's mutex (or during its exclusive startup).
// Only an incomplete unacknowledged last line can be repaired from the outbox.
// Missing acknowledged records, interior corruption, foreign identity or future
// format versions fail closed; none of these are an empty conversation.
func (s *Store) loadContext(ctx context.Context, id string) (localContext, error) {
	state := localContext{Messages: []Message{}}
	if !validID(id) {
		return state, errors.New("invalid local context identity")
	}
	var head, lastMessage int64
	var digest string
	err := s.db.QueryRowContext(ctx, "SELECT records,message_seq,digest FROM context_heads WHERE conversation_id=?", id).Scan(&head, &lastMessage, &digest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return state, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT seq,record FROM context_outbox WHERE conversation_id=? ORDER BY seq", id)
	if err != nil {
		return state, err
	}
	pending := map[int64]string{}
	for rows.Next() {
		var seq int64
		var raw string
		if err = rows.Scan(&seq, &raw); err != nil {
			rows.Close()
			return state, err
		}
		pending[seq] = raw
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return state, err
	}
	rows.Close()
	name := id + ".jsonl"
	if info, e := s.journals.Lstat(name); e == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return state, errors.New("local context journal must be a regular file")
	}
	f, err := s.journals.OpenFile(name, os.O_CREATE|os.O_RDWR|stateOpenFlags, 0600)
	if err != nil {
		return state, err
	}
	defer f.Close()
	info, statErr := f.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		return state, errors.New("invalid local context journal file")
	}
	var offset int64
	torn := false
	// Cache only an unchanged, previously verified file. Replacements, external
	// appends and truncation force a fresh integrity check. Return owned copies.
	cache := s.contextCache
	if cache != nil && cache.id == id && os.SameFile(cache.info, info) && cache.info.Size() == info.Size() && cache.info.ModTime().Equal(info.ModTime()) {
		state = cloneLocalContext(cache.state)
		offset = info.Size()
	} else {
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64<<10), maxJournalRecord)
		// Keep the newline to distinguish a committed line from a torn append.
		scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
			if i := bytes.IndexByte(data, '\n'); i >= 0 {
				return i + 1, data[:i+1], nil
			}
			if atEOF && len(data) > 0 {
				return len(data), data, nil
			}
			return 0, nil, nil
		})
		for scanner.Scan() {
			if err = ctx.Err(); err != nil {
				return state, err
			}
			raw := scanner.Bytes()
			if raw[len(raw)-1] != '\n' {
				torn = true
				break
			}
			var record contextRecord
			if err = json.Unmarshal(raw, &record); err != nil {
				return state, errors.New("corrupt local context journal")
			}
			if err = s.validateRecord(id, record, &state); err != nil {
				return state, err
			}
			if staged, ok := pending[record.Seq]; ok {
				var other contextRecord
				if json.Unmarshal([]byte(staged), &other) != nil || other.Digest != record.Digest {
					return state, errors.New("local context outbox conflicts with journal")
				}
			}
			offset += int64(len(raw))
		}
		if err = scanner.Err(); err != nil {
			return state, err
		}
	}
	if state.records > head {
		return state, errors.New("local context journal is ahead of its receipt")
	}
	if torn && (pending[state.records+1] == "" || state.records == head) {
		return state, errors.New("acknowledged local context was truncated")
	}
	// Validate the entire suffix before altering the journal.
	var suffix bytes.Buffer
	for seq := state.records + 1; seq <= head; seq++ {
		raw, ok := pending[seq]
		if !ok {
			return state, errors.New("acknowledged local context is missing")
		}
		var record contextRecord
		if json.Unmarshal([]byte(raw), &record) != nil {
			return state, errors.New("corrupt local context outbox")
		}
		if err = s.validateRecord(id, record, &state); err != nil {
			return state, err
		}
		suffix.WriteString(raw)
		suffix.WriteByte('\n')
	}
	if state.digest != digest || int64(len(state.Messages)) != lastMessage {
		return state, errors.New("local context receipt does not match journal")
	}
	if torn {
		if err = f.Truncate(offset); err != nil {
			return state, err
		}
	}
	if suffix.Len() > 0 {
		if _, err = f.Seek(offset, io.SeekStart); err != nil {
			return state, err
		}
		if _, err = io.Copy(f, &suffix); err != nil {
			return state, err
		}
	}
	if len(pending) > 0 {
		if err = f.Sync(); err != nil {
			return state, err
		}
		// Sync the directory too: the first journal filename must survive a crash.
		if err = syncStateDirectory(s.journals); err != nil {
			return state, err
		}
		if _, err = s.db.ExecContext(ctx, "DELETE FROM context_outbox WHERE conversation_id=? AND seq<=?", id, head); err != nil {
			return state, err
		}
	}
	info, err = f.Stat()
	if err != nil {
		return state, err
	}
	s.contextCache = &cachedContext{id: id, info: info, state: cloneLocalContext(state)}
	return state, nil
}

func validateLocalCheckpoint(messages []Message, previous, next *toolcontext.Checkpoint) error {
	if next == nil || !validID(next.ID) || next.Model == "" || next.SourceDigest == "" || next.SourceCount < 1 || next.ThroughSeq < 1 || next.ThroughSeq > int64(len(messages)) {
		return errors.New("invalid local context checkpoint")
	}
	if _, err := toolcontext.ValidateSummary(next.Summary); err != nil {
		return err
	}
	if previous != nil && (next.SourceCount <= previous.SourceCount || next.ThroughSeq < previous.ThroughSeq) {
		return errors.New("local context checkpoint regressed")
	}
	for _, id := range next.RetainedIDs {
		found := false
		for _, m := range messages {
			if m.ID == id && m.Seq <= next.ThroughSeq && !m.Partial {
				found = true
				break
			}
		}
		if !found {
			return errors.New("local context checkpoint retained an unknown message")
		}
	}
	return nil
}

func (s *Store) SaveCheckpoint(ctx context.Context, id, runID string, previous *toolcontext.Checkpoint, plan toolcontext.CompactionPlan, summary, model string) (*toolcontext.Checkpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadContext(ctx, id)
	if err != nil {
		return nil, err
	}
	if (state.Checkpoint == nil) != (previous == nil) || previous != nil && state.Checkpoint.ID != previous.ID {
		return nil, ErrReceiptConflict
	}
	// Recompute provenance from the durable complete messages, never from a model.
	if err = validateLocalPlan(state, previous, plan); err != nil {
		return nil, err
	}
	next := &toolcontext.Checkpoint{ID: uuid.NewString(), ThroughSeq: plan.ThroughSeq, RetainedIDs: plan.RetainedIDs, Summary: summary, SourceDigest: plan.Digest, SourceCount: plan.SourceCount, Model: model}
	if err = validateLocalCheckpoint(state.Messages, previous, next); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status string
	if err = tx.QueryRowContext(ctx, "SELECT status FROM runs WHERE id=? AND conversation_id=?", runID, id).Scan(&status); err != nil {
		return nil, err
	}
	if status != "running" {
		return nil, ErrReceiptConflict
	}
	if err = s.stageRecord(ctx, tx, id, nil, next); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if _, err = s.loadContext(ctx, id); err != nil {
		return nil, err
	}
	return next, nil
}

func validateLocalPlan(state localContext, previous *toolcontext.Checkpoint, plan toolcontext.CompactionPlan) error {
	var history []modelruntime.Message
	for _, m := range state.Messages {
		if !m.Partial {
			history = append(history, m.Runtime())
		}
	}
	working := toolcontext.ApplyCheckpoint(history, previous)
	durable := map[string]modelruntime.Message{}
	for _, m := range working {
		if m.MessageID != "" {
			durable[m.MessageID] = m
		}
	}
	selected := map[string]bool{}
	if len(plan.SourceIDs) == 0 || len(plan.SourceIDs) != len(plan.Sources) {
		return errors.New("invalid local checkpoint sources")
	}
	var through int64
	var count int
	var priorDigest string
	if previous != nil {
		through = previous.ThroughSeq
		count = previous.SourceCount
		priorDigest = previous.SourceDigest
	}
	var last int64
	for i, id := range plan.SourceIDs {
		m, ok := durable[id]
		a, _ := json.Marshal(m)
		b, _ := json.Marshal(plan.Sources[i])
		if !ok || selected[id] || !bytes.Equal(a, b) || m.Seq <= last {
			return errors.New("local checkpoint source is not a durable message")
		}
		selected[id] = true
		last = m.Seq
		through = max(through, m.Seq)
	}
	if through != plan.ThroughSeq || count+len(selected) != plan.SourceCount {
		return errors.New("invalid local checkpoint boundary")
	}
	var retained []string
	for _, m := range working {
		if m.MessageID != "" && m.Seq <= through && !selected[m.MessageID] {
			retained = append(retained, m.MessageID)
		}
	}
	a, _ := json.Marshal(retained)
	b, _ := json.Marshal(plan.RetainedIDs)
	if !bytes.Equal(a, b) {
		return errors.New("invalid local checkpoint retained messages")
	}
	raw, _ := json.Marshal(struct {
		Previous string
		IDs      []string
		Messages []modelruntime.Message
	}{priorDigest, plan.SourceIDs, plan.Sources})
	sum := sha256.Sum256(raw)
	if plan.Digest != hex.EncodeToString(sum[:]) {
		return errors.New("invalid local checkpoint provenance")
	}
	return nil
}
