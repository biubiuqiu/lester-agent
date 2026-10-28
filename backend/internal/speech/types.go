// Package speech provides deployment-configured input transcription. It never
// starts an Agent, writes a task message, or receives Computer/tool authority.
package speech

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"net/http"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const MaxAudioBytes = 10 << 20
const MaxDurationSeconds = 120 // Client recording limit; container duration is not inferred from filenames.
const MaxTextRunes = 20000

var (
	ErrUnauthorized = errors.New("语音输入登录已失效，请重新登录")
	ErrAdmin        = errors.New("只有管理员可以配置语音输入")
	ErrDisabled     = errors.New("语音输入尚未启用，请联系管理员")
	ErrConfig       = errors.New("语音配置无效，请检查服务商、接口、模型和密钥")
	ErrConflict     = errors.New("语音配置或请求已变化，请刷新状态后重试")
	ErrLimit        = errors.New("语音内容超过限制，请缩短录音后重试")
	ErrUnsupported  = errors.New("录音格式不受支持，请更换浏览器后重试")
	ErrNoSpeech     = errors.New("没有识别到语音，请重新录音")
	ErrProvider     = errors.New("语音转写结果尚未确认，请检查原请求状态，不要重复上传")
	ErrProviderAuth = errors.New("语音服务授权失败，请联系管理员检查密钥")
	ErrRate         = errors.New("语音请求过于频繁，请稍后再试")
	ErrBusy         = errors.New("已有语音正在转写，请等待完成")
	ErrNotFound     = errors.New("语音转写记录不存在或已过期")
	ErrExpired      = errors.New("语音转写记录已过期，请重新录音")
	ErrRequest      = errors.New("语音请求无效，请重新录音")
	ErrStorage      = errors.New("语音服务暂时不可用，请稍后检查状态")
)

type Actor = auth.Principal

type Config struct {
	Enabled           bool   `json:"enabled"`
	Provider          string `json:"provider"`
	Endpoint          string `json:"endpoint"`
	ModelID           string `json:"model_id"`
	HasCredential     bool   `json:"has_credential"`
	Revision          int64  `json:"revision"`
	ciphertext, nonce []byte
}
type ConfigChange struct {
	Revision        int64  `json:"revision"`
	Enabled         bool   `json:"enabled"`
	Provider        string `json:"provider"`
	Endpoint        string `json:"endpoint"`
	ModelID         string `json:"model_id"`
	Credential      string `json:"credential,omitempty"`
	ClearCredential bool   `json:"clear_credential,omitempty"`
}
type Capability struct {
	Enabled            bool   `json:"enabled"`
	Provider           string `json:"provider"`
	MaxAudioBytes      int    `json:"max_audio_bytes"`
	MaxDurationSeconds int    `json:"max_duration_seconds"`
	Revision           int64  `json:"revision"`
}
type Upload struct {
	Data                     []byte
	Filename, MIME, Language string
	ConfigurationRevision    int64
}
type Receipt struct {
	ID     uuid.UUID `json:"id"`
	Status string    `json:"status"`
	Text   string    `json:"text,omitempty"`
	Error  string    `json:"error,omitempty"`
}
type Service struct {
	DB     *pgxpool.Pool
	vault  cipher.AEAD
	client *http.Client
}

func New(db *pgxpool.Pool, key []byte) (*Service, error) {
	if len(key) != 32 {
		return nil, errors.New("speech encryption key must contain 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	vault, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Service{DB: db, vault: vault, client: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
