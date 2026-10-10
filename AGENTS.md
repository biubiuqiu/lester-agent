# AGENTS.md

This file defines the working agreement for coding agents in the Lester repository. It applies to the entire repository. If a subdirectory later adds its own `AGENTS.md`, the nearest file takes precedence for that subtree.

## Product contract

Lester is an open-source, self-hostable AI Agent Workspace. The primary experience is conversation-first: the home screen centers a composer, and submitting the first message creates a conversation with the selected Agent (Lester by default) and starts the task in that conversation's directory inside the user's Computer. Existing conversations retain their original Agent for compatibility.

Lester is not a Workflow/DAG orchestration product. Do not add a workflow editor, node canvas, conditional branches, DAG runtime, or workflow-oriented product language unless the product direction is explicitly changed.

The current implementation intentionally does not include:

- multi-Agent orchestration UI
- Knowledge Base/RAG products
- Memory
- browser automation
- Computer snapshots, or automatic cross-provider workspace migration

First-task UI changes must preserve drafts when users leave to configure models and return to the original project. Task examples only fill the composer; sending remains explicit. Model setup distinguishes saved configuration from verified provider access: the current connection-test endpoint does not call the provider. Preserve advanced cloud-provider configuration and the separate administrator model controls. Link assistant file references only to files verified in the current conversation inventory.

Do not implement, simulate, or silently scaffold these capabilities without an explicit request. A disabled UI placeholder must remain clearly disabled and must not imply that the feature works.

Implementation phase labels are internal planning terms. Do not expose labels such as `Phase 0–4` or `Phase 5+` in the user-facing UI or README.

## Public website and documentation

- `/` is the anonymous, server-rendered project homepage; `/docs` and its authored chapters are public help. `/app` remains the authenticated conversation-first workspace. Keep public pages independent of API availability and session state; do not redirect public visitors to login or fetch workspace data there.
- Public-site routes live under `frontend/src/app/(public)/`; scope their styles so workspace, login, and settings layouts remain independent. Small client components support labeled examples and clipboard controls; do not ship design images as interactive UI.
- Keep the homepage minimal: a centered opening statement, one primary action, and a short name story, with detailed capabilities/setup in `/docs`. The name is inspired by GTA V's Lester Crest; describe resourcefulness without promising omnipotence. Text and controls stay native HTML; do not add decorative artwork or competing section headlines by default.
- Homepage examples are explicitly illustrative, never real model runs, test results, or evidence of validation. Workspace entry must use the existing login flow and never auto-send. Link the actual `biubiuqiu/lester-agent` repository/Issues; do not fabricate a hosted demo, metrics, a licence, or unavailable Memory/connector/scheduling/browser capabilities.
- Maintain `frontend/src/lib/site-docs.tsx` with README when supported behavior, environment variables, or migration requirements change. Distinguish fresh initialization from existing-volume upgrades, preserve the encryption key/data, and accurately explain model configuration versus actual provider verification.
- Verify anonymous access, documentation navigation, copy success/error, desktop/mobile layout, and the existing workspace/login entry after public-site changes.

## Internationalization contract

- Support `zh-CN`, `en`, `ja`, `ko`, `fr` and `es` across interface copy, full public help and feature guides. Select language per request from a validated `lester_locale` cookie, then weighted `Accept-Language`, with English fallback. Public pages now render per request for language selection and must still work anonymously without API access or JavaScript. Never cache a mutable global locale across users.
- Use `useT` / `T` for interface copy and matching source keys in `frontend/src/lib/i18n/messages/*.json`. Keep all six catalogues complete, use numeric placeholders for natural sentence ordering, and dynamically load only the selected foreign dictionary. Chinese source copy remains the identity language. Translate known stored interface notices at display time so they follow language changes.
- Never translate user-authored messages, editable profile/project/Agent fields, model replies, paths or file bytes. Preserve code, commands and configuration identifiers in help. Localize built-in interface labels, dates and accessible names.
- Language switching updates the provider and refreshes server content without a full reload. Preserve drafts, local File objects and terminal sessions; a locale change must not restart sockets, trigger mutations or resend a task. Keep the selector compact and keyboard accessible, including mobile layouts and long translations.
- `pnpm test` runs `scripts/check-i18n.mjs` before unit tests. Catalogue checks reject missing source keys, unequal language key sets, empty translations and changed placeholders. Browser checks cover six-language public pages and all help chapters, server HTML without JavaScript, translated auth/onboarding and preserved drafts/attachments during switching.

## Account and authentication contract

- Email registration and configured Google/GitHub OAuth create one personal workspace/default project and member role. `AUTH_REGISTRATION_ENABLED=false` closes both new signup paths; existing sign-in and explicit linking remain supported. Server validation is authoritative.
- OAuth uses fixed server-owned endpoints, PKCE S256, hashed single-use ten-minute state and a provider-scoped HttpOnly browser cookie. Link callbacks also require the original still-valid session. Identify by immutable provider subject; require a provider-verified email and never auto-merge matching email accounts. Link from an authenticated account, preserve its profile/workspaces/files, recheck the binding after user locks, and block disabled users. Never persist provider access tokens or log codes/state/mail tokens/secrets.
- Identity unlinking is serialized on the user, must preserve a password or another configured provider, revokes other sessions and rotates the current one. Password/security/admin changes revoke sessions, email tokens and pending link flows as applicable; recheck active session under the same user lock so stale authenticated requests cannot undo revocation. Preserve user → token/session lock order.
- SMTP is optional. New email signups require verification when enabled; existing accounts are grandfathered without retroactively claiming email verification. Mail tokens are hashed, expiring and single-use; links use fragments removed from the page URL and explicit submission. Mail requests do not disclose registered mailbox status. Password recovery proves mailbox ownership, verifies email and revokes all sessions; never silently grant pending users access when SMTP is disabled.
- Serve uploaded avatars only from a user-scoped authenticated API/object-store boundary. Decode bounded PNG/JPEG/GIF, reject oversized dimensions, crop/re-encode 256px PNG and strip metadata/animation. Provider avatar imports require an HTTPS host allowlist, no redirects or credentials, bounded reads/timeouts and safe failure fallback; browser avatars must never load arbitrary external URLs. User-uploaded photos are not replaced on subsequent OAuth login.
- Keep public auth options free of secrets. Unconfigured providers/mail controls must not imply working integration. Production OAuth requires HTTPS/Secure cookies and exact WEB_ORIGIN callback via gateway/Ingress. Protect browser mutations against foreign Origin, including same-site sibling hosts and opaque sandbox origins.
- User guides are account-scoped and topic-specific. Keep the welcome skippable, replay available, and feature hints nonblocking. Tutorials explain real UI without creating conversations, running models, installing Skills, or publishing. Preserve task drafts on navigation. Migration 014 opts existing users out of automatic welcome; new users have no welcome row. Test persistence, isolation, revocation, upgrade behavior, keyboard access, and mobile fit.
- Access tokens expire in two hours; refresh tokens rotate on use with a rolling thirty-day expiry and no absolute lifetime. Store only hashes in PostgreSQL and credentials in HttpOnly cookies (refresh scoped to `/api/v1/auth`). Keep session families stable for OAuth link binding, cascade revocation on security changes/logout, serialize user → token locks, and reject refresh replay outside the short concurrent-tab grace. Browser renewal is single-flight/cross-tab serialized; retry only middleware `access_required` responses whose handler never ran. Migration 015 and its rollback invalidate credentials, preserving user data.
- HTML new-tab previews live at `/preview/{conversationId}?path=...`, independent of workspace/tutorial panels. Preserve authenticated local assets and opaque `allow-scripts` iframe/CSP isolation. Avatar crop confirmation uploads only the selected square PNG; cancellation must leave the stored avatar untouched.
- Current schema requires migrations 001–015. Existing volumes upgrade explicitly; migration 013 rollback must refuse while passwordless accounts would lose their only login method. Update README, public help, Compose and Helm with auth configuration changes. Test real PostgreSQL transactions, provider/SMTP fixtures, replay/conflict/revocation and browser entry/profile flows; distinguish fixtures from live provider/mail delivery.

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
  cmd/artifact-host/          independent public static-site host
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
- `backend/internal/` contains non-exported backend implementation shared by the Go executables.
- Only Sandbox Service may mount the Docker Socket, and only when the selected provider is `docker`. ACS deployments must not mount it.
- API and Sandbox Service must remain independently buildable and deployable.
- Compose exposes the Nginx gateway and a separate Artifact Host by default: `/api` and `/api/*` proxy unchanged to API, other paths to Web. Build Web with an empty `NEXT_PUBLIC_API_URL`, keep `WEB_ORIGIN` aligned with the public gateway origin, and expose API/MinIO only via the loopback-bound debug override when requested. Kubernetes uses Ingress directly, not an additional gateway pod.
- Gateway changes must preserve unbuffered SSE, Last-Event-ID, WebSocket Upgrade, cookies, authenticated preview CSP, escaped paths, and the 25 MiB attachment allowance. Do not retry API mutations automatically or serve sandbox files directly. Trust forwarded scheme/client identity only from explicitly trusted ingress hops; the default HTTP gateway overwrites incoming forwarding headers.
- Sandbox Service management, file, command, and terminal routes are private service APIs protected by `SANDBOX_SERVICE_TOKEN`; only `/healthz` is unauthenticated. Never expose Sandbox Service through Ingress or a public Service.
- Docker Sandbox Provider owns installation of the versioned `lester-toolbox` binary into new and existing user Computers. Cloud providers may use equivalent native runtime APIs; do not reintroduce ad-hoc Python/Shell snippets for file semantics.

Do not move backend implementation back to repository-root `internal/`, or frontend code back to `apps/web/`. Keep the `frontend/` and `backend/` boundary unless an explicit architecture decision changes it.

## Runtime invariants

Preserve these behaviors when changing the implementation:

- Every user belongs to a Personal Workspace created during registration, with exactly one default project. Conversations belong to a project in the same workspace; project/conversation pins are durable. Moving a conversation must not move its Computer directory or change artifact URLs.
- Published artifacts are explicit immutable object snapshots switched through a transactional manifest. Keep management scoped by workspace and source conversation. Never publish automatically merely because HTML was generated. `deploy_html` and the UI share the same service and validation.
- Artifact Host must remain a separate executable/container and hostname from the application. Serve only published manifest members, never live sandbox paths or arbitrary object keys. Preserve opaque sandbox origins, no-store, CORS for media/modules, byte-range/HEAD support and revocation for all resources. Do not give it sandbox tokens, model encryption keys, or Docker access.
- Bundle only supported static files within the current conversation, enforce file/count/size limits, reject missing dependencies and path escapes, and retain the old deployment if bundling/uploading fails. Keep object storage behind the blob boundary.
- Context library entries are workspace-private, explicitly selected with `@`, and snapshotted into user-message metadata during send. Model history must use stored snapshots, never resolve historical IDs against live entries. Preserve reference drafts, reject inaccessible/deleted IDs, enforce count/content bounds, and protect edits with version checks. This is a user-maintained reference library, not automatic retrieval, RAG, or Memory.
- All workspace-owned reads and writes must be scoped by `workspace_id`.
- System administrators use independent `/admin` pages and `/api/v1/admin` routes guarded by a fresh database role check. Registration always creates members. Bootstrap the first administrator explicitly through an authorized database session, never by first-signup order. Disabled accounts cannot authenticate; password resets, disabling, and role changes revoke sessions.
- Shared models live in the reserved system workspace `00000000-0000-0000-0000-000000000001`, which has no user membership. Member model selection/runtime may access enabled shared deployments, but never shared connection configuration or credentials. Personal defaults take precedence; disabling shared deployments preserves history. Keep default changes transactional and workspace-serialized.
- Provider credentials must be encrypted at rest with the existing secret store and must never be returned or logged in plaintext.
- Model-provider differences must stay behind the model abstraction instead of leaking into conversation handlers.
- Do not impose a global model output-token cap. Omit optional output-limit fields when unset; only provider adapters whose protocols require a limit may supply a provider-specific fallback.
- Do not impose a fixed model/tool-loop count. A run continues until the model completes, an operation fails, or its run context is cancelled.
- User cancellation is a durable run transition (`running` → `cancelling` → `cancelled`). Propagate cancellation through model streams and foreground tool contexts, stop before any later tool or model iteration, close any persisted unresolved tool call with an explicit cancelled/unknown result, and emit `RUN_CANCELLED` exactly once instead of reporting completion or failure. Reloaded clients must recover the active run ID and status from PostgreSQL; Redis/SSE remains live delivery only.
- The selected Agent is fixed for the lifetime of a conversation. Custom Agent names, instructions and selected Skill slugs are snapshotted at conversation creation; edits or deletion affect only future conversations. Default new conversations use Lester. The Agent Designer discusses a concrete definition with the user and invokes `save_agent` after the user agrees; the right-side configuration panel can also save explicit edits.
- Agent Designer is an ordinary project conversation with the built-in `agent-designer` persona. It learns the user's intent before calling the scoped `save_agent` tool; the tool creates or updates only the Agent bound to that design conversation. Other conversations must not be offered the tool, and server-side execution must reject them even if they call it directly. Show the right-panel Agent configuration only after the tool has saved an Agent. The existing Computer tabs remain available.
- Agent-managed files are workspace-scoped immutable objects with bounded file names/counts/sizes. Snapshot references into a conversation at creation, then copy them into `agent-resources/` inside that conversation before the first model run. Never inject file contents into prompts automatically, and retain objects referenced by historical conversations when Agent files or definitions are removed.
- Registered deliverables are workspace/conversation-scoped HTML/Markdown metadata, created by `register_deliverable` after reading the actual entry during an active run. Re-registering a path preserves its ID and updates source run/digest. Join metadata with verified current inventory; never manufacture cards for missing files or equate registration/Agent summaries with validation, publication, or a content snapshot.
- Live successful file writes/edits and deliverable registration open verified files in the right-side multi-tab viewer. Initial inventory/history must not auto-open every file; suppress duplicate/replayed notifications and bound pending requests. Run-time inventory changes also cover shell-generated files. Never invent metadata for missing files or follow paths outside this conversation. Reuse stable tabs, preserve each file's preview/source choice, refresh genuine edits even with unchanged size/mtime, and keep HTML in the existing opaque sandbox/CSP boundary. Opening a preview never publishes it.
- Messages, runs, and events are durable in PostgreSQL. Redis is used for live SSE fan-out, not as the source of truth. Persist critical run state and its event/outbox intent in the same transaction. Use `eventlog.Append` as the last domain operation before commit, preserving conversation → run → workspace event lock order. Retry ordered outbox delivery with stable event IDs; browser clients must deduplicate repeats.
- The run executor remains inside API and owns active contexts, guard release, panic cleanup, and bounded shutdown. Keep HTTP/domain services separate from model/tool execution. Never imply that this is an independent Worker, durable job queue, or automatic tool replay.
- Keep one authenticated Workspace-level SSE per browser workspace for live events across conversations. Conversation navigation must load bounded durable event history, merge by numeric event ID, and resume the Workspace stream from its last persisted browser cursor so refreshes and navigation neither duplicate nor lose visible output. The conversation rail must derive latest run state from PostgreSQL on load and live events afterward.
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
- Each user maps to one logical Computer and one provider-owned workspace; Docker uses a persistent volume while ACS preserves the workspace through pause/resume.
- Treat `sandboxes.provider_ref` as an opaque, provider-generated identifier. Persist the value returned by `Provider.Create` before data-plane work, and use a PostgreSQL advisory transaction fence so multiple API replicas cannot create two Computers for one user.
- Sandbox lifecycle, command, file, and interactive terminal behavior must stay behind `sandbox.Provider`. Provider-specific SDK types, URL rules, Docker commands, and PTY behavior must not leak into conversation or HTTP services.
- Conversations are rooted at `/workspace/conversations/{conversationId}` inside that Computer; file APIs and terminal sessions must stay scoped to that directory.
- User Computers default to no network access and retain CPU, memory, and PID limits.
- Computer state must be reconciled with the sandbox provider; use should recover a stopped or missing Computer while idle suspend/resume preserves the user workspace.
- Conversation Skills must be installed under `.agent/skills/{slug}` and only installed Skills may be exposed to or loaded by the Agent runtime.
- Conversation attachments must be stored under `.agent/upload`; do not parse or inject attachment contents into model context automatically.
- Images pasted into a conversation composer are ordinary attachments: upload the browser `File` unchanged to `.agent/upload`, keep it out of browser persistence, and expose only attachment metadata/path hints to the model until it explicitly reads the file.
- File browsing and previews must stay scoped to the conversation directory. Render private HTML only through the authenticated preview endpoint in a sandboxed iframe; explicit public deployments use the isolated Artifact Host; never inject workspace HTML into the Lester application DOM or grant it same-origin, form, popup, or top-navigation privileges.
- Skill package storage must remain behind the object-store interface so MinIO can be replaced with S3 or another implementation without changing application behavior.
- Large tool results must be bounded and must tell the model when output was truncated and how to continue.
- Bound command stdout and stderr independently at the provider boundary. File reads must be streaming/ranged so a large file is not loaded in full merely to return a small line page.
- `read` content uses `%6d\t%s` (1-based line number, TAB, original text) for text files. Preserve indentation and blank lines. Page at complete line boundaries with `next_offset`; do not concatenate a head and tail and imply a contiguous range. Prefixes must never be included in edit/write input. Image files return a bounded base64 image envelope (`images[]`) that model integrations convert to native vision content parts; never inject the base64 into an ordinary text-only result.
- Keep `lester-toolbox` model-agnostic and versioned through its CLI protocol. Validate lexical and resolved paths inside the Computer, reject symbolic-link escapes, cap file operations at 25 MiB, and write through a synced same-directory temporary file followed by atomic replacement.
- `edit` must execute next to the file through `Provider.EditFile`; do not restore the API-side read/replace/write round trip. Preserve exact-string, ambiguity, replacement-count, and replace-all semantics.

- Provider HTTP requests must never follow redirects with credentials or conversation content. Terminal WebSocket frames are bounded to 1 MiB at both the API relay and Sandbox Service.
- Interactive terminals use a real PTY for both Docker and ACS, share the embedded Bash/readline initialization, and persist history only in the starting conversation's `.agent/terminal/bash_history`. Keep PTY input/output/resize behind Provider, preserve split UTF-8 characters through the WebSocket relay, and close the connection on Shell EOF. Docker closure must hang up the remote Shell, not merely detach its CLI; ephemeral PID markers include the Linux process start time to guard PID reuse. Native Tab, Escape, Ctrl+C, bracketed paste and mobile control keys must reach the Shell; provide Shift+Esc to leave keyboard focus and browser clipboard paste shortcuts. Reconnection creates a new Shell, never claim process restoration. Custom Bash-free images explicitly fall back to sh. Docker integration checks can opt in with `LESTER_TEST_DOCKER_IMAGE` pointing to a locally available Bash image; set `LESTER_TEST_DOCKER_COMPLETION=1` for the supplied runtime's Git argument completion and less checks.
- Authentication rate limits use the TCP peer by default. Only `AUTH_TRUSTED_PROXY_CIDRS` peers may supply `X-Forwarded-For`; do not restore unqualified `middleware.RealIP`. Redis errors must not bypass limits. Long authenticated requests recheck session-family/account/workspace validity every 30 seconds and close on revocation or verification failure.

## Backend conventions

- Use Go `1.26.9` and keep the module rooted at `backend/`.
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
- Keep the right panel file-centered and read-only: bounded open tabs, preview/source, download and explicit file references to the Agent, not a heavyweight editor or Checkpoint system. File cards must resolve to verified conversation files; references carry paths, not automatically injected file contents.
- The conversation deliverables collection groups current HTML/Markdown files from the shared verified inventory, excluding hidden/dependency/Agent-resource and common support files. Keep the thread collection hidden during active runs; the dedicated Computer results tab explains running, loading, empty, partial-sync and failure/cancellation states. File-type grouping does not prove completion, checks, publication, or historical versions. Preview opens rendered mode explicitly; Continue modifying sets only the file reference, preserves the user's draft, closes the mobile overlay, and never sends automatically. HTML deployment remains explicit through the existing dialog.
- Share conversation-scoped file inventory through `FileWorkspaceProvider`. Invalidate after file/tool/run events and use bounded, visibility-aware polling for bash/background writes; preserve selections and avoid refetching unchanged previews. Mark partial scans visibly and never infer deletions from incomplete listings. Observed metadata changes are not a durable complete audit trail or content diff.
- Keep responsive behavior usable on mobile.
- New-chat navigation must not create an empty server conversation. Default to `agent_slug=lester`, allow an explicit Agent selection, and create only on submit. Preserve first-message drafts on failure, prevent duplicate submits, and never auto-send through mount effects. Keep model selection and attachment upload available in the centered home composer.
- Keep reply-footer artifact cards hidden while sending, running or stopping (including recovered active runs). Show verified files only after the run settles; right-panel inventory and previews must continue updating during execution.
- Keep workspace chrome compact: the file tree takes only the space its rows need, changes are available on demand, and preview actions share one toolbar. Preserve readable typography, keyboard focus, and a visible composer on desktop and mobile. Conversation search filters titles locally without rewriting persisted titles.
- Keep the new-task content aligned to one composer column. Task examples live under the keyboard-accessible “试试一个任务” disclosure and only fill the draft; Agent options remain explicit and close with Escape. Quiet header icons retain accessible names/tooltips. Hover-only project/conversation actions must reveal on keyboard focus, remain visible on touch, and never hide run/unread status. Avoid removing errors, first-model setup, or user guides to reduce visual density.
- Deliverable cards keep pending-review status visible; file metadata, full summaries, downloads and explicit deployments expand on demand. Do not infer successful validation from registration or type discovery. Explicit rendered-preview opens collapse the directory through the shared file workspace; ordinary tree selection and background updates retain directory visibility, and closing the final tab restores it. Mobile exposes results directly and files through panel tabs or the conversation menu. Personal model settings start with the saved list when an enabled model exists, including shared models; preserve first-setup steps, drafts, advanced provider fields and administrator controls.
- Conversation view state is tab-local and keyed by user/workspace/conversation. Preserve drafts, file references/tabs/modes, expanded directories and transcript/source reading positions across navigation; never put view state in the model transcript. Bound sessionStorage (50 conversations; 100,000 draft characters), clear it on logout, and store only attachment names there: browser File objects survive client navigation in memory, not reloads. A reload must explicitly disclose missing local attachments.
- Follow streaming content only while the user is at the bottom; show a jump-to-new-content control while reading history. File updates must not steal selected tabs. Refresh source content in place and retain its scroll position. Sandboxed HTML must not gain same-origin access to restore internal page state.
- Progress labels must reflect actual events, not inferred thinking, fabricated percentages, or unverified success. Keep tool details collapsed until requested; keep elapsed timers out of live announcements. Recovery fills a prompt for explicit confirmation and must not silently replay tools.
- Do not present unavailable functionality as enabled.
- Every asynchronous page load and mutation must surface a visible error state and prevent duplicate submission while pending. Clear conversation-scoped state before loading a different conversation.
- Avoid introducing a state-management or UI framework unless the existing React structure is no longer sufficient and the dependency is justified.

### Preview regression checks

- Source previews must render plain text when the extension has no registered syntax grammar (including `.txt` and extensionless files), while highlighting is pending, or when grammar loading fails. Check that the highlight state exists before reading its fields: `highlighted?.language === language` can be true when both sides are undefined and does not guard a subsequent `highlighted.source` access.
- When changing previews, exercise a plain-text file, a supported source language, and switching between them. Preserve the visible composer and bounded scrolling with long files.
- HTML srcdoc navigation must resolve relative to the source file and open only verified conversation inventory entries. Validate messages against the current iframe window; never grant same-origin access. Separate-page previews use the focused workspace viewer, and direct preview responses must also enforce a CSP sandbox.
- Raw file-content responses are attachment byte streams with `application/octet-stream`, `nosniff`, and `no-store`; never let content sniffing execute generated HTML/SVG in the application origin.
- Distinguish errors from generated HTML inside the sandboxed preview iframe from Lester application errors. An artifact's JavaScript syntax error is not evidence of a failure in the application's preview component; report it separately and do not silently edit user artifacts during application debugging.

## Local development

### Existing Windows checkout and startup

- The established checkout on the current development machine is `G:\codespace\lester-agent`. A desktop task may start in `C:\Users\deniswen\Documents\ChatGPT\lester`; verify the Git root before running repository commands.
- As of 2026-10-08, this machine's deployment uses `http://localhost:13280/app` through the gateway and port `13281` for Artifact Host. Windows reserved the former `13180/13181` ports. Treat this as local context, not the repository default: verify `GATEWAY_PORT`, `WEB_ORIGIN`, `ARTIFACT_PORT`, and `ARTIFACT_PUBLIC_URL` in the ignored `deploy/.env` without printing secrets. For a Docker port-binding permission error, inspect `netsh interface ipv4 show excludedportrange protocol=tcp` and listening ports; choose available ports and keep their configured origins aligned.
- Preserve the existing `deploy/.env` and persistent volumes. Copy the example only for a fresh setup. Routine startup from the repository root is `docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d`; verify Docker is available, service status, the app route, and API readiness before reporting success.
- After frontend changes, use `docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --no-deps --build web` to deploy the new bundle. Reload the browser before checking the fix; a successful local build alone does not update the running container.
- PostgreSQL entrypoint migration mounts initialize a new data directory only. For an existing volume, inspect migration requirements after pulling changes and apply missing migrations explicitly using the repository's documented procedure; never delete the volume to upgrade the schema.
- Runtime status is transient: recheck Docker and HTTP readiness on each startup request rather than assuming the previous session's containers are still running.

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
pnpm test
pnpm lint
pnpm build
pnpm exec playwright install chromium
pnpm test:e2e
```

Equivalent root commands are available through `make test` and `make web-check`.

Compose starts the application at `http://localhost:13000` and Artifact Host at `http://127.0.0.1:13181`; change both `GATEWAY_PORT` and `WEB_ORIGIN` in `deploy/.env` to use another port. `make dev-debug` additionally exposes API and MinIO on loopback only. Run `make gateway-check` after proxy/deployment changes; its isolated fixture stack verifies route/header/preview preservation, upload limits, unbuffered SSE and bidirectional WebSocket. If it fails, clean up with `docker compose -p lester-gateway-test -f deploy/gateway/compose.test.yaml down`. No application credentials or data volumes are used by these tests.

Validate the Helm chart with non-production test values:

```bash
helm lint deploy/helm/lester \
  --set secrets.existingSecret=lester-test-secrets
```

For `sandbox.provider=docker`, Helm keeps `sandbox-service` at one replica and requires a dedicated Docker worker with `/var/run/docker.sock`; user Computers and volumes are node-local. For `sandbox.provider=acs`, the service is stateless and may have multiple replicas, does not mount the Docker Socket, and uses the configured E2B-compatible Sandbox Manager endpoint. Native ACS protocol is the production default; Private protocol is for single-domain/internal or test setups.

## Change discipline

- GitHub CI runs on main pushes, PRs and manual dispatch; weekly runs scan dependencies. Keep path selection in `.github/scripts/ci_changes.py` conservative: unavailable comparisons, unknown paths and CI changes run all checks; include both sides of renames and deletions. Preserve real PostgreSQL integration tests, dependency security failures and the stable `CI result` aggregate check. Documentation-only changes may skip unrelated jobs. Validate workflow edits with actionlint and the selection regression tests; do not suppress real failures or introduce live model/OAuth/SMTP credentials into CI.

- Browser regressions exercise the production standalone bundle with fixed REST and native HTTP SSE fixtures on desktop/mobile Chromium. Keep task creation explicit, check draft recovery and private preview isolation, reject unexpected API calls, and do not hide failures with retries. CI retains failure screenshots/traces; these fixtures do not validate live model/OAuth/SMTP integration.

- Make the smallest coherent change that satisfies the request.
- Preserve unrelated user changes in a dirty worktree.
- Do not expand the product scope while fixing or refactoring existing behavior.
- Keep Docker build contexts limited to `frontend/` and `backend/`.
- Keep `Dockerfile.sandbox-runtime` compatible with ACS Agent Runtime: `/bin/bash`, `cp`, `mv`, and `mkdir` are mandatory; preserve the common coding utilities and static `lester-toolbox` unless the runtime contract changes.
- When changing service paths, ports, environment variables, or startup commands, update Dockerfiles, `deploy/docker-compose.yaml`, `Makefile`, CI, and documentation together.
- When changing user-visible capabilities or setup steps, update `README.md`.
- When changing architecture boundaries, invariants, conventions, or validation commands, update this `AGENTS.md` in the same change.
- Never commit real credentials, generated secrets, local `.env` files, build output, or dependency directories.
- After completing and validating a requested code change in this repository, commit the task-scoped changes and push to the current branch's configured upstream without requiring another reminder, unless the user explicitly asks otherwise. Preserve unrelated work, never force-push, and report the commit ID and push result. If validation or pushing is blocked, explain the blocker rather than claiming completion.

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
