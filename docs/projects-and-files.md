# Projects, files and publishing

[Documentation](README.md) · [Project home](../README.md) · [简体中文](zh-CN/projects-and-files.md)

Project workspaces, deliverables, previews and published sites.

> Commands run from the repository root unless stated otherwise. For existing databases, follow the [complete upgrade checklist](deployment.md#upgrading-an-existing-installation); feature-specific migration examples are partial steps.

## In this chapter

- [Project workspace](#project-workspace)
- [Projects and published artifacts](#projects-and-published-artifacts)
- [File synchronization and view state](#file-synchronization-and-view-state)
- [Actual file review and project rules](#actual-file-review-and-project-rules)

## Project workspace

Click a project name in the sidebar to open its workspace. The separate chevron expands conversations; **+** starts a task using the existing project draft. The workspace opens with **Deliverables** and also provides **Conversations**, project rules and a project-filtered **Published sites** entry.

The catalogue combines registered HTML/Markdown deliverables from the project's conversations, including unpublished work. Search by title, file path or source conversation, filter by type, and load 24 records at a time. Each card retains its source and registration update time. Registration does not prove file availability, review or task success. Preview/continue first check the source file; continuing returns to the original conversation with the file reference and existing draft, without sending. Files discovered only through a live directory listing remain available inside their conversation.

`/app/p/{projectId}/overview` is the workspace; `/app/p/{projectId}` continues to start a task. This is a metadata view: no file relocation, automatic publication, extra SSE or model call. It reuses existing schema/indexes; rebuild API and Web together, with no migration beyond the existing 001–032 requirement.

## Projects and published artifacts

The sidebar shows a collapsible project/conversation tree. Each project has its own **+** button for starting a conversation and a menu for renaming or pinning. Project headings expand or collapse their conversations without navigating away. Each project initially shows five recent conversations; expand the history to see all, and the current conversation remains visible. Search displays matching conversations under their projects. Conversation menus support pinning and moving between projects. New drafts only become conversations when you send a message.

Open **Artifacts** from the independent link in the workspace header to enter its standalone page; **Return to workspace** takes you back to the originating conversation or project. Search deployments, filter by project, preview a published page in an isolated viewer, or open and copy its link. Each card’s more menu contains the source conversation, update, and take-offline actions. You can also deploy from an HTML file’s preview toolbar. Moving a conversation changes the project shown for its artifacts without changing their public URLs.

The agent has a built-in `deploy_html` tool, so no Skill installation is necessary. Ask it to publish explicitly, for example:

> Publish `website/` with `index.html` as the entry and give me a shareable link.

```json
{"name":"My website","source_path":"website","entry":"index.html"}
```

The tool returns both `artifact_id` and the legacy `id` alias. Pass the returned `artifact_id` as `artifact_id` on a later deployment to update the same deployment and keep its URL. A successful deployment is a snapshot: it survives Computer suspension and later file edits. Updating it is explicit. An upload or validation failure leaves the previous deployment available. Taking a deployment offline makes its entry and every asset return 404; it cannot recall copies already downloaded by viewers.

| Source | Deployment behavior |
| --- | --- |
| `report.html` | Collects the HTML and statically referenced local files |
| `website/` | Includes supported static files recursively; defaults to `index.html` |
| HTML with local media | Copies images, video, audio, fonts, CSS and JS into object storage and rewrites discovered paths |
| Sandbox absolute paths | Current conversation paths and explicitly referenced `.agent/upload` files are supported; other conversations and escaping paths are rejected |
| Inline / external media | Ordinary `data:` image/media URLs stay embedded; HTTP(S) URLs remain external and produce a warning |

Limits are **256 files, 256 directories, 12 directory levels, 25 MiB per file, and 100 MiB per deployment**. Missing local dependencies fail the deployment. Hidden files, dependency directories and executable/server files are excluded. Build React/Vue/framework projects into a static output directory first; the host does not execute backends or perform SPA fallback routing. Remove existing `<base>` tags. Use separate files instead of `data:` entries in `srcset`. Dynamic JavaScript URLs cannot be completely discovered; publish the complete directory and use relative paths. Static hosting uses a sandboxed document origin: cookies, localStorage, service workers and same-origin-only APIs are unavailable. Ordinary DOM interaction, scripts, images and range-based video playback are supported; external APIs must permit CORS.

### Hosting configuration

Compose uses `ARTIFACT_PUBLIC_URL=http://127.0.0.1:13181` and `ARTIFACT_PORT=13181`. This loopback URL works on the host computer. For access from other devices or the Internet, configure a reachable hostname and TLS reverse proxy to Artifact Host, then set the same public origin on API and Artifact Host:

```dotenv
WEB_ORIGIN=https://lester.example.com
ARTIFACT_PUBLIC_URL=https://sites.example.net
ARTIFACT_PORT=13181
```

**Use a different hostname from the application**, preferably a separate registrable domain. A different port on the same hostname is rejected because cookies are not port-scoped. Do not proxy artifacts under the application origin or expose MinIO/Sandbox Service. Anyone with a published link can access it; deployment management remains authenticated and workspace-scoped. Artifact Host reads only published manifest entries, streams media with byte-range/HEAD support, sets `no-store`, and sends no authentication cookies. Keep reverse proxies/CDNs from overriding its cache and security headers.

The existing `OBJECT_STORE_*` configuration works with MinIO or an S3-compatible endpoint. Back up both PostgreSQL and the object bucket. Superseded object versions are retained to allow in-flight reads; there is currently no automatic storage garbage collection or version-restore UI. Unpublishing revokes access without deleting stored objects. Do not apply an age-only bucket expiration rule to `artifacts/`, as an old object may still belong to a live deployment.

For an existing installation, back up the database and apply migration 006 once before starting the updated API:

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api
cat backend/migrations/000006_projects_artifacts.up.sql | \
  docker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres \
  sh -c 'psql -v ON_ERROR_STOP=1 -1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --build
```

In PowerShell, replace `cat` with `Get-Content -Raw` and use a single line for the pipeline. Earlier migrations must already be applied. Fresh Compose volumes apply all migrations automatically. Do not delete volumes to upgrade.

## File synchronization and view state

File and tool/run events invalidate the shared inventory. While the page is visible, metadata polling runs every 5 seconds during execution and every 15 seconds while idle to detect Bash and background-script changes. Automatic scans cover up to 64 directories, 2,000 files, and five nested levels. Dependencies, caches, and `.agent` are skipped by default but can be expanded manually; partial scans are disclosed.

The changes list combines persisted file events from the current run with changes observed while the view is open. It is not a complete history, content diff, or checkpoint system. Metadata checks cannot detect changed contents with identical size and modification time.

View layout is scoped by user/workspace/conversation in tab-local `sessionStorage` (50 conversations). Text drafts (up to 100,000 characters), references, selected mode and pending receipt identities persist in browser `localStorage` across tab/window closure, under the same account scope. Logging out clears both. Scroll/layout updates do not overwrite another window’s persisted draft. Local attachments survive client navigation in memory only; after reload, the UI asks you to select them again. File contents are never saved in browser storage. HTML refreshes may reset the generated page's own interaction state; iframe permissions are not relaxed to preserve it.

## Actual file review and project rules

**Review file changes** is a collapsed conversation section. Expanding a file loads the actual retained before/after bytes from successful Agent `write`/`edit` calls, displays an exact line comparison, and exports a JSON review record. Comparisons above 1,000 lines use original before/after text panes. Private capture reads do not authorize Agent edits of unseen content. Each side retains at most 512 KiB of UTF-8 text without NUL bytes; the conversation retention cap is 64 MiB. Large, binary, quota-limited or unconfirmed operations display their limitation instead of a fabricated diff. Existing history is not reconstructed retroactively.

Restoring a confirmed modification of an existing retained text file requires explicit confirmation and an idle conversation. The native writer checks that the current file still matches the recorded AFTER SHA-256 before restoring the original bytes. Later edits are refused. New-file deletion, shell/network/background effects and other files are not undone. A lost native write receipt remains unknown and prevents blind replay; matching content is not proof of ownership. Stable scoped rollback receipts recover lost HTTP responses without a second write. Up to 20 recent unconfirmed mutation/restore facts are restored from platform records into the Agent request after compaction, with no file bodies or new write permissions. A confirmed historic digest does not assert current live file contents.

The project menu provides **Project rules** with content, version and SHA-256. Saving a stale version preserves the editor draft and refuses overwrite. Initial sends and accepted queued tasks snapshot rules from the project settings at acceptance; later edits or conversation moves affect future accepted tasks only. The input message exposes the accepted source/version/content. Rules are explicit user guidance and grant no tools, @ access or Plan approval. Platform constraints and the latest direct user requirements take precedence. File arrivals during an open settings/review dialog still add tabs without opening a covering preview.

For databases already on 001–022, back up and stop API writes, then apply `000023_file_changes.up.sql` and `000024_project_rules.up.sql` once in order. Earlier versions apply every missing migration; fresh Compose volumes initialize 001–032. Stop active tasks and restore compatible API/Web before rollback. Roll back 024 before 023 when downgrading this batch. 024 removes editable project rules while accepted message snapshots remain; 023 removes review/rollback records. Neither rollback restores file or external effects. Preserve database backups, keys and Computer volumes.

---

[← User guide](usage.md) · [Agents, context and Skills →](agents-and-context.md)
