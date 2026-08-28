package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"time"
)

type Config struct {
	HTTPAddr, DatabaseURL, RedisURL, WebOrigin, SandboxURL string
	MasterKey                                              []byte
	SessionTTL, SandboxIdleTTL                             time.Duration
}

func Load() (Config, error) {
	c := Config{HTTPAddr: env("HTTP_ADDR", ":8080"), DatabaseURL: env("DATABASE_URL", "postgres://lester:lester@localhost:5432/lester?sslmode=disable"), RedisURL: env("REDIS_URL", "redis://localhost:6379/0"), WebOrigin: env("WEB_ORIGIN", "http://localhost:3000"), SandboxURL: env("SANDBOX_SERVICE_URL", "http://localhost:8090"), SessionTTL: 30 * 24 * time.Hour, SandboxIdleTTL: 30 * time.Minute}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("MASTER_KEY_BASE64"))
	if err != nil || len(key) != 32 {
		return Config{}, fmt.Errorf("MASTER_KEY_BASE64 must decode to 32 bytes")
	}
	c.MasterKey = key
	return c, nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
