# Desktop and local tasks

[Documentation](README.md) · [Project home](../README.md) · [简体中文](zh-CN/desktop.md)

The Electron desktop app puts cloud and local tasks in one sidebar. Choose where a new task runs; that task keeps its execution host. Opening a cloud task displays the existing Web workspace. A local task works in a folder you select on this device, with chat, operation approvals, file inspection, Git diff and an interactive terminal.

## Platforms and setup

- **macOS:** the bundled Go runner executes directly using native POSIX processes and terminals.
- **Windows:** the Electron shell runs on Windows; the bundled Linux runner executes through **WSL2**. Install WSL2, initialize a Linux distribution, and confirm `wsl.exe --exec uname -s` prints `Linux` before opening a project. Choose a project folder on a Windows drive, which the runner opens under `/mnt/<drive>/...`. UNC/network paths and `\\wsl$` folders are rejected; tools use the default distribution's Linux environment. Native Win32 task execution is not provided.
- **Linux:** available for development checks. If OS-backed credential encryption is unavailable or Electron selects `basic_text`, secrets remain in memory for the current session and must be entered again after quitting.

A local task does not require a cloud account or a local Docker/PostgreSQL installation. Its chosen model and project tools must be reachable on the device; a remote model endpoint still needs a network connection. Local commands run with your OS user's access. Choosing a project scopes file tools, but does not create an OS sandbox for commands or terminal sessions. Review the displayed operation before granting permission.

From a source checkout, use Node.js 22 and the Go version in `backend/go.mod`:

```bash
cd desktop
npm ci
npm run build:runner
npm start
```

On Windows, `build:runner` produces the Linux runner used by WSL2. macOS builds use the host architecture. `npm run build:runner:all` cross-compiles the macOS and Linux x64/arm64 runners. `npm run pack`, `npm run dist:mac` and `npm run dist:win` package the shell; release distribution still requires the platform's signing/notarization configuration. Build success is not evidence of a signed release or a native-device test.

## Navigation and settings

Windows and macOS use the same project tree, composer, workspace and settings. macOS additionally uses its system font stack, native inset window controls with reserved space above the sidebar, and draggable title-bar/brand areas; header buttons and selectable task titles remain non-draggable. Windows and Linux retain their normal OS frame. In the local workspace, **⌘,** opens settings and **⌘N** opens a new draft on macOS; use **Ctrl+,** and **Ctrl+N** on Windows/Linux. New-task shortcuts do not dismiss an open settings form or submit a task.

Projects expand in place to show their conversations. The **+** beside a local project opens a draft in that folder; expanding a project never starts a task. Each group initially shows five recent conversations, keeps the current task visible, and offers **Show more**. Search matches project names and conversation titles without changing your selection or saved fold state. Tasks advertised by another device remain labeled separately; they do not become local conversations.

New tasks keep the introduction and composer together. Existing conversations keep the composer below their transcript. Project, model and permission choices remain explicit, and no task is sent merely by choosing a project or opening settings. Settings has **General**, **Models** and **Lester cloud** tabs; switching tabs or language preserves message and unsaved model drafts. Model protocol overrides and JSON configuration live under **Advanced**. Workspace tabs support arrow keys and Home/End; settings tabs use Up/Down and Home/End.

## Models and credentials

Add a device model in desktop model settings, select its provider, endpoint and Model ID, and enter its credential. Configuration is saved separately from proving a real provider request. Device credentials are encrypted through Electron's OS-backed secure storage (Keychain on macOS, Windows protected storage on Windows); the runner receives them only through private main-process IPC. They are not stored in SQLite or sent to the cloud directory.

You can also sign in to your own Lester deployment and select an authorized cloud model for a local task. Inference goes through the scoped desktop device gateway; the cloud provider's credential stays on the server. Local tool execution stays on this device. The model receives the task context needed for inference, including selected file/tool content.

Cloud inference failures preserve bounded error categories such as authentication, rate limiting and context limits, including failures before streaming starts. Raw provider responses are never displayed, and failed or uncertain tasks are not automatically resent.

**Share with cloud** is off by default for a device model. Enabling it advertises that model's metadata and allows your cloud tasks to request inference over the authenticated device WebSocket. The desktop app calls the configured provider locally; the provider credential stays on the device. Sharing requires the device to remain connected. Revoking the device or turning sharing off removes that route; neither creates a replacement run or changes a task's host.

## Storage, privacy and recovery

Local messages, tool calls/results and summary checkpoints live in per-conversation JSONL files under the app's user-data directory: `profiles/<profile>/contexts/<conversation-id>.jsonl`. Projects, runs, approvals, events and original submission receipts remain in the profile's `workspace.sqlite`. A transactional SQLite outbox keeps acceptance consistent with JSONL; entries are removed after the journal is synced. Project files stay in their selected folders. A different server/account uses a different profile; mismatched profile/device identities are rejected.

The updated runner automatically migrates existing SQLite transcripts, preserving message IDs, order, partial replies and submission receipts. Long context uses a validated summary from a tool-free model call for subsequent requests; the complete original transcript is never overwritten. Back up and restore the entire profile directory (SQLite and JSONL together). Do not open an upgraded profile with an older runner.

Only one runner may write a profile at a time. Restart recovery repairs an unfinished last line only from committed outbox records. Missing acknowledged records, corruption or unsupported format versions produce errors instead of an empty conversation. Cloud execution continues to store its history in backend PostgreSQL.

After cloud sign-in, the owner-private directory synchronizes only local task IDs, titles, project labels, status, timestamps and device metadata. Local transcript bodies, project paths and files are not uploaded to that directory. Other workspace members cannot browse this owner's local metadata.

An offline device is shown as offline; that does not mean its tasks failed. Cloud directory metadata can be stale while disconnected. Missing responses or runner restarts use the original local receipt and persisted state for inspection; interrupted/unknown operations are not automatically replayed. There is no automatic continuation, cloud/local task migration, file synchronization or transcript transfer. Stop or resume work explicitly after checking what actually happened.

## Server upgrade and checks

Back up PostgreSQL, object storage and Computer data, stop API writes and active tasks, and apply every missing migration through `000038_memory_management.up.sql` in order. Preserve `.env`, `MASTER_KEY_BASE64` and all data volumes; rebuild API/Web together. Fresh Compose mounts **001–038**. Existing volumes never rerun initialization SQL; Helm operators apply SQL externally. See the [upgrade checklist](deployment.md#upgrading-an-existing-installation).

Migration 037 adds device identities, derived shared-model definitions and the metadata-only local task directory. Its rollback removes this directory and model routing while retaining historical cloud messages and device-local files/SQLite. Disconnect desktop clients and use a compatible API version before rolling back; rollback does not undo task or provider effects.

```bash
cd desktop
npm test
npm run check
npm run test:ui
npm run test:ui:mac
npm run build:runner:all
npm run test:e2e
```

Focused CI covers shell unit/static checks, headless renderer interaction checks, shared harness/local runner tests and runner cross-compilation. These checks use fixtures and do not establish live model-provider access, macOS/Windows signing, or real WSL2/native terminal behavior. Test those on the target OS before distributing a release.

`test:ui` uses an isolated, headless Chromium browser and a test bridge, without opening an Electron window or accessing your real desktop profile. It checks project trees/search, six languages, drafts, settings, keyboard navigation, HTML preview isolation and compact layouts. Windows uses the installed Chrome channel by default; other systems use Playwright's Chromium (`npx playwright install chromium`). `LESTER_BROWSER_CHANNEL` can select another installed channel. `test:ui:mac` repeats the same checks with macOS presentation metadata, including Command shortcuts and native-control clearance. Screenshots are separated into platform subdirectories beneath the temporary directory or `LESTER_DESKTOP_ARTIFACTS`. These fixtures do not validate native macOS fonts, traffic-light clicks, dragging, Retina behavior, IPC, WSL2 or a live model.

`test:e2e` uses the actual Electron app, runner, project filesystem and terminal with a deterministic model-provider fixture; it does not access a live model account. Linux needs a graphical display, for example `xvfb-run -a npm run test:e2e`. Set `LESTER_ELECTRON_NO_SANDBOX=1` only when an isolated container test requires disabling Electron's sandbox; normal app startup does not use that setting. Screenshots go to a temporary directory unless `LESTER_DESKTOP_ARTIFACTS` is set.

An optional PostgreSQL system test exercises both model directions. After installing desktop dependencies, set `LESTER_TEST_DATABASE_URL` to a test database and `LESTER_TEST_HYBRID_SYSTEM=1`, then run `go test -race ./internal/desktopcloud -run '^TestHybridDesktopSystem$' -count=1 -timeout=2m` from `backend`. It uses fixtures to verify the real WebSocket, gateway, stdio and local tool chain.
