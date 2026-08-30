package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr, DatabaseURL, RedisURL, WebOrigin, SandboxURL string
	ObjectStoreEndpoint, ObjectStoreAccessKey              string
	ObjectStoreSecretKey, ObjectStoreBucket                string
	ObjectStoreUseSSL                                      bool
	MasterKey                                              []byte
	SessionTTL, SandboxIdleTTL, SandboxMonitorInterval     time.Duration
}

func Load() (Config, error) {
	useSSL, err := strconv.ParseBool(env("OBJECT_STORE_USE_SSL", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("OBJECT_STORE_USE_SSL must be a boolean")
	}
	c := Config{HTTPAddr: env("HTTP_ADDR", ":8080"), DatabaseURL: env("DATABASE_URL", "postgres://lester:lester@localhost:5432/lester?sslmode=disable"), RedisURL: env("REDIS_URL", "redis://localhost:6379/0"), WebOrigin: env("WEB_ORIGIN", "http://localhost:3000"), SandboxURL: env("SANDBOX_SERVICE_URL", "http://localhost:8090"), ObjectStoreEndpoint: env("OBJECT_STORE_ENDPOINT", "localhost:9000"), ObjectStoreAccessKey: env("OBJECT_STORE_ACCESS_KEY", "lester"), ObjectStoreSecretKey: env("OBJECT_STORE_SECRET_KEY", "lester-development"), ObjectStoreBucket: env("OBJECT_STORE_BUCKET", "lester-skills"), ObjectStoreUseSSL: useSSL, SessionTTL: 30 * 24 * time.Hour, SandboxIdleTTL: 30 * time.Minute, SandboxMonitorInterval: 30 * time.Second}
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
