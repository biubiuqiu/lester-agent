# AGENTS.md

This file defines the working agreement for coding agents in the Lester repository. It applies to the entire repository. If a subdirectory later adds its own `AGENTS.md`, the nearest file takes precedence for that subtree.

## Product contract

Lester is an open-source, self-hostable AI Agent Workspace. The primary experience is conversation-first: a user starts a conversation, selects an Agent and model, and the Agent works with a Computer dedicated to that conversation.

Lester is not a Workflow/DAG orchestration product. Do not add a workflow editor, node canvas, conditional branches, DAG runtime, or workflow-oriented product language unless the product direction is explicitly changed.

The current implementation intentionally does not include:

- multi-Agent orchestration UI
- Knowledge Base/RAG products
- Memory
- browser automation
- Skill installation or a Skill marketplace
- uploads, Artifact persistence, or Computer snapshots
- Kubernetes, Helm, or E2B sandbox providers

Do not implement, simulate, or silently scaffold these capabilities without an explicit request. A disabled UI placeholder must remain clearly disabled and must not imply that the feature works.

Implementation phase labels are internal planning terms. Do not expose labels such as `Phase 0–4` or `Phase 5+` in the user-facing UI or README.

## Terminology

`Workflow` has two possible meanings in this repository:

- Product Workflow/DAG: intentionally unsupported.
- GitHub Actions workflow under `.github/workflows/`: repository CI only.

Never describe GitHub Actions CI as a Lester product capability.

## Repository boundaries

```text
frontend/                     Next.js frontend
backend/
  cmd/api/                    API executable
  cmd/sandbox-service/        Sandbox Service executable
  internal/                   backend implementation
  prompts/                    embedded Agent prompts
  migrations/                 PostgreSQL migrations
deploy/                       Docker Compose and environment templates
```

This is a Monorepo with separate frontend and backend build contexts.

- `frontend/` contains all browser-facing code. It communicates through the API and must not access PostgreSQL, Redis, or Docker directly.
- `backend/cmd/api/` is a thin composition root for authentication, workspaces, model configuration, conversations, the Agent runtime, and API transport.
- `backend/cmd/sandbox-service/` is a separate executable and container. It owns Computer lifecycle, command execution, files, and terminal sessions.
- `backend/internal/` contains non-exported backend implementation shared by the two Go executables.
- Only Sandbox Service may mount the Docker Socket.
- API and Sandbox Service must remain independently buildable and deployable.

Do not move backend implementation back to repository-root `internal/`, or frontend code back to `apps/web/`. Keep the `frontend/` and `backend/` boundary unless an explicit architecture decision changes it.

## Runtime invariants

Preserve these behaviors when changing the implementation:

- Every user belongs to a Personal Workspace created during registration.
- All workspace-owned reads and writes must be scoped by `workspace_id`.
- Provider credentials must be encrypted at rest with the existing secret store and must never be returned or logged in plaintext.
- Model-provider differences must stay behind the model abstraction instead of leaking into conversation handlers.
- The selected persona is fixed for the lifetime of a conversation.
- Messages, runs, and events are durable in PostgreSQL. Redis is used for live SSE fan-out, not as the source of truth.
- Tool-call fragments must be assembled before execution, and tool results must remain associated with the correct call ID.
- Each conversation maps to one logical Computer and one persistent workspace volume.
- Conversation Computers default to no network access and retain CPU, memory, and PID limits.
- Idle suspend/resume must preserve the conversation workspace.

## Backend conventions

- Use Go `1.24` and keep the module rooted at `backend/`.
- Keep `cmd/*/main.go` focused on dependency wiring and process lifecycle.
- Put application behavior in the appropriate `backend/internal/*` package.
- Keep Agent Prompt text in `backend/prompts/`; do not scatter system prompts across handlers.
- Format all Go files with `gofmt`.
- Wrap errors with useful operation context, but never include credentials or sensitive payloads.
- Add forward and rollback SQL when changing the database schema.
- Reuse existing interfaces before adding provider-specific branching to higher layers.

## Frontend conventions

- Use TypeScript and the existing Next.js App Router structure.
- Keep API access in `frontend/src/lib/api.ts` or a focused module under `frontend/src/lib/`.
- Preserve the conversation-first interaction model and the three-panel desktop layout unless a product change explicitly replaces it.
- Keep responsive behavior usable on mobile.
- Do not present unavailable functionality as enabled.
- Avoid introducing a state-management or UI framework unless the existing React structure is no longer sufficient and the dependency is justified.

## Local development

Start the full stack:

```bash
cp deploy/.env.example deploy/.env
docker compose --env-file deploy/.env \
  -f deploy/docker-compose.yaml \
  up --build
```

Run backend checks:

```bash
cd backend
go mod tidy
test -z "$(gofmt -l cmd internal prompts)"
go test ./...
```

Run frontend checks:

```bash
cd frontend
pnpm install --frozen-lockfile
pnpm lint
pnpm build
```

Equivalent root commands are available through `make test` and `make web-check`.

## Change discipline

- Make the smallest coherent change that satisfies the request.
- Preserve unrelated user changes in a dirty worktree.
- Do not expand the product scope while fixing or refactoring existing behavior.
- Keep Docker build contexts limited to `frontend/` and `backend/`.
- When changing service paths, ports, environment variables, or startup commands, update Dockerfiles, `deploy/docker-compose.yaml`, `Makefile`, CI, and documentation together.
- When changing user-visible capabilities or setup steps, update `README.md`.
- When changing architecture boundaries, invariants, conventions, or validation commands, update this `AGENTS.md` in the same change.
- Never commit real credentials, generated secrets, local `.env` files, build output, or dependency directories.

## Definition of done

Before handing off a code change:

1. Confirm it stays within the requested product scope.
2. Run relevant focused tests while developing.
3. Run `go test ./...` from `backend/` for backend changes.
4. Run `pnpm lint` and `pnpm build` from `frontend/` for frontend changes.
5. Validate Docker Compose paths when deployment files or repository layout changes.
6. Update README and AGENTS guidance when the change makes either document inaccurate.
7. Report what changed, what was verified, and any remaining limitation without overstating support.
