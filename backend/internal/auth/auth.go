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
	"net/url"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/argon2"
)

type Principal struct {
	UserID         uuid.UUID `json:"user_id"`
	WorkspaceID    uuid.UUID `json:"workspace_id"`
	Email          string    `json:"email"`
	DisplayName    string    `json:"display_name"`
	AvatarKey      string    `json:"avatar_key"`
	Role           string    `json:"role"`
	AvatarURL      string    `json:"avatar_url,omitempty"`
	EmailVerified  bool      `json:"email_verified"`
	HasPassword    bool      `json:"has_password"`
	SessionHash    []byte    `json:"-"`
	AccessExpires  time.Time `json:"-"`
	RefreshExpires time.Time `json:"-"`
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
	db        *pgxpool.Pool
	redis     *redis.Client
	accessTTL time.Duration
	secure    bool
	options   Options
}

func New(db *pgxpool.Pool, redisClient *redis.Client, accessTTL time.Duration, secure bool) *Service {
	return &Service{db: db, redis: redisClient, accessTTL: accessTTL, secure: secure, options: Options{RegistrationEnabled: true}}
}

func (s *Service) Register(w http.ResponseWriter, r *http.Request) {
	if !s.options.RegistrationEnabled {
		httpapi.Error(w, 403, errors.New("当前部署已关闭新账号注册"))
		return
	}
	var req struct{ Email, Password, DisplayName string }
	if !httpapi.Decode(w, r, &req) {
		return
	}
	email, err := normalizeEmail(req.Email)
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	if !s.allowAttempt(r, "register", email, 5) {
		w.Header().Set("Retry-After", "60")
		httpapi.Error(w, 429, errors.New("注册尝试过于频繁，请稍后再试"))
		return
	}
	name, _, err := normalizeProfile(req.DisplayName, "forest")
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	if err = validatePassword(req.Password); err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		httpapi.Error(w, 500, errors.New("暂时无法创建账号"))
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpapi.Error(w, 500, errors.New("暂时无法创建账号"))
		return
	}
	defer tx.Rollback(r.Context())
	userID, workspaceID, err := createAccount(r.Context(), tx, email, name, &hash, false, s.options.Mailer != nil)
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			httpapi.Error(w, 409, errors.New("该邮箱已注册，请登录或找回密码"))
		} else {
			httpapi.Error(w, 500, errors.New("暂时无法创建账号"))
		}
		return
	}
	var token string
	var session sessionRecord
	if s.options.Mailer != nil {
		token, err = issueEmailToken(r.Context(), tx, userID, "verify")
	} else {
		session, err = newSession(s.accessTTL)
		if err == nil {
			err = insertSession(r.Context(), tx, userID, session)
		}
	}
	if err != nil {
		httpapi.Error(w, 500, errors.New("暂时无法创建账号"))
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		httpapi.Error(w, 500, errors.New("暂时无法创建账号"))
		return
	}
	if s.options.Mailer != nil {
		if err = s.sendAccountMail(r.Context(), email, "verify", token); err != nil {
			httpapi.Error(w, 503, errors.New("账号已创建，但验证邮件发送失败。请稍后重新发送验证邮件"))
			return
		}
		httpapi.JSON(w, 202, map[string]any{"verification_required": true})
		return
	}
	s.setCookie(w, session)
	httpapi.JSON(w, 201, map[string]any{"user_id": userID, "workspace_id": workspaceID, "verification_required": false})
}

func createAccount(ctx context.Context, tx pgx.Tx, email, name string, password *string, verified, verificationRequired bool) (uuid.UUID, uuid.UUID, error) {
	var userID, workspaceID uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO users(email,display_name,password_hash,email_verified,email_verification_required) VALUES($1,$2,$3,$4,$5) RETURNING id`, email, name, password, verified, verificationRequired).Scan(&userID)
	if err != nil {
		return userID, workspaceID, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO workspaces(name) VALUES($1) RETURNING id`, name+" 的 Personal Workspace").Scan(&workspaceID)
	if err != nil {
		return userID, workspaceID, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id) VALUES($1,$2)`, workspaceID, userID)
	return userID, workspaceID, err
}

func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	var req struct{ Email, Password string }
	if !httpapi.Decode(w, r, &req) {
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if len(req.Password) > 1024 || len(req.Email) > 254 {
		httpapi.Error(w, 401, errors.New("邮箱或密码不正确"))
		return
	}
	if !s.allowAttempt(r, "login", req.Email, 10) {
		w.Header().Set("Retry-After", "60")
		httpapi.Error(w, http.StatusTooManyRequests, errors.New("too many login attempts; try again in one minute"))
		return
	}
	var id uuid.UUID
	var hash *string
	var verificationRequired, verified bool
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpapi.Error(w, 500, errors.New("could not sign in"))
		return
	}
	defer tx.Rollback(r.Context())
	// Coordinate login with password resets and account suspension. Otherwise a
	// stale password check could insert a session after the admin revoked it.
	if err := tx.QueryRow(r.Context(), `SELECT id,password_hash,email_verification_required,email_verified FROM users WHERE email=$1 AND NOT disabled FOR SHARE`, req.Email).Scan(&id, &hash, &verificationRequired, &verified); err != nil || hash == nil || !verifyPassword(req.Password, *hash) {
		httpapi.Error(w, 401, errors.New("邮箱或密码不正确"))
		return
	}
	if verificationRequired && !verified {
		httpapi.Error(w, 403, errors.New("请先验证邮箱，再登录；可在登录页重新发送验证邮件"))
		return
	}
	session, err := newSession(s.accessTTL)
	if err != nil {
		httpapi.Error(w, http.StatusInternalServerError, err)
		return
	}
	if err = insertSession(r.Context(), tx, id, session); err != nil {
		httpapi.Error(w, http.StatusInternalServerError, fmt.Errorf("create session: %w", err))
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		httpapi.Error(w, 500, errors.New("could not sign in"))
		return
	}
	s.setCookie(w, session)
	w.WriteHeader(204)
}
func (s *Service) Me(w http.ResponseWriter, r *http.Request) {
	p, _ := FromContext(r.Context())
	httpapi.JSON(w, 200, p)
}

func (s *Service) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	p, _ := FromContext(r.Context())
	var req struct {
		DisplayName      string `json:"display_name"`
		AvatarKey        string `json:"avatar_key"`
		UseDefaultAvatar bool   `json:"use_default_avatar"`
	}
	if !httpapi.Decode(w, r, &req) {
		return
	}
	displayName, avatarKey, err := normalizeProfile(req.DisplayName, req.AvatarKey)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpapi.Error(w, 500, errors.New("资料保存失败"))
		return
	}
	defer tx.Rollback(r.Context())
	if err = lockAccountSession(r.Context(), tx, p.UserID, sessionDigest(r)); err != nil {
		httpapi.Error(w, 401, errors.New("请重新登录"))
		return
	}
	var old *string
	if req.UseDefaultAvatar {
		if err = tx.QueryRow(r.Context(), `SELECT avatar_object_key FROM users WHERE id=$1`, p.UserID).Scan(&old); err != nil {
			httpapi.Error(w, 500, errors.New("资料保存失败"))
			return
		}
	}
	if _, err = tx.Exec(r.Context(), `UPDATE users SET display_name=$2,avatar_key=$3,avatar_object_key=CASE WHEN $4 THEN NULL ELSE avatar_object_key END WHERE id=$1`, p.UserID, displayName, avatarKey, req.UseDefaultAvatar); err != nil {
		httpapi.Error(w, 500, errors.New("资料保存失败"))
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		httpapi.Error(w, 500, errors.New("资料保存失败"))
		return
	}
	if req.UseDefaultAvatar {
		p.AvatarURL = ""
		if old != nil && s.options.Avatars != nil {
			s.deleteAvatarObject(*old)
		}
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
		sum := cookieDigest(r, accessCookieName)
		if sum == nil {
			accessRequired(w)
			return
		}
		var p Principal
		var avatarKey string
		err := s.db.QueryRow(r.Context(), `SELECT u.id,u.email,u.display_name,wm.workspace_id,COALESCE(u.avatar_key,'forest'),u.role,COALESCE(u.avatar_object_key,''),u.email_verified,u.password_hash IS NOT NULL,s.token_hash,a.expires_at,s.expires_at FROM auth_access_tokens a JOIN sessions s ON s.id=a.session_id JOIN users u ON u.id=s.user_id JOIN workspace_members wm ON wm.user_id=u.id WHERE a.token_hash=$1 AND a.expires_at>now() AND s.expires_at>now() AND NOT u.disabled AND (NOT u.email_verification_required OR u.email_verified) ORDER BY wm.workspace_id LIMIT 1`, sum).Scan(&p.UserID, &p.Email, &p.DisplayName, &p.WorkspaceID, &p.AvatarKey, &p.Role, &avatarKey, &p.EmailVerified, &p.HasPassword, &p.SessionHash, &p.AccessExpires, &p.RefreshExpires)
		if errors.Is(err, pgx.ErrNoRows) {
			accessRequired(w)
			return
		}
		if err != nil {
			httpapi.Error(w, 503, errors.New("暂时无法验证登录状态，请稍后重试"))
			return
		}
		if avatarKey != "" {
			p.AvatarURL = "/api/v1/me/avatar?v=" + url.QueryEscape(avatarKey)
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, p)))
	})
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
	if err1 != nil || err2 != nil || len(salt) != 16 || len(expected) != 32 || memory != 65536 || iterations != 3 || parallelism != 2 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}
