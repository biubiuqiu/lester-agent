package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/biubiuqiu/lester-agent/internal/auth"
	"github.com/biubiuqiu/lester-agent/internal/config"
	"github.com/biubiuqiu/lester-agent/internal/conversation"
	"github.com/biubiuqiu/lester-agent/internal/database"
	"github.com/biubiuqiu/lester-agent/internal/httpapi"
	"github.com/biubiuqiu/lester-agent/internal/model"
	"github.com/biubiuqiu/lester-agent/internal/sandbox"
	"github.com/biubiuqiu/lester-agent/internal/secret"
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
	modelStore := model.NewStore(db, secrets)
	sandboxClient := sandbox.NewClient(cfg.SandboxURL)
	conversationService := conversation.New(db, redisClient, modelStore, sandboxClient)
	conversationHandler := conversation.NewHandler(conversationService, db, redisClient, sandboxClient, cfg.SandboxURL)
	authService := auth.New(db, cfg.SessionTTL, false)
	handler := httpapi.Router(httpapi.Dependencies{Logger: logger, WebOrigin: cfg.WebOrigin, Auth: authService, Models: model.NewHandler(modelStore), Conversations: conversationHandler})
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go conversationService.SuspendIdle(ctx, cfg.SandboxIdleTTL)
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
