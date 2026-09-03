package main

import (
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func main() {
	address := os.Getenv("HTTP_ADDR")
	if address == "" {
		address = ":8090"
	}
	token := os.Getenv("SANDBOX_SERVICE_TOKEN")
	if len(token) < 32 {
		slog.Error("SANDBOX_SERVICE_TOKEN must be at least 32 characters")
		os.Exit(1)
	}
	provider, err := sandbox.NewProviderFromEnv()
	if err != nil {
		slog.Error("configure sandbox provider", "error", err)
		os.Exit(1)
	}
	handler := sandbox.NewServiceHandler(provider, token)
	server := &http.Server{Addr: address, Handler: handler.Router(), ReadHeaderTimeout: 10 * time.Second}
	slog.Info("sandbox service listening", "address", address, "provider", provider.Name())
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("sandbox service", "error", err)
		os.Exit(1)
	}
}
