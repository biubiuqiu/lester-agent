# Deployment and upgrades

[Documentation](README.md) · [Project home](../README.md) · [简体中文](zh-CN/deployment.md)

Upgrade checklist, gateway, Docker/ACS and Kubernetes/Helm.

## Upgrading an existing installation

1. Back up PostgreSQL, object storage and Computer data; preserve `.env`, the encryption key and persistent volumes.
2. Stop active tasks and API writes. Confirm which migrations have already been applied.
3. Apply **every missing migration through 031**, once and in numeric order. Fresh-database initialization does not upgrade existing volumes. The feature-specific SQL examples in other chapters are partial historical steps, not a complete upgrade.
4. Rebuild and deploy matching API, Web and Sandbox Service versions. Never delete volumes to upgrade.

Start a new installation with [Getting started](getting-started.md). Use the [account migration notes](accounts-and-security.md#avatars-and-account-upgrade) and [storage migration notes](architecture.md#upgrading-an-existing-database) for the transactional SQL procedure; complete all remaining migrations before restarting the latest services.

## In this chapter

- [Deployment](#deployment)
- [Runtime reliability upgrade](#runtime-reliability-upgrade)

## Deployment

### Same-origin gateway

Compose proxies `/api` and `/api/*` to API without rewriting paths; other traffic goes to Web. Cookies, SSE, terminal WebSockets, uploads, and HTML previews share one browser origin. The gateway does not implement business authentication, read user files, or expose Sandbox Service. API authenticates previews and provides their CSP.

- Keep `WEB_ORIGIN` aligned with the real public scheme, hostname, and port, or terminal Origin checks reject connections.
- Compose builds Web with an empty `NEXT_PUBLIC_API_URL`. Older `.env` values no longer override it. Rebuild Web after upgrading; runtime environment changes cannot alter bundled browser code. Standalone frontend development can still specify an API origin.
- SSE buffering/cache are disabled; terminal WebSocket Upgrade is supported. The API timeout is one hour of **idle time**, not an agent run limit. Heartbeats keep SSE alive; failed API requests are not replayed automatically.
- Gateway request bodies allow 26 MiB, including multipart overhead; API retains a 25 MiB per-file limit. `/healthz` is gateway liveness only, not upstream readiness.
- For temporary debugging, append `-f deploy/docker-compose.debug.yaml` or use `make dev-debug`. API `18080` and MinIO `9000/9001` bind only to `127.0.0.1`; browsers still use the gateway. Reapply default Compose afterward to remove debug bindings.
- Public deployments should use HTTPS and `SESSION_COOKIE_SECURE=true`. Configure gateway TLS or a trusted external load balancer. With external TLS termination, restrict gateway access to that proxy and forward the public scheme correctly. The supplied gateway overwrites incoming `X-Forwarded-Proto` with its own scheme; do not trust arbitrary public forwarding headers.

Gateway upgrades require no new database migration. Preserve `.env` and volumes, update `WEB_ORIGIN`, and run the full `docker compose ... up -d --build`. Remove old Web/API port overrides and configure public ports on the gateway.

Run the isolated proxy suite, which uses no application data or host ports:

```bash
make gateway-check
# Clean up test containers after a failed run; application volumes are unaffected:
docker compose -p lester-gateway-test -f deploy/gateway/compose.test.yaml down
```

It covers routes/escaped paths, cookies/headers, preview CSP, 25 MiB uploads, initial SSE delivery, and bidirectional WebSockets. CI runs the same checks.

### Kubernetes and Helm

`deploy/helm/lester` deploys Web, API, Sandbox Service, ClusterIP services, optional Ingress, and NetworkPolicy. Provide external PostgreSQL, Redis, and S3-compatible storage. Apply `backend/migrations/*.up.sql` in numeric order before installation.

Build and push separate Web, API, and Sandbox Service images, plus the Computer runtime for Docker workers or ACS templates. The included runtime provides the [preinstalled development environment](architecture.md#preinstalled-development-environment) and `lester-toolbox`:

```bash
docker build -f backend/Dockerfile.sandbox-runtime -t registry.example.com/lester-sandbox-runtime:v1 backend
docker push registry.example.com/lester-sandbox-runtime:v1
```

Create private values without committing real secrets:

```yaml
images:
  api: {repository: registry.example.com/lester-api, tag: v0.1.0}
  web: {repository: registry.example.com/lester-web, tag: v0.1.0}
  sandboxService: {repository: registry.example.com/lester-sandbox-service, tag: v0.1.0}

config:
  webOrigin: https://lester.example.com
  objectStore: {endpoint: s3.example.com, bucket: lester-skills, useSSL: true}

secrets:
  databaseURL: postgres://...
  redisURL: redis://...
  masterKeyBase64: ...
  sandboxServiceToken: ...
  objectStoreAccessKey: ...
  objectStoreSecretKey: ...

ingress:
  enabled: true
  className: nginx
  host: lester.example.com
  tls:
    - secretName: lester-tls
      hosts: [lester.example.com]

sandbox:
  provider: docker
  nodeSelector: {lester.dev/docker-worker: "true"}
```

```bash
helm upgrade --install lester deploy/helm/lester \
  --namespace lester --create-namespace \
  -f values.production.yaml
```

Ingress routes `/api` to API and other paths to Web, without an extra Compose gateway pod or `NEXT_PUBLIC_API_URL`. Configure your controller for unbuffered SSE, WebSockets, suitable idle timeouts/upload sizes, and external scheme forwarding. These settings are controller-specific.

Docker is the default provider. Sandbox Service runs as one replica on a dedicated worker selected by `sandbox.nodeSelector`, with `/var/run/docker.sock`. Socket access grants extensive node privileges, is incompatible with Restricted Pod Security, and keeps user volumes node-local.

For ACS Agent Sandbox:

```yaml
secrets:
  # Must match ack-sandbox-manager's adminApiKey.
  acsSandboxAPIKey: ...

sandbox:
  provider: acs
  replicas: 2
  acs:
    domain: sandbox.example.com
    protocol: native # Production default; private is for internal/test access.
    template: lester-agent
    secure: true
    autoPause: true
    sandboxSet:
      enabled: true
      replicas: 4
      image: registry.example.com/lester-sandbox-runtime:v1
```

ACS mode does not mount the Docker socket and allows multiple stateless Sandbox Service replicas. The chart can create a warm-pool `SandboxSet` named after `sandbox.acs.template`, or use an existing template with `sandboxSet.enabled=false`. The included image uses an unprivileged `sandbox` user and provides the required `/bin/bash`, `cp`, `mv`, and `mkdir`.

Install or upgrade `ack-agent-sandbox-controller` and `ack-sandbox-manager`, then configure DNS, TLS, and the API key using the [Alibaba Cloud E2B integration documentation](https://help.aliyun.com/zh/cs/user-guide/connect-to-agent-sandbox-using-the-e2b-sdk). Native needs wildcard DNS/TLS; Private uses a single domain with `/kruise`.

For `secrets.existingSecret`, provide `DATABASE_URL`, `REDIS_URL`, `MASTER_KEY_BASE64`, `SANDBOX_SERVICE_TOKEN`, `OBJECT_STORE_ACCESS_KEY`, and `OBJECT_STORE_SECRET_KEY`, plus `ACS_SANDBOX_API_KEY` for ACS. Switching providers creates a new Computer without migrating old files. Back up or migrate workspaces separately before switching.

## Runtime reliability upgrade

Sends use stable submission IDs to avoid duplicate runs when a response is lost. Accepted sends and stops remain accepted when a later read fails; attachment retries reuse successful uploads. A running task keeps its selected model. Native file writes validate the digest of visible reads: full replacement requires a full read, precise editing requires its untruncated observed range, and stale reads require reinspection. Helper writers share conditional-write locks; arbitrary Bash writes do not.

Managed command logs retain up to 64 MiB of raw combined output. Larger logs disclose omitted bytes. Nonzero exits, timeouts, cancelled partial output and unknown cleanup are explicit. Task state is reconciled separately from Agent completion, and active or unconfirmed work prevents idle suspension. Lost launch receipts are inspected without relaunch; unknown process identity cannot be stopped blindly. New Docker Computers use `--init`; existing containers are not replaced to add it. Rebuild ACS templates for file protocol 2/task protocol 1 and deploy the matching Sandbox Service helper. Local ACS fixtures do not verify live cloud access.

Model settings offer an explicit text-only verification call; saved configuration does not verify provider access. Shared models can be verified without exposing credentials or budget edits. Expand Context budget to set a known window, output reserve and local policy. Zero means unknown/automatic/default 64k policy; it does not invent a model limit or cap ordinary replies. Whole-request estimates and actual/missing provider usage are persisted per request, with cached input counted once.

Long conversations now save scoped history checkpoints while retaining the complete ordered transcript. The latest user input, latest assistant, entire unobserved tool batches, unresolved failures and current loaded Skill instructions remain literal. Skill bodies carry source paths and SHA-256 versions; older successful loads become retrievable historical references. File contents omitted from the request no longer authorize edits. Background task facts are restored from platform records as cached observations, not inferred from summary claims.

Summarization is a separate tool-free model request with its own usage record and bounded output. Only validated summaries that reduce the complete request commit a checkpoint and event atomically. Malformed output, cancellation or ineffective summaries preserve the previous checkpoint and open the compaction circuit for that run. Compaction attempts are limited to two per turn; a provider context rejection gets one retry only before any partial output. Ordinary model/tool loops remain unlimited. Protected inputs that cannot fit fail explicitly; no tools are automatically replayed and current input is never truncated. Restarted tasks load the saved checkpoint; a model change triggers a new complete-request estimate. Remaining collaboration/review goals are recorded in [the alignment implementation](claude-code-alignment.md).

Existing databases on 001–015 must apply 016–024 once in order, before starting the updated services. Back up PostgreSQL, stop API writes and preserve keys, files and volumes. Earlier databases must first complete their missing migrations. Fresh volumes initialize all migrations. Stop managed tasks before rolling back 018; schema rollback does not undo file or external effects.

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api
for migration in 000016_run_submissions 000017_model_budget 000018_command_tasks 000019_model_request_usage 000020_context_checkpoints 000021_run_interactions 000022_run_inputs 000023_file_changes 000024_project_rules 000025_run_permissions 000026_cloud_task_indexes 000027_automations 000028_permission_reviews 000029_conversational_schedules 000030_native_signin 000031_personal_assistant; do
  docker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < "backend/migrations/${migration}.up.sql" || exit 1
done
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --build api web sandbox-service
```

Deployments on 001–019 apply missing 020–024; never re-run previously applied migrations. Rolling back 020 removes summary checkpoints only and preserves original messages and files.

---

[← Architecture and Computers](architecture.md) · [Accounts, administration and security →](accounts-and-security.md)
