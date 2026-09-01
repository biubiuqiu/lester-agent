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
	image := os.Getenv("SANDBOX_IMAGE")
	token := os.Getenv("SANDBOX_SERVICE_TOKEN")
	if len(token) < 32 {
		slog.Error("SANDBOX_SERVICE_TOKEN must be at least 32 characters")
		os.Exit(1)
	}
	handler := sandbox.NewServiceHandler(sandbox.NewDockerProvider(image), token)
	server := &http.Server{Addr: address, Handler: handler.Router(), ReadHeaderTimeout: 10 * time.Second}
	slog.Info("sandbox service listening", "address", address)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("sandbox service", "error", err)
		os.Exit(1)
	}
}
