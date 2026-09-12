package conversation

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/google/uuid"
)

type OrganizationUpdate struct {
	ProjectID *uuid.UUID `json:"project_id"`
	Pinned    *bool      `json:"pinned"`
	Title     *string    `json:"title"`
}

func (s *Service) Organize(ctx context.Context, workspace, id uuid.UUID, in OrganizationUpdate) error {
	if in.Title != nil {
		v := strings.TrimSpace(*in.Title)
		if v == "" || utf8.RuneCountInString(v) > 120 {
			return errors.New("会话标题需要 1–120 个字符")
		}
		in.Title = &v
	}
	tag, err := s.db.Exec(ctx, `UPDATE conversations SET project_id=COALESCE($3,project_id),pinned=COALESCE($4,pinned),title=COALESCE($5,title) WHERE workspace_id=$1 AND id=$2 AND ($3::uuid IS NULL OR EXISTS(SELECT 1 FROM projects WHERE workspace_id=$1 AND id=$3))`, workspace, id, in.ProjectID, in.Pinned, in.Title)
	if err == nil && tag.RowsAffected() == 0 {
		return errors.New("会话或项目不存在")
	}
	return err
}
func (h *Handler) Organize(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in OrganizationUpdate
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if err := h.service.Organize(r.Context(), p.WorkspaceID, id, in); err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	w.WriteHeader(204)
}
