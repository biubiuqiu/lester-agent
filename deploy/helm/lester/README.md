# Lester Helm Chart

This chart deploys Lester Web, API, and a provider-neutral Sandbox Service. It expects external PostgreSQL, Redis, and S3-compatible object storage. Computers may run on a local Docker worker (default) or Alibaba Cloud ACS Agent Sandbox.

Before installation:

1. Build and push the three Lester service images. For ACS, also build `backend/Dockerfile.sandbox-runtime` or provide another compatible runtime image.
2. Apply `backend/migrations/*.up.sql` to PostgreSQL in numeric order.
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

