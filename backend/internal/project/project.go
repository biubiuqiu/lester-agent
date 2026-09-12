package project

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Project struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	IsDefault         bool      `json:"is_default"`
	Pinned            bool      `json:"pinned"`
	ConversationCount int       `json:"conversation_count"`
	CreatedAt         time.Time `json:"created_at"`
}
type Service struct{ DB *pgxpool.Pool }

func ValidName(name string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(name))
	return n > 0 && n <= 80
}
func (s *Service) List(ctx context.Context, workspace uuid.UUID) ([]Project, error) {
	rows, err := s.DB.Query(ctx, `SELECT p.id,p.name,p.is_default,p.pinned,count(c.id),p.created_at FROM projects p LEFT JOIN conversations c ON c.project_id=p.id AND c.workspace_id=p.workspace_id WHERE p.workspace_id=$1 GROUP BY p.id ORDER BY p.pinned DESC,p.is_default DESC,p.created_at,p.id`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Project{}
	for rows.Next() {
		var p Project
		if err = rows.Scan(&p.ID, &p.Name, &p.IsDefault, &p.Pinned, &p.ConversationCount, &p.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}
func (s *Service) Create(ctx context.Context, workspace uuid.UUID, name string) (Project, error) {
	var p Project
	name = strings.TrimSpace(name)
	if !ValidName(name) {
		return p, errors.New("项目名称需要 1–80 个字符")
	}
	err := s.DB.QueryRow(ctx, `INSERT INTO projects(workspace_id,name) VALUES($1,$2) RETURNING id,name,is_default,pinned,created_at`, workspace, name).Scan(&p.ID, &p.Name, &p.IsDefault, &p.Pinned, &p.CreatedAt)
	return p, err
}
func (s *Service) Update(ctx context.Context, workspace, id uuid.UUID, name *string, pinned *bool) error {
	if name != nil {
		v := strings.TrimSpace(*name)
		name = &v
		if !ValidName(v) {
			return errors.New("项目名称需要 1–80 个字符")
		}
	}
	tag, err := s.DB.Exec(ctx, `UPDATE projects SET name=COALESCE($3,name),pinned=COALESCE($4,pinned),updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspace, id, name, pinned)
	if err == nil && tag.RowsAffected() == 0 {
		return errors.New("项目不存在")
	}
	return err
}

type Handler struct{ Service *Service }

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	items, err := h.Service.List(r.Context(), p.WorkspaceID)
	if err != nil {
		httpapi.Error(w, 500, errors.New("项目加载失败"))
		return
	}
	httpapi.JSON(w, 200, map[string]any{"projects": items})
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if !httpapi.Decode(w, r, &req) {
		return
	}
	item, err := h.Service.Create(r.Context(), p.WorkspaceID, req.Name)
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	httpapi.JSON(w, 201, item)
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid project id"))
		return
	}
	var req struct {
		Name   *string `json:"name"`
		Pinned *bool   `json:"pinned"`
	}
	if !httpapi.Decode(w, r, &req) {
		return
	}
	if err = h.Service.Update(r.Context(), p.WorkspaceID, id, req.Name, req.Pinned); err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	w.WriteHeader(204)
}
