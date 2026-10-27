package githubwork

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/connector"
	"github.com/google/uuid"
)

var repoRE = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)
var shaRE = regexp.MustCompile(`^[a-f0-9]{40}$`)
var branchRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./-]{0,199}$`)
var directoryRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,239}$`)

func validRepo(value string) bool {
	if !repoRE.MatchString(value) {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}
func validBranch(value string) bool {
	if !branchRE.MatchString(value) || strings.Contains(value, "..") || strings.Contains(value, "@{") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}
func validPath(value string) bool {
	if value == "" || len(value) > 512 || strings.ContainsAny(value, "\\\x00\r\n\t") || strings.HasPrefix(value, "/") || path.Clean(value) != value {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		switch strings.ToLower(part) {
		case "", ".", "..", ".git", ".agent", ".lester", "node_modules", ".venv":
			return false
		}
	}
	return true
}
func validTask(task Task) bool {
	return task.ConversationID != uuid.Nil && task.RunID != uuid.Nil && task.CallID != "" && len(task.CallID) <= 200 && task.Sandbox != nil && task.SandboxID != "" && task.WorkDir != ""
}
func hashJSON(value any) string {
	raw, _ := json.Marshal(value)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func blobSHA(data []byte) string {
	hash := sha1.New()
	_, _ = fmt.Fprintf(hash, "blob %d\x00", len(data))
	_, _ = hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil))
}
func (s *Service) seal(value any, aad string) ([]byte, []byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, s.vault.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return s.vault.Seal(nil, nonce, raw, []byte(aad)), nonce, nil
}
func (s *Service) open(data, nonce []byte, aad string, value any) error {
	if len(nonce) != s.vault.NonceSize() {
		return ErrProposal
	}
	raw, err := s.vault.Open(nil, nonce, data, []byte(aad))
	if err != nil {
		return ErrProposal
	}
	if json.Unmarshal(raw, value) != nil {
		return ErrProposal
	}
	return nil
}
func proposalAAD(id, workspace, user uuid.UUID) string {
	return "github-publish:" + id.String() + ":" + workspace.String() + ":" + user.String()
}
func (s *Service) call(ctx context.Context, task Task, grant connector.GitHubGrant, write bool, method, target string, payload, out any) error {
	raw, err := s.api.GitHubRequestForTask(ctx, task.ConversationID, task.RunID, grant, write, method, target, payload)
	if err != nil {
		return err
	}
	if out != nil && json.Unmarshal(raw, out) != nil {
		return connector.ErrResponse
	}
	return nil
}
func (s *Service) actor(ctx context.Context, task Task) (uuid.UUID, uuid.UUID, error) {
	var workspace, user uuid.UUID
	err := s.DB.QueryRow(ctx, `SELECT c.workspace_id,r.submitted_by FROM runs r JOIN conversations c ON c.id=r.conversation_id JOIN users u ON u.id=r.submitted_by JOIN workspace_members wm ON wm.workspace_id=c.workspace_id AND wm.user_id=u.id WHERE r.id=$1 AND c.id=$2 AND r.status='running' AND NOT u.disabled AND (NOT u.email_verification_required OR u.email_verified)`, task.RunID, task.ConversationID).Scan(&workspace, &user)
	if err != nil {
		return workspace, user, connector.ErrSession
	}
	return workspace, user, nil
}
func sameGrant(a, b connector.GitHubGrant) bool {
	return a.ID == b.ID && a.Generation == b.Generation && a.WorkspaceID == b.WorkspaceID && a.UserID == b.UserID && a.AccountID == b.AccountID
}
func timeBound(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, duration)
}
