package server

import (
	"log/slog"
	"net/http"

	"github.com/biubiuqiu/lester-agent/backend/internal/agent"
	"github.com/biubiuqiu/lester-agent/backend/internal/artifact"
	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/contextlibrary"
	"github.com/biubiuqiu/lester-agent/backend/internal/conversation"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/internal/project"
	"github.com/biubiuqiu/lester-agent/backend/internal/skill"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Dependencies struct {
	Contexts      *contextlibrary.Handler
	Agents        *agent.Handler
	AgentBuilder  *agent.BuilderHandler
	Logger        *slog.Logger
	WebOrigin     string
	Auth          *auth.Service
	Models        *model.Handler
	Conversations *conversation.Handler
	Skills        *skill.Handler
	Projects      *project.Handler
	Artifacts     *artifact.Handler
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
			if deps.Contexts != nil {
				private.Get("/contexts", deps.Contexts.List)
				private.Post("/contexts", deps.Contexts.Save)
				private.Get("/contexts/{id}", deps.Contexts.Get)
				private.Patch("/contexts/{id}", deps.Contexts.Save)
				private.Delete("/contexts/{id}", deps.Contexts.Delete)
			}
			private.Route("/admin", func(admin chi.Router) {
				admin.Use(auth.RequireAdmin)
				admin.Get("/users", deps.Auth.ListUsers)
				admin.Post("/users", deps.Auth.AddUser)
				admin.Patch("/users/{id}", deps.Auth.UpdateUser)
				admin.Get("/model-connections", deps.Models.ListConnections)
				admin.Post("/model-connections", deps.Models.CreateConnection)
				admin.Patch("/model-connections/{id}", deps.Models.UpdateConnection)
				admin.Get("/model-deployments", deps.Models.ListDeployments)
				admin.Post("/model-deployments", deps.Models.CreateDeployment)
				admin.Patch("/model-deployments/{id}", deps.Models.UpdateDeployment)
			})
			if deps.Projects != nil {
				private.Get("/projects", deps.Projects.List)
				private.Post("/projects", deps.Projects.Create)
				private.Patch("/projects/{id}", deps.Projects.Update)
			}
			if deps.Artifacts != nil {
				private.Get("/artifacts", deps.Artifacts.List)
				private.Post("/conversations/{id}/artifacts", deps.Artifacts.Publish)
				private.Post("/artifacts/{id}/unpublish", deps.Artifacts.Unpublish)
			}
			private.Patch("/me", deps.Auth.UpdateProfile)
			if deps.Agents != nil {
				private.Get("/agents", deps.Agents.List)
				private.Post("/agents", deps.Agents.Save)
				private.Get("/agents/{slug}", deps.Agents.Get)
				private.Patch("/agents/{id}", deps.Agents.Save)
				private.Delete("/agents/{id}", deps.Agents.Delete)
			}
			if deps.AgentBuilder != nil {
				private.Post("/agent-builder/chat", deps.AgentBuilder.Chat)
			}
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
			private.Patch("/conversations/{id}/organization", deps.Conversations.Organize)
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
