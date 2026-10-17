package deliverable

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var ErrProjectNotFound = errors.New("项目不存在或无法访问")
var ErrProjectQuery = errors.New("项目成果查询无效，请刷新后重试")

type ProjectQuery struct {
	Search, Kind, Cursor string
}

type ProjectDeliverable struct {
	Deliverable
	ConversationTitle string `json:"conversation_title"`
	RunStatus         string `json:"run_status"`
}

type ProjectPage struct {
	Deliverables []ProjectDeliverable `json:"deliverables"`
	NextCursor   string               `json:"next_cursor"`
}

type projectCursor struct {
	Project uuid.UUID `json:"project"`
	Search  string    `json:"search"`
	Kind    string    `json:"kind"`
	Updated time.Time `json:"updated"`
	ID      uuid.UUID `json:"id"`
}

// ProjectList reads the durable catalogue only. It never wakes Computers or
// treats registration as proof that the live file still exists or passed review.
func (s *Service) ProjectList(ctx context.Context, workspace, project uuid.UUID, query ProjectQuery) (ProjectPage, error) {
	page := ProjectPage{Deliverables: []ProjectDeliverable{}}
	query.Search = strings.TrimSpace(query.Search)
	if !utf8.ValidString(query.Search) || utf8.RuneCountInString(query.Search) > 120 || len(query.Cursor) > 1024 || (query.Kind != "" && query.Kind != "html" && query.Kind != "markdown") {
		return page, ErrProjectQuery
	}
	var cursor projectCursor
	if query.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(query.Cursor)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.ID == uuid.Nil || cursor.Updated.IsZero() || cursor.Project != project || cursor.Search != query.Search || cursor.Kind != query.Kind {
			return page, ErrProjectQuery
		}
	}
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE workspace_id=$1 AND id=$2)`, workspace, project).Scan(&exists); err != nil {
		return page, err
	}
	if !exists {
		return page, ErrProjectNotFound
	}
	rows, err := s.DB.Query(ctx, `SELECT d.id,d.conversation_id,d.run_id,d.title,d.summary,d.entry_path,d.kind,d.content_sha256,d.created_at,d.updated_at,c.title,COALESCE(r.status,'unknown')
		FROM conversations c JOIN deliverables d ON d.conversation_id=c.id AND d.workspace_id=c.workspace_id
		LEFT JOIN runs r ON r.id=d.run_id AND r.conversation_id=c.id
		WHERE c.workspace_id=$1 AND c.project_id=$2 AND ($3='' OR d.kind=$3)
		AND ($4='' OR strpos(lower(d.title||' '||d.entry_path||' '||c.title),lower($4))>0)
		AND ($5::timestamptz IS NULL OR (d.updated_at,d.id)<($5,$6::uuid))
		ORDER BY d.updated_at DESC,d.id DESC LIMIT 25`, workspace, project, query.Kind, query.Search, nullableCursorTime(cursor.Updated), cursor.ID)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ProjectDeliverable
		if err = rows.Scan(&item.ID, &item.ConversationID, &item.RunID, &item.Title, &item.Summary, &item.EntryPath, &item.Kind, &item.ContentSHA256, &item.CreatedAt, &item.UpdatedAt, &item.ConversationTitle, &item.RunStatus); err != nil {
			return page, err
		}
		page.Deliverables = append(page.Deliverables, item)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if len(page.Deliverables) > 24 {
		page.Deliverables = page.Deliverables[:24]
		last := page.Deliverables[23]
		raw, _ := json.Marshal(projectCursor{Project: project, Search: query.Search, Kind: query.Kind, Updated: last.UpdatedAt, ID: last.ID})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}

func nullableCursorTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func (h *Handler) ProjectList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || id == uuid.Nil {
		httpapi.Error(w, 400, ErrProjectQuery)
		return
	}
	query := r.URL.Query()
	page, err := h.Service.ProjectList(r.Context(), p.WorkspaceID, id, ProjectQuery{Search: query.Get("q"), Kind: query.Get("kind"), Cursor: query.Get("cursor")})
	if err != nil {
		switch {
		case errors.Is(err, ErrProjectNotFound):
			httpapi.Error(w, 404, err)
		case errors.Is(err, ErrProjectQuery):
			httpapi.Error(w, 400, err)
		default:
			httpapi.Error(w, 503, errors.New("项目成果加载失败，请重试"))
		}
		return
	}
	httpapi.JSON(w, 200, page)
}
