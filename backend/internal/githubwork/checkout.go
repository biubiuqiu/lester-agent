package githubwork

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/connector"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type treeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
}
type checkout struct {
	ID                                                              uuid.UUID
	Grant                                                           connector.GitHubGrant
	Repository, Directory, Ref, BaseSHA, TreeSHA, LocalHead, Status string
	Manifest                                                        map[string]treeEntry
}

func (s *Service) ListRepositories(ctx context.Context, conversation, run uuid.UUID, cursor string) (any, error) {
	page := 1
	if cursor != "" {
		var err error
		page, err = strconv.Atoi(cursor)
		if err != nil || page < 1 || page > 1000 {
			return nil, ErrArguments
		}
	}
	task := Task{ConversationID: conversation, RunID: run}
	grant, err := s.api.GitHubGrantForTask(ctx, conversation, run, true)
	if err != nil {
		return nil, err
	}
	if err = s.checkApp(ctx, task, grant); err != nil {
		return nil, err
	}
	var repos []struct {
		ID            int64  `json:"id"`
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
		Private       bool   `json:"private"`
		Archived      bool   `json:"archived"`
		Permissions   struct {
			Push bool `json:"push"`
		} `json:"permissions"`
	}
	if err = s.call(ctx, task, grant, false, "GET", fmt.Sprintf("/user/repos?per_page=20&sort=updated&page=%d", page), nil, &repos); err != nil {
		return nil, err
	}
	values := make([]map[string]any, 0, len(repos))
	for _, repo := range repos {
		if !validRepo(repo.FullName) {
			return nil, connector.ErrResponse
		}
		values = append(values, map[string]any{"repository": repo.FullName, "default_branch": repo.DefaultBranch, "private": repo.Private, "archived": repo.Archived, "user_can_push": repo.Permissions.Push})
	}
	next := ""
	if len(repos) == 20 {
		next = strconv.Itoa(page + 1)
	}
	return map[string]any{"repositories": values, "next_cursor": next, "notice": "Repository visibility and user push permission do not verify the GitHub App's write permission. Ask the user to select a repository before checkout; never choose an unrelated repository."}, nil
}
func (s *Service) checkApp(ctx context.Context, task Task, grant connector.GitHubGrant) error {
	var value struct {
		Installations []json.RawMessage `json:"installations"`
	}
	if err := s.call(ctx, task, grant, false, "GET", "/user/installations?per_page=1", nil, &value); err != nil {
		return err
	}
	if len(value.Installations) == 0 {
		return connector.ErrPermission
	}
	return nil
}
func (s *Service) Checkout(ctx context.Context, task Task, request CheckoutRequest) (any, error) {
	if !validTask(task) || !validRepo(request.Repository) || !validPath(request.Directory) || !directoryRE.MatchString(request.Directory) {
		return nil, ErrArguments
	}
	ctx, cancel := timeBound(ctx, 4*time.Minute)
	defer cancel()
	grant, err := s.api.GitHubGrantForTask(ctx, task.ConversationID, task.RunID, true)
	if err != nil {
		return nil, err
	}
	if err = s.checkApp(ctx, task, grant); err != nil {
		return nil, err
	}
	existing, err := s.findCheckout(ctx, task, grant, request.Directory)
	if err == nil {
		if existing.Repository != request.Repository || request.Ref != "" && existing.Ref != request.Ref {
			return nil, ErrChanged
		}
		if existing.Status != "ready" {
			return nil, ErrCheckout
		}
		return checkoutResult(existing), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	var repo struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
		Archived      bool   `json:"archived"`
	}
	prefix := "/repos/" + request.Repository
	if err = s.call(ctx, task, grant, false, "GET", prefix, nil, &repo); err != nil {
		return nil, err
	}
	if !strings.EqualFold(repo.FullName, request.Repository) || repo.Archived {
		return nil, connector.ErrPermission
	}
	ref := request.Ref
	if ref == "" {
		ref = repo.DefaultBranch
	}
	if !validBranch(ref) {
		return nil, ErrArguments
	}
	var resolved struct {
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	if err = s.call(ctx, task, grant, false, "GET", prefix+"/git/ref/heads/"+url.PathEscape(ref), nil, &resolved); err != nil {
		return nil, err
	}
	if resolved.Object.Type != "commit" || !shaRE.MatchString(resolved.Object.SHA) {
		return nil, connector.ErrResponse
	}
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err = s.call(ctx, task, grant, false, "GET", prefix+"/git/commits/"+resolved.Object.SHA, nil, &commit); err != nil {
		return nil, err
	}
	if !shaRE.MatchString(commit.Tree.SHA) {
		return nil, connector.ErrResponse
	}
	var tree struct {
		Truncated bool        `json:"truncated"`
		Tree      []treeEntry `json:"tree"`
	}
	if err = s.call(ctx, task, grant, false, "GET", prefix+"/git/trees/"+commit.Tree.SHA+"?recursive=1", nil, &tree); err != nil {
		return nil, err
	}
	if tree.Truncated {
		return nil, ErrBounds
	}
	entries := make([]treeEntry, 0)
	manifest := map[string]treeEntry{}
	var total int64
	for _, entry := range tree.Tree {
		if entry.Type == "tree" {
			continue
		}
		if !validPath(entry.Path) || !shaRE.MatchString(entry.SHA) {
			return nil, ErrUnsupported
		}
		if entry.Type != "blob" || entry.Mode != "100644" && entry.Mode != "100755" {
			return nil, ErrUnsupported
		}
		if entry.Size < 0 || entry.Size > maxCheckoutBlobBytes {
			return nil, ErrBounds
		}
		total += entry.Size
		if total > maxCheckoutBytes || len(entries) >= maxCheckoutFiles {
			return nil, ErrBounds
		}
		if _, ok := manifest[entry.Path]; ok {
			return nil, connector.ErrResponse
		}
		manifest[entry.Path] = entry
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, ErrCheckout
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	current := checkout{ID: uuid.New(), Grant: grant, Repository: repo.FullName, Directory: request.Directory, Ref: ref, BaseSHA: resolved.Object.SHA, TreeSHA: commit.Tree.SHA, Status: "preparing", Manifest: manifest}
	rawManifest, _ := json.Marshal(manifest)
	_, err = s.DB.Exec(ctx, `INSERT INTO github_checkouts(id,workspace_id,user_id,conversation_id,run_id,tool_call_id,connection_id,grant_generation,account_id,repository,directory,source_ref,base_sha,tree_sha,manifest,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'preparing')`, current.ID, grant.WorkspaceID, grant.UserID, task.ConversationID, task.RunID, task.CallID, grant.ID, grant.Generation, grant.AccountID, current.Repository, current.Directory, current.Ref, current.BaseSHA, current.TreeSHA, rawManifest)
	if err != nil {
		return nil, ErrCheckout
	}
	succeeded := false
	defer func() {
		if !succeeded {
			ending, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, _ = s.DB.Exec(ending, `UPDATE github_checkouts SET status='failed' WHERE id=$1 AND status='preparing'`, current.ID)
		}
	}()
	bodies := make([][]byte, len(entries))
	workers := make(chan struct{}, 8)
	var wg sync.WaitGroup
	var first error
	var mu sync.Mutex
	var readBytes atomic.Int64
	for index, entry := range entries {
		wg.Add(1)
		go func(index int, entry treeEntry) {
			defer wg.Done()
			select {
			case workers <- struct{}{}:
				defer func() { <-workers }()
			case <-ctx.Done():
				mu.Lock()
				if first == nil {
					first = ctx.Err()
				}
				mu.Unlock()
				return
			}
			var blob struct {
				SHA, Encoding, Content string
				Size                   int64
			}
			failure := s.call(ctx, task, grant, false, "GET", prefix+"/git/blobs/"+entry.SHA, nil, &blob)
			var data []byte
			if failure == nil {
				data, failure = base64.StdEncoding.DecodeString(strings.ReplaceAll(blob.Content, "\n", ""))
				if failure != nil || blob.Encoding != "base64" || blob.SHA != entry.SHA || blobSHA(data) != entry.SHA || int64(len(data)) != entry.Size {
					failure = connector.ErrResponse
				}
				if readBytes.Add(int64(len(data))) > maxCheckoutBytes {
					failure = ErrBounds
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if failure != nil {
				if first == nil {
					first = failure
				}
				return
			}
			bodies[index] = data
		}(index, entry)
	}
	wg.Wait()
	if first != nil {
		return nil, first
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for index, entry := range entries {
		header := &zip.FileHeader{Name: entry.Path, Method: zip.Deflate}
		mode := os.FileMode(0644)
		if entry.Mode == "100755" {
			mode = 0755
		}
		header.SetMode(mode)
		target, err := writer.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err = target.Write(bodies[index]); err != nil {
			return nil, err
		}
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	if archive.Len() > maxCheckoutBytes {
		return nil, ErrBounds
	}
	internal := ".lester/github/" + current.ID.String() + ".zip"
	if err = task.Sandbox.WriteFile(ctx, task.SandboxID, task.WorkDir, internal, archive.Bytes()); err != nil {
		return nil, ErrComputer
	}
	result, err := task.Sandbox.GitHubTransfer(ctx, task.SandboxID, task.WorkDir, sandbox.GitHubTransferRequest{Operation: "import", Archive: internal, Directory: current.Directory, CheckoutID: current.ID.String(), BaseSHA: current.BaseSHA})
	if err != nil || result == nil || !result.ExitCodeKnown || result.ExitCode != 0 || result.StdoutTruncated {
		return nil, ErrComputer
	}
	var imported struct {
		Directory string `json:"directory"`
		LocalHead string `json:"local_head"`
		FileCount int    `json:"file_count"`
	}
	if json.Unmarshal([]byte(result.Stdout), &imported) != nil || imported.Directory != current.Directory || !shaRE.MatchString(imported.LocalHead) || imported.FileCount != len(entries) {
		return nil, ErrComputer
	}
	latest, err := s.api.GitHubGrantForTask(ctx, task.ConversationID, task.RunID, true)
	if err != nil || !sameGrant(latest, grant) {
		return nil, ErrChanged
	}
	current.LocalHead = imported.LocalHead
	current.Status = "ready"
	_, err = s.DB.Exec(ctx, `UPDATE github_checkouts SET local_head=$2,status='ready' WHERE id=$1 AND status='preparing'`, current.ID, current.LocalHead)
	if err != nil {
		return nil, err
	}
	succeeded = true
	return checkoutResult(current), nil
}
func checkoutResult(item checkout) any {
	return map[string]any{"checkout_id": item.ID, "repository": item.Repository, "directory": item.Directory, "base_branch": item.Ref, "base_sha": item.BaseSHA, "local_head": item.LocalHead, "file_count": len(item.Manifest), "notice": "The snapshot is now a local Git working tree. Edit and test with ordinary sandbox tools; no GitHub credential or remote is installed. Use git diff " + item.LocalHead + " to inspect changes, then prepare an explicit selected-file publication. Git LFS objects, submodules and symlinks are not downloaded."}
}
func (s *Service) findCheckout(ctx context.Context, task Task, grant connector.GitHubGrant, directory string) (checkout, error) {
	return s.loadCheckout(ctx, task, grant, uuid.Nil, directory)
}
func (s *Service) loadCheckout(ctx context.Context, task Task, grant connector.GitHubGrant, id uuid.UUID, directory string) (checkout, error) {
	var item checkout
	var raw []byte
	err := s.DB.QueryRow(ctx, `SELECT id,connection_id,grant_generation,workspace_id,user_id,account_id,repository,directory,source_ref,base_sha,tree_sha,local_head,status,manifest FROM github_checkouts WHERE conversation_id=$1 AND workspace_id=$2 AND user_id=$3 AND (($4::uuid<>$5::uuid AND id=$4) OR ($4::uuid=$5::uuid AND directory=$6))`, task.ConversationID, grant.WorkspaceID, grant.UserID, id, uuid.Nil, directory).Scan(&item.ID, &item.Grant.ID, &item.Grant.Generation, &item.Grant.WorkspaceID, &item.Grant.UserID, &item.Grant.AccountID, &item.Repository, &item.Directory, &item.Ref, &item.BaseSHA, &item.TreeSHA, &item.LocalHead, &item.Status, &raw)
	if err != nil {
		return item, err
	}
	if !sameGrant(item.Grant, grant) {
		return item, ErrChanged
	}
	if json.Unmarshal(raw, &item.Manifest) != nil {
		return item, ErrCheckout
	}
	return item, nil
}
