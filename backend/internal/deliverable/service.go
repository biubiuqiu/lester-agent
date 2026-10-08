package deliverable

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/biubiuqiu/lester-agent/backend/internal/eventlog"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("conversation not found")
var ErrInactiveRun = errors.New("deliverables can only be registered by the active run")

type Input struct {
	EntryPath string `json:"entry_path"`
	Title     string `json:"title"`
	Summary   string `json:"summary"`
}
type Deliverable struct {
	ID             uuid.UUID `json:"id"`
	ConversationID uuid.UUID `json:"conversation_id"`
	RunID          uuid.UUID `json:"run_id"`
	Title          string    `json:"title"`
	Summary        string    `json:"summary"`
	EntryPath      string    `json:"entry_path"`
	Kind           string    `json:"kind"`
	ContentSHA256  string    `json:"content_sha256"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
type Files interface {
	ReadFile(context.Context, string, string, string) ([]byte, error)
}
type Service struct {
	DB    *pgxpool.Pool
	Files Files
}

func Validate(input Input) (Input, string, error) {
	input.EntryPath = strings.TrimSpace(input.EntryPath)
	input.Title = strings.TrimSpace(input.Title)
	input.Summary = strings.TrimSpace(input.Summary)
	if !utf8.ValidString(input.EntryPath+input.Title+input.Summary) || utf8.RuneCountInString(input.Title) < 1 || utf8.RuneCountInString(input.Title) > 120 || utf8.RuneCountInString(input.Summary) > 2000 {
		return input, "", errors.New("title must contain 1–120 characters and summary at most 2000")
	}
	if input.EntryPath == "" || utf8.RuneCountInString(input.EntryPath) > 1000 || strings.ContainsAny(input.EntryPath, "\\\x00") || path.IsAbs(input.EntryPath) {
		return input, "", errors.New("entry_path must be a relative conversation file")
	}
	for _, part := range strings.Split(input.EntryPath, "/") {
		if part == "" || strings.HasPrefix(part, ".") || part == "agent-resources" || part == "node_modules" || part == "vendor" || part == "venv" || part == "__pycache__" {
			return input, "", errors.New("entry_path contains an unsupported directory")
		}
	}
	ext := strings.ToLower(path.Ext(input.EntryPath))
	switch ext {
	case ".html", ".htm":
		return input, "html", nil
	case ".md":
		return input, "markdown", nil
	default:
		return input, "", errors.New("deliverable entry must be HTML or Markdown")
	}
}

func (s *Service) Register(ctx context.Context, workspaceID, conversationID, runID uuid.UUID, sandboxID, workDir string, input Input) (Deliverable, error) {
	var result Deliverable
	input, kind, err := Validate(input)
	if err != nil {
		return result, err
	}
	if workDir != "/workspace/conversations/"+conversationID.String() {
		return result, errors.New("deliverable directory does not match conversation")
	}
	var exists bool
	if err = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations c JOIN runs r ON r.conversation_id=c.id WHERE c.workspace_id=$1 AND c.id=$2 AND r.id=$3 AND r.status='running')`, workspaceID, conversationID, runID).Scan(&exists); err != nil {
		return result, err
	}
	if !exists {
		return result, ErrInactiveRun
	}
	if s.Files == nil {
		return result, errors.New("deliverable files unavailable")
	}
	content, err := s.Files.ReadFile(ctx, sandboxID, workDir, input.EntryPath)
	if err != nil {
		return result, errors.New("deliverable entry could not be read")
	}
	if len(content) > 25<<20 {
		return result, errors.New("deliverable entry exceeds 25 MiB")
	}
	digest := sha256.Sum256(content)
	hash := hex.EncodeToString(digest[:])
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	var workspace uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT workspace_id FROM conversations WHERE id=$1 AND workspace_id=$2 FOR UPDATE`, conversationID, workspaceID).Scan(&workspace); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, ErrNotFound
		}
		return result, err
	}
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM runs WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, runID, conversationID).Scan(&status); err != nil {
		return result, err
	}
	if status != "running" {
		return result, ErrInactiveRun
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM deliverables WHERE conversation_id=$1 AND entry_path<>$2`, conversationID, input.EntryPath).Scan(&count); err != nil {
		return result, err
	}
	if count >= 200 {
		return result, errors.New("conversation already contains 200 registered deliverables")
	}
	err = tx.QueryRow(ctx, `INSERT INTO deliverables(workspace_id,conversation_id,run_id,title,summary,entry_path,kind,content_sha256)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(conversation_id,entry_path) DO UPDATE SET run_id=EXCLUDED.run_id,title=EXCLUDED.title,summary=EXCLUDED.summary,content_sha256=EXCLUDED.content_sha256,updated_at=now()
        RETURNING id,conversation_id,run_id,title,summary,entry_path,kind,content_sha256,created_at,updated_at`, workspaceID, conversationID, runID, input.Title, input.Summary, input.EntryPath, kind, hash).Scan(&result.ID, &result.ConversationID, &result.RunID, &result.Title, &result.Summary, &result.EntryPath, &result.Kind, &result.ContentSHA256, &result.CreatedAt, &result.UpdatedAt)
	if err != nil {
		return result, err
	}
	if err = eventlog.Append(ctx, tx, runID, conversationID, "DELIVERABLE_REGISTERED", map[string]any{"deliverable_id": result.ID, "entry_path": result.EntryPath, "title": result.Title}); err != nil {
		return result, err
	}
	err = tx.Commit(ctx)
	return result, err
}

func (s *Service) List(ctx context.Context, workspaceID, conversationID uuid.UUID) ([]Deliverable, error) {
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2)`, workspaceID, conversationID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := s.DB.Query(ctx, `SELECT id,conversation_id,run_id,title,summary,entry_path,kind,content_sha256,created_at,updated_at FROM deliverables WHERE workspace_id=$1 AND conversation_id=$2 ORDER BY updated_at DESC,id LIMIT 200`, workspaceID, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Deliverable{}
	for rows.Next() {
		var item Deliverable
		if err = rows.Scan(&item.ID, &item.ConversationID, &item.RunID, &item.Title, &item.Summary, &item.EntryPath, &item.Kind, &item.ContentSHA256, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
