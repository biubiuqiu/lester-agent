package server

import (
	"log/slog"
	"net/http"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/conversation"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/skill"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Dependencies struct {
	Logger        *slog.Logger
	WebOrigin     string
	Auth          *auth.Service
	Models        *model.Handler
	Conversations *conversation.Handler
	Skills        *skill.Handler
}

func Router(deps Dependencies) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID, middleware.RealIP, httpapi.Recover(deps.Logger), httpapi.AccessLog(deps.Logger), httpapi.CORS(deps.WebOrigin))
	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { httpapi.JSON(w, 200, map[string]bool{"ok": true}) })
	router.Route("/api/v1", func(api chi.Router) {
		api.Post("/auth/register", deps.Auth.Register)
		api.Post("/auth/login", deps.Auth.Login)
		api.Post("/auth/logout", deps.Auth.Logout)
		api.Group(func(private chi.Router) {
			private.Use(deps.Auth.Middleware)
			private.Get("/me", deps.Auth.Me)
			private.Patch("/me", deps.Auth.UpdateProfile)
			private.Get("/agents", func(w http.ResponseWriter, _ *http.Request) {
				httpapi.JSON(w, 200, map[string]any{"agents": []map[string]string{{"slug": "lester", "name": "Lester"}, {"slug": "franklin", "name": "Franklin"}, {"slug": "michael", "name": "Michael"}, {"slug": "trevor", "name": "Trevor"}}})
			})
			private.Get("/model-connections", deps.Models.ListConnections)
			private.Post("/model-connections", deps.Models.CreateConnection)
			private.Post("/model-connections/{id}/test", deps.Models.TestConnection)
			private.Get("/model-deployments", deps.Models.ListDeployments)
			private.Post("/model-deployments", deps.Models.CreateDeployment)
			private.Get("/skills", deps.Skills.List)
			private.Get("/events", deps.Conversations.WorkspaceEvents)
			private.Get("/conversations", deps.Conversations.List)
			private.Post("/conversations", deps.Conversations.Create)
			private.Get("/conversations/{id}", deps.Conversations.Get)
			private.Patch("/conversations/{id}", deps.Conversations.Update)
			private.Post("/conversations/{id}/messages", deps.Conversations.Send)
			private.Post("/conversations/{id}/runs/{runID}/cancel", deps.Conversations.CancelRun)
			private.Post("/conversations/{id}/attachments", deps.Conversations.UploadAttachment)
			private.Get("/conversations/{id}/skills", deps.Skills.Installed)
			private.Post("/conversations/{id}/skills/{slug}/install", deps.Skills.Install)
			private.Delete("/conversations/{id}/skills/{slug}", deps.Skills.Uninstall)
			private.Get("/conversations/{id}/events", deps.Conversations.Events)
			private.Get("/conversations/{id}/events/history", deps.Conversations.EventHistory)
			private.Get("/conversations/{id}/computer", deps.Conversations.Computer)
			private.Get("/conversations/{id}/files", deps.Conversations.Files)
			private.Get("/conversations/{id}/files/content", deps.Conversations.ReadFile)
			private.Get("/conversations/{id}/preview/*", deps.Conversations.PreviewFile)
			private.Post("/conversations/{id}/files", deps.Conversations.WriteFile)
			private.Post("/conversations/{id}/exec", deps.Conversations.Exec)
			private.Get("/conversations/{id}/terminal", deps.Conversations.Terminal)
		})
	})
	return router
}
