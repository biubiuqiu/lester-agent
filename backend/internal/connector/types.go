// Package connector owns private work-app grants. Sign-in identities
// and Personal Lester never use these credentials. Nothing is put in Computers.
package connector

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrUnavailable  = errors.New("此应用尚未配置，请联系管理员")
	ErrConflict     = errors.New("应用连接状态已变化，请刷新后重试")
	ErrSession      = errors.New("登录已失效，请重新登录后连接应用")
	ErrNotConnected = errors.New("请先在应用连接中授权此应用")
	ErrReauthorize  = errors.New("应用授权已失效，请重新连接")
	ErrProvider     = errors.New("应用服务暂时不可用，请稍后重试")
	ErrPermission   = errors.New("应用未授予此操作权限，或当前服务商套餐不支持")
	ErrRateLimit    = errors.New("应用请求已限流，请稍后重试")
	ErrArguments    = errors.New("应用读取参数无效，请检查资源标识与查询条件")
	ErrResponse     = errors.New("应用返回内容过大或格式无效，请缩小读取范围")
)

type Actor struct {
	WorkspaceID, UserID uuid.UUID
	SessionHash         []byte
}
type Connection struct {
	Provider          string     `json:"provider"`
	Configured        bool       `json:"configured"`
	Status            string     `json:"status"`
	AccountName       string     `json:"account_name"`
	Scopes            []string   `json:"scopes"`
	Revision          int64      `json:"revision"`
	UpdatedAt         *time.Time `json:"updated_at,omitempty"`
	GitHubCodeEnabled bool       `json:"github_code_enabled"`
}
type ReadRequest struct {
	Provider string `json:"provider"`
	Action   string `json:"action"`
	Kind     string `json:"kind,omitempty"`
	Query    string `json:"query,omitempty"`
	Resource string `json:"resource,omitempty"`
	Cursor   string `json:"cursor,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}
type Item struct {
	ID       string `json:"id"`
	Title    string `json:"title,omitempty"`
	Text     string `json:"text,omitempty"`
	Author   string `json:"author,omitempty"`
	URL      string `json:"url,omitempty"`
	Resource string `json:"resource"`
}
type ReadResult struct {
	Provider   string `json:"provider"`
	Items      []Item `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	Truncated  bool   `json:"truncated"`
	Notice     string `json:"notice"`
}
type token struct {
	Access  string    `json:"access"`
	Refresh string    `json:"refresh,omitempty"`
	Expires time.Time `json:"expires,omitempty"`
	Scopes  []string  `json:"scopes"`
}
type identity struct{ ID, Name string }
type Provider struct {
	Name, ClientID, ClientSecret          string
	AuthURL, TokenURL, APIURL, ProfileURL string
	Scopes                                []string
	PKCE                                  bool
}
type Options struct {
	WebOrigin    string
	SecureCookie bool
	MasterKey    []byte
	Providers    []Provider
	HTTPClient   *http.Client
}
type Service struct {
	DB        *pgxpool.Pool
	options   Options
	providers map[string]Provider
	vault     cipher.AEAD
	client    *http.Client
}

var providerOrder = []string{"github", "gmail", "x", "feishu", "slack"}

func New(db *pgxpool.Pool, options Options) (*Service, error) {
	if len(options.MasterKey) != 32 {
		return nil, errors.New("app connection master key must contain 32 bytes")
	}
	block, err := aes.NewCipher(options.MasterKey)
	if err != nil {
		return nil, err
	}
	vault, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	// Never forward a provider token through an HTTP redirect, including to a
	// different API endpoint on the same host. Tests inject a transport, not redirects.
	copyClient := *client
	copyClient.Jar = nil
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	s := &Service{DB: db, options: options, providers: map[string]Provider{}, vault: vault, client: &copyClient}
	for _, p := range options.Providers {
		if p.ClientID != "" && p.ClientSecret != "" {
			s.providers[p.Name] = p
		}
	}
	return s, nil
}
func digest(value string) []byte { sum := sha256.Sum256([]byte(value)); return sum[:] }
func randomToken() (string, error) {
	v := make([]byte, 32)
	_, err := rand.Read(v)
	return base64.RawURLEncoding.EncodeToString(v), err
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
func (s *Service) open(ciphertext, nonce []byte, aad string, value any) error {
	if len(nonce) != s.vault.NonceSize() {
		return ErrReauthorize
	}
	raw, err := s.vault.Open(nil, nonce, ciphertext, []byte(aad))
	if err != nil {
		return ErrReauthorize
	}
	return json.Unmarshal(raw, value)
}
func connectionAAD(id uuid.UUID, a Actor, provider string) string {
	return "app-connection:" + id.String() + ":" + a.WorkspaceID.String() + ":" + a.UserID.String() + ":" + provider
}
