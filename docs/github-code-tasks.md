# GitHub code tasks

[Documentation](README.md) · [App connections](app-connections.md) · [简体中文](zh-CN/github-code-tasks.md)

A task can select an authorized GitHub repository, import a source snapshot into its Computer, edit files, run the repository's tests, prepare a publication preview, and push a working branch/create a pull request. This is separate from read-only app research. Gmail, X, Feishu and Slack remain search/read connections. The personal assistant delegates the work to an ordinary task; it never receives GitHub or Computer tools.

## Enable code access

Configure the GitHub connector's OAuth client ID/secret as described in [App connections](app-connections.md). Use an installed **GitHub App** with **Contents: read and write**, **Pull requests: read and write**, and Metadata read access. Install it only on the repositories you want Lester to access; account and organization approval may be required. Classic OAuth apps with no `repo` scope retain public research access and cannot provide this code workflow.

Open **Settings → App connections → GitHub → Code tasks** and explicitly authorize code access. The ordinary Connect action stays read-only by default. Reauthorization/installation permissions are distinct: enabling Lester's code access records consent, while every repository operation still has to pass GitHub's actual permissions and branch protections. Turning code access on does not bypass organization policy or grant access to another user's connection.

## Run a task

For example:

> In `owner/repository`, fix the settings-page layout. Start from `main`, run the relevant tests, and prepare a pull request on a new working branch.

If you have not named a repository, the Agent lists the repositories available to the authorized account and asks you to choose. It imports a pinned commit into a **new conversation-local directory**. Existing directories are never overwritten. The resulting local Git repository has an initial source-snapshot commit for ordinary status/diff operations; it does not contain the remote repository's full history, a remote credential helper or an authenticated origin URL.

Use existing file tools and the command tool for editing and testing. Node.js, Python, Go, Git and the supplied browser runtime are available in the standard Computer image. Test success is reported only from actual command results. Missing dependencies, failing tests, unavailable network and interrupted commands remain visible; authorization does not make tests pass.

Publication has two steps:

1. **Prepare:** capture the explicitly selected added/changed files and actual deletions into an immutable, encrypted server-side preview. The preview names the repository, branch, commit and optional PR, with a digest. It does not change GitHub.
2. **Publish:** review the repository, branch and summary in the ordinary approval card. GitHub publication requires a single explicit approval by the accepted task submitter, including in full-access mode. A later local edit cannot change the already prepared upload. Prepare a fresh preview to include further changes.

Default publication uses a new Lester working branch. An explicitly requested existing branch must still match the expected source head; concurrent changes are rejected. GitHub's expected-head commit operation prevents a stale task overwriting newer commits. No force push or automatic PR merge is performed. A PR link or commit URL is reported only after a real provider receipt is observed.

## Credentials and transfer

The API uses the server-side GitHub grant to read the pinned commit/tree/blob objects and perform publication. It transfers a bounded source ZIP to the trusted `lester-toolbox` importer, then later reads only the selected file snapshot from the exporter. A dedicated native transfer protocol executes the fixed helper without loading user shell profiles or resolving a user-controlled executable path. The sandbox never receives a GitHub access token, refresh token, SSH key or authenticated remote URL. Local Git and tests run without access to the connector's credentials.

Each checkout binds the accepted task actor and the original GitHub grant generation. Disconnecting, switching the connected GitHub account, revoking membership or stopping the run prevents stale operations. Files and publication previews stay scoped to that actor/conversation; the conversation's original creator cannot lend their grant to another submitter.

The first version imports at most **5,000 regular files / 25 MiB**, with each source blob limited to **4 MiB**. Checkout accepts branch names; tags and direct commit-SHA inputs are not supported. It rejects truncated GitHub trees, symlinks and submodules rather than silently omitting them. Git LFS pointers are ordinary source text; LFS objects are not automatically downloaded. A publication captures at most **100 changes / 1 MiB of file content**. Existing executable files may be imported/run, but publication of executable-mode files or mode changes is rejected because the selected GitHub API does not expose mode control. Split larger changes or handle these unsupported repository features through your own development environment.

## Dependencies and network

Source transfer and push run server-side, so a disconnected Computer can still receive code and publish through these tools. Dependency installation is separate. Docker Computers retain `none` networking by default. A deployment administrator may set `SANDBOX_DOCKER_NETWORK` to `bridge` or, preferably, a dedicated existing Docker network with an independently managed egress/dependency policy. The task cannot choose its network. Host and container-namespace sharing are rejected.

This option chooses a Docker network; it does not implement a domain allowlist, firewall, package mirror or proxy. Provision and enforce those controls on the worker/network. Changing it affects **new** Computers, not existing containers. Do not delete user volumes or silently replace Computers to apply network settings. ACS egress remains controlled by its template/tenant network policy, not this Docker option.

## Uncertain publication

Publication records its intent before external mutation. The uploaded bytes, expected head, target branch and operation identity are fixed. If a response is lost, later calls inspect the original branch/commit/PR receipt; they do not blindly repeat the write. A pending or unknown result must not be described as success. Inspect the original publication and prepare a new operation only when the actual result has been established. Reopening a UI panel never pushes or creates a PR.

## Upgrade and verification

Back up the database, object storage and Computer volumes. Stop API writes/active tasks, apply all missing migrations through `000034_speech_input.up.sql`, then rebuild **API, Web and Sandbox Service** together. Preserve the existing encryption key. Fresh Compose mounts 001–034; Helm installations apply SQL separately. Migration 033 rollback removes only code grants/checkouts/proposal records; it does not delete working trees, undo commits or close PRs. Unresolved publication/approval records must be handled before rollback.

Docker's Sandbox Service installs its new Toolbox into existing Computers with atomic replacement on startup. Retain their workspace volumes and installed runtime. ACS requires an updated runtime image/template and an explicit existing-Computer upgrade plan that preserves workspace data. Custom or older images also need a usable Git executable.

Backend protocol tests use a fake GitHub service and real PostgreSQL; transfer tests use a real local Git executable. Browser fixtures verify consent, approvals, recovery and all six languages on desktop/mobile. These checks do not establish live GitHub App registration, organization approval, hosted CI success or that a particular repository's dependencies/tests work in your deployment.
