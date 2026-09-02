# AGENTS.md

This file defines the working agreement for coding agents in the Lester repository. It applies to the entire repository. If a subdirectory later adds its own `AGENTS.md`, the nearest file takes precedence for that subtree.

## Product contract

Lester is an open-source, self-hostable AI Agent Workspace. The primary experience is conversation-first: a user starts a conversation, selects an Agent and model, and the Agent works in that conversation's directory inside the user's Computer.

Lester is not a Workflow/DAG orchestration product. Do not add a workflow editor, node canvas, conditional branches, DAG runtime, or workflow-oriented product language unless the product direction is explicitly changed.

The current implementation intentionally does not include:

- multi-Agent orchestration UI
- Knowledge Base/RAG products
- Memory
- browser automation
- Artifact persistence or Computer snapshots
- Kubernetes-native or E2B sandbox providers (the Helm deployment still uses DockerSandboxProvider)

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
  cmd/lester-toolbox/         static filesystem helper injected into user Computers
  internal/                   backend implementation
    agenttool/                Agent tool registry and individual handlers
    toolboxfs/                bounded and scoped filesystem helper implementation
    model/runtime/            Provider-neutral model contracts
    model/integration/        Model provider registry and adapters
  prompts/                    embedded Agent prompts
  migrations/                 PostgreSQL migrations
deploy/                       Docker Compose, environment templates, and Helm chart
```

This is a Monorepo with separate frontend and backend build contexts.

- `frontend/` contains all browser-facing code. It communicates through the API and must not access PostgreSQL, Redis, or Docker directly.
- `backend/cmd/api/` is a thin composition root for authentication, workspaces, model configuration, conversations, the Agent runtime, and API transport.
- `backend/cmd/sandbox-service/` is a separate executable and container. It owns Computer lifecycle, command execution, files, and terminal sessions.
- `backend/internal/` contains non-exported backend implementation shared by the two Go executables.
- Only Sandbox Service may mount the Docker Socket.
- API and Sandbox Service must remain independently buildable and deployable.
- Sandbox Service management, file, command, and terminal routes are private service APIs protected by `SANDBOX_SERVICE_TOKEN`; only `/healthz` is unauthenticated. Never expose Sandbox Service through Ingress or a public Service.
- Sandbox Service owns installation of the versioned `lester-toolbox` binary into new and existing user Computers. File providers must use this helper or an equivalent native provider API; do not reintroduce ad-hoc Python/Shell snippets for file operations.

Do not move backend implementation back to repository-root `internal/`, or frontend code back to `apps/web/`. Keep the `frontend/` and `backend/` boundary unless an explicit architecture decision changes it.

## Runtime invariants

Preserve these behaviors when changing the implementation:

- Every user belongs to a Personal Workspace created during registration.
- All workspace-owned reads and writes must be scoped by `workspace_id`.
- Provider credentials must be encrypted at rest with the existing secret store and must never be returned or logged in plaintext.
- Model-provider differences must stay behind the model abstraction instead of leaking into conversation handlers.
- Do not impose a global model output-token cap. Omit optional output-limit fields when unset; only provider adapters whose protocols require a limit may supply a provider-specific fallback.
- Do not impose a fixed model/tool-loop count. A run continues until the model completes, an operation fails, or its run context is cancelled.
- The selected persona is fixed for the lifetime of a conversation.
- Messages, runs, and events are durable in PostgreSQL. Redis is used for live SSE fan-out, not as the source of truth.
- Persist every complete model-visible assistant message (including intermediate text and tool calls) and every tool result before continuing execution. Restore `tool_calls` and `tool_call_id`, not only role/content. Events are not the transcript source of truth.
- Message order is `messages.seq` within a conversation, allocated by the database trigger; never restore context by timestamps or random UUIDs. New messages carry `run_id`; runs link their input message and snapshot system/tools/model settings plus the initial history cursor.
- Only one run may execute in a conversation at once. Use the PostgreSQL session advisory guard, return HTTP 409 for overlapping sends before storing their message, and release the guard on completion. Database connections must be direct or session-pooled, not transaction-pooled.
- After acquiring a released guard, mark abandoned running records failed and fill missing tool results with explicit interrupted/unknown outcomes. Never automatically re-execute tools after a crash. Partial model streams are stored as incomplete audit records and excluded from model history.
- Keep the default conversation GET response compatible with chat rendering (user/final assistant only); `include_internal=true` returns the full ordered transcript. Do not mistake display filtering for loss of stored context.
- Tool-call fragments must be assembled before execution, and tool results must remain associated with the correct call ID.
- Tool context is a request-time projection, not a storage policy. Keep the complete transcript in the execution loop and call `toolcontext.Build` before every model iteration; never persist projected references or append to pruned history.
- Count individual ToolExchanges (call plus result), default to the latest 10 FULL, and preserve the entire latest unobserved batch. Downgrade/evict pairs atomically, including large call arguments, while keeping original assistant prose and valid mixed batches.
- Pin unresolved tool failures, including nonzero bash exit codes; only later verified matching successes release pins. Use a strict allowlist for consumed low-value output. Keep load_skill/unknown result semantics conservative, and never replay side effects to reconstruct omitted output.
- Reference metadata must be historical, bounded and factual. Do not fabricate edit line ranges, treat a background launch as test success, or claim character savings are exact token counts. No summary/Memory/RAG or total context budget is implied by tool-context projection.
- Each user maps to one logical Computer and one persistent workspace volume.
- Conversations are rooted at `/workspace/conversations/{conversationId}` inside that Computer; file APIs and terminal sessions must stay scoped to that directory.
- User Computers default to no network access and retain CPU, memory, and PID limits.
- Computer state must be reconciled with the sandbox provider; use should recover a stopped or missing Computer while idle suspend/resume preserves the user workspace.
- Conversation Skills must be installed under `.agent/skills/{slug}` and only installed Skills may be exposed to or loaded by the Agent runtime.
- Conversation attachments must be stored under `.agent/upload`; do not parse or inject attachment contents into model context automatically.
- File browsing and previews must stay scoped to the conversation directory. Render HTML only through the authenticated preview endpoint in a sandboxed iframe; never inject workspace HTML into the Lester application DOM or grant it same-origin, form, popup, or top-navigation privileges.
- Skill package storage must remain behind the object-store interface so MinIO can be replaced with S3 or another implementation without changing application behavior.
- Large tool results must be bounded and must tell the model when output was truncated and how to continue.
- Bound command stdout and stderr independently at the provider boundary. File reads must be streaming/ranged so a large file is not loaded in full merely to return a small line page.
- `read` content uses `%6d\t%s` (1-based line number, TAB, original text). Preserve indentation and blank lines. Page at complete line boundaries with `next_offset`; do not concatenate a head and tail and imply a contiguous range. Prefixes must never be included in edit/write input.
- Keep `lester-toolbox` model-agnostic and versioned through its CLI protocol. Validate lexical and resolved paths inside the Computer, reject symbolic-link escapes, cap file operations at 25 MiB, and write through a synced same-directory temporary file followed by atomic replacement.
- `edit` must execute next to the file through `Provider.EditFile`; do not restore the API-side read/replace/write round trip. Preserve exact-string, ambiguity, replacement-count, and replace-all semantics.

## Backend conventions

- Use Go `1.24` and keep the module rooted at `backend/`.
- Keep `cmd/*/main.go` focused on dependency wiring and process lifecycle.
- Put application behavior in the appropriate `backend/internal/*` package.
- Keep the HTTP transport on standard `net/http` with `chi`. Do not introduce Gin or another HTTP framework unless a measured requirement cannot be met by the current stack.
- Keep HTTP handlers thin: decode and validate transport input, resolve request identity, call an application service, and encode the response. Business rules belong in services or focused domain packages.
- Organize routes by domain as the API grows. The root router owns global middleware and mounting; feature packages own their handlers and must not depend on the concrete router implementation.
- Keep Agent Prompt text in `backend/prompts/`; do not scatter system prompts across handlers.
- Format all Go files with `gofmt`.
- Wrap errors with useful operation context, but never include credentials or sensitive payloads.
- Add forward and rollback SQL when changing the database schema.
- Reuse existing interfaces before adding provider-specific branching to higher layers.
- Add Agent tools as independent `agenttool.Handler` implementations and register them in the tool registry; do not add tool-name switches to the conversation service.
- Add model providers through `model/integration.Provider`; provider authentication, endpoints, and protocol adaptation must not branch inside `model.Store` or conversation code.

## Frontend conventions

- Use TypeScript and the existing Next.js App Router structure.
- Keep API access in `frontend/src/lib/api.ts` or a focused module under `frontend/src/lib/`.
- Preserve the conversation-first interaction model and the three-panel desktop layout unless a product change explicitly replaces it.
- Keep the conversation rail fixed-width but collapsible, and keep the desktop Computer panel user-resizable with bounded, persisted sizing.
- Grid and flex panes that own scroll containers must use bounded viewport tracks and `min-height: 0`; long file, terminal, or transcript content must scroll inside its pane and must never push the composer below the viewport.
- Keep responsive behavior usable on mobile.
- Do not present unavailable functionality as enabled.
- Every asynchronous page load and mutation must surface a visible error state and prevent duplicate submission while pending. Clear conversation-scoped state before loading a different conversation.
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

Validate the Helm chart with non-production test values:

```bash
helm lint deploy/helm/lester \
  --set secrets.existingSecret=lester-test-secrets
```

The current Helm deployment requires a dedicated Kubernetes worker with Docker Engine and `/var/run/docker.sock`. Keep `sandbox-service` at one replica and pin it with `sandbox.nodeSelector`; user Computers and Docker volumes are node-local to that worker.

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
6. Run `helm lint` and render the chart when Helm deployment files change.
7. Update README and AGENTS guidance when the change makes either document inaccurate.
8. Report what changed, what was verified, and any remaining limitation without overstating support.
