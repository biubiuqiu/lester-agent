package connector

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// GitHubGrant is a non-secret fence for one accepted task actor's authorization.
// Reconnecting or disconnecting changes Generation; another actor cannot reuse it.
type GitHubGrant struct {
	ID                     uuid.UUID
	Generation             int64
	WorkspaceID, UserID    uuid.UUID
	AccountID, AccountName string
}

type githubHTTPError struct {
	status int
	kind   error
}

func (e *githubHTTPError) Error() string { return e.kind.Error() }
func (e *githubHTTPError) Unwrap() error { return e.kind }
func githubStatusError(status int, kind error) error {
	return &githubHTTPError{status: status, kind: kind}
}

// GitHubStatusCode reports an observed HTTP status without exposing provider
// response bodies. Zero means no HTTP status was observed, not "not found".
func GitHubStatusCode(err error) int {
	var value *githubHTTPError
	if errors.As(err, &value) {
		return value.status
	}
	return 0
}

func githubTask(ctx context.Context, tx pgx.Tx, a Actor, conversation, run uuid.UUID) error {
	if err := authorize(ctx, tx, a); err != nil {
		return err
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT r.id FROM conversations c JOIN runs r ON r.conversation_id=c.id WHERE c.id=$1 AND r.id=$2 AND c.workspace_id=$3 AND r.submitted_by=$4 AND r.status='running'`, conversation, run, a.WorkspaceID, a.UserID).Scan(&id); err != nil {
		return ErrSession
	}
	return nil
}

// GitHubGrantForTask resolves the live accepted run actor; code permission is
// explicit Lester consent, not a claim that GitHub granted every repository.
func (s *Service) GitHubGrantForTask(ctx context.Context, conversation, run uuid.UUID, requireCode bool) (GitHubGrant, error) {
	var grant GitHubGrant
	a, err := s.runActor(ctx, conversation, run)
	if err != nil {
		return grant, err
	}
	p, ok := s.providers["github"]
	if !ok {
		return grant, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if err = s.readyToken(ctx, a, p); err != nil {
		return grant, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return grant, err
	}
	defer tx.Rollback(ctx)
	if err = githubTask(ctx, tx, a, conversation, run); err != nil {
		return grant, err
	}
	var status string
	var enabled bool
	err = tx.QueryRow(ctx, `SELECT id,generation,account_id,account_name,status,github_code_enabled FROM app_connections WHERE workspace_id=$1 AND user_id=$2 AND provider='github' FOR SHARE`, a.WorkspaceID, a.UserID).Scan(&grant.ID, &grant.Generation, &grant.AccountID, &grant.AccountName, &status, &enabled)
	if errors.Is(err, pgx.ErrNoRows) || status == "disconnected" {
		return GitHubGrant{}, ErrNotConnected
	}
	if err != nil {
		return GitHubGrant{}, err
	}
	if status != "connected" {
		return GitHubGrant{}, ErrReauthorize
	}
	if requireCode && !enabled {
		return GitHubGrant{}, ErrPermission
	}
	grant.WorkspaceID, grant.UserID = a.WorkspaceID, a.UserID
	if err = tx.Commit(ctx); err != nil {
		return GitHubGrant{}, err
	}
	return grant, nil
}

var githubRepoComponent = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
var githubBlobPath = regexp.MustCompile(`^/repos/[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}/git/blobs/[0-9a-f]{40}$`)

// A 4 MiB source blob is base64-encoded in GitHub's JSON response. This larger
// bound is private to exact code-task blob reads, never read_app or OAuth.
const maxGitHubBlobResponseBytes = 6 << 20

func githubTaskPath(method, raw string, write bool) (string, error) {
	if len(raw) == 0 || len(raw) > 4096 || strings.ContainsAny(raw, "\x00\r\n\\#") || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "", ErrArguments
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Fragment != "" || strings.ContainsAny(u.Path, "%\\\x00\r\n") {
		return "", ErrArguments
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." || strings.ContainsRune(part, 0) {
			return "", ErrArguments
		}
	}
	if method != http.MethodGet && !write {
		return "", ErrPermission
	}
	if method != http.MethodGet && method != http.MethodPost && method != http.MethodPatch {
		return "", ErrArguments
	}
	if u.Path == "/graphql" {
		if method != http.MethodPost || !write || u.RawQuery != "" {
			return "", ErrArguments
		}
		return raw, nil
	}
	if method == http.MethodGet && (u.Path == "/user/repos" || u.Path == "/user/installations") {
		return raw, nil
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "repos" || !githubRepoComponent.MatchString(parts[1]) || !githubRepoComponent.MatchString(parts[2]) {
		return "", ErrArguments
	}
	if method == http.MethodGet {
		return raw, nil
	}
	if u.RawQuery != "" {
		return "", ErrArguments
	}
	// The coding bridge may create Git data/refs and PRs. It does not expose
	// account administration, repository deletion, merging, or arbitrary APIs.
	if len(parts) == 4 && parts[3] == "pulls" && method == http.MethodPost {
		return raw, nil
	}
	if len(parts) >= 5 && parts[3] == "git" {
		if method == http.MethodPost && len(parts) == 5 && (parts[4] == "refs" || parts[4] == "blobs" || parts[4] == "trees" || parts[4] == "commits") {
			return raw, nil
		}
		if method == http.MethodPatch && len(parts) >= 7 && parts[4] == "refs" && parts[5] == "heads" {
			return raw, nil
		}
	}
	return "", ErrArguments
}

// GitHubRequestForTask is a server-only transport seam for the coding workflow.
// Each request rechecks a pinned grant and active task before sending credentials
// to the fixed GitHub endpoint. Tokens never leave this package or reach a Computer.
func (s *Service) GitHubRequestForTask(ctx context.Context, conversation, run uuid.UUID, grant GitHubGrant, write bool, method, path string, payload any) (json.RawMessage, error) {
	path, err := githubTaskPath(method, path, write)
	if err != nil {
		return nil, err
	}
	if method == http.MethodGet && payload != nil {
		return nil, ErrArguments
	}
	if grant.ID == uuid.Nil || grant.Generation < 0 || grant.WorkspaceID == uuid.Nil || grant.UserID == uuid.Nil || grant.AccountID == "" {
		return nil, ErrArguments
	}
	var raw []byte
	if payload != nil {
		raw, err = json.Marshal(payload)
		if err != nil || len(raw) > 2<<20 {
			return nil, ErrArguments
		}
		// Ref updates are always ordinary fast-forward publication; this
		// transport never offers force-push even to a trusted coding caller.
		if method == http.MethodPatch && strings.Contains(path, "/git/refs/") {
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) != nil || fields == nil {
				return nil, ErrArguments
			}
			if force, exists := fields["force"]; exists && string(bytes.TrimSpace(force)) != "false" {
				return nil, ErrArguments
			}
		}
	}
	a, err := s.runActor(ctx, conversation, run)
	if err != nil {
		return nil, err
	}
	if a.WorkspaceID != grant.WorkspaceID || a.UserID != grant.UserID {
		return nil, ErrSession
	}
	p, ok := s.providers["github"]
	if !ok {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if err = s.readyToken(ctx, a, p); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = githubTask(ctx, tx, a, conversation, run); err != nil {
		return nil, err
	}
	var current GitHubGrant
	var status string
	var enabled bool
	var ciphertext, nonce []byte
	err = tx.QueryRow(ctx, `SELECT id,generation,account_id,status,github_code_enabled,token_ciphertext,token_nonce FROM app_connections WHERE workspace_id=$1 AND user_id=$2 AND provider='github' FOR SHARE`, a.WorkspaceID, a.UserID).Scan(&current.ID, &current.Generation, &current.AccountID, &status, &enabled, &ciphertext, &nonce)
	if errors.Is(err, pgx.ErrNoRows) || status == "disconnected" {
		return nil, ErrNotConnected
	}
	if err != nil {
		return nil, err
	}
	if status != "connected" {
		return nil, ErrReauthorize
	}
	if current.ID != grant.ID || current.Generation != grant.Generation || current.AccountID != grant.AccountID {
		return nil, ErrConflict
	}
	if write && !enabled {
		return nil, ErrPermission
	}
	var credential token
	if s.open(ciphertext, nonce, connectionAAD(grant.ID, a, "github"), &credential) != nil {
		return nil, ErrReauthorize
	}
	var body *bytes.Reader
	if payload != nil {
		body = bytes.NewReader(raw)
	} else {
		body = bytes.NewReader(nil)
	}
	contentType := ""
	if payload != nil {
		contentType = "application/json"
	}
	responseLimit := maxResponseBytes
	if method == http.MethodGet && githubBlobPath.MatchString(path) {
		responseLimit = maxGitHubBlobResponseBytes
	}
	value, requestErr := s.requestWithResponseLimit(ctx, method, strings.TrimRight(p.APIURL, "/")+path, credential.Access, body, contentType, nil, responseLimit)
	if requestErr == nil {
		requestErr = githubGraphQLError(value)
	}
	if requestErr != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(requestErr, ErrReauthorize) {
			s.invalidateGitHubCredential(ctx, a, grant, ciphertext)
		}
		return nil, requestErr
	}
	// Stop must remain able to update the run while a provider request is in
	// flight. Recheck after it returns instead of holding a run row lock over
	// HTTP; an issued write can be uncertain and must be reconciled, not replayed.
	if err = githubTask(ctx, tx, a, conversation, run); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, ErrResponse
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return encoded, nil
}

func githubGraphQLError(value any) error {
	errorValue, present := object(value)["errors"]
	failures, valid := errorValue.([]any)
	if present && errorValue != nil && !valid {
		return ErrResponse
	}
	if len(failures) == 0 {
		return nil
	}
	result := ErrProvider
	for _, raw := range failures {
		switch text(object(raw)["type"]) {
		case "UNAUTHENTICATED":
			return ErrReauthorize
		case "RATE_LIMITED":
			return ErrRateLimit
		case "FORBIDDEN", "INSUFFICIENT_SCOPES", "NOT_FOUND":
			result = ErrPermission
		case "UNPROCESSABLE", "CONFLICT":
			if result == ErrProvider {
				result = ErrConflict
			}
		}
	}
	return result
}

func (s *Service) invalidateGitHubCredential(ctx context.Context, a Actor, grant GitHubGrant, ciphertext []byte) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	if authorize(ctx, tx, a) != nil {
		return
	}
	var generation int64
	var current []byte
	var account string
	if tx.QueryRow(ctx, `SELECT generation,account_id,token_ciphertext FROM app_connections WHERE id=$1 AND workspace_id=$2 AND user_id=$3 FOR UPDATE`, grant.ID, a.WorkspaceID, a.UserID).Scan(&generation, &account, &current) == nil && generation == grant.Generation && account == grant.AccountID && subtle.ConstantTimeCompare(current, ciphertext) == 1 {
		_ = s.invalidate(ctx, tx, grant.ID)
	}
}
