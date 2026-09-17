package auth

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// RequireAdmin runs after session authentication. Roles are read from the DB on
// every request, so changing a role does not leave privileged sessions behind.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := FromContext(r.Context())
		if !ok || p.Role != "admin" {
			httpapi.Error(w, http.StatusForbidden, errors.New("administrator access required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

type ManagedUser struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	Disabled    bool      `json:"disabled"`
	CreatedAt   time.Time `json:"created_at"`
}
type UserInput struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	Role        string `json:"role"`
}
type UserChange struct {
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Disabled    bool   `json:"disabled"`
	Password    string `json:"password"`
}

func validRole(role string) bool { return role == "admin" || role == "member" }
func validateManagedUser(name, role, password string, create bool) error {
	if strings.TrimSpace(name) == "" || len([]rune(strings.TrimSpace(name))) > 60 {
		return errors.New("display name must be between 1 and 60 characters")
	}
	if !validRole(role) {
		return errors.New("invalid role")
	}
	if (create || password != "") && (len(password) < 10 || len(password) > 1024) {
		return errors.New("password must be between 10 and 1024 bytes")
	}
	return nil
}

func (s *Service) ListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `SELECT id,email,display_name,role,disabled,created_at FROM users ORDER BY created_at,id`)
	if err != nil {
		adminFailure(w, err)
		return
	}
	defer rows.Close()
	items := []ManagedUser{}
	for rows.Next() {
		var u ManagedUser
		if err = rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &u.Disabled, &u.CreatedAt); err != nil {
			adminFailure(w, err)
			return
		}
		items = append(items, u)
	}
	if err = rows.Err(); err != nil {
		adminFailure(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"users": items})
}

func (s *Service) CreateManagedUser(ctx context.Context, in UserInput) (ManagedUser, error) {
	var u ManagedUser
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	parsed, err := mail.ParseAddress(in.Email)
	if err != nil || parsed.Address != in.Email {
		return u, errors.New("valid email required")
	}
	if err = validateManagedUser(in.DisplayName, in.Role, in.Password, true); err != nil {
		return u, err
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		return u, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return u, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `INSERT INTO users(email,display_name,password_hash,role) VALUES($1,$2,$3,$4) RETURNING id,email,display_name,role,disabled,created_at`, in.Email, in.DisplayName, hash, in.Role).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &u.Disabled, &u.CreatedAt)
	if err != nil {
		return u, err
	}
	var workspace uuid.UUID
	if err = tx.QueryRow(ctx, `INSERT INTO workspaces(name) VALUES($1) RETURNING id`, in.DisplayName+" 的 Personal Workspace").Scan(&workspace); err != nil {
		return u, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2)`, workspace, u.ID); err != nil {
		return u, err
	}
	return u, tx.Commit(ctx)
}

func (s *Service) ChangeManagedUser(ctx context.Context, actor, id uuid.UUID, in UserChange) error {
	if err := validateManagedUser(in.DisplayName, in.Role, in.Password, false); err != nil {
		return err
	}
	if actor == id && (in.Disabled || in.Role != "admin") {
		return errors.New("you cannot disable or demote your own account")
	}
	var hash string
	if in.Password != "" {
		var err error
		hash, err = hashPassword(in.Password)
		if err != nil {
			return err
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialize role/status mutations across all API replicas, including the actor
	// check: an administrator demoted while waiting cannot continue the operation.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(764203117)`); err != nil {
		return err
	}
	var authorized bool
	if err = tx.QueryRow(ctx, `SELECT role='admin' AND NOT disabled FROM users WHERE id=$1`, actor).Scan(&authorized); err != nil {
		return err
	}
	if !authorized {
		return errors.New("administrator access required")
	}
	var role string
	var disabled bool
	if err = tx.QueryRow(ctx, `SELECT role,disabled FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&role, &disabled); err != nil {
		return err
	}
	if role == "admin" && !disabled && (in.Disabled || in.Role != "admin") {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='admin' AND NOT disabled`).Scan(&count); err != nil {
			return err
		}
		if count <= 1 {
			return errors.New("at least one active administrator is required")
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET display_name=$2,role=$3,disabled=$4,password_hash=CASE WHEN $5='' THEN password_hash ELSE $5 END WHERE id=$1`, id, strings.TrimSpace(in.DisplayName), in.Role, in.Disabled, hash); err != nil {
		return err
	}
	if in.Disabled || hash != "" || role != in.Role {
		if _, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Service) AddUser(w http.ResponseWriter, r *http.Request) {
	var in UserInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	u, err := s.CreateManagedUser(r.Context(), in)
	if err != nil {
		adminFailure(w, err)
		return
	}
	httpapi.JSON(w, 201, u)
}
func (s *Service) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpapi.Error(w, 400, errors.New("invalid user ID"))
		return
	}
	var in UserChange
	if !httpapi.Decode(w, r, &in) {
		return
	}
	p, _ := FromContext(r.Context())
	if err = s.ChangeManagedUser(r.Context(), p.UserID, id, in); err != nil {
		adminFailure(w, err)
		return
	}
	w.WriteHeader(204)
}
func adminFailure(w http.ResponseWriter, err error) {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpapi.Error(w, 404, errors.New("user not found"))
	case errors.As(err, &pgErr):
		if pgErr.Code == "23505" {
			httpapi.Error(w, 409, errors.New("account already exists"))
		} else {
			httpapi.Error(w, 500, errors.New("could not save account"))
		}
	default:
		httpapi.Error(w, 400, err)
	}
}
