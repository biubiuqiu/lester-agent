# AGENTS.md

This file defines the working agreement for coding agents in the Lester repository. It applies to the entire repository. If a subdirectory later adds its own `AGENTS.md`, the nearest file takes precedence for that subtree.

## Product contract

Lester is an open-source, self-hostable AI Agent Workspace. The primary experience is conversation-first: the home screen centers a composer, and submitting the first message creates a conversation with the selected Agent (Lester by default) and starts the task in that conversation's directory inside the user's Computer. Existing conversations retain their original Agent for compatibility.

Lester is not a Workflow/DAG orchestration product. Do not add a workflow editor, node canvas, conditional branches, DAG runtime, or workflow-oriented product language unless the product direction is explicitly changed.

The current implementation intentionally does not include:

- multi-Agent orchestration UI
- Knowledge Base/RAG products
- Memory
- dedicated browser-control tools, browser UI, or automatic browser-session management
- Computer snapshots, or automatic cross-provider workspace migration

First-task UI changes must preserve drafts when users leave to configure models and return to the original project. Task examples only fill the composer; sending remains explicit. Model setup distinguishes saved configuration from verified provider access: the current connection-test endpoint does not call the provider. Preserve advanced cloud-provider configuration and the separate administrator model controls. Link assistant file references only to files verified in the current conversation inventory.

Do not implement, simulate, or silently scaffold these capabilities without an explicit request. A disabled UI placeholder must remain clearly disabled and must not imply that the feature works.

Implementation phase labels are internal planning terms. Do not expose labels such as `Phase 0–4` or `Phase 5+` in the user-facing UI or README.

## Public website and documentation

- `/` is the anonymous, server-rendered project homepage; `/docs` and its authored chapters are public help. `/app` remains the authenticated conversation-first workspace. Keep public pages independent of API availability and session state; do not redirect public visitors to login or fetch workspace data there.
- Public-site routes live under `frontend/src/app/(public)/`; scope their styles so workspace, login, and settings layouts remain independent. Small client components support labeled examples and clipboard controls; do not ship design images as interactive UI.
- Keep the homepage minimal: a centered opening statement, one primary action, and a short name story, with detailed capabilities/setup in `/docs`. The name is inspired by GTA V's Lester Crest; describe resourcefulness without promising omnipotence. Text and controls stay native HTML; do not add decorative artwork or competing section headlines by default.
- Homepage examples are explicitly illustrative, never real model runs, test results, or evidence of validation. Workspace entry must use the existing login flow and never auto-send. Link the actual `biubiuqiu/lester-agent` repository/Issues; do not fabricate a hosted demo, metrics, a licence, or unavailable Memory/native connector/dedicated browser UI capabilities.
- Homepage motion is brief and tied to reading or choices, with no perpetual loops or invented progress. The compact output sketch uses native radio controls and CSS to switch illustrative web/document/code previews without JavaScript; examples never run or fetch a model. Respect reduced motion and preserve server-rendered copy, anonymous navigation and all six languages.
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
- Current schema requires migrations 001–027. Existing volumes upgrade explicitly; migration 013 rollback must refuse while passwordless accounts would lose their only login method. Update README, public help, Compose and Helm with auth configuration changes. Test real PostgreSQL transactions, provider/SMTP fixtures, replay/conflict/revocation and browser entry/profile flows; distinguish fixtures from live provider/mail delivery.

## File review and project rules

- Agent write/edit tools record trusted before/after snapshots and a durable intent before mutation. Private capture reads never grant model-visible FileState access. Keep at most 512 KiB per text side and 64 MiB per conversation; unretained or unconfirmed operations must not fabricate diffs.
- File restore is explicit, conversation-guarded and limited to confirmed retained modifications of existing text files. Native conditional writes bind the current AFTER digest to the retained BEFORE bytes. Active runs, later edits, new-file deletion and unretained snapshots are unsupported. An unknown native receipt never grants replay or inferred success from matching bytes. Record rollback identities durably and scope receipts to actor/workspace/conversation. Restore at most 20 recent unconfirmed mutation/restore facts into complete-request budgets after compaction; confirmed historical digests never imply current file contents or grant FileState permissions.
- Project settings rules have a monotonic version and SHA-256. Snapshot source/name/content/version during acceptance through the shared input helper, including queued tasks. Restore only the accepted snapshot into model context; later moves or edits affect future accepted tasks. Rules cannot override platform/tool/Plan permissions or latest direct user requirements. They are explicitly authored guidance, not Memory/RAG.
- Background file arrivals can record tabs while a native modal is open, but must not cover the active settings/review dialog.

## Running-task inputs

- Adjustments are explicit text-only inputs, applied as real user messages at complete tool-batch boundaries in the same run. They cannot change Plan mode or grant attachment/@ access. Check pending adjustments under the completion lock; never lose an accepted final-turn adjustment or replay tools.
- Next-task queue entries snapshot the accepted actor, model, mode and materials. Dispatch under the existing conversation session guard, recheck actor membership, and preserve acceptance order. Failure, stopping or interrupted execution holds future inputs for explicit action. Draft typing/navigation never queues a task. During execution, explicit Enter/Send accepts a next-task input by default; the separate adjustment action inserts text at safe tool-batch boundaries, preserving Stop and Plan permissions.
- Stable input IDs and exact request digests bind scoped receipts. Uncertain acceptance checks the original receipt without automatic POST replay. Revision checks keep stale reads from reverting confirmed input actions; logout clears account-scoped receipt tracking.

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
- The unified `@` picker supports context, conversation and Skill references (eight combined), stores only typed IDs/titles in drafts, and never installs or sends on selection/preview. Conversation references snapshot authoritative source title/project and message cutoff. `read_conversation` derives workspace/run authority from the trusted environment and the current running task's input message only; historical/transitive references grant no access. Scope every read, respect the saved cutoff, omit incomplete/tool traffic and files, and preserve Unicode content with bounded sequence/character pagination. Referenced text is data, not instructions.
- Explicitly selected Skills install only for the current input before model execution; preserve existing installed copies, report actual installation events/failures, and use `load_skill` for instructions. Old selections must not reinstall Skills in later tasks. The built-in Conversation Reader guides scoped reading, decision summaries and source citations; it does not introduce Memory or automatic replay/publication. Keep `read_conversation` on the conservative tool-context policy unless explicit projection tests justify a policy change.
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
- Task permission mode (standard/auto/full) is captured at acceptance, hashed into submission identity, persisted on runs and queued inputs, and independent of Plan. New clients submit through dedicated permission-aware routes and never fall back to an old API that ignores profiles. Registry authorization checks Plan first, then server-side operation permissions from actual durable calls. Never trust model text, summaries or permission-choice UI as an approval receipt. Default requires mutation/command consent; auto only allows ordinary file operations and a small exact command allowlist. Unknown commands and sensitive paths require per-operation review; full stays inside existing platform/Computer bounds. Allow-once receipts are consumed before execution; run grants only cover ordinary writes/edits for the same active run. Scope response reads/decisions by authenticated workspace/conversation and bind exact digests. Reconcile abandoned owners, revoke pending approval on stop/adjustment, preserve drafts on uncertain responses, and never replay tools to recover lost receipts. Apply migration 025 and rebuild API/Web together for existing databases.
- The composer starts new and queued tasks in execute mode; API clients can still explicitly choose Plan. For complex work, enter_plan_mode reduces this active run to read-only from an exact durable call, preserving operation permissions. Persist and emit EXECUTION_MODE_CHANGED atomically; repeated calls cannot revoke an already approved plan again. Every later call in the same batch checks the fresh mode. Server-side authorization checks fresh run/mode state before every tool, normalizing aliases and denying unknown/mutating tools in Plan. Plan exposes read-only directory listing instead of Bash. Structured question answers are data, never approvals. Proposed plans bind the durable call's exact parameters and digest to this run; explicit approval enables execution only for that version and restores the approved scope from platform records. Do not ask for redundant Plan approval in execute mode. Cancel/failure/completion revokes pending interactions; refresh/response checks the session guard when the executor is absent and marks abandoned runs interrupted without replay. Keep exact response retries idempotent and drafts scoped to user/workspace/conversation. User-initiated uploads and Skill installation remain separate platform actions.
- Keep one authenticated Workspace-level SSE per browser workspace for live events across conversations. Conversation navigation must load bounded durable event history, merge by numeric event ID, and resume the Workspace stream from its last persisted browser cursor so refreshes and navigation neither duplicate nor lose visible output. The conversation rail must derive latest run state from PostgreSQL on load and live events afterward.
- Persist every complete model-visible assistant message (including intermediate text and tool calls) and every tool result before continuing execution. Restore `tool_calls` and `tool_call_id`, not only role/content. Events are not the transcript source of truth.
- Message order is `messages.seq` within a conversation, allocated by the database trigger; never restore context by timestamps or random UUIDs. New messages carry `run_id`; runs link their input message and snapshot system/tools/model settings plus the initial history cursor.
- Only one run may execute in a conversation at once. Use the PostgreSQL session advisory guard, return HTTP 409 for overlapping sends before storing their message, and release the guard on completion. Database connections must be direct or session-pooled, not transaction-pooled.
- Sends carry a stable, user-scoped submission UUID and a digest of the exact request. A retry returns the original run even after it finishes; a changed payload conflicts. Capture the selected model deployment when accepting the run. Reject model switches while running/cancelling. Once a send/stop is accepted, a failed GET is a sync failure, never a failed mutation or permission to replay it. Reuse successful attachment receipts on explicit retry.
- Normalize usage per model request, replacing cumulative stream snapshots rather than summing them. Include cached input exactly once; keep actual, estimated and missing usage distinct. Persist request budget/usage independently of SSE. Budget estimates cover system, schemas, messages, call arguments and native images; never count image base64 as ordinary text. An unknown provider window remains unknown; 64k is only the configurable local default policy. Output reserve must not become a global API output cap.
- Persistent current-conversation checkpoints are separate from tool projection. Keep complete audit messages and local message IDs/seq out of provider wire fields. Preserve the latest user input, latest assistant, entire unobserved batches, unresolved failures and effective loaded Skill bodies. Generate summaries without tools, validate their shape and complete-request reduction before an atomic checkpoint/event commit, and reload checkpoints only in the scoped active run. Rebuild FileState from literal visible reads; summaries and historical references never grant edit permissions. Restore cached task facts from platform records and label their freshness. Limit compaction attempts to two per turn and context rejection recovery to one without partial output; open the circuit on compaction failure, never truncate protected input or replay tools.
- After acquiring a released guard, mark abandoned running records failed and fill missing tool results with explicit interrupted/unknown outcomes. Never automatically re-execute tools after a crash. Partial model streams are stored as incomplete audit records and excluded from model history.
- Keep the default conversation GET response compatible with chat rendering (user/final assistant only); `include_internal=true` returns the full ordered transcript. Do not mistake display filtering for loss of stored context.
- Tool-call fragments must be assembled before execution, and tool results must remain associated with the correct call ID.
- Tool context is a request-time projection, not a storage policy. Keep the complete transcript in the execution loop and call `toolcontext.Build` before every model iteration; never persist projected references or append to pruned history.
- Count individual ToolExchanges (call plus result), default to the latest 10 FULL, and preserve the entire latest unobserved batch. Downgrade/evict pairs atomically, including large call arguments, while keeping original assistant prose and valid mixed batches.
- Pin unresolved tool failures, including nonzero bash exit codes; only later verified matching successes release pins. Use a strict allowlist for consumed low-value output. Keep load_skill/unknown result semantics conservative, and never replay side effects to reconstruct omitted output.
- Reference metadata must be historical, bounded and factual. Do not fabricate edit line ranges, treat a background launch as test success, or claim character savings are exact token counts. No summary/Memory/RAG or total context budget is implied by tool-context projection.
- read_tool_result pages original stored tool messages only within the current conversation and an active trusted run. It must not read live files, rerun commands, grant transitive conversation access, manufacture data shortened before storage, or convert historical read bodies into fresh FileState receipts. Keep native image envelopes out of ordinary base64 text pages; reject ambiguous historical call IDs instead of picking an unrelated result.
- Each user maps to one logical Computer and one provider-owned workspace; Docker uses a persistent volume while ACS preserves the workspace through pause/resume.
- Treat `sandboxes.provider_ref` as an opaque, provider-generated identifier. Persist the value returned by `Provider.Create` before data-plane work, and use a PostgreSQL advisory transaction fence so multiple API replicas cannot create two Computers for one user.
- Sandbox lifecycle, command, file, and interactive terminal behavior must stay behind `sandbox.Provider`. Provider-specific SDK types, URL rules, Docker commands, and PTY behavior must not leak into conversation or HTTP services.
- Conversations are rooted at `/workspace/conversations/{conversationId}` inside that Computer; file APIs and terminal sessions must stay scoped to that directory.
- User Computers default to no network access and retain CPU, memory, and PID limits.
- Browser tasks use ordinary `bash` execution of headless Node/Python Playwright scripts in the supplied Computer image; no browser Skill is required. Keep this guidance brief in the base prompt, check actual availability in older/custom images, reuse installed browsers, scope screenshots/downloads to the current conversation, verify actions from real page results and close browsers. Preserve deployment network restrictions; this does not add dedicated browser tools, UI, or managed browser sessions.
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
- Build FileState from the messages actually visible after projection on every turn. Full overwrite/replace-all requires an untruncated full read; a precise edit requires its observed range. Missing string fields differ from explicit empty strings. Bind reads to SHA-256 and use native conditional writes; do not silently fall back to unchecked writes. Native helper writers share a lock and atomic replacement, but arbitrary Bash writes do not participate in that lock. File protocol 2 and task protocol 1 require matching helper/template versions.
- Managed commands record their platform UUID and private ownership handle before launch, then persist the returned supervisor PID/Linux start time. Never expose or accept PID/token controls in public task APIs. Retain raw combined output up to 64 MiB, publish terminal state only after flushing the log, page by stable byte offsets and disclose omissions. Keep ordinary nonzero exits and partial cancelled output as observations. Read logs with task_output; stop a background task independently with stop_task or the explicit UI action. A lost Start receipt remains unknown and must never trigger an automatic relaunch or an unverified signal.
- Reconcile command tasks independently of Agent completion and after API restart. Terminal task states must not regress when an older response arrives; keep DB revisions on public snapshots. Active Agent runs and starting/running/stopping/unknown command tasks prevent idle suspension. New Docker Computers use --init; never replace existing Computers or volumes merely to obtain a process reaper. This native task supervisor does not turn the Agent executor into a remote worker or conversation queue.
- Install Docker helpers with same-directory atomic replacement so a restarted Sandbox Service can inspect and stop supervisors still executing the previous inode. Verify process identity across the deleted-inode suffix; never overwrite a running executable directly.

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
- Composer context pickers and language menus use a body-level `FloatingPopover` with viewport-bounded positioning; keep composer scrolling intact rather than disabling overflow. Reposition on ancestor scrolling, anchor resize and visual viewport changes. Keep keyboard-selected entries visible, Escape/outside dismissal available and context selection separate from task submission. Language menus use local SVG flags plus native names and a selected state; flags never replace accessible names. Reuse the SVG Lester brand and favicon.
- Conversation options use the same body-level popover on desktop and mobile, independent of layout breakpoints. Opening it must not change header/panel geometry; keep long model names bounded, focus after positioning, restore the trigger on Escape and close on outside clicks or actions. Keep native tutorial/file dialogs functional. Workspace home entrance and example animations preserve drafts, attachments and explicit submission; reduced motion disables them, including feedback already in progress.
- New-chat navigation must not create an empty server conversation. Default to `agent_slug=lester`, allow an explicit Agent selection, and create only on submit. Preserve first-message drafts on failure, prevent duplicate submits, and never auto-send through mount effects. Keep model selection and attachment upload available in the centered home composer.
- First submission moves the existing home composer to the bottom, then bridges route loading with an inert, aria-hidden visual copy and restores focus in the actual conversation input. Failed creation returns the same draft to the center. Respect reduced motion; cancel visual copies on unrelated navigation, resize, unmount or timeout. Animation never delays, retries or determines API mutations.
- Keep reply-footer artifact cards hidden while sending, running or stopping (including recovered active runs). Show verified files only after the run settles; right-panel inventory and previews must continue updating during execution.
- Settings use a compact, horizontally scrollable navigation row on mobile; keep the active section visible after resize/locale changes and retain named guide/back actions. Shared audit refinements live in `frontend/src/app/interface-polish.css`. Skill catalogue loading, failed reads and an empty successful response are distinct states; retry only the read, never infer an empty catalogue from failure.
- Shared workspace control recipes live in `frontend/src/app/workspace-controls.css`: use the same icon size/stroke, button geometry, focus ring and state treatments across composer/header/file chrome. Keep styles scoped away from rendered documents, previews and terminals. File tabs retain accessible names and keyboard navigation; inactive close controls reveal on hover/focus and stay visible on touch.
- Keep workspace chrome compact: the file tree takes only the space its rows need, changes are available on demand, and preview actions share one toolbar. Preserve readable typography, keyboard focus, and a visible composer on desktop and mobile. Conversation search filters titles locally without rewriting persisted titles.
- Keep the new-task content aligned to one composer column. Empty input shows three short task examples; drafts, references and attachments fold them under the keyboard-accessible “试试一个任务” disclosure. Examples only fill the draft and close the disclosure; Agent options remain explicit and close with Escape. Keep the input readable throughout entrance motion. Quiet header icons retain accessible names/tooltips. Hover-only project/conversation actions must reveal on keyboard focus, remain visible on touch, and never hide run/unread status. Avoid removing errors, first-model setup, or user guides to reduce visual density.
- Deliverable cards keep pending-review status visible; file metadata, full summaries, downloads and explicit deployments expand on demand. Do not infer successful validation from registration or type discovery. Explicit rendered-preview opens collapse the directory through the shared file workspace; ordinary tree selection and background updates retain directory visibility, and closing the final tab restores it. Mobile exposes results directly and files through panel tabs or the conversation menu. Personal model settings start with the saved list when an enabled model exists, including shared models; preserve first-setup steps, drafts, advanced provider fields and administrator controls.
- Conversation view layout is tab-local and keyed by user/workspace/conversation. Text/reference/mode drafts and unconfirmed receipt identities persist in account-scoped localStorage across window closure; logout clears them and disables late writes. Persist drafts only on draft-field changes so layout/scroll updates do not overwrite another tab’s newer draft. Preserve drafts, file references/tabs/modes, expanded directories and transcript/source reading positions across navigation; never put view state in the model transcript. Bound sessionStorage (50 conversations; 100,000 draft characters), clear it on logout, and store only attachment names there: browser File objects survive client navigation in memory, not reloads. A reload must explicitly disclose missing local attachments.
- Keep the right panel closed before the first output, then allow generous preview width without overflowing the desktop grid. Background file updates add/refresh tabs without switching the selected file, evicting it at the tab cap, or reopening a dismissed panel. Keep a concise result receipt in the conversation and full review actions in the panel. Keep Terminal, Skills and created-Agent configuration reachable through the panel Tools menu. Compact and expanded panels use native modal dialogs; preserve nested deployment dialogs and restore focus on dismissal.
- Desktop conversation chrome has one Workspace toggle that retains the last panel tab; Results/Files switching belongs to the panel. Compact screens retain a direct Results entry and file access through More. Keep Agent/state adjacent, prioritize the readable run label over the Agent name, and show only the state beneath the title on phones. Result cards expose the verified entry path and a primary Preview button; Continue modifying is separate, and download/publish remain in More.
- Follow streaming content only while the user is at the bottom; show a jump-to-new-content control while reading history. File updates must not steal selected tabs. Refresh source content in place and retain its scroll position. Sandboxed HTML must not gain same-origin access to restore internal page state.
- Progress labels must reflect actual events, not inferred thinking, fabricated percentages, or unverified success. Keep tool details collapsed until requested; keep elapsed timers out of live announcements. Recovery fills a prompt for explicit confirmation and must not silently replay tools.
- Do not present unavailable functionality as enabled.
- Every asynchronous page load and mutation must surface a visible error state and prevent duplicate submission while pending. Clear conversation-scoped state before loading a different conversation.
- Avoid introducing a state-management or UI framework unless the existing React structure is no longer sufficient and the dependency is justified.

- Shared composer sizing grows to approximately six lines, then scrolls internally, respecting reduced motion and keeping the action row visible. Active-run follow-ups remain drafts, with an explicit send-after-completion hint; text, context and attachment editing must never introduce an automatic queue.
- Pending next-task and adjustment receipts attach above the composer, outside transcript scrolling, in acceptance order. Render confirmed submission receipts immediately and merge durable revisions monotonically; stale polling must not resurrect cancelled inputs or hide accepted ones. Bound queue/history scrolling and preserve drafts, explicit submission, pause/resume and cancellation confirmation semantics.
- Pending attachments use bounded, revoked object URLs for raster-image thumbnails/modal previews, display sizes, reject files over the existing 25 MiB limit before any task submission and skip identical metadata duplicates. Disable removal while that draft is being submitted; preserve attachments on failed submission/navigation.
- Clipboard success must follow the completed write, with visible error feedback. Rich copying uses only the already-safe rendered Markdown, excludes interface controls and never enables raw generated HTML. Text quotation appends a visible blockquote to the latest stored draft, includes source path/line range where available and focuses the composer without sending; selections over 6,000 characters visibly disclose truncation. Selection actions inside native modal panels must remain in that dialog's top layer.
- Unseen file-update dots come from observed live changes, persist in conversation-scoped view state and clear only when the file is successfully read in the visible Files panel. Retain at most eight closed tab paths, reopen only verified existing files, preserve modes/source and Markdown scroll positions, and never imply undoing file edits or version history. Context chip inspection fetches current user-scoped contents on demand; historical message snapshots remain immutable.

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

Run `go mod tidy` even when no new module was added: direct imports and platform-specific files can change direct/indirect classification. Include its `go.mod`/`go.sum` changes in the commit, then verify a second tidy produces no further changes before pushing; CI intentionally rejects untidied committed module files.

```bash
cd backend
go mod tidy
test -z "$(gofmt -l cmd internal prompts)"
go vet ./...
go test ./...
go build ./cmd/...
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

- Browser regressions exercise the production standalone bundle with fixed REST and native HTTP SSE fixtures on desktop/mobile Chromium. Keep task creation explicit, check draft recovery and private preview isolation, reject unexpected API calls, and do not hide failures with retries. CI retains failure screenshots/traces and annotates failing Go/Docker integration tests; these fixtures do not validate live model/OAuth/SMTP integration.
- Legacy Docker/ACS command launchers must wait for a forked `setsid` child. Keep the forced-fork cancellation regression, start-time identity protection and cleanup assertions; local ACS transport fixtures must map `/workspace` into an isolated temporary directory rather than require host write access.
- Browser assertions must wait for the state they depend on (initial menu focus, settled composer animation, new SSE connections after reload). Compare measured CSS geometry with a small explicit tolerance rather than exact floating-point equality; keep behavioral assertions intact and avoid fixed sleeps or retries to mask failures. Command-task fixtures must observe the active-run UI before completion, wait for generated previews to settle, and close mobile preview dialogs before operating transcript controls.
- Server-rendered authentication inputs and actions must remain disabled until client hydration can retain edits. Readiness must not depend on the auth-options request succeeding; email login remains available when that request fails. Exercise delayed script loading before claiming authentication browser checks are stable.

- Make the smallest coherent change that satisfies the request.
- Preserve unrelated user changes in a dirty worktree.
- Do not expand the product scope while fixing or refactoring existing behavior.
- Keep Docker build contexts limited to `frontend/` and `backend/`.
- Keep `Dockerfile.sandbox-runtime` compatible with ACS Agent Runtime: `/bin/bash`, `cp`, `mv`, and `mkdir` are mandatory; preserve the common coding utilities and static `lester-toolbox` unless the runtime contract changes.
- The bundled Computer runtime includes Node/npm/pnpm, Python/pip/venv, Go/native build tools, and matching pinned Node/Python Playwright clients with shared predownloaded Chromium/CJK fonts. Keep Node dependencies locked under `backend/runtime/` and both client versions aligned. Preserve module resolution from conversation directories and toolchain PATH in login shells. Run `make sandbox-check` with non-root, networking disabled and production resource limits; CI also exercises Docker volume access/recreation and terminal integration. Compose builds `lester-sandbox-runtime:local` before Sandbox Service; keep Provider/Compose/Helm defaults aligned. Runtime rebuilds never replace existing Computers or delete volumes; upgrades from root-owned images require an explicit ownership migration. Optional build-only CA/cache inputs must not persist session credentials or weaken TLS checks.
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

- Session recap is a local, bounded factual presentation, never execution authority or model context. It quotes the latest goal/reply and verified current-file links; no invented validation or completion claims. Automatic return recaps require two minutes away and new evidence, never expand over unsent text/files/references, and respect account-scoped opt-out/logout cleanup. `/recap` in an existing conversation must not submit a task or queue input. Context overview distinguishes latest accepted message snapshots from draft materials and never claims a complete provider request or that a reference was read. Compact process groups only confirmed completions; failures, cancellations and unknown outcomes remain inspectable. Recovery adds instructions to the draft; it never replays tools automatically.

- Cloud task monitoring checks the existing PostgreSQL session advisory guard at startup and in bounded fair pages, never task age or browser presence. Preserve live owners on other replicas; abandoned runs close with explicit interrupted/unknown tool results, revoked pending permissions/questions, held future inputs and no replay. The executor remains in API.
- Cloud tasks are workspace-scoped, read-only latest-run-per-conversation views with cursor pagination and explicit error states. Reuse the one workspace SSE; do not steal focus or send/approve/resume from opening the panel. Preserve drafts and show registered deliverable counts as metadata, never verification or file snapshots. Lazy usage reads distinguish provider-reported counts, estimates and missing fields, count cached input once, include compaction and label wall time as including waits. Never fabricate prices or charge amounts. Apply index-only migration 026 and rebuild API/Web together; retain data/volumes.

- Automatic file arrivals may register/refresh tabs but must not raise the workspace panel over an expanded inline file review or another modal. A running-to-completed transition must keep file restore confirmation reachable on mobile; only explicit file/result actions may override this focus intent.

## Scheduled tasks, inbound webhooks and structured questions

- Automations are explicitly configured, owned by their creator inside the authenticated workspace, and disabled on creation unless enabled by the user. Fixed task/model/project/Agent/permission snapshots are captured when a trigger is accepted; project rules and Agent resources use the ordinary conversation acceptance path. Each delivery preallocates a conversation and submission UUID. No browser visit is needed to dispatch due work.
- Schedule scans use bounded `FOR UPDATE SKIP LOCKED` batches. Use five-field Cron plus a real IANA time zone (never server `Local`, inline TZ overrides, descriptors or second fields), or a one-off UTC instant entered in browser local time. Coalesce downtime into one missed occurrence and advance recurring schedules into the future. Record skips while an earlier run, question/approval or active/unknown background command remains; webhook/manual events wait in accepted order. Pause/delete cancels pending deliveries, not already-dispatching tasks or their side effects.
- Keep definition → delivery lock ordering, creator/version checks, session advisory dispatch ownership, and account/membership authorization at execution acceptance. A persisted `dispatching`/`unknown` delivery is reconciled only through the original scoped submission receipt; never call Start again, manufacture a fresh submission ID or replay tools. The executor and scheduler remain in API, not an independent Worker. Keep response uncertainty explicit in UI and preserve stable manual trigger identities.
- Inbound webhooks require an independent high-entropy Bearer credential in the Authorization header and a bounded X-Lester-Event-ID. Store only the token hash, reveal credentials only at creation/reset, revoke the old token atomically and never persist them in browser storage, URLs, logs or model input. Accept bounded JSON objects as data only; payloads cannot choose permissions, workspace, model, references or tools. Same event ID with the same canonical data returns the original record; differing data conflicts. Retain body/queue/rate bounds and test disabled/deleted definitions, revoked actors, concurrency and lost receipts. This generic inbound protocol does not imply native GitHub/other-app adapters or outgoing callbacks.
- Migration 027 adds automation definitions/trigger receipts. Fresh Compose must mount every migration 001–027; existing volumes upgrade explicitly after backup and stopping API writes/active tasks. Rebuild API and Web together. Rollback removes automation configuration/receipts only; it never undoes tools or deletes conversation files. Do not delete volumes.
- Structured questions reuse the existing durable interaction IDs/digests and exact Plan/permission boundaries. Optional short headers, one recommended option and multi_select are backward compatible; single-choice replies retain option_id, multi-choice uses unique offered option_ids, and free text is retained with either. No preselection, timeout answer or implicit approval. Pin pending questions above the composer through the existing interaction reader, without nesting forms, stealing typing focus or duplicating polling/SSE. Show one question at a time and a final multi-question review; preserve answer drafts through navigation/reload and render confirmed answers in conversation receipts. Stop and the ordinary draft remain usable, including short/mobile viewports. Number shortcuts apply only inside the question card, never in textareas or the message composer.

## Project workspaces

- Project names open `/app/p/{id}/overview`; separate chevrons expand the conversation tree and + keeps the existing `/app/p/{id}` task/draft route. Preserve `/app` as the conversation-first home, source conversation identity, file paths, draft account scoping and the single workspace SSE. New-project creation still starts an explicit task composer.
- Project results query registered HTML/Markdown metadata through the current conversation project assignment, scoped by workspace on both sides. Use bounded 24-record keyset pages with timestamp/UUID ordering and filter-bound cursors, literal bounded search, explicit errors/retry and no directory/model/Computer calls on opening the catalogue. Existing indexes/schema suffice; no additional migration. Results may include incomplete/failed source runs, which must remain labeled, and registration never grants availability, review or publication status.
- Before preview/continue, check the actual source directory using authenticated file inventory. Missing files stay explicit errors. Continue sets only the source conversation's file reference and clears its selected preview to expose the existing draft; retain attachments, text, file tabs/modes and require explicit send. Abort late actions on navigation/logout; do not transfer source-file authority to another conversation.
- Keep published-site management named Published sites, independently filterable by project and safely returnable to project overview. It contains publication records, while the project catalogue contains registered work. Reading either must never publish, copy files, start a task or imply durable approved versions, project shared-file storage or Memory. Keep six-language UI/help complete and exercise source moves, tenant isolation, tied pagination, stale reads, missing files, source draft preservation and desktop/mobile navigation.
