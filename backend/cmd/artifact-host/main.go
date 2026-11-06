package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/artifact"
	"github.com/biubiuqiu/lester-agent/backend/internal/blob"
	"github.com/biubiuqiu/lester-agent/backend/internal/database"
	"github.com/biubiuqiu/lester-agent/backend/internal/telemetry"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(telemetry.NewLogHandler(slog.NewJSONHandler(os.Stdout, nil)))
	slog.SetDefault(logger)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	observability, err := telemetry.Init(ctx, "lester-artifact-host")
	if err != nil {
		logger.Error("OpenTelemetry configuration", "error", err)
		return err
	}
	defer func() {
		flushCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if observability.Shutdown(flushCtx) != nil {
			logger.Warn("OpenTelemetry shutdown did not finish")
		}
	}()
	secure, err := strconv.ParseBool(env("OBJECT_STORE_USE_SSL", "false"))
	if err != nil {
		logger.Error("invalid object store SSL configuration")
		return err
	}
	if err = artifact.ValidateOrigin(os.Getenv("ARTIFACT_PUBLIC_URL"), os.Getenv("WEB_ORIGIN")); err != nil {
		logger.Error("hosting origin", "error", err)
		return err
	}
	db, err := database.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		logger.Error("database", "error", err)
		return err
	}
	defer db.Close()
	store, err := blob.NewMinIO(os.Getenv("OBJECT_STORE_ENDPOINT"), os.Getenv("OBJECT_STORE_ACCESS_KEY"), os.Getenv("OBJECT_STORE_SECRET_KEY"), os.Getenv("OBJECT_STORE_BUCKET"), secure)
	if err != nil {
		logger.Error("object store", "error", err)
		return err
	}
	host := &artifact.Host{Catalog: &artifact.Service{DB: db}, Store: store}
	server := &http.Server{Addr: env("HTTP_ADDR", ":8082"), Handler: telemetry.HTTPMiddleware("artifact-host")(host), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		stop, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_ = server.Shutdown(stop)
	}()
	logger.Info("artifact host listening", "address", server.Addr)
	err = server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		logger.Error("serve", "error", err)
	}
	cancel()
	<-shutdownDone
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
