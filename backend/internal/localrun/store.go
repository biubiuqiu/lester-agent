package localrun

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound        = errors.New("local record not found")
	ErrBusy            = errors.New("a local task is already running in this conversation")
	ErrReceiptConflict = errors.New("local request conflicts with its saved receipt")
)

type Store struct {
	mu                  sync.Mutex
	lock                *os.File
	journals            *os.Root
	closed              bool
	contextCache        *cachedContext
	db                  *sql.DB
	deviceID, profileID string
}

const schema = `
CREATE TABLE IF NOT EXISTS identity(key TEXT PRIMARY KEY,value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS projects(id TEXT PRIMARY KEY,name TEXT NOT NULL,root TEXT NOT NULL UNIQUE,created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS conversations(id TEXT PRIMARY KEY,project_id TEXT NOT NULL REFERENCES projects(id),title TEXT NOT NULL,model_id TEXT NOT NULL,permission_mode TEXT NOT NULL,status TEXT NOT NULL,active_run_id TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS runs(id TEXT PRIMARY KEY,conversation_id TEXT NOT NULL REFERENCES conversations(id),request_id TEXT NOT NULL UNIQUE,digest TEXT NOT NULL,status TEXT NOT NULL,model_id TEXT NOT NULL,permission_mode TEXT NOT NULL,error_code TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,finished_at TEXT);
CREATE TABLE IF NOT EXISTS messages(id TEXT PRIMARY KEY,conversation_id TEXT NOT NULL REFERENCES conversations(id),run_id TEXT NOT NULL REFERENCES runs(id),seq INTEGER NOT NULL,role TEXT NOT NULL,content TEXT NOT NULL,tool_calls TEXT NOT NULL DEFAULT '[]',tool_call_id TEXT NOT NULL DEFAULT '',partial INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,UNIQUE(conversation_id,seq));
CREATE TABLE IF NOT EXISTS events(seq INTEGER PRIMARY KEY AUTOINCREMENT,conversation_id TEXT NOT NULL REFERENCES conversations(id),run_id TEXT NOT NULL DEFAULT '',type TEXT NOT NULL,payload TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS permissions(id TEXT PRIMARY KEY,conversation_id TEXT NOT NULL REFERENCES conversations(id),run_id TEXT NOT NULL REFERENCES runs(id),tool TEXT NOT NULL,arguments TEXT NOT NULL,status TEXT NOT NULL,created_at TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS conversations_recent ON conversations(updated_at DESC,id);
CREATE INDEX IF NOT EXISTS events_conversation ON events(conversation_id,seq);
CREATE INDEX IF NOT EXISTS messages_run ON messages(run_id,seq);
`

func OpenStore(stateDir, profileID, deviceID string) (*Store, error) {
	if stateDir == "" || profileID == "" || deviceID == "" || len(profileID) > 256 || len(deviceID) > 128 {
		return nil, errors.New("a main-process profile and device identity are required")
	}
	stateDir, err := filepath.Abs(stateDir)
	if err != nil {
		return nil, errors.New("invalid local state directory")
	}
	if err = os.MkdirAll(stateDir, 0700); err != nil {
		return nil, errors.New("local state directory is unavailable")
	}
	info, err := os.Lstat(stateDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("local state directory must be a real directory")
	}
	stateRoot, err := os.OpenRoot(stateDir)
	if err != nil {
		return nil, err
	}
	defer stateRoot.Close()
	lock, err := stateRoot.OpenFile("workspace.lock", os.O_CREATE|os.O_RDWR|stateOpenFlags, 0600)
	if err != nil {
		return nil, err
	}
	if info, e := lock.Stat(); e != nil || !info.Mode().IsRegular() {
		lock.Close()
		return nil, errors.New("local profile lock must be a regular file")
	}
	if err = lockState(lock); err != nil {
		lock.Close()
		return nil, errors.New("local profile is already open by another runner")
	}
	success := false
	defer func() {
		if !success {
			lock.Close()
		}
	}()
	if err = stateRoot.Mkdir("contexts", 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	if err = syncStateDirectory(stateRoot); err != nil {
		return nil, err
	}
	info, err = stateRoot.Lstat("contexts")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("local contexts must be a real directory")
	}
	journals, err := stateRoot.OpenRoot("contexts")
	if err != nil {
		return nil, err
	}
	defer func() {
		if !success {
			journals.Close()
		}
	}()
	file := filepath.Join(stateDir, "workspace.sqlite")
	if info, err := os.Lstat(file); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return nil, errors.New("local state database must be a regular file")
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, errors.New("local state database is unavailable")
	}
	_ = f.Close()
	_ = os.Chmod(file, 0600)
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(file)}
	q := url.Values{}
	for _, pragma := range []string{"busy_timeout(5000)", "foreign_keys(1)", "journal_mode(WAL)", "synchronous(FULL)"} {
		q.Add("_pragma", pragma)
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, errors.New("open local state database")
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(schema + journalSchema); err != nil {
		db.Close()
		return nil, errors.New("initialize local state database")
	}
	s := &Store{db: db, deviceID: deviceID, profileID: profileID, lock: lock, journals: journals}
	for key, value := range map[string]string{"profile_id": profileID, "device_id": deviceID} {
		var saved string
		err = db.QueryRow("SELECT value FROM identity WHERE key=?", key).Scan(&saved)
		if errors.Is(err, sql.ErrNoRows) {
			_, err = db.Exec("INSERT INTO identity(key,value) VALUES(?,?)", key, value)
		} else if err == nil && saved != value {
			err = errors.New("local state belongs to a different profile or device")
		}
		if err != nil {
			db.Close()
			return nil, errors.New("local state belongs to a different profile or device")
		}
	}
	if err = s.migrateContext(context.Background()); err == nil {
		err = s.flushContexts(context.Background())
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize local context journals: %w", err)
	}
	if err = s.recoverInterrupted(context.Background()); err != nil {
		db.Close()
		return nil, errors.New("reconcile interrupted local tasks")
	}
	success = true
	return s, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return errors.Join(s.db.Close(), s.journals.Close(), s.lock.Close())
}
func timestamp(t time.Time) string      { return t.UTC().Format(time.RFC3339Nano) }
func parseTimestamp(s string) time.Time { t, _ := time.Parse(time.RFC3339Nano, s); return t }
func validID(id string) bool            { parsed, err := uuid.Parse(id); return err == nil && parsed != uuid.Nil }

func (s *Store) OpenProject(ctx context.Context, id, name, path string) (Project, error) {
	if id != "" && !validID(id) {
		return Project{}, errors.New("invalid project identity")
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 256 {
		return Project{}, errors.New("project name must be between 1 and 256 bytes")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return Project{}, errors.New("selected project directory is unavailable")
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return Project{}, err
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return Project{}, errors.New("select an existing directory")
	}
	var existing Project
	var created, updated string
	err = s.db.QueryRowContext(ctx, "SELECT id,name,root,created_at,updated_at FROM projects WHERE root=?", canonical).Scan(&existing.ID, &existing.Name, &existing.Path, &created, &updated)
	if err == nil {
		existing.CreatedAt = parseTimestamp(created)
		existing.UpdatedAt = parseTimestamp(updated)
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Project{}, err
	}
	if id == "" {
		id = uuid.NewString()
	}
	var oldRoot string
	if err = s.db.QueryRowContext(ctx, "SELECT root FROM projects WHERE id=?", id).Scan(&oldRoot); err == nil {
		return Project{}, errors.New("project directory binding is immutable; open it as a new project")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Project{}, err
	}
	now := time.Now().UTC()
	p := Project{ID: id, Name: name, Path: canonical, CreatedAt: now, UpdatedAt: now}
	_, err = s.db.ExecContext(ctx, "INSERT INTO projects VALUES(?,?,?,?,?)", p.ID, p.Name, p.Path, timestamp(now), timestamp(now))
	return p, err
}

func (s *Store) Projects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,name,root,created_at,updated_at FROM projects ORDER BY updated_at DESC,id LIMIT 500")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Project{}
	for rows.Next() {
		var p Project
		var c, u string
		if err = rows.Scan(&p.ID, &p.Name, &p.Path, &c, &u); err != nil {
			return nil, err
		}
		p.CreatedAt = parseTimestamp(c)
		p.UpdatedAt = parseTimestamp(u)
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *Store) Project(ctx context.Context, id string) (Project, error) {
	var p Project
	var c, u string
	err := s.db.QueryRowContext(ctx, "SELECT id,name,root,created_at,updated_at FROM projects WHERE id=?", id).Scan(&p.ID, &p.Name, &p.Path, &c, &u)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	p.CreatedAt = parseTimestamp(c)
	p.UpdatedAt = parseTimestamp(u)
	return p, err
}

func (s *Store) CreateConversation(ctx context.Context, projectID, modelID, title, mode string) (Conversation, error) {
	if _, err := s.Project(ctx, projectID); err != nil {
		return Conversation{}, err
	}
	if strings.TrimSpace(modelID) == "" || len(modelID) > 256 {
		return Conversation{}, errors.New("select a local or cloud model")
	}
	if mode != "standard" && mode != "auto" && mode != "full" {
		return Conversation{}, errors.New("invalid local permission mode")
	}
	title = strings.TrimSpace(title)
	if len(title) > 512 {
		return Conversation{}, errors.New("conversation title is too long")
	}
	if title == "" {
		title = "New task"
	}
	now := time.Now().UTC()
	c := Conversation{ID: uuid.NewString(), ProjectID: projectID, Title: title, ModelID: modelID, PermissionMode: mode, ExecutionTarget: "local", DeviceID: s.deviceID, Status: "idle", CreatedAt: now, UpdatedAt: now}
	_, err := s.db.ExecContext(ctx, "INSERT INTO conversations(id,project_id,title,model_id,permission_mode,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)", c.ID, c.ProjectID, c.Title, c.ModelID, c.PermissionMode, c.Status, timestamp(now), timestamp(now))
	if err != nil {
		return Conversation{}, err
	}
	return s.Conversation(ctx, c.ID)
}

func scanConversation(row interface{ Scan(...any) error }, deviceID string) (Conversation, error) {
	var c Conversation
	var created, updated string
	err := row.Scan(&c.ID, &c.ProjectID, &c.ProjectName, &c.Title, &c.ModelID, &c.PermissionMode, &c.Status, &c.ActiveRunID, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	c.CreatedAt = parseTimestamp(created)
	c.UpdatedAt = parseTimestamp(updated)
	c.ExecutionTarget = "local"
	c.DeviceID = deviceID
	return c, err
}

const conversationColumns = "c.id,c.project_id,p.name,c.title,c.model_id,c.permission_mode,c.status,c.active_run_id,c.created_at,c.updated_at"

func (s *Store) Conversation(ctx context.Context, id string) (Conversation, error) {
	return scanConversation(s.db.QueryRowContext(ctx, "SELECT "+conversationColumns+" FROM conversations c JOIN projects p ON p.id=c.project_id WHERE c.id=?", id), s.deviceID)
}
func (s *Store) Conversations(ctx context.Context) ([]Conversation, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+conversationColumns+" FROM conversations c JOIN projects p ON p.id=c.project_id ORDER BY c.updated_at DESC,c.id LIMIT 500")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Conversation{}
	for rows.Next() {
		c, err := scanConversation(rows, s.deviceID)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanRun(row interface{ Scan(...any) error }) (Run, error) {
	var r Run
	var c string
	var f sql.NullString
	err := row.Scan(&r.ID, &r.ConversationID, &r.RequestID, &r.Status, &r.ModelID, &r.PermissionMode, &r.ErrorCode, &c, &f)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	r.CreatedAt = parseTimestamp(c)
	if f.Valid {
		value := parseTimestamp(f.String)
		r.FinishedAt = &value
	}
	return r, err
}

const runColumns = "id,conversation_id,request_id,status,model_id,permission_mode,error_code,created_at,finished_at"

func (s *Store) Receipt(ctx context.Context, requestID string) (Run, error) {
	return scanRun(s.db.QueryRowContext(ctx, "SELECT "+runColumns+" FROM runs WHERE request_id=?", requestID))
}

func (s *Store) appendMessage(ctx context.Context, tx *sql.Tx, conversationID, runID string, m modelruntime.Message, partial bool) (Message, error) {
	message := Message{ID: uuid.NewString(), ConversationID: conversationID, RunID: runID, Role: m.Role, Content: m.Content, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID, Partial: partial, CreatedAt: time.Now().UTC()}
	err := s.stageRecord(ctx, tx, conversationID, &message, nil)
	return message, err
}

func appendEvent(ctx context.Context, tx *sql.Tx, conversationID, runID, kind string, payload any) (Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Event{}, err
	}
	e := Event{ConversationID: conversationID, RunID: runID, Type: kind, Payload: payload, CreatedAt: time.Now().UTC()}
	result, err := tx.ExecContext(ctx, "INSERT INTO events(conversation_id,run_id,type,payload,created_at) VALUES(?,?,?,?,?)", conversationID, runID, kind, string(raw), timestamp(e.CreatedAt))
	if err != nil {
		return e, err
	}
	e.Seq, err = result.LastInsertId()
	return e, err
}

func (s *Store) Accept(ctx context.Context, conversationID, requestID, content string) (Run, bool, []Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validID(conversationID) || !validID(requestID) || strings.TrimSpace(content) == "" || len(content) > 64<<10 {
		return Run{}, false, nil, errors.New("valid conversation/request identities and a bounded message are required")
	}
	if _, err := s.loadContext(ctx, conversationID); err != nil {
		return Run{}, false, nil, err
	}
	digest := sha256.Sum256([]byte(conversationID + "\x00" + content))
	hash := hex.EncodeToString(digest[:])
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, false, nil, err
	}
	defer tx.Rollback()
	var savedDigest string
	err = tx.QueryRowContext(ctx, "SELECT digest FROM runs WHERE request_id=?", requestID).Scan(&savedDigest)
	if err == nil {
		if savedDigest != hash {
			return Run{}, false, nil, ErrReceiptConflict
		}
		r, err := scanRun(tx.QueryRowContext(ctx, "SELECT "+runColumns+" FROM runs WHERE request_id=?", requestID))
		return r, false, nil, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Run{}, false, nil, err
	}
	var modelID, mode, status string
	err = tx.QueryRowContext(ctx, "SELECT model_id,permission_mode,status FROM conversations WHERE id=?", conversationID).Scan(&modelID, &mode, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, false, nil, ErrNotFound
	}
	if err != nil {
		return Run{}, false, nil, err
	}
	if status == "running" {
		return Run{}, false, nil, ErrBusy
	}
	now := time.Now().UTC()
	run := Run{ID: uuid.NewString(), ConversationID: conversationID, RequestID: requestID, Status: "running", ModelID: modelID, PermissionMode: mode, CreatedAt: now}
	_, err = tx.ExecContext(ctx, "INSERT INTO runs(id,conversation_id,request_id,digest,status,model_id,permission_mode,created_at) VALUES(?,?,?,?,?,?,?,?)", run.ID, conversationID, requestID, hash, run.Status, modelID, mode, timestamp(now))
	if err != nil {
		return Run{}, false, nil, err
	}
	message, err := s.appendMessage(ctx, tx, conversationID, run.ID, modelruntime.Message{Role: "user", Content: content}, false)
	if err != nil {
		return Run{}, false, nil, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE conversations SET status='running',active_run_id=?,updated_at=? WHERE id=?", run.ID, timestamp(now), conversationID)
	if err != nil {
		return Run{}, false, nil, err
	}
	event, err := appendEvent(ctx, tx, conversationID, run.ID, "RUN_STARTED", map[string]any{"message": message, "run": run})
	if err != nil {
		return Run{}, false, nil, err
	}
	if err = tx.Commit(); err != nil {
		return Run{}, false, nil, err
	}
	if _, err = s.loadContext(ctx, conversationID); err != nil {
		return run, false, nil, err
	}
	return run, true, []Event{event}, nil
}

func (s *Store) AppendMessage(ctx context.Context, conversationID, runID string, m modelruntime.Message, partial bool) (Message, Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.loadContext(ctx, conversationID); err != nil {
		return Message{}, Event{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Message{}, Event{}, err
	}
	defer tx.Rollback()
	var status string
	if err = tx.QueryRowContext(ctx, "SELECT status FROM runs WHERE id=? AND conversation_id=?", runID, conversationID).Scan(&status); err != nil {
		return Message{}, Event{}, err
	}
	if status != "running" {
		return Message{}, Event{}, ErrReceiptConflict
	}
	message, err := s.appendMessage(ctx, tx, conversationID, runID, m, partial)
	if err != nil {
		return Message{}, Event{}, err
	}
	event, err := appendEvent(ctx, tx, conversationID, runID, "MESSAGE_SAVED", message)
	if err != nil {
		return Message{}, Event{}, err
	}
	if err = tx.Commit(); err == nil {
		_, err = s.loadContext(ctx, conversationID)
	}
	return message, event, err
}
func (s *Store) AppendEvent(ctx context.Context, conversationID, runID, kind string, payload any) (Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback()
	event, err := appendEvent(ctx, tx, conversationID, runID, kind, payload)
	if err != nil {
		return Event{}, err
	}
	err = tx.Commit()
	return event, err
}

func (s *Store) Finish(ctx context.Context, runID, status, errorCode string) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status != "completed" && status != "cancelled" && status != "failed" && status != "interrupted" {
		return nil, errors.New("invalid terminal task state")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var conversationID, current string
	err = tx.QueryRowContext(ctx, "SELECT conversation_id,status FROM runs WHERE id=?", runID).Scan(&conversationID, &current)
	if err != nil {
		return nil, err
	}
	if current != "running" {
		return nil, nil
	}
	now := timestamp(time.Now())
	_, err = tx.ExecContext(ctx, "UPDATE runs SET status=?,error_code=?,finished_at=? WHERE id=?", status, errorCode, now, runID)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE conversations SET status=?,active_run_id='',updated_at=? WHERE id=? AND active_run_id=?", status, now, conversationID, runID)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE permissions SET status='revoked' WHERE run_id=? AND status='pending'", runID)
	if err != nil {
		return nil, err
	}
	event, err := appendEvent(ctx, tx, conversationID, runID, "RUN_"+strings.ToUpper(status), map[string]any{"status": status, "error_code": errorCode})
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return []Event{event}, nil
}

func (s *Store) Messages(ctx context.Context, conversationID string) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.Conversation(ctx, conversationID); err != nil {
		return nil, err
	}
	state, err := s.loadContext(ctx, conversationID)
	return state.Messages, err
}

func (s *Store) Events(ctx context.Context, conversationID string, after int64) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT seq,run_id,type,payload,created_at FROM events WHERE conversation_id=? AND seq>? ORDER BY seq LIMIT 1000", conversationID, max(after, 0))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		e := Event{ConversationID: conversationID}
		var raw, c string
		if err = rows.Scan(&e.Seq, &e.RunID, &e.Type, &raw, &c); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &e.Payload); err != nil {
			return nil, err
		}
		e.CreatedAt = parseTimestamp(c)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) Snapshot(ctx context.Context, id string) (Snapshot, error) {
	c, err := s.Conversation(ctx, id)
	if err != nil {
		return Snapshot{}, err
	}
	m, err := s.Messages(ctx, id)
	if err != nil {
		return Snapshot{}, err
	}
	p, err := s.Permissions(ctx, id)
	if err != nil {
		return Snapshot{}, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+runColumns+" FROM runs WHERE conversation_id=? ORDER BY created_at DESC LIMIT 100", id)
	if err != nil {
		return Snapshot{}, err
	}
	runs := []Run{}
	for rows.Next() {
		r, e := scanRun(rows)
		if e != nil {
			rows.Close()
			return Snapshot{}, e
		}
		runs = append(runs, r)
	}
	err = rows.Close()
	if err != nil {
		return Snapshot{}, err
	}
	events, err := s.Events(ctx, id, 0)
	return Snapshot{Conversation: c, Messages: m, Permissions: p, Runs: runs, Events: events}, err
}

func (s *Store) CreatePermission(ctx context.Context, p Permission) (Permission, Event, error) {
	p.ID = uuid.NewString()
	p.Status = "pending"
	p.CreatedAt = time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, Event{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "INSERT INTO permissions VALUES(?,?,?,?,?,?,?)", p.ID, p.ConversationID, p.RunID, p.Tool, string(p.Arguments), p.Status, timestamp(p.CreatedAt))
	if err != nil {
		return p, Event{}, err
	}
	event, err := appendEvent(ctx, tx, p.ConversationID, p.RunID, "PERMISSION_REQUESTED", p)
	if err != nil {
		return p, Event{}, err
	}
	err = tx.Commit()
	return p, event, err
}
func (s *Store) Permissions(ctx context.Context, conversationID string) ([]Permission, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,run_id,tool,arguments,status,created_at FROM permissions WHERE conversation_id=? AND status='pending' ORDER BY created_at LIMIT 100", conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Permission{}
	for rows.Next() {
		p := Permission{ConversationID: conversationID}
		var raw, c string
		if err = rows.Scan(&p.ID, &p.RunID, &p.Tool, &raw, &p.Status, &c); err != nil {
			return nil, err
		}
		p.Arguments = json.RawMessage(raw)
		p.CreatedAt = parseTimestamp(c)
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) RespondPermission(ctx context.Context, conversationID, runID, id, decision string) (Event, error) {
	if decision != "allow" && decision != "deny" {
		return Event{}, errors.New("invalid permission decision")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback()
	var status string
	if err = tx.QueryRowContext(ctx, "SELECT status FROM permissions WHERE id=? AND conversation_id=? AND run_id=?", id, conversationID, runID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	if err != nil {
		return Event{}, err
	}
	var runStatus string
	if err = tx.QueryRowContext(ctx, "SELECT status FROM runs WHERE id=? AND conversation_id=?", runID, conversationID).Scan(&runStatus); err != nil {
		return Event{}, err
	}
	if runStatus != "running" {
		return Event{}, ErrReceiptConflict
	}
	if status != "pending" {
		if status == decision {
			return Event{}, nil
		}
		return Event{}, ErrReceiptConflict
	}
	_, err = tx.ExecContext(ctx, "UPDATE permissions SET status=? WHERE id=? AND status='pending'", decision, id)
	if err != nil {
		return Event{}, err
	}
	event, err := appendEvent(ctx, tx, conversationID, runID, "PERMISSION_RESPONDED", map[string]string{"permission_id": id, "decision": decision})
	if err != nil {
		return Event{}, err
	}
	err = tx.Commit()
	return event, err
}

func (s *Store) recoverInterrupted(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, "SELECT id,conversation_id FROM runs WHERE status='running'")
	if err != nil {
		return err
	}
	type interrupted struct{ id, conversation string }
	var pending []interrupted
	for rows.Next() {
		var r interrupted
		if err = rows.Scan(&r.id, &r.conversation); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, r)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, run := range pending {
		if _, err = s.closeUnresolvedTools(ctx, run.conversation, run.id); err != nil {
			return err
		}
		if _, err = s.Finish(ctx, run.id, "interrupted", "process_restarted"); err != nil {
			return err
		}
	}
	return nil
}

// Cancellation can happen after an assistant batch is persisted but before its
// next tool begins. Close every unmatched call so continuation history remains
// valid, without pretending the effect is known or replaying any operation.
func (s *Store) closeUnresolvedTools(ctx context.Context, conversationID, runID string) ([]Event, error) {
	messages, err := s.Messages(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	unresolved := map[string]bool{}
	order := []string{}
	for _, m := range messages {
		if m.RunID != runID {
			continue
		}
		if m.Role == "assistant" {
			for _, call := range m.ToolCalls {
				unresolved[call.ID] = true
				order = append(order, call.ID)
			}
		} else if m.Role == "tool" {
			delete(unresolved, m.ToolCallID)
		}
	}
	events := []Event{}
	for _, id := range order {
		if !unresolved[id] {
			continue
		}
		_, event, err := s.AppendMessage(ctx, conversationID, runID, modelruntime.Message{Role: "tool", ToolCallID: id, Content: `{"is_error":true,"status":"unknown","error":"The local run ended before this tool result was saved; inspect its outcome before repeating it. Do not replay it automatically."}`}, false)
		if err != nil {
			return events, err
		}
		delete(unresolved, id)
		events = append(events, event)
	}
	return events, nil
}
