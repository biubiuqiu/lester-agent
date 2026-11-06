# OpenTelemetry observability

[Documentation](README.md) · [简体中文](zh-CN/observability.md)

Lester's Go API, Sandbox Service and Artifact Host can export traces and metrics through OTLP to your OpenTelemetry Collector or compatible backend. Their default service names are `lester-api`, `lester-sandbox-service` and `lester-artifact-host`. JSON application logs remain on stdout; logs with an active span include `trace_id` and `span_id`. This is log correlation, not OTLP log export.

Each process also gets a random `service.instance.id` so replicas and restarts do not merge their metric streams. Operators may override it with `OTEL_RESOURCE_ATTRIBUTES`, for example with a unique Pod UID.

## What is recorded

- HTTP route templates, methods, response status and request duration. Streaming request duration includes the stream's lifetime. Raw URLs, query strings, headers and request/response bodies are not collected.
- Agent execution, model calls and tool execution, including bounded operation metadata, duration and outcome. Internal API-to-Sandbox Service calls propagate W3C trace context so these operations can be followed across services.
- Aggregate HTTP and execution metrics. User, workspace, conversation and run identifiers must not become metric labels.

Prompt text, model replies, memory facts, tool arguments, commands, file paths/content, credentials and raw error messages are excluded. Error outcomes use fixed descriptions. `baggage` is not propagated. Resource attributes and collector credentials are operator configuration: do not put secrets or personal data in resource attributes.

Tracing does not persist `traceparent` in tasks or queues. A task dispatched later by a scheduler, restored from a durable queue, or resumed after a restart can begin a new trace; the feature does not claim one uninterrupted original-user trace across those boundaries. Browser rendering, third-party model internals and processes inside a user's Computer are not automatically instrumented.

## Connect an existing collector

No endpoint and no explicit `OTEL_SDK_DISABLED=false` leaves the SDK disabled. Setting `OTEL_SDK_DISABLED=true` always disables it. Setting an endpoint, or explicitly setting the flag to `false`, enables configured signals. Setting both signal exporters to `none` disables export.

For Compose, add the following to your private `deploy/.env`, retaining existing credentials and data:

```dotenv
OTEL_EXPORTER_OTLP_ENDPOINT=https://collector.example.com:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_RESOURCE_ATTRIBUTES=deployment.environment.name=production
# Optional collector authorization; do not commit it or put it in a URL.
OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer%20YOUR_COLLECTOR_TOKEN
```

Then recreate the affected services from the repository root:

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --build api sandbox-service artifact-host
```

Use an HTTP(S) URL without userinfo, query or fragment. Authentication belongs in headers, not `https://user:password@...` or URL parameters. The general HTTP endpoint is a base URL; the SDK appends `/v1/traces` and `/v1/metrics`. A signal-specific HTTP endpoint is a full URL, including its signal path. For a gRPC collector, select `grpc` and an HTTP(S) endpoint such as `http://collector:4317`; the bundled example below receives HTTP only. HTTPS validates certificates normally. Standard SDK TLS/CA variables can be supplied through your own container environment and certificate mounts; paths must exist inside the container.

| Variable | Purpose |
| --- | --- |
| `OTEL_SDK_DISABLED` | Empty/unset uses endpoint-based opt-in; `true` always disables; `false` explicitly enables. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Shared collector endpoint. |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` | Signal-specific endpoints override the shared endpoint. |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` (Lester default) or `grpc`. |
| `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL`, `OTEL_EXPORTER_OTLP_METRICS_PROTOCOL` | Optional per-signal protocols. |
| `OTEL_EXPORTER_OTLP_HEADERS`, `OTEL_EXPORTER_OTLP_TRACES_HEADERS`, `OTEL_EXPORTER_OTLP_METRICS_HEADERS` | Collector authentication, using standard comma-separated, URL-encoded OTLP header values. Signal-specific values take precedence. |
| `OTEL_TRACES_EXPORTER`, `OTEL_METRICS_EXPORTER` | `otlp` or `none`; default `otlp`. |
| `OTEL_RESOURCE_ATTRIBUTES` | Operator-supplied service/environment attributes, without secrets. |
| `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG` | Default `parentbased_always_on`; use `parentbased_traceidratio` and e.g. `0.1` to sample 10% of new root traces. Existing parent sampling is respected; metrics are independent. |
| `OTEL_METRIC_EXPORT_INTERVAL` | Periodic metric export interval in milliseconds; default `60000`. |
| `OTEL_SERVICE_NAME` | Service identity when running a binary directly. Compose maps separate `OTEL_API_SERVICE_NAME`, `OTEL_SANDBOX_SERVICE_NAME` and `OTEL_ARTIFACT_SERVICE_NAME` values to this standard variable. |

Initialization validates configuration. A bad configuration is reported without echoing collector credentials. A temporarily unavailable exporter does not fail an Agent task; bounded SDK queues may drop telemetry. Export failures produce a rate-limited generic warning rather than raw endpoint/provider errors. Normal service shutdown flushes telemetry within a bounded timeout; abrupt termination can lose buffered data. Rebuild the three Go services; no database migration is required.

## Optional local receipt check

The optional override starts a pinned Collector with an HTTP receiver and a basic debug exporter. It has no published host port, persistent storage or dashboard, and is not a dependency of the default deployment. It changes neither user Computers nor their network policy.

```bash
docker compose --env-file deploy/.env \
  -f deploy/docker-compose.yaml -f deploy/docker-compose.otel.yaml \
  up -d --build api sandbox-service artifact-host otel-collector
# Open Lester and run a small task, then wait for the metric export interval.
docker compose --env-file deploy/.env \
  -f deploy/docker-compose.yaml -f deploy/docker-compose.otel.yaml \
  logs --tail=100 otel-collector
```

The override directs all three Go services to `http://otel-collector:4318`; explicit `OTEL_SDK_DISABLED=true` still wins. It clears signal-specific destination/protocol overrides and collector authentication so production credentials are not sent to the local receiver; explicit signal exporters set to `none` remain disabled. Collector logs show received trace and metric batch counts; this is receipt evidence, not a persistent tracing UI or evidence that a real model/provider succeeded. For production, configure your collector's exporters and retention in your existing observability platform.

To return to your own collector, reapply the base Compose file with your intended endpoint settings. To switch off collection, set `OTEL_SDK_DISABLED=true` and recreate the three services. Stop the optional collector separately if no longer needed; do not delete application volumes.

## Kubernetes / Helm

Set `config.telemetry` in your private values file:

```yaml
config:
  telemetry:
    endpoint: https://collector.example.com:4318
    protocol: http/protobuf
    resourceAttributes: deployment.environment.name=production
    tracesSampler: parentbased_traceidratio
    tracesSamplerArg: "0.1"
secrets:
  otelHeaders: Authorization=Bearer%20YOUR_COLLECTOR_TOKEN
```

The chart gives each Go service its own name and selects only its telemetry settings and optional collector headers. Headers go in Secret keys `OTEL_EXPORTER_OTLP_HEADERS`, `OTEL_EXPORTER_OTLP_TRACES_HEADERS` and `OTEL_EXPORTER_OTLP_METRICS_HEADERS`, never the ConfigMap. With `secrets.existingSecret`, these keys are optional; populate the required ones when collector authentication is enabled. The chart does not deploy a Collector or change egress policy. Ensure your cluster permits the Go services to reach your chosen endpoint, and restart workloads after an externally managed Secret changes. See the [Helm reference](../deploy/helm/lester/README.md).
