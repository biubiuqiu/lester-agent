package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const accessCookieName = "lester_access_token"
const refreshCookieName = "lester_refresh_token"
const refreshLifetime = 30 * 24 * time.Hour
const refreshRaceGrace = 5 * time.Second

type sessionRecord struct {
	raw, refreshRaw         []byte
	expires, refreshExpires time.Time
}

type sessionExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func newSession(ttl time.Duration) (sessionRecord, error) {
	session := sessionRecord{raw: make([]byte, 32), refreshRaw: make([]byte, 32)}
	if _, err := rand.Read(session.raw); err != nil {
		return sessionRecord{}, err
	}
	if _, err := rand.Read(session.refreshRaw); err != nil {
		return sessionRecord{}, err
	}
	now := time.Now()
	session.expires, session.refreshExpires = now.Add(ttl), now.Add(refreshLifetime)
	return session, nil
}

func insertSession(ctx context.Context, executor sessionExecutor, userID uuid.UUID, session sessionRecord) error {
	access, refresh := sha256.Sum256(session.raw), sha256.Sum256(session.refreshRaw)
	// The family hash is stable across access rotation, including pending OAuth
	// links and the under-lock checks on profile/security mutations.
	_, err := executor.Exec(ctx, `WITH family AS (
 INSERT INTO sessions(user_id,token_hash,expires_at) VALUES($1,$2,$3) RETURNING id
 ), access AS (
 INSERT INTO auth_access_tokens(token_hash,session_id,expires_at) SELECT $2,id,$4 FROM family
 ) INSERT INTO auth_refresh_tokens(token_hash,session_id,expires_at) SELECT $5,id,$3 FROM family`, userID, access[:], session.refreshExpires, session.expires, refresh[:])
	return err
}

func cookieDigest(r *http.Request, name string) []byte {
	cookie, err := r.Cookie(name)
	if err != nil {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil || len(raw) != 32 {
		return nil
	}
	sum := sha256.Sum256(raw)
	return sum[:]
}

func (s *Service) setCookie(w http.ResponseWriter, session sessionRecord) {
	w.Header().Set("Cache-Control", "no-store")
	for _, cookie := range []*http.Cookie{
		{Name: accessCookieName, Value: base64.RawURLEncoding.EncodeToString(session.raw), Path: "/", Expires: session.expires},
		{Name: refreshCookieName, Value: base64.RawURLEncoding.EncodeToString(session.refreshRaw), Path: "/api/v1/auth", Expires: session.refreshExpires},
	} {
		cookie.HttpOnly, cookie.Secure, cookie.SameSite = true, s.secure, http.SameSiteLaxMode
		http.SetCookie(w, cookie)
	}
}

func (s *Service) clearCookies(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	for _, spec := range [][2]string{{accessCookieName, "/"}, {refreshCookieName, "/api/v1/auth"}, {"lester_session", "/"}} {
		http.SetCookie(w, &http.Cookie{Name: spec[0], Path: spec[1], HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	}
}

func accessRequired(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	// Only this middleware response guarantees the domain handler did not run.
	httpapi.JSON(w, 401, map[string]string{"error": "请重新登录", "code": "access_required"})
}

func (s *Service) SessionStatus(w http.ResponseWriter, r *http.Request) {
	p, _ := FromContext(r.Context())
	w.Header().Set("Cache-Control", "no-store")
	httpapi.JSON(w, 200, map[string]time.Time{"access_expires_at": p.AccessExpires, "refresh_expires_at": p.RefreshExpires})
}

func (s *Service) Refresh(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	digest := cookieDigest(r, refreshCookieName)
	if digest == nil {
		s.refreshExpired(w)
		return
	}
	var userID, familyID uuid.UUID
	err := s.db.QueryRow(r.Context(), `SELECT s.user_id,s.id FROM auth_refresh_tokens t JOIN sessions s ON s.id=t.session_id WHERE t.token_hash=$1`, digest).Scan(&userID, &familyID)
	if errors.Is(err, pgx.ErrNoRows) {
		s.refreshExpired(w)
		return
	}
	if err != nil {
		httpapi.Error(w, 503, errors.New("登录续期暂时不可用，请稍后重试"))
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpapi.Error(w, 503, errors.New("登录续期暂时不可用，请稍后重试"))
		return
	}
	defer tx.Rollback(r.Context())
	// Match password reset, disable, identity unlink, and logout lock ordering.
	var available bool
	err = tx.QueryRow(r.Context(), `SELECT NOT disabled AND (NOT email_verification_required OR email_verified) FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&available)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, 503, errors.New("登录续期暂时不可用，请稍后重试"))
		return
	}
	var expires, familyExpires time.Time
	var consumed *time.Time
	err = tx.QueryRow(r.Context(), `SELECT t.expires_at,t.consumed_at,s.expires_at FROM auth_refresh_tokens t JOIN sessions s ON s.id=t.session_id WHERE t.token_hash=$1 AND s.id=$2 FOR UPDATE OF t,s`, digest, familyID).Scan(&expires, &consumed, &familyExpires)
	if errors.Is(err, pgx.ErrNoRows) {
		s.refreshExpired(w)
		return
	}
	if err != nil {
		httpapi.Error(w, 503, errors.New("登录续期暂时不可用，请稍后重试"))
		return
	}
	now := time.Now()
	if available && expires.After(now) && familyExpires.After(now) && consumed != nil && now.Sub(*consumed) < refreshRaceGrace {
		// Two tabs may have sent the same cookie before the first response arrived.
		// Do not overwrite that response's cookies, and never issue tokens twice.
		w.Header().Set("Retry-After", "1")
		httpapi.JSON(w, 409, map[string]string{"error": "登录正在续期，请重试", "code": "refresh_in_progress"})
		return
	}
	if !available || !expires.After(now) || !familyExpires.After(now) || consumed != nil {
		// Replay outside the short race window revokes this device's entire family.
		if _, err = tx.Exec(r.Context(), `DELETE FROM sessions WHERE id=$1`, familyID); err == nil {
			err = tx.Commit(r.Context())
		}
		if err != nil {
			httpapi.Error(w, 503, errors.New("登录续期暂时不可用，请稍后重试"))
			return
		}
		s.refreshExpired(w)
		return
	}
	session, err := newSession(s.accessTTL)
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE auth_refresh_tokens SET consumed_at=$2 WHERE token_hash=$1`, digest, now)
	}
	if err == nil {
		access, refresh := sha256.Sum256(session.raw), sha256.Sum256(session.refreshRaw)
		_, err = tx.Exec(r.Context(), `UPDATE sessions SET expires_at=$2 WHERE id=$1`, familyID, session.refreshExpires)
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO auth_access_tokens(token_hash,session_id,expires_at) VALUES($1,$2,$3)`, access[:], familyID, session.expires)
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO auth_refresh_tokens(token_hash,session_id,expires_at) VALUES($1,$2,$3)`, refresh[:], familyID, session.refreshExpires)
		}
		// Keep still-valid access tokens for in-flight tabs. Bound retained history by
		// expiry while retaining consumed refresh hashes for replay detection.
		if err == nil {
			_, err = tx.Exec(r.Context(), `DELETE FROM auth_access_tokens WHERE session_id=$1 AND expires_at<=now()`, familyID)
		}
		if err == nil {
			_, err = tx.Exec(r.Context(), `DELETE FROM auth_refresh_tokens WHERE session_id=$1 AND expires_at<=now()`, familyID)
		}
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		httpapi.Error(w, 503, errors.New("登录续期暂时不可用，请稍后重试"))
		return
	}
	s.setCookie(w, session)
	httpapi.JSON(w, 200, map[string]time.Time{"access_expires_at": session.expires, "refresh_expires_at": session.refreshExpires})
}

func (s *Service) refreshExpired(w http.ResponseWriter) {
	s.clearCookies(w)
	httpapi.JSON(w, 401, map[string]string{"error": "登录已过期，请重新登录", "code": "refresh_expired"})
}

func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	access, refresh := cookieDigest(r, accessCookieName), cookieDigest(r, refreshCookieName)
	// Consumed refresh records still identify their family: logout must win even
	// when a concurrent refresh just rotated the credentials.
	var userID, familyID uuid.UUID
	err := s.db.QueryRow(r.Context(), `SELECT s.user_id,s.id FROM sessions s WHERE s.id IN (
 SELECT session_id FROM auth_access_tokens WHERE token_hash=$1 UNION
 SELECT session_id FROM auth_refresh_tokens WHERE token_hash=$2) LIMIT 1`, access, refresh).Scan(&userID, &familyID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, 503, errors.New("暂时无法退出登录，请重试"))
		return
	}
	if err == nil {
		tx, e := s.db.Begin(r.Context())
		if e != nil {
			httpapi.Error(w, 503, errors.New("暂时无法退出登录，请重试"))
			return
		}
		defer tx.Rollback(r.Context())
		var locked uuid.UUID
		e = tx.QueryRow(r.Context(), `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&locked)
		if e == nil {
			_, e = tx.Exec(r.Context(), `DELETE FROM sessions WHERE id=$1`, familyID)
		}
		if e == nil {
			e = tx.Commit(r.Context())
		}
		if e != nil {
			httpapi.Error(w, 503, errors.New("暂时无法退出登录，请重试"))
			return
		}
	}
	s.clearCookies(w)
	w.WriteHeader(204)
}

// OAuth callbacks are public routes and have no Principal in context. Resolve
// the browser's current credential back to its stable family, including a
// refresh-only browser whose two-hour access cookie expired during consent.
func (s *Service) browserSessionDigest(r *http.Request) ([]byte, error) {
	var hash []byte
	err := s.db.QueryRow(r.Context(), `SELECT s.token_hash FROM sessions s WHERE s.expires_at>now() AND s.id IN (
 SELECT session_id FROM auth_access_tokens WHERE token_hash=$1 AND expires_at>now() UNION
 SELECT session_id FROM auth_refresh_tokens WHERE token_hash=$2 AND expires_at>now() AND consumed_at IS NULL) LIMIT 1`, cookieDigest(r, accessCookieName), cookieDigest(r, refreshCookieName)).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return hash, err
}
