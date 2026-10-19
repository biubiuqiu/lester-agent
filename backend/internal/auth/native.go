package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/google/uuid"
)

var nativeChallenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var nativeVerifier = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

func decodeNative(w http.ResponseWriter, r *http.Request, target any) bool {
	w.Header().Set("Cache-Control", "no-store")
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		httpapi.Error(w, 400, errors.New("invalid native sign-in request"))
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		httpapi.Error(w, 400, errors.New("invalid native sign-in request"))
		return false
	}
	return true
}

// The system browser grants a short-lived, PKCE-bound handoff only after an
// explicit authenticated action. Access/refresh credentials never enter a URL.
func (s *Service) NativeHandoff(w http.ResponseWriter, r *http.Request) {
	p, _ := FromContext(r.Context())
	var req struct {
		Challenge string `json:"challenge"`
	}
	if !decodeNative(w, r, &req) {
		return
	}
	if !nativeChallenge.MatchString(req.Challenge) {
		httpapi.Error(w, 400, errors.New("invalid PKCE challenge"))
		return
	}
	raw, err := randomToken()
	if err != nil {
		httpapi.Error(w, 500, errors.New("handoff unavailable"))
		return
	}
	hash := sha256.Sum256([]byte(raw))
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpapi.Error(w, 503, errors.New("handoff unavailable"))
		return
	}
	defer tx.Rollback(r.Context())
	if err = lockAccountSession(r.Context(), tx, p.UserID, p.SessionHash); err != nil {
		httpapi.Error(w, 401, errAccountUnavailable)
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM native_signin_handoffs WHERE user_id=$1 AND expires_at<=now()`, p.UserID); err != nil {
		httpapi.Error(w, 503, errors.New("handoff unavailable"))
		return
	}
	var count int
	if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM native_signin_handoffs WHERE user_id=$1`, p.UserID).Scan(&count); err != nil {
		httpapi.Error(w, 503, errors.New("handoff unavailable"))
		return
	}
	if count >= 8 {
		httpapi.Error(w, 429, errors.New("too many pending sign-ins"))
		return
	}
	expires := time.Now().Add(90 * time.Second)
	created, err := tx.Exec(r.Context(), `INSERT INTO native_signin_handoffs(token_hash,user_id,session_id,challenge,expires_at) SELECT $1,$2,id,$4,$5 FROM sessions WHERE user_id=$2 AND token_hash=$3 AND expires_at>now()`, hash[:], p.UserID, p.SessionHash, req.Challenge, expires)
	if err != nil {
		httpapi.Error(w, 503, errors.New("handoff unavailable"))
		return
	}
	if created.RowsAffected() != 1 {
		httpapi.Error(w, 401, errAccountUnavailable)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		httpapi.Error(w, 503, errors.New("handoff unavailable"))
		return
	}
	httpapi.JSON(w, 201, map[string]any{"code": raw, "expires_at": expires})
}

// The native app exchanges its one-time code and private verifier for ordinary
// HttpOnly cookies. The source browser session must still be live under the user
// lock; logout/password resets/revocation cannot mint a replacement session.
func (s *Service) NativeExchange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code     string `json:"code"`
		Verifier string `json:"verifier"`
	}
	if !decodeNative(w, r, &req) {
		return
	}
	invalid := func() { httpapi.Error(w, 401, errors.New("native sign-in expired or invalid; sign in again")) }
	if !nativeChallenge.MatchString(req.Code) || !nativeVerifier.MatchString(req.Verifier) {
		invalid()
		return
	}
	if !s.allowAttempt(r, "native-exchange", "", 30) {
		httpapi.Error(w, 429, errors.New("too many sign-in attempts"))
		return
	}
	hash := sha256.Sum256([]byte(req.Code))
	challenge := sha256.Sum256([]byte(req.Verifier))
	encoded := base64.RawURLEncoding.EncodeToString(challenge[:])
	var user uuid.UUID
	if err := s.db.QueryRow(r.Context(), `SELECT user_id FROM native_signin_handoffs WHERE token_hash=$1 AND expires_at>now()`, hash[:]).Scan(&user); err != nil {
		invalid()
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpapi.Error(w, 503, errors.New("sign-in unavailable"))
		return
	}
	defer tx.Rollback(r.Context())
	var allowed bool
	if err = tx.QueryRow(r.Context(), `SELECT NOT disabled AND (NOT email_verification_required OR email_verified) FROM users WHERE id=$1 FOR UPDATE`, user).Scan(&allowed); err != nil || !allowed {
		invalid()
		return
	}
	var expected string
	if err = tx.QueryRow(r.Context(), `SELECT h.challenge FROM native_signin_handoffs h JOIN sessions s ON s.id=h.session_id AND s.user_id=h.user_id WHERE h.token_hash=$1 AND h.user_id=$2 AND h.expires_at>now() AND s.expires_at>now() AND EXISTS(SELECT 1 FROM workspace_members WHERE user_id=h.user_id) FOR UPDATE OF h`, hash[:], user).Scan(&expected); err != nil || subtle.ConstantTimeCompare([]byte(expected), []byte(encoded)) != 1 {
		invalid()
		return
	}
	session, err := newSession(s.accessTTL)
	if err != nil {
		httpapi.Error(w, 503, errors.New("sign-in unavailable"))
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM native_signin_handoffs WHERE token_hash=$1`, hash[:]); err != nil {
		httpapi.Error(w, 503, errors.New("sign-in unavailable"))
		return
	}
	if err = insertSession(r.Context(), tx, user, session); err != nil {
		httpapi.Error(w, 503, errors.New("sign-in unavailable"))
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		httpapi.Error(w, 503, errors.New("sign-in result unconfirmed; sign in again"))
		return
	}
	s.setCookie(w, session)
	w.WriteHeader(204)
}
