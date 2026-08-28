# Lester

Lester is an open-source, self-hostable AI Agent Workspace. Each conversation owns a logical Computer, while model-provider and sandbox-provider differences stay below the Agent runtime.

This repository implements **Phase 0 through Phase 4** from the v0.1 architecture handoff:

- Next.js conversation-first UI based on the interactive prototype
- Go modular-monolith control plane and a separately deployable sandbox service
- email/password authentication, secure sessions, and Personal Workspace creation
- encrypted provider credentials and workspace-scoped data access
- unified OpenAI/Anthropic model layer and required provider configuration
- four fixed-per-conversation personas: Lester, Franklin, Michael, and Trevor
- durable messages/runs/events in PostgreSQL with Redis SSE fan-out
- one Docker Computer per conversation with exec, files, xterm PTY, and idle suspend/resume
- Docker Compose for web, API, sandbox service, PostgreSQL, Redis, and MinIO

Phase 5+ features remain intentionally excluded: Skill installation, Artifacts/snapshots, Kubernetes/Helm, E2B, Memory, browser use, workflow/DAG building, RAG products, and multi-agent orchestration.

## Repository layout

```text
frontend/                    Next.js web application
backend/
  cmd/api/                   API executable
  cmd/sandbox-service/       sandbox executable
  internal/                  shared backend implementation
  prompts/                   embedded Agent prompts
  migrations/                PostgreSQL migrations
deploy/                      local Docker Compose deployment
```

The repository is a monorepo, but frontend and backend have independent build contexts. The API and sandbox service are separate backend executables and run in separate containers.

## Run locally

```bash
cp deploy/.env.example deploy/.env
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up --build
```

Open <http://localhost:3000>, register, configure a provider under **Settings → Models**, and start a conversation. Replace the development master key before any non-local deployment.

## Checks

```bash
cd backend && go test ./...
pnpm --dir frontend install --frozen-lockfile
pnpm --dir frontend lint
pnpm --dir frontend build
```

The Skills tab is a visible Phase 5 placeholder only; uploads and Artifact persistence are disabled.
