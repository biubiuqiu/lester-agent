package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"unicode/utf8"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Rules struct {
	ProjectID   uuid.UUID `json:"project_id"`
	ProjectName string    `json:"project_name"`
	Source      string    `json:"source"`
	Version     int64     `json:"version"`
	SHA256      string    `json:"sha256"`
	Content     string    `json:"content"`
}

func (r *Rules) Digest() {
	digest := sha256.Sum256([]byte(r.Content))
	r.SHA256 = hex.EncodeToString(digest[:])
	r.Source = "project_settings"
}
func (s *Service) Rules(ctx context.Context, workspace, id uuid.UUID) (Rules, error) {
	r := Rules{ProjectID: id}
	err := s.DB.QueryRow(ctx, `SELECT name,rules_content,rules_version FROM projects WHERE workspace_id=$1 AND id=$2`, workspace, id).Scan(&r.ProjectName, &r.Content, &r.Version)
	r.Digest()
	return r, err
}

var ErrRulesConflict = errors.New("project rules changed; reload before saving")

func (s *Service) SaveRules(ctx context.Context, workspace, id uuid.UUID, content string, version int64) (Rules, error) {
	if !utf8.ValidString(content) || utf8.RuneCountInString(content) > 20000 || version < 1 {
		return Rules{}, errors.New("invalid project rules")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Rules{}, err
	}
	defer tx.Rollback(ctx)
	r := Rules{ProjectID: id}
	err = tx.QueryRow(ctx, `SELECT name,rules_content,rules_version FROM projects WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspace, id).Scan(&r.ProjectName, &r.Content, &r.Version)
	if err != nil {
		return r, err
	}
	// Lost response retries with exactly matching content are idempotent.
	if r.Content != content {
		if r.Version != version {
			return r, ErrRulesConflict
		}
		r.Content = content
		r.Version++
		if _, err = tx.Exec(ctx, `UPDATE projects SET rules_content=$2,rules_version=$3,updated_at=now() WHERE id=$1`, id, content, r.Version); err != nil {
			return r, err
		}
	}
	r.Digest()
	return r, tx.Commit(ctx)
}
func (h *Handler) Rules(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid project id"))
		return
	}
	item, err := h.Service.Rules(r.Context(), p.WorkspaceID, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpapi.Error(w, 404, errors.New("project not found"))
		} else {
			httpapi.Error(w, 503, errors.New("project rules unavailable"))
		}
		return
	}
	httpapi.JSON(w, 200, item)
}
func (h *Handler) SaveRules(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid project id"))
		return
	}
	var request struct {
		Content string `json:"content"`
		Version int64  `json:"version"`
	}
	if !httpapi.Decode(w, r, &request) {
		return
	}
	item, err := h.Service.SaveRules(r.Context(), p.WorkspaceID, id, request.Content, request.Version)
	if err != nil {
		switch {
		case errors.Is(err, ErrRulesConflict):
			httpapi.Error(w, 409, err)
		case errors.Is(err, pgx.ErrNoRows):
			httpapi.Error(w, 404, errors.New("project not found"))
		default:
			httpapi.Error(w, 400, errors.New("project rules could not be saved"))
		}
		return
	}
	httpapi.JSON(w, 200, item)
}
