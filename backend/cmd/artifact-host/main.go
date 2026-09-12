package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/artifact"
	"github.com/biubiuqiu/lester-agent/backend/internal/blob"
	"github.com/biubiuqiu/lester-agent/backend/internal/database"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	secure, err := strconv.ParseBool(env("OBJECT_STORE_USE_SSL", "false"))
	if err != nil {
		logger.Error("invalid object store SSL configuration")
		os.Exit(1)
	}
	if err = artifact.ValidateOrigin(os.Getenv("ARTIFACT_PUBLIC_URL"), os.Getenv("WEB_ORIGIN")); err != nil {
		logger.Error("hosting origin", "error", err)
		os.Exit(1)
	}
	db, err := database.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		logger.Error("database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	store, err := blob.NewMinIO(os.Getenv("OBJECT_STORE_ENDPOINT"), os.Getenv("OBJECT_STORE_ACCESS_KEY"), os.Getenv("OBJECT_STORE_SECRET_KEY"), os.Getenv("OBJECT_STORE_BUCKET"), secure)
	if err != nil {
		logger.Error("object store", "error", err)
		os.Exit(1)
	}
	host := &artifact.Host{Catalog: &artifact.Service{DB: db}, Store: store}
	server := &http.Server{Addr: env("HTTP_ADDR", ":8082"), Handler: host, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		stop, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_ = server.Shutdown(stop)
	}()
	logger.Info("artifact host listening", "address", server.Addr)
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("serve", "error", err)
		os.Exit(1)
	}
}
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
