package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr, DatabaseURL, RedisURL, WebOrigin, SandboxURL                 string
	SandboxToken                                                           string
	ObjectStoreEndpoint, ObjectStoreAccessKey                              string
	ObjectStoreSecretKey, ObjectStoreBucket                                string
	ObjectStoreUseSSL, SessionCookieSecure                                 bool
	MasterKey                                                              []byte
	SessionTTL, SandboxIdleTTL, SandboxMonitorInterval                     time.Duration
	RegistrationEnabled                                                    bool
	GoogleClientID, GoogleClientSecret, GitHubClientID, GitHubClientSecret string
	SMTPHost, SMTPUsername, SMTPPassword, SMTPFrom, SMTPTLSMode            string
	SMTPPort                                                               int
}

func Load() (Config, error) {
	databaseURL, err := requiredEnv("DATABASE_URL")
	if err != nil {
		return Config{}, err
	}
	objectStoreSecretKey, err := requiredEnv("OBJECT_STORE_SECRET_KEY")
	if err != nil {
		return Config{}, err
	}
	useSSL, err := strconv.ParseBool(env("OBJECT_STORE_USE_SSL", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("OBJECT_STORE_USE_SSL must be a boolean")
	}
	webOrigin := strings.TrimRight(env("WEB_ORIGIN", "http://localhost:3000"), "/")
	secureCookie, err := strconv.ParseBool(env("SESSION_COOKIE_SECURE", strconv.FormatBool(strings.HasPrefix(strings.ToLower(webOrigin), "https://"))))
	if err != nil {
		return Config{}, fmt.Errorf("SESSION_COOKIE_SECURE must be a boolean")
	}
	c := Config{HTTPAddr: env("HTTP_ADDR", ":8080"), DatabaseURL: databaseURL, RedisURL: env("REDIS_URL", "redis://localhost:6379/0"), WebOrigin: webOrigin, SandboxURL: env("SANDBOX_SERVICE_URL", "http://localhost:8090"), SandboxToken: os.Getenv("SANDBOX_SERVICE_TOKEN"), ObjectStoreEndpoint: env("OBJECT_STORE_ENDPOINT", "localhost:9000"), ObjectStoreAccessKey: env("OBJECT_STORE_ACCESS_KEY", "lester"), ObjectStoreSecretKey: objectStoreSecretKey, ObjectStoreBucket: env("OBJECT_STORE_BUCKET", "lester-skills"), ObjectStoreUseSSL: useSSL, SessionCookieSecure: secureCookie, SessionTTL: 30 * 24 * time.Hour, SandboxIdleTTL: 30 * time.Minute, SandboxMonitorInterval: 30 * time.Second}
	if len(c.SandboxToken) < 32 {
		return Config{}, fmt.Errorf("SANDBOX_SERVICE_TOKEN must be at least 32 characters")
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("MASTER_KEY_BASE64"))
	if err != nil || len(key) != 32 {
		return Config{}, fmt.Errorf("MASTER_KEY_BASE64 must decode to 32 bytes")
	}
	c.MasterKey = key
	c.RegistrationEnabled, err = strconv.ParseBool(env("AUTH_REGISTRATION_ENABLED", "true"))
	if err != nil {
		return Config{}, fmt.Errorf("AUTH_REGISTRATION_ENABLED must be a boolean")
	}
	c.GoogleClientID = strings.TrimSpace(os.Getenv("GOOGLE_OAUTH_CLIENT_ID"))
	c.GoogleClientSecret = os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET")
	c.GitHubClientID = strings.TrimSpace(os.Getenv("GITHUB_OAUTH_CLIENT_ID"))
	c.GitHubClientSecret = os.Getenv("GITHUB_OAUTH_CLIENT_SECRET")
	if (c.GoogleClientID == "") != (c.GoogleClientSecret == "") {
		return Config{}, fmt.Errorf("GOOGLE_OAUTH_CLIENT_ID and GOOGLE_OAUTH_CLIENT_SECRET must both be set")
	}
	if (c.GitHubClientID == "") != (c.GitHubClientSecret == "") {
		return Config{}, fmt.Errorf("GITHUB_OAUTH_CLIENT_ID and GITHUB_OAUTH_CLIENT_SECRET must both be set")
	}
	c.SMTPHost = strings.TrimSpace(os.Getenv("SMTP_HOST"))
	c.SMTPUsername = os.Getenv("SMTP_USERNAME")
	c.SMTPPassword = os.Getenv("SMTP_PASSWORD")
	c.SMTPFrom = os.Getenv("SMTP_FROM")
	c.SMTPTLSMode = env("SMTP_TLS_MODE", "starttls")
	c.SMTPPort, err = strconv.Atoi(env("SMTP_PORT", "587"))
	if err != nil || c.SMTPPort < 1 || c.SMTPPort > 65535 {
		return Config{}, fmt.Errorf("SMTP_PORT must be between 1 and 65535")
	}
	if c.SMTPHost == "" && (c.SMTPUsername != "" || c.SMTPPassword != "" || c.SMTPFrom != "") {
		return Config{}, fmt.Errorf("SMTP_HOST is required when mail credentials or sender are set")
	}
	return c, nil
}

func requiredEnv(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
