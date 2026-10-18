package conversation

import (
	"context"
	"errors"
	"net/http"

	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/eventlog"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type RunPermissionMode struct {
	RunID    uuid.UUID `json:"run_id"`
	Mode     string    `json:"permission_mode"`
	Revision int64     `json:"revision"`
	Status   string    `json:"status"`
}

func (s *Service) runPermissionMode(ctx context.Context, workspace, conversation, run uuid.UUID) (RunPermissionMode, error) {
	var result RunPermissionMode
	err := s.db.QueryRow(ctx, `SELECT r.id,r.permission_mode,r.permission_revision,r.status FROM runs r JOIN conversations c ON c.id=r.conversation_id WHERE r.id=$1 AND c.id=$2 AND c.workspace_id=$3`, run, conversation, workspace).Scan(&result.RunID, &result.Mode, &result.Revision, &result.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrInteractionNotFound
	}
	return result, err
}

// Call only after locking conversation -> run. Changing mode takes effect at
// the next authorization boundary and never changes queued task snapshots.
func setRunPermissionModeTx(ctx context.Context, tx pgx.Tx, conversation, run, actor uuid.UUID, mode string, revision int64) error {
	if _, err := tx.Exec(ctx, `UPDATE runs SET permission_mode=$2,permission_revision=$3 WHERE id=$1`, run, mode, revision); err != nil {
		return err
	}
	status, decision := "pending", (*string)(nil)
	if mode == "full" {
		status = "approved"
		value := "full"
		decision = &value
	}
	_, err := tx.Exec(ctx, `UPDATE run_permissions SET status=CASE WHEN $6='auto' AND tool IN ('write','edit') AND allow_run THEN 'approved' WHEN $6='auto' AND tool='bash' THEN 'reviewing' ELSE $2 END,decision=CASE WHEN $6='auto' AND tool IN ('write','edit') AND allow_run THEN 'once' ELSE $3 END,decision_source='mode',responded_by=$4,permission_revision=$5,review_token=NULL,review_decision=NULL,review_reason='',reviewer_model_deployment_id=NULL WHERE run_id=$1 AND (status IN ('pending','reviewing') OR status='approved' AND consumed_at IS NULL)`, run, status, decision, actor, revision, mode)
	return err
}

func (s *Service) consumePermission(ctx context.Context, conversation, run, id uuid.UUID, digest string) (bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM conversations WHERE id=$1 FOR UPDATE`, conversation); err != nil {
		return false, err
	}
	var status, mode string
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT status,execution_mode,permission_revision FROM runs WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, run, conversation).Scan(&status, &mode, &revision); err != nil {
		return false, err
	}
	if status != "running" {
		return false, ErrRunNotActive
	}
	var tool, permissionStatus, recordedDigest string
	var permissionRevision int64
	var consumed bool
	if err = tx.QueryRow(ctx, `SELECT tool,status,request_digest,permission_revision,consumed_at IS NOT NULL FROM run_permissions WHERE id=$1 AND run_id=$2 FOR UPDATE`, id, run).Scan(&tool, &permissionStatus, &recordedDigest, &permissionRevision, &consumed); err != nil {
		return false, err
	}
	if recordedDigest != digest || permissionRevision != revision {
		return false, ErrInteractionConflict
	}
	if permissionStatus != "approved" {
		return false, tx.Commit(ctx)
	}
	if consumed {
		return false, errors.New("permission was already consumed; do not replay this operation")
	}
	if mode == "plan" && !agenttool.PlanReadOnlyTool(tool) {
		return false, ErrInteractionConflict
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if _, err = tx.Exec(ctx, `UPDATE run_permissions SET consumed_at=now() WHERE id=$1`, id); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (s *Service) changeRunPermissionMode(ctx context.Context, workspace, actor, conversation, run uuid.UUID, mode string, expected int64) (RunPermissionMode, error) {
	var result RunPermissionMode
	if _, err := permissionMode(mode); err != nil || mode == "" {
		return result, errors.New("invalid permission mode")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	var allowed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations c JOIN workspace_members m ON m.workspace_id=c.workspace_id JOIN users u ON u.id=m.user_id WHERE c.id=$1 AND c.workspace_id=$2 AND m.user_id=$3 AND NOT u.disabled)`, conversation, workspace, actor).Scan(&allowed); err != nil {
		return result, err
	}
	if !allowed {
		return result, ErrInteractionNotFound
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM conversations WHERE id=$1 FOR UPDATE`, conversation); err != nil {
		return result, err
	}
	err = tx.QueryRow(ctx, `SELECT id,permission_mode,permission_revision,status FROM runs WHERE id=$1 AND conversation_id=$2 FOR UPDATE`, run, conversation).Scan(&result.RunID, &result.Mode, &result.Revision, &result.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrInteractionNotFound
	}
	if err != nil {
		return result, err
	}
	if result.Status != "running" || result.Revision != expected {
		return result, ErrInteractionConflict
	}
	if result.Mode == mode {
		return result, tx.Commit(ctx)
	}
	result.Mode = mode
	result.Revision++
	if err = setRunPermissionModeTx(ctx, tx, conversation, run, actor, mode, result.Revision); err != nil {
		return result, err
	}
	if err = eventlog.Append(ctx, tx, run, conversation, "PERMISSION_MODE_CHANGED", map[string]any{"permission_mode": mode, "revision": result.Revision, "changed_by": actor}); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (h *Handler) RunPermissionMode(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.FromContext(r.Context())
	conversation, ok := pathID(w, r)
	if !ok {
		return
	}
	run, err := uuid.Parse(chi.URLParam(r, "runID"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid run ID"))
		return
	}
	if err = h.service.reconcileInteractionOwner(r.Context(), p.WorkspaceID, conversation); err != nil {
		httpapi.Error(w, 503, errors.New("permission status unavailable"))
		return
	}
	if r.Method == http.MethodGet {
		item, err := h.service.runPermissionMode(r.Context(), p.WorkspaceID, conversation, run)
		if err != nil {
			permissionHTTPError(w, err)
			return
		}
		httpapi.JSON(w, 200, item)
		return
	}
	var input struct {
		Mode     string `json:"permission_mode"`
		Revision *int64 `json:"revision"`
	}
	if !httpapi.Decode(w, r, &input) {
		return
	}
	if input.Revision == nil || *input.Revision < 0 {
		httpapi.Error(w, 400, errors.New("permission revision required"))
		return
	}
	item, err := h.service.changeRunPermissionMode(r.Context(), p.WorkspaceID, p.UserID, conversation, run, input.Mode, *input.Revision)
	if err != nil {
		permissionHTTPError(w, err)
		return
	}
	httpapi.JSON(w, 200, item)
}
