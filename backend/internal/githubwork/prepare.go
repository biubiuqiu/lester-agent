package githubwork

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/biubiuqiu/lester-agent/backend/internal/connector"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
)

type fileChange struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Mode    string `json:"mode"`
	SHA256  string `json:"sha256"`
	BlobSHA string `json:"blob_sha"`
	Bytes   int    `json:"bytes"`
}
type publishPayload struct {
	CheckoutID                              uuid.UUID             `json:"checkout_id"`
	Grant                                   connector.GitHubGrant `json:"grant"`
	Repository, Branch, BaseBranch, BaseSHA string
	NewBranch                               bool
	CommitMessage, PRTitle, PRBody          string
	CreatePR                                bool
	Files                                   []fileChange
	DeleteFiles                             []string
}
type proposal struct {
	ID, CheckoutID, WorkspaceID, UserID, RunID                                      uuid.UUID
	Digest, Repository, Branch, Summary, Status, Stage, CommitSHA, CommitURL, PRURL string
	Expires                                                                         time.Time
	Payload                                                                         publishPayload `json:"-"`
}

func (s *Service) Prepare(ctx context.Context, task Task, request PrepareRequest) (any, error) {
	if !validTask(task) || request.CheckoutID == uuid.Nil || len(request.Files)+len(request.DeleteFiles) < 1 || len(request.Files)+len(request.DeleteFiles) > maxChanges || strings.TrimSpace(request.CommitMessage) == "" || len(request.CommitMessage) > 4000 || len(request.PRTitle) > 200 || len(request.PRBody) > 10000 || strings.ContainsRune(request.CommitMessage, 0) || strings.ContainsRune(request.PRBody, 0) || strings.ContainsAny(request.PRTitle, "\x00\r\n") {
		return nil, ErrArguments
	}
	headline, _, _ := strings.Cut(strings.TrimSpace(request.CommitMessage), "\n")
	if len(headline) > 200 {
		return nil, ErrArguments
	}
	grant, err := s.api.GitHubGrantForTask(ctx, task.ConversationID, task.RunID, true)
	if err != nil {
		return nil, err
	}
	checkout, err := s.loadCheckout(ctx, task, grant, request.CheckoutID, "")
	if err != nil || checkout.Status != "ready" {
		return nil, ErrCheckout
	}
	seen := map[string]bool{}
	for _, name := range append(append([]string{}, request.Files...), request.DeleteFiles...) {
		if !validPath(name) || seen[name] {
			return nil, ErrArguments
		}
		seen[name] = true
	}
	for _, name := range request.DeleteFiles {
		if _, ok := checkout.Manifest[name]; !ok {
			return nil, ErrArguments
		}
	}
	id := uuid.New()
	branch := request.Branch
	if branch == "" {
		branch = "lester/task-" + task.ConversationID.String()[:8] + "-" + id.String()[:8]
	}
	if !validBranch(branch) || request.CreatePR && branch == checkout.Ref {
		return nil, ErrArguments
	}
	expected, exists, err := s.branchHead(ctx, task, grant, checkout.Repository, branch)
	if err != nil {
		return nil, err
	}
	if exists && expected != checkout.BaseSHA {
		return nil, ErrChanged
	}
	manifest := map[string]any{"files": request.Files, "delete_files": request.DeleteFiles}
	raw, _ := json.Marshal(manifest)
	internal := ".lester/github/" + id.String()
	manifestPath := internal + "-paths.json"
	outputPath := internal + ".json"
	if err = task.Sandbox.WriteFile(ctx, task.SandboxID, task.WorkDir, manifestPath, raw); err != nil {
		return nil, ErrComputer
	}
	exported, err := task.Sandbox.GitHubTransfer(ctx, task.SandboxID, task.WorkDir, sandbox.GitHubTransferRequest{Operation: "export", Directory: checkout.Directory, Manifest: manifestPath, Output: outputPath})
	if err != nil || exported == nil || !exported.ExitCodeKnown || exported.ExitCode != 0 {
		return nil, ErrComputer
	}
	raw, err = task.Sandbox.ReadFile(ctx, task.SandboxID, task.WorkDir, outputPath)
	if err != nil || len(raw) > 2<<20 {
		return nil, ErrComputer
	}
	var source struct {
		Files       []struct{ Path, Content, Mode string }
		DeleteFiles []string `json:"delete_files"`
	}
	if json.Unmarshal(raw, &source) != nil || len(source.Files) != len(request.Files) || len(source.DeleteFiles) != len(request.DeleteFiles) {
		return nil, ErrComputer
	}
	sort.Strings(request.Files)
	sort.Strings(request.DeleteFiles)
	sort.Strings(source.DeleteFiles)
	if strings.Join(source.DeleteFiles, "\x00") != strings.Join(request.DeleteFiles, "\x00") {
		return nil, ErrComputer
	}
	sort.Slice(source.Files, func(i, j int) bool { return source.Files[i].Path < source.Files[j].Path })
	payload := publishPayload{CheckoutID: checkout.ID, Grant: grant, Repository: checkout.Repository, Branch: branch, BaseBranch: checkout.Ref, BaseSHA: checkout.BaseSHA, NewBranch: !exists, CommitMessage: strings.TrimSpace(request.CommitMessage), PRTitle: request.PRTitle, PRBody: request.PRBody, CreatePR: request.CreatePR, DeleteFiles: request.DeleteFiles}
	total := 0
	changes := make([]map[string]any, 0, len(source.Files)+len(source.DeleteFiles))
	previewBudget := 12000
	for i, file := range source.Files {
		if file.Path != request.Files[i] || !validPath(file.Path) || file.Mode != "100644" {
			return nil, ErrUnsupported
		}
		before, tracked := checkout.Manifest[file.Path]
		if tracked && before.Mode != "100644" {
			return nil, ErrUnsupported
		}
		data, decodeErr := base64.StdEncoding.DecodeString(file.Content)
		if decodeErr != nil {
			return nil, ErrComputer
		}
		total += len(data)
		if total > maxPublishBytes {
			return nil, ErrBounds
		}
		sha := blobSHA(data)
		if tracked && sha == before.SHA {
			return nil, ErrArguments
		}
		digest := sha256.Sum256(data)
		value := fileChange{Path: file.Path, Content: base64.StdEncoding.EncodeToString(data), Mode: "100644", SHA256: hex.EncodeToString(digest[:]), BlobSHA: sha, Bytes: len(data)}
		payload.Files = append(payload.Files, value)
		action := "add"
		if tracked {
			action = "modify"
		}
		change := map[string]any{"path": file.Path, "action": action, "bytes": len(data), "sha256": value.SHA256, "before_blob_sha": before.SHA}
		if utf8.Valid(data) {
			limit := min(previewBudget, 1200)
			runes := []rune(string(data))
			change["after_excerpt"] = string(runes[:min(len(runes), limit)])
			change["excerpt_truncated"] = len(runes) > limit
			previewBudget -= min(len(runes), limit)
		} else {
			change["binary"] = true
		}
		changes = append(changes, change)
	}
	for _, name := range request.DeleteFiles {
		changes = append(changes, map[string]any{"path": name, "action": "delete", "before_blob_sha": checkout.Manifest[name].SHA})
	}
	if payload.PRTitle == "" {
		payload.PRTitle = headline
	}
	digest := hashJSON(payload)
	summary := fmt.Sprintf("%s → %s · %d file changes · %s", payload.Repository, payload.Branch, len(changes), headline)
	encrypted, nonce, err := s.seal(payload, proposalAAD(id, grant.WorkspaceID, grant.UserID))
	if err != nil {
		return nil, err
	}
	latest, err := s.api.GitHubGrantForTask(ctx, task.ConversationID, task.RunID, true)
	if err != nil || !sameGrant(latest, grant) {
		return nil, ErrChanged
	}
	_, err = s.DB.Exec(ctx, `INSERT INTO github_publish_proposals(id,checkout_id,workspace_id,user_id,conversation_id,run_id,tool_call_id,digest,repository,branch,summary,payload_ciphertext,payload_nonce,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,now()+interval '1 hour')`, id, checkout.ID, grant.WorkspaceID, grant.UserID, task.ConversationID, task.RunID, task.CallID, digest, payload.Repository, payload.Branch, summary, encrypted, nonce)
	if err != nil {
		return nil, err
	}
	args := PublishRequest{ProposalID: id, Digest: digest, Repository: payload.Repository, Branch: payload.Branch, Summary: summary}
	return map[string]any{"proposal_id": id, "digest": digest, "repository": payload.Repository, "branch": payload.Branch, "base_branch": payload.BaseBranch, "expected_head": payload.BaseSHA, "new_branch": payload.NewBranch, "changes": changes, "total_bytes": total, "create_pr": payload.CreatePR, "publish_arguments": args, "notice": "This immutable selected-file snapshot has not been pushed. Review the exact files, branch and test receipts before calling github_publish with publish_arguments unchanged. Later sandbox edits are not included. Publication does not attest that tests passed; source excerpts may be partial."}, nil
}
func (s *Service) branchHead(ctx context.Context, task Task, grant connector.GitHubGrant, repository, branch string) (string, bool, error) {
	var ref struct {
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	err := s.call(ctx, task, grant, false, "GET", "/repos/"+repository+"/git/ref/heads/"+url.PathEscape(branch), nil, &ref)
	if err != nil {
		if connector.GitHubStatusCode(err) == 404 {
			return "", false, nil
		}
		return "", false, err
	}
	if ref.Object.Type != "commit" || !shaRE.MatchString(ref.Object.SHA) {
		return "", false, connector.ErrResponse
	}
	return ref.Object.SHA, true, nil
}
func (s *Service) loadProposal(ctx context.Context, task Task, request PublishRequest) (proposal, error) {
	var item proposal
	var encrypted, nonce []byte
	workspace, user, err := s.actor(ctx, task)
	if err != nil {
		return item, err
	}
	err = s.DB.QueryRow(ctx, `SELECT id,checkout_id,workspace_id,user_id,run_id,digest,repository,branch,summary,status,stage,commit_sha,commit_url,pr_url,expires_at,payload_ciphertext,payload_nonce FROM github_publish_proposals WHERE id=$1 AND conversation_id=$2 AND workspace_id=$3 AND user_id=$4`, request.ProposalID, task.ConversationID, workspace, user).Scan(&item.ID, &item.CheckoutID, &item.WorkspaceID, &item.UserID, &item.RunID, &item.Digest, &item.Repository, &item.Branch, &item.Summary, &item.Status, &item.Stage, &item.CommitSHA, &item.CommitURL, &item.PRURL, &item.Expires, &encrypted, &nonce)
	if err != nil {
		return item, ErrProposal
	}
	if item.Digest != request.Digest || item.Repository != request.Repository || item.Branch != request.Branch || item.Summary != request.Summary {
		return item, ErrProposal
	}
	if err = s.open(encrypted, nonce, proposalAAD(item.ID, workspace, user), &item.Payload); err != nil || hashJSON(item.Payload) != item.Digest {
		return item, ErrProposal
	}
	return item, nil
}
func (s *Service) ValidatePublish(ctx context.Context, task Task, request PublishRequest) error {
	if request.ProposalID == uuid.Nil || len(request.Digest) != 64 || !validRepo(request.Repository) || !validBranch(request.Branch) || len(request.Summary) > 1000 {
		return ErrArguments
	}
	item, err := s.loadProposal(ctx, task, request)
	if err != nil {
		return err
	}
	if item.Status == "prepared" {
		if item.RunID != task.RunID || !item.Expires.After(time.Now()) {
			return ErrProposal
		}
		grant, err := s.api.GitHubGrantForTask(ctx, task.ConversationID, task.RunID, true)
		if err != nil {
			return err
		}
		if !sameGrant(grant, item.Payload.Grant) {
			return ErrChanged
		}
	}
	return nil
}
