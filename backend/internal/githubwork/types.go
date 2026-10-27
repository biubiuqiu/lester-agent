// Package githubwork coordinates credential-free sandbox checkouts and bounded,
// immutable GitHub publication proposals. External writes never replay after an
// uncertain response; subsequent calls inspect the original receipt only.
package githubwork

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"errors"

	"github.com/biubiuqiu/lester-agent/backend/internal/connector"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrArguments   = errors.New("GitHub 代码任务参数无效，请检查仓库、分支和文件路径")
	ErrCheckout    = errors.New("GitHub 工作目录不可用，请重新选择仓库并创建工作目录")
	ErrChanged     = errors.New("GitHub 仓库或授权已变化，请重新拉取并准备发布")
	ErrBounds      = errors.New("仓库或改动超过代码任务范围，请缩小文件或仓库范围")
	ErrUnsupported = errors.New("此代码任务暂不支持符号链接、子模块或修改可执行文件模式")
	ErrProposal    = errors.New("发布预览已失效或不属于本次任务，请重新准备发布")
	ErrUnknown     = errors.New("GitHub 操作结果尚未确认，请检查原发布记录，不要重复推送")
	ErrComputer    = errors.New("代码工作目录处理失败，请检查沙箱中的 Git 与 Lester 工具箱")
)

const maxChanges = 100
const maxPublishBytes = 1 << 20
const maxCheckoutBytes = 25 << 20
const maxCheckoutBlobBytes = 4 << 20
const maxCheckoutFiles = 5000

type Source interface {
	GitHubTransfer(context.Context, string, string, sandbox.GitHubTransferRequest) (*sandbox.CommandResult, error)
	ReadFile(context.Context, string, string, string) ([]byte, error)
	WriteFile(context.Context, string, string, string, []byte) error
}
type Task struct {
	ConversationID, RunID      uuid.UUID
	CallID, SandboxID, WorkDir string
	Sandbox                    Source
}
type CheckoutRequest struct {
	Repository string `json:"repository"`
	Ref        string `json:"ref,omitempty"`
	Directory  string `json:"directory"`
}
type PrepareRequest struct {
	CheckoutID    uuid.UUID `json:"checkout_id"`
	Files         []string  `json:"files"`
	DeleteFiles   []string  `json:"delete_files,omitempty"`
	Branch        string    `json:"branch,omitempty"`
	CommitMessage string    `json:"commit_message"`
	PRTitle       string    `json:"pr_title,omitempty"`
	PRBody        string    `json:"pr_body,omitempty"`
	CreatePR      bool      `json:"create_pr"`
}
type PublishRequest struct {
	ProposalID uuid.UUID `json:"proposal_id"`
	Digest     string    `json:"digest"`
	Repository string    `json:"repository"`
	Branch     string    `json:"branch"`
	Summary    string    `json:"summary"`
}

// Adapter below keeps the gateway's json.RawMessage result out of the public
// source seam, making deterministic protocol tests independent of live tokens.
type Service struct {
	DB    *pgxpool.Pool
	api   gateway
	vault cipher.AEAD
}
type gateway interface {
	GitHubGrantForTask(context.Context, uuid.UUID, uuid.UUID, bool) (connector.GitHubGrant, error)
	GitHubRequestForTask(context.Context, uuid.UUID, uuid.UUID, connector.GitHubGrant, bool, string, string, any) (json.RawMessage, error)
}

func New(db *pgxpool.Pool, connections *connector.Service, key []byte) (*Service, error) {
	if len(key) != 32 {
		return nil, errors.New("GitHub work key must contain 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	vault, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Service{DB: db, api: connections, vault: vault}, nil
}
