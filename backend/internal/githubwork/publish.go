package githubwork

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/connector"
	"github.com/jackc/pgx/v5"
)

const commitMutation = `mutation LesterCommit($input:CreateCommitOnBranchInput!){createCommitOnBranch(input:$input){clientMutationId commit{oid}}}`

func (s *Service) Publish(ctx context.Context, task Task, request PublishRequest) (any, error) {
	if err := s.ValidatePublish(ctx, task, request); err != nil {
		return nil, err
	}
	item, err := s.loadProposal(ctx, task, request)
	if err != nil {
		return nil, err
	}
	// A private session guard outlives short state transactions without consuming
	// pooled slots needed by the credential gateway. No transaction is held over HTTP.
	conn, err := pgx.ConnectConfig(ctx, s.DB.Config().ConnConfig.Copy())
	if err != nil {
		return nil, err
	}
	defer func() {
		closing, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = conn.Close(closing)
	}()
	var held bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, "lester:github-publish:"+item.ID.String()).Scan(&held); err != nil {
		return nil, err
	}
	if !held {
		return receipt(item, "Publication is still owned by another executor. Inspect this original proposal again; do not create a replacement push."), nil
	}
	item, err = s.loadProposal(ctx, task, request)
	if err != nil {
		return nil, err
	}
	if item.Status != "prepared" {
		return s.reconcile(ctx, task, item)
	}
	if item.RunID != task.RunID || !item.Expires.After(time.Now()) {
		return nil, ErrProposal
	}
	grant, err := s.api.GitHubGrantForTask(ctx, task.ConversationID, task.RunID, true)
	if err != nil {
		return nil, err
	}
	if !sameGrant(grant, item.Payload.Grant) {
		return nil, ErrChanged
	}
	// Persist acceptance before the first possible remote write. Re-entering this
	// proposal after any crash/lost response takes the read-only path above.
	tag, err := s.DB.Exec(ctx, `UPDATE github_publish_proposals SET status='publishing',stage='checking_branch',updated_at=now() WHERE id=$1 AND status='prepared' AND expires_at>now()`, item.ID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, ErrProposal
	}
	item.Status = "publishing"
	item.Stage = "checking_branch"
	current, exists, err := s.branchHead(ctx, task, grant, item.Repository, item.Branch)
	if err != nil {
		return s.failBeforeWrite(ctx, item, err)
	}
	if item.Payload.NewBranch && exists || !item.Payload.NewBranch && (!exists || current != item.Payload.BaseSHA) {
		return s.failBeforeWrite(ctx, item, ErrChanged)
	}
	if item.Payload.NewBranch {
		if err = s.stage(ctx, &item, "branch_intent"); err != nil {
			return nil, err
		}
		var ref struct {
			Object struct {
				SHA string `json:"sha"`
			} `json:"object"`
		}
		err = s.call(ctx, task, grant, true, "POST", "/repos/"+item.Repository+"/git/refs", map[string]any{"ref": "refs/heads/" + item.Branch, "sha": item.Payload.BaseSHA}, &ref)
		if err != nil || ref.Object.SHA != item.Payload.BaseSHA {
			item = s.uncertain(ctx, item, "")
			return receipt(item, "The working-branch creation result is unconfirmed. Inspect this proposal; no commit was automatically retried."), nil
		}
	}
	if err = s.stage(ctx, &item, "commit_intent"); err != nil {
		return nil, err
	}
	additions := make([]map[string]string, 0, len(item.Payload.Files))
	for _, file := range item.Payload.Files {
		additions = append(additions, map[string]string{"path": file.Path, "contents": file.Content})
	}
	deletions := make([]map[string]string, 0, len(item.Payload.DeleteFiles))
	for _, name := range item.Payload.DeleteFiles {
		deletions = append(deletions, map[string]string{"path": name})
	}
	headline, body := commitMessage(item)
	input := map[string]any{"branch": map[string]string{"repositoryNameWithOwner": item.Repository, "branchName": item.Branch}, "expectedHeadOid": item.Payload.BaseSHA, "fileChanges": map[string]any{"additions": additions, "deletions": deletions}, "message": map[string]string{"headline": headline, "body": body}, "clientMutationId": item.ID.String()}
	var mutation struct {
		Data struct {
			CreateCommitOnBranch struct {
				ClientMutationID string `json:"clientMutationId"`
				Commit           struct {
					OID string `json:"oid"`
				} `json:"commit"`
			} `json:"createCommitOnBranch"`
		} `json:"data"`
	}
	err = s.call(ctx, task, grant, true, "POST", "/graphql", map[string]any{"query": commitMutation, "variables": map[string]any{"input": input}}, &mutation)
	observed := mutation.Data.CreateCommitOnBranch
	if err != nil || observed.ClientMutationID != item.ID.String() || !shaRE.MatchString(observed.Commit.OID) {
		item = s.uncertain(ctx, item, "The commit result is unconfirmed. Only read-only receipt reconciliation is allowed for this proposal.")
		return s.reconcile(ctx, task, item)
	}
	item.CommitSHA = observed.Commit.OID
	item.CommitURL = commitURL(item.Repository, item.CommitSHA)
	item.Status = "pushed"
	item.Stage = "commit_observed"
	if err = s.saveReceipt(ctx, item); err != nil {
		return nil, err
	}
	if !item.Payload.CreatePR {
		item.Status = "completed"
		if err = s.saveReceipt(ctx, item); err != nil {
			return nil, err
		}
		return receipt(item, "The selected snapshot was pushed. Test success is reported separately in the task's actual test receipts."), nil
	}
	if err = s.stage(ctx, &item, "pr_intent"); err != nil {
		return nil, err
	}
	var pull struct {
		Number int64 `json:"number"`
	}
	err = s.call(ctx, task, grant, true, "POST", "/repos/"+item.Repository+"/pulls", map[string]any{"title": item.Payload.PRTitle, "head": item.Branch, "base": item.Payload.BaseBranch, "body": pullBody(item)}, &pull)
	if err != nil || pull.Number <= 0 {
		item = s.uncertain(ctx, item, "The commit was pushed, but PR creation is unconfirmed. Inspect the original receipt; do not create a duplicate PR.")
		return s.reconcile(ctx, task, item)
	}
	item.PRURL = pullURL(item.Repository, pull.Number)
	item.Status = "completed"
	item.Stage = "pr_observed"
	if err = s.saveReceipt(ctx, item); err != nil {
		return nil, err
	}
	return receipt(item, "The selected snapshot was pushed and the PR was created. No merge was performed. Review the task's actual test results separately."), nil
}
func (s *Service) stage(ctx context.Context, item *proposal, stage string) error {
	_, err := s.DB.Exec(ctx, `UPDATE github_publish_proposals SET stage=$2,updated_at=now() WHERE id=$1`, item.ID, stage)
	if err == nil {
		item.Stage = stage
	}
	return err
}
func (s *Service) saveReceipt(ctx context.Context, item proposal) error {
	finishing, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err := s.DB.Exec(finishing, `UPDATE github_publish_proposals SET status=$2,stage=$3,commit_sha=$4,commit_url=$5,pr_url=$6,updated_at=now() WHERE id=$1`, item.ID, item.Status, item.Stage, item.CommitSHA, item.CommitURL, item.PRURL)
	return err
}
func (s *Service) uncertain(ctx context.Context, item proposal, notice string) proposal {
	item.Status = "unknown"
	_ = s.saveReceipt(ctx, item)
	return item
}
func (s *Service) failBeforeWrite(ctx context.Context, item proposal, err error) (any, error) {
	item.Status = "failed"
	_ = s.saveReceipt(ctx, item)
	return nil, err
}
func receipt(item proposal, notice string) any {
	return map[string]any{"proposal_id": item.ID, "repository": item.Repository, "branch": item.Branch, "status": item.Status, "stage": item.Stage, "commit_sha": item.CommitSHA, "commit_url": item.CommitURL, "pr_url": item.PRURL, "notice": notice}
}
func commitURL(repository, sha string) string {
	return "https://github.com/" + repository + "/commit/" + sha
}
func pullURL(repository string, number int64) string {
	return fmt.Sprintf("https://github.com/%s/pull/%d", repository, number)
}
func marker(item proposal) string { return "Lester-Operation: " + item.ID.String() + " " + item.Digest }
func commitMessage(item proposal) (string, string) {
	headline, body, _ := strings.Cut(item.Payload.CommitMessage, "\n")
	body = strings.TrimSpace(body)
	if body != "" {
		body += "\n\n"
	}
	return strings.TrimSpace(headline), body + marker(item)
}
func fullCommitMessage(item proposal) string {
	headline, body := commitMessage(item)
	return headline + "\n\n" + body
}
func pullMarker(item proposal) string {
	return "<!-- lester-publish:" + item.ID.String() + ":" + item.Digest + " -->"
}
func pullBody(item proposal) string {
	return strings.TrimSpace(item.Payload.PRBody) + "\n\n" + pullMarker(item)
}

// Reconciliation never issues POST/PATCH or manufactures a replacement identity.
// A missing observation remains unknown rather than being treated as permission
// to replay the remote write, even when a remote response was probably lost.
func (s *Service) reconcile(ctx context.Context, task Task, item proposal) (any, error) {
	if item.Status == "completed" {
		return receipt(item, "This is the original completed publication receipt; no write was repeated."), nil
	}
	if item.Status == "failed" {
		return receipt(item, "This proposal failed before publication. Prepare a fresh snapshot after reviewing the current branch and authorization."), nil
	}
	grant, err := s.api.GitHubGrantForTask(ctx, task.ConversationID, task.RunID, true)
	if err != nil || !sameGrant(grant, item.Payload.Grant) {
		return receipt(item, "The original publication cannot be checked with the current authorization. Its result remains unconfirmed; no write was repeated."), nil
	}
	if item.CommitSHA == "" && (item.Stage == "commit_intent" || item.Stage == "pr_intent" || item.Stage == "commit_observed") {
		var commits []struct {
			SHA    string `json:"sha"`
			Commit struct {
				Message string `json:"message"`
				Tree    struct {
					SHA string `json:"sha"`
				} `json:"tree"`
			} `json:"commit"`
			Parents []struct {
				SHA string `json:"sha"`
			} `json:"parents"`
		}
		values := url.Values{"sha": {item.Branch}, "per_page": {"20"}}
		if err = s.call(ctx, task, grant, false, "GET", "/repos/"+item.Repository+"/commits?"+values.Encode(), nil, &commits); err != nil {
			return receipt(item, "The original commit could not be observed. Its result remains unconfirmed; no write was repeated."), nil
		}
		for _, commit := range commits {
			if !shaRE.MatchString(commit.SHA) || commit.Commit.Message != fullCommitMessage(item) || len(commit.Parents) != 1 || commit.Parents[0].SHA != item.Payload.BaseSHA || !shaRE.MatchString(commit.Commit.Tree.SHA) {
				continue
			}
			matches, checkErr := s.matchesPublishedTree(ctx, task, grant, item, commit.Commit.Tree.SHA)
			if checkErr != nil || !matches {
				continue
			}
			item.CommitSHA = commit.SHA
			item.CommitURL = commitURL(item.Repository, commit.SHA)
			item.Status = "pushed"
			break
		}
	}
	if item.CommitSHA == "" {
		return receipt(item, "No verifiable commit receipt was found. The operation remains unconfirmed; inspect the working branch before preparing any new publication."), nil
	}
	if !item.Payload.CreatePR {
		item.Status = "completed"
		item.Stage = "commit_reconciled"
		if err = s.saveReceipt(ctx, item); err != nil {
			return nil, err
		}
		return receipt(item, "The original commit was verified by its operation marker, parent and exact tree. No write was repeated."), nil
	}
	if item.Stage == "pr_intent" || item.PRURL != "" {
		owner, _, _ := strings.Cut(item.Repository, "/")
		values := url.Values{"state": {"all"}, "head": {owner + ":" + item.Branch}, "base": {item.Payload.BaseBranch}, "per_page": {"100"}}
		var pulls []struct {
			Number int64  `json:"number"`
			Body   string `json:"body"`
			Head   struct {
				Ref string `json:"ref"`
			} `json:"head"`
			Base struct {
				Ref string `json:"ref"`
			} `json:"base"`
		}
		if err = s.call(ctx, task, grant, false, "GET", "/repos/"+item.Repository+"/pulls?"+values.Encode(), nil, &pulls); err == nil {
			for _, pull := range pulls {
				if pull.Number > 0 && pull.Head.Ref == item.Branch && pull.Base.Ref == item.Payload.BaseBranch && strings.Contains(pull.Body, pullMarker(item)) {
					item.PRURL = pullURL(item.Repository, pull.Number)
					item.Status = "completed"
					item.Stage = "pr_reconciled"
					if err = s.saveReceipt(ctx, item); err != nil {
						return nil, err
					}
					return receipt(item, "The original PR receipt was found. No push or PR creation was repeated."), nil
				}
			}
		}
	}
	item.Status = "pushed"
	if err = s.saveReceipt(ctx, item); err != nil {
		return nil, err
	}
	return receipt(item, "The commit is confirmed, but no original PR receipt is confirmed. Inspect this branch; PR creation was not automatically replayed."), nil
}
func (s *Service) matchesPublishedTree(ctx context.Context, task Task, grant connector.GitHubGrant, item proposal, treeSHA string) (bool, error) {
	original, err := s.loadCheckout(ctx, task, grant, item.CheckoutID, "")
	if err != nil {
		return false, err
	}
	expected := original.Manifest
	for _, name := range item.Payload.DeleteFiles {
		delete(expected, name)
	}
	for _, file := range item.Payload.Files {
		expected[file.Path] = treeEntry{Path: file.Path, Mode: "100644", Type: "blob", SHA: file.BlobSHA}
	}
	var tree struct {
		Truncated bool        `json:"truncated"`
		Tree      []treeEntry `json:"tree"`
	}
	if err = s.call(ctx, task, grant, false, "GET", "/repos/"+item.Repository+"/git/trees/"+treeSHA+"?recursive=1", nil, &tree); err != nil {
		return false, err
	}
	if tree.Truncated {
		return false, ErrBounds
	}
	seen := map[string]bool{}
	for _, entry := range tree.Tree {
		if entry.Type == "tree" {
			continue
		}
		want, ok := expected[entry.Path]
		if !ok || seen[entry.Path] || entry.Type != "blob" || want.SHA != entry.SHA || want.Mode != entry.Mode {
			return false, nil
		}
		seen[entry.Path] = true
	}
	return len(seen) == len(expected), nil
}
