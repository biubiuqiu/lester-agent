# Personal Lester: memory and coordination

The fixed **Lester** entry (bottom right on wide screens, in navigation on narrow screens) opens an independent, persistent assistant panel throughout authenticated `/app` routes, including settings. It is owned by the app layout: navigation and minimizing preserve its transcript, draft and in-flight request. Wide displays reserve space for a non-modal companion so the task composer stays usable; an open Computer pane retains its existing space. Constrained displays use a viewport-aware modal panel with native focus trapping, with a full sheet on phones. Opening a task from the sheet minimizes it. `/app` and `/app/new` are task composers; project composers and existing `/app/c/{id}` conversations retain execution. Model/project choices live in assistant settings, outside its composer.

```mermaid
flowchart TD
    User[User] --> Lester[Personal Lester]
    Lester <--> Memory[Private preferences, project facts and work context]
    Lester -->|Create / continue / queue| Task[Independent task conversation]
    Task --> Computer[Scoped Computer files and commands]
    Task -->|Live status and public reply| Lester
```

Personal Lester has no Computer, file, command, browser, Skill, approval, Stop or current-run adjustment tools. It uses the existing selected model in a tool-free call with JSON Schema, independently validates the result, and offers only a fixed set of memory/coordination actions. For tool-free personal calls only, an OpenAI-compatible provider explicitly rejecting `json_schema` with HTTP 400/422 can negotiate `json_object` once, with the full schema in the system instructions. The same strict server-side parser, evidence checks and action validation still apply. Authentication, rate limits, invalid schemas, malformed output and uncertain transport failures never negotiate or replay. Approval requests remain strict JSON Schema. There is no plain-text/action fallback. Concrete work uses the existing Agent runtime, queue, project rules and operation approvals.

## Memory behavior

- **Personal preferences**: communication, design and working habits.
- **Project facts**: decisions and constraints, saved only when the user selects that project. They never enter another project's task.
- **Work context**: meaningful personal or project intentions. Current run status is always queried from platform records, not recalled as a fact.

Rows are private to the authenticated user/workspace; project scope does not turn them into shared project knowledge. Every model-generated fact must quote an exact substring of the latest user's message and points back to that personal turn. Task replies, inspected history and files are never automatically mined into memory. Existing task transcripts are not imported on upgrade.

Stable case-insensitive topics and IDs support correction instead of accumulating contradictory rows. The selected model sees a bounded topic directory and relevant facts, not every full transcript. Retrieval uses Latin words and Han bigrams, with personal preference/project-fact fallback; at most 24 facts / 10,000 fact characters are attached. The model's recent personal history is bounded to six completed turns and can be further projected for the configured context limit, always retaining the latest user input. This is explicit PostgreSQL memory, without vectors or a provider-owned memory store.

There are at most 128 active facts per user/workspace. A full memory store returns a visible unsaved-memory notice while the dialogue and task coordination continue. Forgetting frees active space. Tombstones remain to prevent automatic restoration of a forgotten topic; only a new explicit request can restore it.

Open **Memory** to search, inspect original quotes, edit, forget or undo. Automatic inference is optional and shows an undo receipt; explicit memory requests still work when it is off. Corrections, forgetting, undo and the inference setting advance a context barrier and revision. Earlier personal chat remains visible but is excluded from subsequent model history. A model response started before a memory change is rejected, preventing stale in-flight inference from restoring the removed fact.

Forgetting removes effective memory. Original chat, prior revisions needed for undo, and already accepted task snapshots remain; it does not claim server-wide erasure. Undo checks the exact resulting revision and cannot overwrite a newer correction or exceed active capacity.

## Task handoff

You can say **“Create a new task in the Product project to prepare a review checklist”** or ask Lester to continue a previous conversation. An explicit request starts the ordinary task handoff; it does not require opening the task composer first. Welcome shortcuts only add a suggested request to the current draft. Review and send it explicitly.

Initially the model sees bounded, owned task metadata with project names, current run state and unread-reply information. It can perform one read-only title/project search, unread-reply lookup or inspect the bounded public transcript of one offered, owned conversation. It cannot enumerate other users' work or inspect tool internals/files. Ambiguous targets require clarification.

New task IDs and submission IDs are persisted before dispatch. New delegated tasks use Lester and **standard permissions**. Existing conversations retain their Agent, model and current project. Active runs receive the existing ordered next-task queue input. The personal assistant cannot insert into, stop, approve or raise the permissions of an existing run.

The server selects only relevant personal/same-project memory, labels it reference data and stores it in the accepted task prompt. Ordinary project-rule snapshots still apply. The shared immediate/queued acceptance transaction checks that the conversation still belongs to the prepared project; concurrent moves cannot transfer the old project's facts. Editing or forgetting memory later does not retroactively alter accepted task inputs.

Pending questions/approvals appear as Needs your attention and link back to the original conversation. Cards show the current project and live state through the existing workspace SSE; public replies are bounded previews. Completion means Agent execution ended, not independently verified results. Open the original task for files, approvals and full execution detail. No native background/lock-screen push is added.

## Unread replies

Ask **“Which conversations have new replies I haven’t read?”**, or open **Unread replies** in the assistant. The list shows each conversation’s title, project and reply time. It queries all your conversations in the current workspace, independently of the recent-task limit, with 30 conversations per page and a total count. Loading more and retrying a failed read only fetch the list; they never create or restart a task. The model’s lookup respects a selected project when one is set.

Unread means a saved, nonempty public assistant reply newer than your confirmed read position. Tool calls, internal process, partial streams and other users’ conversations do not count. Current task status and a new reply are separate: an unfinished task can already have a public reply.

Opening the assistant, asking for unread replies or expanding a reply preview does not mark a task read. A confirmed read position advances only when the end of a saved reply is visible in the focused, foreground task conversation. Hidden tabs and replies below the current scroll position remain unread. The API verifies the displayed message and stores a monotonic position per account, workspace and conversation, so an older acknowledgement cannot hide a newer reply. Read positions persist across devices; open clients refresh after confirmed acknowledgements and the existing workspace events, without another execution stream or background worker.

Tracking begins with migration 036. Existing replies become an upgrade baseline because Lester has no trustworthy earlier read history; it does not claim to reconstruct which old replies you had seen. New saved replies after that baseline participate in unread tracking.

## Persistence, failure and APIs

`personal_threads`, `personal_turns`, `personal_memories`, `personal_memory_changes`, `personal_delegations` and `conversation_read_states` are PostgreSQL authority. A dedicated session advisory guard serializes personal turns without holding a pooled connection during model waits. Request UUIDs and exact digests prevent duplicate model calls. Live account/session/membership and thread revision are checked again before accepting a response, using user → session → thread lock ordering.

Incomplete owners become interrupted after their session guard is released. Missing responses and uncertain delegation use the original run/input receipt only; no automatic model retry, tool replay, replacement conversation or background worker is introduced. Request timeouts are bounded, errors do not contain provider credentials, and personal-model usage observations are retained on the turn.

All endpoints require the ordinary session and mutation-Origin checks:

| Endpoint | Behavior |
| --- | --- |
| `GET /api/v1/personal?before={seq}` | Up to 30 chronological turns, memory, current owned tasks and first unread page |
| `POST /api/v1/personal/messages` | `{id, content, model_id, project_id}`; one scoped durable turn |
| `GET /api/v1/personal/messages/{id}` | Original turn/receipt; recovery never dispatches |
| `GET /api/v1/personal/tasks` | Bounded current task cards and first unread page |
| `GET /api/v1/personal/unread?cursor={cursor}&project_id={id}` | Read-only unread conversations, total and next cursor; optional project scope |
| `POST /api/v1/conversations/{id}/read` | `{message_id}`; confirm an owned public reply and monotonically advance its read position |
| `PATCH /api/v1/personal/settings` | `{auto_memory}` |
| `PATCH /api/v1/personal/memories/{id}` | `{revision, content, forget}` |
| `POST /api/v1/personal/changes/{id}/undo` | Revision-checked, idempotent undo |

Personal drafts and pending receipt IDs use the existing account-scoped browser draft store. Closing/reopening preserves text and refreshes saved dialogue/memories through a read, including recovery after a failed initial read. Reopening never resends a message or delegation. Memory searches distinguish an empty result, and short viewports scroll the editor without squeezing it beneath the fixed controls. Logout clears drafts and blocks late writes. Native iOS/Android Home opens the task composer; the same Web assistant launcher is available throughout the app. There is no separate native memory/runtime.

## Upgrade and validation

Back up and stop API writes/active tasks, apply every missing migration through `000036_conversation_reads.up.sql`, then rebuild API and Web together. Fresh Compose mounts 001–036. Helm operators apply SQL externally. Do not delete volumes or replace encryption keys. Migration 036 establishes read tracking and the existing-history baseline; its rollback removes read positions and the supporting index, preserving conversations, messages, tasks and memories. Rollback 031 deletes personal dialogue, memories and delegation records, preserving task conversations, files and external effects.

PostgreSQL tests cover provenance, corrections, undo, deletion barriers, capacity, user/project isolation, revocation, abandoned owners, idempotency, real task/queue acceptance, unread pagination, verified read positions and rollback. Tests use a two-connection pool to catch nested acceptance starvation. Production-browser fixtures cover desktop/mobile dialogue, drafts, lost responses, memory management, task cards, short screens and six-language help. Fixture output is not a live provider or native-device validation.

[简体中文](zh-CN/personal-assistant.md) · The deployed `/docs/usage#personal-assistant` guide is available in Chinese, English, Japanese, Korean, French and Spanish. Personal Lester coordinates work; independent task conversations perform it.
