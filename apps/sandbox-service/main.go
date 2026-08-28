package main

import (
	"github.com/biubiuqiu/lester-agent/internal/sandbox"
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
	handler := sandbox.NewServiceHandler(sandbox.NewDockerProvider(image))
	server := &http.Server{Addr: address, Handler: handler.Router(), ReadHeaderTimeout: 10 * time.Second}
	slog.Info("sandbox service listening", "address", address)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("sandbox service", "error", err)
		os.Exit(1)
	}
}
