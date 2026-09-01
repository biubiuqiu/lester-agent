package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/blob"
	"github.com/biubiuqiu/lester-agent/backend/internal/config"
	"github.com/biubiuqiu/lester-agent/backend/internal/conversation"
	"github.com/biubiuqiu/lester-agent/backend/internal/database"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/model/integration"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/biubiuqiu/lester-agent/backend/internal/secret"
	"github.com/biubiuqiu/lester-agent/backend/internal/server"
	"github.com/biubiuqiu/lester-agent/backend/internal/skill"
	"github.com/redis/go-redis/v9"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration", "error", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	redisOptions, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		logger.Error("redis url", "error", err)
		os.Exit(1)
	}
	redisClient := redis.NewClient(redisOptions)
	defer redisClient.Close()
	secrets, err := secret.New(db, cfg.MasterKey)
	if err != nil {
		logger.Error("secret store", "error", err)
		os.Exit(1)
	}
	modelStore := model.NewStore(db, secrets, integration.NewDefaultRegistry())
	sandboxClient := sandbox.NewClient(cfg.SandboxURL, cfg.SandboxToken)
	toolRegistry := agenttool.NewDefaultRegistry(db)
	conversationService := conversation.New(db, redisClient, modelStore, sandboxClient, toolRegistry)
	conversationHandler := conversation.NewHandler(conversationService, db, redisClient, sandboxClient, cfg.SandboxURL, cfg.SandboxToken, cfg.WebOrigin)
	objectStore, err := blob.NewMinIO(cfg.ObjectStoreEndpoint, cfg.ObjectStoreAccessKey, cfg.ObjectStoreSecretKey, cfg.ObjectStoreBucket, cfg.ObjectStoreUseSSL)
	if err != nil {
		logger.Error("object store", "error", err)
		os.Exit(1)
	}
	if err = waitForObjectStore(ctx, objectStore); err != nil {
		logger.Error("object store", "error", err)
		os.Exit(1)
	}
	skillService := skill.New(db, objectStore, sandboxClient)
	if err = skillService.SeedDefaults(ctx); err != nil {
		logger.Error("seed skills", "error", err)
		os.Exit(1)
	}
	authService := auth.New(db, redisClient, cfg.SessionTTL, cfg.SessionCookieSecure)
	handler := server.Router(server.Dependencies{Logger: logger, WebOrigin: cfg.WebOrigin, Auth: authService, Models: model.NewHandler(modelStore), Conversations: conversationHandler, Skills: skill.NewHandler(skillService, conversationService)})
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go conversationService.SuspendIdle(ctx, cfg.SandboxIdleTTL)
	go conversationService.MonitorSandboxes(ctx, cfg.SandboxMonitorInterval)
	go func() {
		<-ctx.Done()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = server.Shutdown(shutdownCtx)
	}()
	logger.Info("api listening", "address", cfg.HTTPAddr)
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server", "error", err)
		os.Exit(1)
	}
}

func waitForObjectStore(ctx context.Context, store blob.Store) error {
	var err error
	for attempt := 0; attempt < 30; attempt++ {
		if err = store.Ensure(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return err
}
