package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func issueEmailToken(ctx context.Context, tx pgx.Tx, userID uuid.UUID, purpose string) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	expiry := time.Now().Add(24 * time.Hour)
	if purpose == "reset" {
		expiry = time.Now().Add(30 * time.Minute)
	}
	_, err = tx.Exec(ctx, `INSERT INTO auth_email_tokens(token_hash,user_id,purpose,expires_at) VALUES($1,$2,$3,$4) ON CONFLICT(user_id,purpose) DO UPDATE SET token_hash=EXCLUDED.token_hash,expires_at=EXCLUDED.expires_at`, tokenDigest(token), userID, purpose, expiry)
	return token, err
}
func (s *Service) ResendVerification(w http.ResponseWriter, r *http.Request) {
	s.requestAccountMail(w, r, "verify")
}
func (s *Service) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	s.requestAccountMail(w, r, "reset")
}
func (s *Service) requestAccountMail(w http.ResponseWriter, r *http.Request, purpose string) {
	if s.options.Mailer == nil {
		httpapi.Error(w, 503, errors.New("此部署尚未启用邮件服务，请联系管理员"))
		return
	}
	var req struct{ Email string }
	if !httpapi.Decode(w, r, &req) {
		return
	}
	email, err := normalizeEmail(req.Email)
	if err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	if !s.allowAttempt(r, "email", email, 3) {
		w.Header().Set("Retry-After", "60")
		httpapi.Error(w, 429, errors.New("邮件请求过于频繁，请稍后再试"))
		return
	}
	// Same response for unknown, disabled, already verified and eligible accounts.
	// No response includes the token or whether this mailbox is registered.
	err = s.issueAndSendAccountMail(r.Context(), email, purpose)
	if err != nil {
		slog.Warn("account mail request failed", "purpose", purpose)
	}
	httpapi.JSON(w, 202, map[string]string{"message": "如果该账号可以进行此操作，你将收到邮件。请检查收件箱和垃圾邮件。"})
}
func (s *Service) issueAndSendAccountMail(ctx context.Context, email, purpose string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM users WHERE email=$1 AND NOT disabled AND ($2='reset' OR NOT email_verified) FOR UPDATE`, email, purpose).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	token, err := issueEmailToken(ctx, tx, userID, purpose)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return s.sendAccountMail(ctx, email, purpose, token)
}
func (s *Service) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req struct{ Token string }
	if !httpapi.Decode(w, r, &req) {
		return
	}
	if err := s.consumeEmailToken(r.Context(), req.Token, "verify", ""); err != nil {
		httpapi.Error(w, 400, errors.New("验证链接无效或已过期，请重新发送验证邮件"))
		return
	}
	w.WriteHeader(204)
}
func (s *Service) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct{ Token, Password string }
	if !httpapi.Decode(w, r, &req) {
		return
	}
	if err := validatePassword(req.Password); err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	if !s.allowAttempt(r, "reset", req.Token, 5) {
		w.Header().Set("Retry-After", "60")
		httpapi.Error(w, 429, errors.New("操作过于频繁，请稍后再试"))
		return
	}
	if err := s.consumeEmailToken(r.Context(), req.Token, "reset", req.Password); err != nil {
		httpapi.Error(w, 400, errors.New("重设链接无效或已过期，请重新申请"))
		return
	}
	w.WriteHeader(204)
}
func (s *Service) consumeEmailToken(ctx context.Context, token, purpose, password string) error {
	if len(token) != 43 {
		return errors.New("invalid token")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT user_id FROM auth_email_tokens WHERE token_hash=$1 AND purpose=$2 AND expires_at>now()`, tokenDigest(token), purpose).Scan(&userID); err != nil {
		return err
	}
	var enabled bool
	if err = tx.QueryRow(ctx, `SELECT NOT disabled FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&enabled); err != nil || !enabled {
		return errAccountUnavailable
	}
	// Lock user before the token, just as admin/security changes do. Recheck after
	// waiting: replacement, expiry, password reset or revocation wins the race.
	if err = tx.QueryRow(ctx, `DELETE FROM auth_email_tokens WHERE token_hash=$1 AND purpose=$2 AND expires_at>now() RETURNING user_id`, tokenDigest(token), purpose).Scan(&userID); err != nil {
		return err
	}
	if purpose == "reset" {
		hash, e := hashPassword(password)
		if e != nil {
			return e
		}
		_, err = tx.Exec(ctx, `UPDATE users SET password_hash=$2,email_verified=true,email_verification_required=false WHERE id=$1`, userID, hash)
		if err == nil {
			_, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, userID)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `DELETE FROM auth_oauth_flows WHERE user_id=$1`, userID)
		}
		if err == nil {
			_, err = tx.Exec(ctx, `DELETE FROM auth_email_tokens WHERE user_id=$1`, userID)
		}
	} else {
		_, err = tx.Exec(ctx, `UPDATE users SET email_verified=true,email_verification_required=false WHERE id=$1`, userID)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Service) ChangePassword(w http.ResponseWriter, r *http.Request) {
	p, _ := FromContext(r.Context())
	var req struct {
		CurrentPassword string `json:"current_password"`
		Password        string `json:"password"`
	}
	if !httpapi.Decode(w, r, &req) {
		return
	}
	if err := validatePassword(req.Password); err != nil {
		httpapi.Error(w, 400, err)
		return
	}
	if len(req.CurrentPassword) > 1024 {
		httpapi.Error(w, 400, errors.New("当前密码不正确"))
		return
	}
	if !s.allowAttempt(r, "password", p.UserID.String(), 5) {
		w.Header().Set("Retry-After", "60")
		httpapi.Error(w, 429, errors.New("操作过于频繁，请稍后再试"))
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpapi.Error(w, 500, errors.New("无法修改密码"))
		return
	}
	defer tx.Rollback(r.Context())
	if err = lockAccountSession(r.Context(), tx, p.UserID, sessionDigest(r)); err != nil {
		httpapi.Error(w, 401, errors.New("请重新登录"))
		return
	}
	var hash *string
	if err = tx.QueryRow(r.Context(), `SELECT password_hash FROM users WHERE id=$1`, p.UserID).Scan(&hash); err != nil {
		httpapi.Error(w, 500, errors.New("无法修改密码"))
		return
	}
	if hash != nil && !verifyPassword(req.CurrentPassword, *hash) {
		httpapi.Error(w, 400, errors.New("当前密码不正确"))
		return
	}
	updated, err := hashPassword(req.Password)
	if err != nil {
		httpapi.Error(w, 500, errors.New("无法修改密码"))
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE users SET password_hash=$2 WHERE id=$1`, p.UserID, updated); err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM sessions WHERE user_id=$1`, p.UserID)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM auth_email_tokens WHERE user_id=$1`, p.UserID)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM auth_oauth_flows WHERE user_id=$1`, p.UserID)
	}
	var session sessionRecord
	if err == nil {
		session, err = newSession(s.accessTTL)
	}
	if err == nil {
		err = insertSession(r.Context(), tx, p.UserID, session)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpapi.Error(w, 500, errors.New("无法修改密码"))
		return
	}
	s.setCookie(w, session)
	w.WriteHeader(204)
}
