package connector

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Keep the existing user -> session -> domain-row lock order. Never trust the
// request's old middleware snapshot after an account/session security change.
func authorize(ctx context.Context, tx pgx.Tx, a Actor) error {
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 AND NOT disabled AND (NOT email_verification_required OR email_verified) FOR SHARE`, a.UserID).Scan(&id); err != nil {
		return ErrSession
	}
	if len(a.SessionHash) > 0 {
		var family []byte
		if err := tx.QueryRow(ctx, `SELECT token_hash FROM sessions WHERE user_id=$1 AND token_hash=$2 AND expires_at>now() FOR SHARE`, a.UserID, a.SessionHash).Scan(&family); err != nil {
			return ErrSession
		}
	}
	if err := tx.QueryRow(ctx, `SELECT user_id FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, a.WorkspaceID, a.UserID).Scan(&id); err != nil {
		return ErrSession
	}
	return nil
}

func (s *Service) List(ctx context.Context, a Actor) ([]Connection, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = authorize(ctx, tx, a); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT provider,status,account_name,scopes,revision,updated_at,github_code_enabled FROM app_connections WHERE workspace_id=$1 AND user_id=$2`, a.WorkspaceID, a.UserID)
	if err != nil {
		return nil, err
	}
	saved := map[string]Connection{}
	for rows.Next() {
		var c Connection
		if err = rows.Scan(&c.Provider, &c.Status, &c.AccountName, &c.Scopes, &c.Revision, &c.UpdatedAt, &c.GitHubCodeEnabled); err != nil {
			rows.Close()
			return nil, err
		}
		saved[c.Provider] = c
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	result := make([]Connection, 0, len(providerOrder))
	for _, name := range providerOrder {
		c, ok := saved[name]
		if !ok {
			c = Connection{Provider: name, Status: "disconnected", Scopes: []string{}}
		}
		_, c.Configured = s.providers[name]
		if name != "github" || c.Status == "disconnected" {
			c.GitHubCodeEnabled = false
		}
		result = append(result, c)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) Disconnect(ctx context.Context, a Actor, provider string, revision int64) error {
	known := false
	for _, name := range providerOrder {
		if provider == name {
			known = true
		}
	}
	if !known || revision < 0 {
		return ErrArguments
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = authorize(ctx, tx, a); err != nil {
		return err
	}
	var id uuid.UUID
	var current int64
	err = tx.QueryRow(ctx, `SELECT id,revision FROM app_connections WHERE workspace_id=$1 AND user_id=$2 AND provider=$3 FOR UPDATE`, a.WorkspaceID, a.UserID, provider).Scan(&id, &current)
	if errors.Is(err, pgx.ErrNoRows) && revision == 0 {
		return tx.Commit(ctx)
	}
	if err != nil || current != revision {
		return ErrConflict
	}
	_, err = tx.Exec(ctx, `UPDATE app_connections SET status='disconnected',github_code_enabled=false,refresh_pending=false,account_id='',account_name='',scopes='{}',token_ciphertext=NULL,token_nonce=NULL,expires_at=NULL,generation=generation+1,revision=revision+1,updated_at=now() WHERE id=$1`, id)
	if err == nil {
		_, err = tx.Exec(ctx, `DELETE FROM app_connection_oauth_flows WHERE connection_id=$1`, id)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) runActor(ctx context.Context, conversation, run uuid.UUID) (Actor, error) {
	var a Actor
	err := s.DB.QueryRow(ctx, `SELECT c.workspace_id,r.submitted_by FROM runs r JOIN conversations c ON c.id=r.conversation_id JOIN users u ON u.id=r.submitted_by JOIN workspace_members wm ON wm.user_id=u.id AND wm.workspace_id=c.workspace_id WHERE r.id=$1 AND c.id=$2 AND r.status='running' AND NOT u.disabled AND (NOT u.email_verification_required OR u.email_verified)`, run, conversation).Scan(&a.WorkspaceID, &a.UserID)
	if err != nil {
		return a, ErrSession
	}
	return a, nil
}
func (s *Service) ListForTask(ctx context.Context, conversation, run uuid.UUID) (map[string]any, error) {
	a, err := s.runActor(ctx, conversation, run)
	if err != nil {
		return nil, err
	}
	items, err := s.List(ctx, a)
	if err != nil {
		return nil, err
	}
	return map[string]any{"connections": items, "read_only": true, "instruction": "Only this accepted task actor's connections are available. Missing authorization must be completed by the user in Settings > App connections. Never request credentials or use a Computer to access saved tokens. This is read-only metadata: github_code_enabled records user consent for the separate GitHub coding tools, subject to actual repository permissions; read_app remains read-only."}, nil
}
func (s *Service) ReadForTask(ctx context.Context, conversation, run uuid.UUID, request ReadRequest) (*ReadResult, error) {
	a, err := s.runActor(ctx, conversation, run)
	if err != nil {
		return nil, err
	}
	result, err := s.Read(ctx, a, request)
	if err != nil {
		return nil, err
	}
	// Stop, membership removal and account revocation cannot accept stale data.
	current, err := s.runActor(ctx, conversation, run)
	if err != nil || current.UserID != a.UserID || current.WorkspaceID != a.WorkspaceID {
		return nil, ErrSession
	}
	return result, nil
}

// Token refresh is serialized and committed before a resource read. A failed
// later GET must never roll back a successfully rotated refresh credential.
func (s *Service) readyToken(ctx context.Context, a Actor, provider Provider) error {
	// A session advisory guard spans the two short transactions. Keep the same
	// pooled connection so concurrent readers cannot reuse a rotating token.
	conn, err := s.DB.Acquire(ctx)
	if err != nil {
		return err
	}
	lock := "lester:app-refresh:" + a.WorkspaceID.String() + ":" + a.UserID.String() + ":" + provider.Name
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		// Never return a possibly locked session to the pool, including cancellation
		// while pg_advisory_lock was acquiring the lock.
		if _, e := conn.Exec(cleanup, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, lock); e != nil {
			_ = conn.Hijack().Close(cleanup)
		} else {
			conn.Release()
		}
	}()
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, lock); err != nil {
		return err
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = authorize(ctx, tx, a); err != nil {
		return err
	}
	var id uuid.UUID
	var state string
	var ciphertext, nonce []byte
	var generation int64
	var pending bool
	err = tx.QueryRow(ctx, `SELECT id,status,token_ciphertext,token_nonce,generation,refresh_pending FROM app_connections WHERE workspace_id=$1 AND user_id=$2 AND provider=$3 FOR UPDATE`, a.WorkspaceID, a.UserID, provider.Name).Scan(&id, &state, &ciphertext, &nonce, &generation, &pending)
	if errors.Is(err, pgx.ErrNoRows) || state == "disconnected" {
		return ErrNotConnected
	}
	if err != nil {
		return err
	}
	if state != "connected" {
		return ErrReauthorize
	}
	if pending {
		return s.invalidate(ctx, tx, id)
	}
	var current token
	if s.open(ciphertext, nonce, connectionAAD(id, a, provider.Name), &current) != nil {
		return s.invalidate(ctx, tx, id)
	}
	if current.Expires.IsZero() || time.Until(current.Expires) > 60*time.Second {
		return tx.Commit(ctx)
	}
	if current.Refresh == "" {
		return s.invalidate(ctx, tx, id)
	}
	// Persist intent BEFORE the POST. A crash, canceled request or failed commit
	// after a provider consumed its refresh token can never cause replay.
	if _, err = tx.Exec(ctx, `UPDATE app_connections SET refresh_pending=true WHERE id=$1`, id); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	next, exchangeErr := s.exchange(ctx, provider, "", "", current.Refresh)
	// Finishing the receipt is independent of a disconnected browser/task. The
	// durable pending bit still protects the grant if DB availability is lost.
	finishing, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	final, err := conn.Begin(finishing)
	if err != nil {
		return err
	}
	defer final.Rollback(finishing)
	if err = authorize(finishing, final, a); err != nil {
		return err
	}
	var same bool
	if err = final.QueryRow(finishing, `SELECT refresh_pending AND generation=$2 AND token_ciphertext=$3 FROM app_connections WHERE id=$1 FOR UPDATE`, id, generation, ciphertext).Scan(&same); err != nil {
		return err
	}
	if !same {
		return ErrConflict
	}
	if exchangeErr != nil {
		return s.invalidate(finishing, final, id)
	}
	if next.Refresh == "" {
		next.Refresh = current.Refresh
	}
	if len(next.Scopes) == 0 {
		next.Scopes = current.Scopes
	}
	encrypted, iv, err := s.seal(next, connectionAAD(id, a, provider.Name))
	if err != nil {
		return err
	}
	_, err = final.Exec(finishing, `UPDATE app_connections SET token_ciphertext=$2,token_nonce=$3,expires_at=$4,scopes=$5,refresh_pending=false,updated_at=now() WHERE id=$1`, id, encrypted, iv, expires(next), next.Scopes)
	if err != nil {
		return err
	}
	return final.Commit(finishing)
}
func expires(t token) *time.Time {
	if t.Expires.IsZero() {
		return nil
	}
	return &t.Expires
}
func (s *Service) invalidate(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE app_connections SET status='reauthorization_required',refresh_pending=false,token_ciphertext=NULL,token_nonce=NULL,expires_at=NULL,revision=revision+1,updated_at=now() WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return ErrReauthorize
}

func (s *Service) Read(ctx context.Context, a Actor, request ReadRequest) (*ReadResult, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	p, ok := s.providers[request.Provider]
	if !ok {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if err := s.readyToken(ctx, a, p); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = authorize(ctx, tx, a); err != nil {
		return nil, err
	}
	var id uuid.UUID
	var state string
	var ciphertext, nonce []byte
	err = tx.QueryRow(ctx, `SELECT id,status,token_ciphertext,token_nonce FROM app_connections WHERE workspace_id=$1 AND user_id=$2 AND provider=$3 FOR SHARE`, a.WorkspaceID, a.UserID, p.Name).Scan(&id, &state, &ciphertext, &nonce)
	if err != nil || state == "disconnected" {
		return nil, ErrNotConnected
	}
	if state != "connected" {
		return nil, ErrReauthorize
	}
	var t token
	if err = s.open(ciphertext, nonce, connectionAAD(id, a, p.Name), &t); err != nil {
		return nil, ErrReauthorize
	}
	result, readErr := s.readProvider(ctx, p, t.Access, request)
	if readErr != nil {
		// Release the shared credential lock before an exclusive invalidation.
		_ = tx.Rollback(ctx)
		if errors.Is(readErr, ErrReauthorize) {
			invalid, beginErr := s.DB.Begin(ctx)
			if beginErr == nil {
				defer invalid.Rollback(ctx)
				if authorize(ctx, invalid, a) == nil {
					var current []byte
					if invalid.QueryRow(ctx, `SELECT token_ciphertext FROM app_connections WHERE id=$1 FOR UPDATE`, id).Scan(&current) == nil && subtle.ConstantTimeCompare(current, ciphertext) == 1 {
						_ = s.invalidate(ctx, invalid, id)
					}
				}
			}
		}
		return nil, readErr
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
