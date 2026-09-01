# Lester Helm Chart

This chart deploys Lester Web, API, and the Docker-based Sandbox Service. It expects external PostgreSQL, Redis, and S3-compatible object storage.

Before installation:

1. Build and push the three repository images.
2. Apply `backend/migrations/*.up.sql` to PostgreSQL in numeric order.
3. Create a dedicated Kubernetes worker with Docker Engine and label it.
4. Copy `values.yaml` to a private values file and configure images, service URLs, secrets, Ingress, and `sandbox.nodeSelector`.

Install with:

```bash
helm upgrade --install lester deploy/helm/lester \
  --namespace lester --create-namespace \
  -f values.production.yaml
```

The supported Ingress layout is same-origin: `/api` routes to the API Service and `/` routes to Web. The frontend image therefore uses an empty `NEXT_PUBLIC_API_URL` at build time.

The Sandbox Service mounts `/var/run/docker.sock`. Keep it at the chart-defined single replica on a dedicated Docker worker. This is a privileged node capability, is incompatible with the Kubernetes Restricted Pod Security profile, and does not provide cross-node migration for user Docker volumes. The Service is ClusterIP-only and its private APIs also require `SANDBOX_SERVICE_TOKEN`.

For an externally managed Secret, set `secrets.existingSecret`. It must contain:

- `DATABASE_URL`
- `REDIS_URL`
- `MASTER_KEY_BASE64`
- `SANDBOX_SERVICE_TOKEN`
- `OBJECT_STORE_ACCESS_KEY`
- `OBJECT_STORE_SECRET_KEY`

