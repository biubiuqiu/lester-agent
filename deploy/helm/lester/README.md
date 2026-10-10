# Lester Helm Chart

This chart deploys Lester Web, API, and a provider-neutral Sandbox Service. It expects external PostgreSQL, Redis, and S3-compatible object storage. Computers may run on a local Docker worker (default) or Alibaba Cloud ACS Agent Sandbox.

Before installation:

1. Build and push the three Lester service images and `backend/Dockerfile.sandbox-runtime` (or provide another compatible Computer runtime). The bundled runtime includes Node.js/npm/pnpm, Python/pip/venv, Go, native build tools, Node/Python Playwright and predownloaded Chromium with CJK fonts. Set `sandbox.image` to the image available on the dedicated Docker worker; ACS uses `sandbox.acs.sandboxSet.image` or an externally provisioned template. Run `make sandbox-check` before publishing the image. Existing Computers keep their image; plan migrations with persistent data and UID/GID 1000 ownership preserved.
2. Apply `backend/migrations/*.up.sql` (001–015) to PostgreSQL in numeric order. Existing installations already on 013 must apply 014 and then `000015_rotating_tokens.up.sql` once before upgrading API/Web; installations on 014 need only 015. Migration 015 invalidates legacy login credentials and requires one sign-in, preserving accounts/workspaces/files. Access cookies expire in 2 hours; rotating refresh cookies last a rolling 30 days with no absolute active-session cutoff. Rollback 015 also signs everyone out and requires a compatible API. Migration 014 preserves existing accounts as opted out of the automatic welcome; new accounts get the first-use guide. Rollback removes tutorial progress only. Back up first and retain all existing credentials/data.
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
