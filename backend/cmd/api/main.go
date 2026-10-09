package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/agent"
	"github.com/biubiuqiu/lester-agent/backend/internal/agenttool"
	"github.com/biubiuqiu/lester-agent/backend/internal/artifact"
	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/blob"
	"github.com/biubiuqiu/lester-agent/backend/internal/config"
	"github.com/biubiuqiu/lester-agent/backend/internal/contextlibrary"
	"github.com/biubiuqiu/lester-agent/backend/internal/conversation"
	"github.com/biubiuqiu/lester-agent/backend/internal/database"
	"github.com/biubiuqiu/lester-agent/backend/internal/deliverable"
	"github.com/biubiuqiu/lester-agent/backend/internal/eventlog"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/model/integration"
	"github.com/biubiuqiu/lester-agent/backend/internal/project"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/biubiuqiu/lester-agent/backend/internal/secret"
	"github.com/biubiuqiu/lester-agent/backend/internal/server"
	"github.com/biubiuqiu/lester-agent/backend/internal/skill"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration", "error", err)
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database", "error", err)
		return err
	}
	defer db.Close()
	redisOptions, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		logger.Error("redis url", "error", err)
		return err
	}
	redisClient := redis.NewClient(redisOptions)
	defer redisClient.Close()
	secrets, err := secret.New(db, cfg.MasterKey)
	if err != nil {
		logger.Error("secret store", "error", err)
		return err
	}
	modelStore := model.NewStore(db, secrets, integration.NewDefaultRegistry())
	sandboxClient := sandbox.NewClient(cfg.SandboxURL, cfg.SandboxToken)
	toolRegistry := agenttool.NewDefaultRegistry(db)
	toolRegistry.Register(agenttool.SaveAgent{DB: db})
	conversationService := conversation.New(db, modelStore, sandboxClient, toolRegistry)
	conversationHandler := conversation.NewHandler(conversationService, db, redisClient, sandboxClient, cfg.SandboxURL, cfg.SandboxToken, cfg.WebOrigin)
	objectStore, err := blob.NewMinIO(cfg.ObjectStoreEndpoint, cfg.ObjectStoreAccessKey, cfg.ObjectStoreSecretKey, cfg.ObjectStoreBucket, cfg.ObjectStoreUseSSL)
	if err != nil {
		logger.Error("object store", "error", err)
		return err
	}
	if err = waitForObjectStore(ctx, objectStore); err != nil {
		logger.Error("object store", "error", err)
		return err
	}
	skillService := skill.New(db, objectStore, sandboxClient)
	conversationService.SetAgentObjectStore(objectStore)
	conversationService.SetAgentSkillInstaller(func(ctx context.Context, workspaceID, userID, conversationID uuid.UUID, sandboxID, workDir, slug string) error {
		_, err := skillService.Install(ctx, workspaceID, userID, conversationID, sandboxID, workDir, slug)
		return err
	})
	artifactURL := os.Getenv("ARTIFACT_PUBLIC_URL")
	if artifactURL == "" {
		artifactURL = "http://127.0.0.1:13181"
	}
	if err = artifact.ValidateOrigin(artifactURL, cfg.WebOrigin); err != nil {
		logger.Error("artifact origin", "error", err)
		return err
	}
	artifactService := &artifact.Service{DB: db, Store: objectStore, Files: sandboxClient, BaseURL: artifactURL, Prepare: func(ctx context.Context, workspaceID, conversationID uuid.UUID) (string, string, error) {
		computer, err := conversationService.ComputerForConversation(ctx, workspaceID, conversationID)
		if err != nil {
			return "", "", err
		}
		return computer.SandboxID, computer.WorkDir, nil
	}}
	toolRegistry.Register(agenttool.DeployHTML{Service: artifactService})
	deliverableService := &deliverable.Service{DB: db, Files: sandboxClient}
	toolRegistry.Register(agenttool.RegisterDeliverable{Service: deliverableService})
	if err = skillService.SeedDefaults(ctx); err != nil {
		logger.Error("seed skills", "error", err)
		return err
	}
	authService := auth.New(db, redisClient, cfg.AccessTokenTTL, cfg.SessionCookieSecure)
	var mailer auth.Mailer
	if cfg.SMTPHost != "" {
		smtpMailer, mailErr := auth.NewSMTPMailer(auth.SMTPConfig{Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername, Password: cfg.SMTPPassword, From: cfg.SMTPFrom, TLSMode: cfg.SMTPTLSMode})
		if mailErr != nil {
			return mailErr
		}
		mailer = smtpMailer
	}
	if err = authService.Configure(auth.Options{WebOrigin: cfg.WebOrigin, RegistrationEnabled: cfg.RegistrationEnabled, Providers: auth.DefaultProviders(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.GitHubClientID, cfg.GitHubClientSecret), Mailer: mailer, Avatars: objectStore}); err != nil {
		return err
	}
	handler := server.Router(server.Dependencies{Deliverables: &deliverable.Handler{Service: deliverableService}, Contexts: &contextlibrary.Handler{Service: &contextlibrary.Service{DB: db}}, Agents: &agent.Handler{Service: &agent.Service{DB: db, Objects: objectStore}}, AgentBuilder: &agent.BuilderHandler{DB: db, Models: modelStore}, Logger: logger, WebOrigin: cfg.WebOrigin, Auth: authService, Models: model.NewHandler(modelStore), Conversations: conversationHandler, Skills: skill.NewHandler(skillService, conversationService), Projects: &project.Handler{Service: &project.Service{DB: db}}, Artifacts: &artifact.Handler{Service: artifactService}})
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	dispatcher := &eventlog.Dispatcher{DB: db, Publish: func(ctx context.Context, channel string, data []byte) error {
		return redisClient.Publish(ctx, channel, data).Err()
	}}
	go dispatcher.Run(ctx)
	shutdownDone := make(chan struct{})
	go conversationService.SuspendIdle(ctx, cfg.SandboxIdleTTL)
	go conversationService.MonitorSandboxes(ctx, cfg.SandboxMonitorInterval)
	go func() {
		<-ctx.Done()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = server.Shutdown(shutdownCtx)
		close(shutdownDone)
	}()
	logger.Info("api listening", "address", cfg.HTTPAddr)
	serveErr := server.ListenAndServe()
	if serveErr != nil && serveErr != http.ErrServerClosed {
		logger.Error("server", "error", serveErr)
		cancel()
	}
	<-shutdownDone
	shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	if err = conversationService.Shutdown(shutdownCtx); err != nil {
		logger.Error("run shutdown", "error", err)
	}
	stop()
	deliveryCtx, stopDelivery := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopDelivery()
	for deliveryCtx.Err() == nil {
		count, deliveryErr := dispatcher.Flush(deliveryCtx)
		if deliveryErr != nil {
			logger.Error("events remain queued for next startup", "error", deliveryErr)
			break
		}
		if count == 0 {
			break
		}
	}
	if serveErr == http.ErrServerClosed {
		return nil
	}
	return serveErr
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
