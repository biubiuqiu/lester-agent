package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/argon2"
)

type Principal struct {
	UserID      uuid.UUID `json:"user_id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	AvatarKey   string    `json:"avatar_key"`
}
type contextKey struct{}

var allowedAvatarKeys = map[string]struct{}{
	"forest": {}, "ocean": {}, "clay": {}, "lilac": {}, "amber": {}, "graphite": {},
}

func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(contextKey{}).(Principal)
	return p, ok
}

type Service struct {
	db     *pgxpool.Pool
	redis  *redis.Client
	ttl    time.Duration
	secure bool
}

func New(db *pgxpool.Pool, redisClient *redis.Client, ttl time.Duration, secure bool) *Service {
	return &Service{db: db, redis: redisClient, ttl: ttl, secure: secure}
}

func (s *Service) Register(w http.ResponseWriter, r *http.Request) {
	var req struct{ Email, Password, DisplayName string }
	if !httpapi.Decode(w, r, &req) {
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if !s.allowAttempt(r, "register", req.Email, 5) {
		w.Header().Set("Retry-After", "60")
		httpapi.Error(w, http.StatusTooManyRequests, errors.New("too many registration attempts; try again in one minute"))
		return
	}
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if !strings.Contains(req.Email, "@") || len(req.Password) < 10 || req.DisplayName == "" {
		httpapi.Error(w, 400, errors.New("valid email, display name, and password of at least 10 characters are required"))
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	session, err := newSession(s.ttl)
	if err != nil {
		httpapi.Error(w, http.StatusInternalServerError, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	defer tx.Rollback(r.Context())
	var userID, workspaceID uuid.UUID
	if err = tx.QueryRow(r.Context(), `INSERT INTO users(email,display_name,password_hash) VALUES($1,$2,$3) RETURNING id`, req.Email, req.DisplayName, hash).Scan(&userID); err != nil {
		httpapi.Error(w, 409, errors.New("account already exists"))
		return
	}
	if err = tx.QueryRow(r.Context(), `INSERT INTO workspaces(name) VALUES($1) RETURNING id`, req.DisplayName+" 的 Personal Workspace").Scan(&workspaceID); err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2)`, workspaceID, userID); err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	if err = insertSession(r.Context(), tx, userID, session); err != nil {
		httpapi.Error(w, http.StatusInternalServerError, fmt.Errorf("create session: %w", err))
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	s.setCookie(w, session)
	httpapi.JSON(w, 201, map[string]any{"user_id": userID, "workspace_id": workspaceID})
}

func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	var req struct{ Email, Password string }
	if !httpapi.Decode(w, r, &req) {
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if !s.allowAttempt(r, "login", req.Email, 10) {
		w.Header().Set("Retry-After", "60")
		httpapi.Error(w, http.StatusTooManyRequests, errors.New("too many login attempts; try again in one minute"))
		return
	}
	var id uuid.UUID
	var hash string
	if err := s.db.QueryRow(r.Context(), `SELECT id,password_hash FROM users WHERE email=$1`, req.Email).Scan(&id, &hash); err != nil || !verifyPassword(req.Password, hash) {
		httpapi.Error(w, 401, errors.New("invalid email or password"))
		return
	}
	session, err := newSession(s.ttl)
	if err != nil {
		httpapi.Error(w, http.StatusInternalServerError, err)
		return
	}
	if err = insertSession(r.Context(), s.db, id, session); err != nil {
		httpapi.Error(w, http.StatusInternalServerError, fmt.Errorf("create session: %w", err))
		return
	}
	s.setCookie(w, session)
	w.WriteHeader(204)
}
func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("lester_session"); err == nil {
		if raw, e := base64.RawURLEncoding.DecodeString(cookie.Value); e == nil {
			sum := sha256.Sum256(raw)
			_, _ = s.db.Exec(r.Context(), `DELETE FROM sessions WHERE token_hash=$1`, sum[:])
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "lester_session", Value: "", Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.WriteHeader(204)
}
func (s *Service) Me(w http.ResponseWriter, r *http.Request) {
	p, _ := FromContext(r.Context())
	httpapi.JSON(w, 200, p)
}

func (s *Service) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	p, _ := FromContext(r.Context())
	var req struct {
		DisplayName string `json:"display_name"`
		AvatarKey   string `json:"avatar_key"`
	}
	if !httpapi.Decode(w, r, &req) {
		return
	}
	displayName, avatarKey, err := normalizeProfile(req.DisplayName, req.AvatarKey)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, err)
		return
	}
	if _, err = s.db.Exec(r.Context(), `UPDATE users SET display_name=$2,avatar_key=$3 WHERE id=$1`, p.UserID, displayName, avatarKey); err != nil {
		httpapi.Error(w, http.StatusInternalServerError, fmt.Errorf("update profile: %w", err))
		return
	}
	p.DisplayName = displayName
	p.AvatarKey = avatarKey
	httpapi.JSON(w, http.StatusOK, p)
}

func normalizeProfile(displayName, avatarKey string) (string, string, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || len([]rune(displayName)) > 60 {
		return "", "", errors.New("display name must be between 1 and 60 characters")
	}
	if _, ok := allowedAvatarKeys[avatarKey]; !ok {
		return "", "", errors.New("invalid avatar")
	}
	return displayName, avatarKey, nil
}

func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("lester_session")
		if err != nil {
			httpapi.Error(w, 401, errors.New("authentication required"))
			return
		}
		raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
		if err != nil {
			httpapi.Error(w, 401, errors.New("invalid session"))
			return
		}
		sum := sha256.Sum256(raw)
		var p Principal
		err = s.db.QueryRow(r.Context(), `SELECT u.id,u.email,u.display_name,wm.workspace_id,COALESCE(u.avatar_key,'forest') FROM sessions s JOIN users u ON u.id=s.user_id JOIN workspace_members wm ON wm.user_id=u.id WHERE s.token_hash=$1 AND s.expires_at>now() ORDER BY wm.workspace_id LIMIT 1`, sum[:]).Scan(&p.UserID, &p.Email, &p.DisplayName, &p.WorkspaceID, &p.AvatarKey)
		if err != nil {
			httpapi.Error(w, 401, errors.New("session expired"))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, p)))
	})
}

type sessionRecord struct {
	raw     []byte
	expires time.Time
}

type sessionExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func newSession(ttl time.Duration) (sessionRecord, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return sessionRecord{}, fmt.Errorf("generate session token: %w", err)
	}
	return sessionRecord{raw: raw, expires: time.Now().Add(ttl)}, nil
}

func insertSession(ctx context.Context, executor sessionExecutor, userID uuid.UUID, session sessionRecord) error {
	sum := sha256.Sum256(session.raw)
	_, err := executor.Exec(ctx, `INSERT INTO sessions(user_id,token_hash,expires_at) VALUES($1,$2,$3)`, userID, sum[:], session.expires)
	return err
}

func (s *Service) setCookie(w http.ResponseWriter, session sessionRecord) {
	http.SetCookie(w, &http.Cookie{Name: "lester_session", Value: base64.RawURLEncoding.EncodeToString(session.raw), Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode, Expires: session.expires})
}

var rateLimitScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
if current == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]) end
return current
`)

func (s *Service) allowAttempt(r *http.Request, action, identity string, limit int64) bool {
	if s.redis == nil {
		return true
	}
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	for _, subject := range []string{"ip:" + ip, "identity:" + identity} {
		digest := sha256.Sum256([]byte(subject))
		key := fmt.Sprintf("auth:rate:%s:%x", action, digest[:12])
		count, err := rateLimitScript.Run(r.Context(), s.redis, []string{key}, 60).Int64()
		if err == nil && count > limit {
			return false
		}
	}
	return true
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}
func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	expected, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}
