package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
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
	observability, err := telemetry.Init(ctx, "lester-sandbox-service")
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
	address := os.Getenv("HTTP_ADDR")
	if address == "" {
		address = ":8090"
	}
	token := os.Getenv("SANDBOX_SERVICE_TOKEN")
	if len(token) < 32 {
		err := errors.New("SANDBOX_SERVICE_TOKEN must be at least 32 characters")
		logger.Error(err.Error())
		return err
	}
	provider, err := sandbox.NewProviderFromEnv()
	if err != nil {
		logger.Error("configure sandbox provider", "error", err)
		return err
	}
	handler := sandbox.NewServiceHandler(provider, token)
	server := &http.Server{Addr: address, Handler: handler.Router(), ReadHeaderTimeout: 10 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = server.Shutdown(shutdownCtx)
	}()
	logger.Info("sandbox service listening", "address", address, "provider", provider.Name())
	err = server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		logger.Error("sandbox service", "error", err)
	}
	cancel()
	<-shutdownDone
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
