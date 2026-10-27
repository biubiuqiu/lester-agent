# Lester Helm Chart

This chart deploys Lester Web, API, and a provider-neutral Sandbox Service. It expects external PostgreSQL, Redis, and S3-compatible object storage. Computers may run on a local Docker worker (default) or Alibaba Cloud ACS Agent Sandbox.

Before installation:

1. Build and push the three Lester service images and `backend/Dockerfile.sandbox-runtime` (or provide another compatible Computer runtime). The bundled runtime includes Node.js/npm/pnpm, Python/pip/venv, Go, native build tools, Node/Python Playwright and predownloaded Chromium with CJK fonts. Set `sandbox.image` to the image available on the dedicated Docker worker; ACS uses `sandbox.acs.sandboxSet.image` or an externally provisioned template. Run `make sandbox-check` before publishing the image. Existing Computers keep their image; plan migrations with persistent data and UID/GID 1000 ownership preserved.
2. Apply `backend/migrations/*.up.sql` (001–033) to PostgreSQL in numeric order. Back up PostgreSQL and stop API writes/active tasks, then apply every missing migration before upgrading API/Web together. Installations through 027 apply `000028_permission_reviews.up.sql` once for live task permissions and independent command review. Earlier installations through 026 apply `000027_automations.up.sql` once; earlier installations must also complete all missing migrations 016–026 after the auth upgrades. Migration 027 adds schedules and inbound webhook receipts; rollback removes their definitions/records while preserving conversations/files and previously performed operations. Scheduler and executor remain in API, without an additional Worker deployment. Migration 015 invalidates legacy login credentials and requires one sign-in, preserving accounts/workspaces/files. Access cookies expire in 2 hours; rotating refresh cookies last a rolling 30 days with no absolute active-session cutoff. Rollback 015 also signs everyone out and requires a compatible API. Migration 014 preserves existing accounts as opted out of the automatic welcome; new accounts get the first-use guide. Rollback removes tutorial progress only. Back up first and retain all existing credentials/data.
3. Choose `sandbox.provider: docker` or `sandbox.provider: acs`.
4. Copy `values.yaml` to a private values file and configure images, service URLs, secrets, Ingress, and provider settings.

Install with:

```bash
helm upgrade --install lester deploy/helm/lester \
  --namespace lester --create-namespace \
  -f values.production.yaml
```

The supported Ingress layout is same-origin: `/api` routes to the API Service and `/` routes to Web. The frontend image therefore uses an empty `NEXT_PUBLIC_API_URL` at build time.

With `sandbox.provider: docker`, the Sandbox Service mounts `/var/run/docker.sock`, stays at one replica, and must run on a dedicated Docker worker. This is a privileged node capability, is incompatible with the Kubernetes Restricted Pod Security profile, and does not provide cross-node migration for user Docker volumes.

With `sandbox.provider: acs`, no Docker Socket is mounted and `sandbox.replicas` controls the stateless Sandbox Service replicas. Configure `sandbox.acs.domain`, protocol, template, and `ACS_SANDBOX_API_KEY`. Native routing is the production default and requires wildcard DNS/TLS. Private routing uses one domain with `/kruise` paths and is intended for internal/test integration. Set `sandbox.acs.sandboxSet.enabled` when this release should create the warm-pool `SandboxSet`; otherwise provision the named template separately. The runtime image must contain `/bin/bash`, `cp`, `mv`, and `mkdir`.

The Sandbox Service is ClusterIP-only in either mode and its private APIs require `SANDBOX_SERVICE_TOKEN`. There is currently no automatic workspace migration between providers.

For an externally managed Secret, set `secrets.existingSecret`. It must contain:

- `DATABASE_URL`
- `REDIS_URL`
- `MASTER_KEY_BASE64`
- `SANDBOX_SERVICE_TOKEN`
- `OBJECT_STORE_ACCESS_KEY`
- `OBJECT_STORE_SECRET_KEY`

ACS deployments additionally require:

- `ACS_SANDBOX_API_KEY`


## Public HTML artifacts

Build and publish `backend/Dockerfile.artifact-host`, then configure `images.artifactHost` alongside the API/Web images. The chart creates a separate Deployment and ClusterIP Service on port 8082. Set `config.artifactPublicURL` to the public HTTPS origin and enable `artifactIngress` with a matching host/TLS configuration. Use a different hostname from `config.webOrigin`, preferably a separate registrable domain. Never route this service under the application's origin.

Artifact Host receives database and object-store credentials only. In production, use dedicated read-only database/object-store credentials by adapting its Secret references. The bucket stays private; only published manifest entries are served. Preserve the service's CSP, CORS and no-store headers at the ingress/CDN. NetworkPolicy continues to allow only API to reach Sandbox Service.

Apply `000006_projects_artifacts.up.sql` once to existing databases after earlier migrations and before upgrading API. Back up PostgreSQL and the bucket together. Superseded deployment objects are retained; automatic garbage collection is not implemented.

## App connections

Apply migration 032 after all earlier migrations, preserving the existing encryption key and volumes, then rebuild API/Web together. See [provider setup and scopes](../../../docs/app-connections.md).

Set `config.appConnectors.githubClientID`, `gmailClientID`, `xClientID`, `feishuClientID`, `slackClientID` and the corresponding `secrets.connectorGithubClientSecret`, `connectorGmailClientSecret`, `connectorXClientSecret`, `connectorFeishuClientSecret`, `connectorSlackClientSecret`. With `secrets.existingSecret`, supply the enabled provider keys `CONNECTOR_GITHUB_CLIENT_SECRET`, `CONNECTOR_GMAIL_CLIENT_SECRET`, `CONNECTOR_X_CLIENT_SECRET`, `CONNECTOR_FEISHU_CLIENT_SECRET`, `CONNECTOR_SLACK_CLIENT_SECRET` in that Secret. Missing pairs keep the provider disabled. Client IDs belong in the ConfigMap; secrets are API-only, independent of sign-in OAuth settings. Register the exact HTTPS `config.webOrigin` + `/api/v1/connectors/{provider}/callback` at each provider. These user grants enable task searches/reads only; provider review, installations and API plan requirements still apply.

## GitHub code tasks and dependency access

Apply migration 033, retain the master key and volumes, then update API/Web/Sandbox Service together. See [GitHub code task setup](../../../docs/github-code-tasks.md) for GitHub App Contents/PR write permissions and per-user code consent. Ordinary app connections remain read-only. Publications require a single explicit task-submitter approval even in full-access mode.

`sandbox.dockerNetwork` defaults to `none`. An administrator may choose a pre-existing dedicated Docker network for new Computers, with independently enforced egress controls, or explicitly use `bridge`. This is a network selector, not an allowlist/firewall/proxy. `host` and container namespaces are refused; old Computers are not silently replaced. ACS uses its own template/tenant network controls. Docker updates the native helper through Sandbox Service; ACS/custom runtime upgrades must preserve workspace data and provide Git plus the new Toolbox commands.
