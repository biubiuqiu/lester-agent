# OpenTelemetry 可观测性

[文档目录](README.md) · [English](../observability.md)

Lester 的 Go API、Sandbox Service 和 Artifact Host 支持通过 OTLP 向 OpenTelemetry Collector 或兼容平台导出追踪与指标。三个服务默认分别叫 `lester-api`、`lester-sandbox-service`、`lester-artifact-host`。应用继续向 stdout 输出 JSON 日志；存在活动 Span 时附带 `trace_id`、`span_id` 以便关联。这是日志关联，不是 OTLP 日志导出。

每个进程还会生成随机的 `service.instance.id`，区分不同副本和重启后的指标流。也可通过 `OTEL_RESOURCE_ATTRIBUTES` 覆盖，例如填写唯一的 Pod UID。

## 采集范围

- HTTP 路由模板、方法、响应状态与请求耗时。流式请求的耗时包含整个连接生命周期；不采集原始 URL、查询参数、请求头或请求／响应正文。
- Agent 执行、模型调用与工具执行的有限操作元数据、耗时与结果。API 调用内部 Sandbox Service 时传递 W3C 追踪上下文，可跨服务查看链路。
- HTTP 与执行的聚合指标；用户、工作区、会话和任务 ID 不作为指标标签。

不采集提示词、模型回复、记忆内容、工具参数、命令、文件路径／正文、凭证和原始错误文字；错误结果使用固定描述，不传递 `baggage`。资源属性和 Collector 凭证由部署者配置，请勿将秘密或个人信息写入资源属性。

任务和队列不会持久化 `traceparent`。定时器稍后分派、从持久队列恢复、服务重启后继续的任务可能开启新 Trace，不保证跨越这些边界仍属于最初用户请求的同一条链路。浏览器渲染、第三方模型服务内部和用户 Computer 中的进程不会自动获得埋点。

## 接入已有 Collector

未配置端点且未明确设置 `OTEL_SDK_DISABLED=false` 时，SDK 默认关闭；`OTEL_SDK_DISABLED=true` 始终关闭。配置端点或显式设置 `false` 会启用已配置的信号；两个信号的 exporter 都为 `none` 时不导出。

Compose 用户在私有 `deploy/.env` 中添加以下配置，保留现有凭证和数据：

```dotenv
OTEL_EXPORTER_OTLP_ENDPOINT=https://collector.example.com:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_RESOURCE_ATTRIBUTES=deployment.environment.name=production
# 可选 Collector 认证；不要提交凭证，也不要把它放进 URL。
OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer%20YOUR_COLLECTOR_TOKEN
```

从仓库根目录重建并重新创建相关服务：

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --build api sandbox-service artifact-host
```

端点必须是 HTTP(S) URL，不能包含用户信息、查询参数或片段。认证放在请求头中，不使用 `https://user:password@...` 或 URL 参数。通用 HTTP 端点填写基础地址，由 SDK 拼接 `/v1/traces`、`/v1/metrics`；单信号 HTTP 端点填写含对应路径的完整 URL。使用 gRPC Collector 时选择 `grpc`，例如 `http://collector:4317`；下方内置示例只接收 HTTP。HTTPS 正常校验证书；可通过自定义容器环境和证书挂载提供标准 SDK TLS／CA 变量，证书路径必须在容器内存在。

| 变量 | 用途 |
| --- | --- |
| `OTEL_SDK_DISABLED` | 空／未设置时按端点启用；`true` 始终关闭；`false` 显式启用。 |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | 通用 Collector 地址。 |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`、`OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` | 单信号端点，优先于通用端点。 |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf`（Lester 默认）或 `grpc`。 |
| `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL`、`OTEL_EXPORTER_OTLP_METRICS_PROTOCOL` | 可选的单信号协议。 |
| `OTEL_EXPORTER_OTLP_HEADERS`、`OTEL_EXPORTER_OTLP_TRACES_HEADERS`、`OTEL_EXPORTER_OTLP_METRICS_HEADERS` | Collector 认证，使用标准 OTLP 逗号分隔、URL 编码的请求头值；单信号配置优先。 |
| `OTEL_TRACES_EXPORTER`、`OTEL_METRICS_EXPORTER` | `otlp` 或 `none`，默认 `otlp`。 |
| `OTEL_RESOURCE_ATTRIBUTES` | 部署方提供的服务／环境属性，不放秘密。 |
| `OTEL_TRACES_SAMPLER`、`OTEL_TRACES_SAMPLER_ARG` | 默认 `parentbased_always_on`；可设为 `parentbased_traceidratio` 和 `0.1`，对新根 Trace 采样 10%。继承已有父级采样决定，指标不依赖追踪采样。 |
| `OTEL_METRIC_EXPORT_INTERVAL` | 指标周期导出间隔，单位毫秒，默认 `60000`。 |
| `OTEL_SERVICE_NAME` | 直接运行二进制时的服务名。Compose 分别将 `OTEL_API_SERVICE_NAME`、`OTEL_SANDBOX_SERVICE_NAME`、`OTEL_ARTIFACT_SERVICE_NAME` 映射到该标准变量。 |

初始化时会校验配置，错误不会回显 Collector 凭证。Exporter 临时不可达不会使 Agent 任务失败；SDK 使用有界队列，遥测数据可能丢弃。导出故障只产生限频的通用警告，不输出原始端点／服务商错误。正常退出会在有界超时内刷新缓冲，强制终止仍可能丢失数据。需要重建三个 Go 服务，不需要数据库迁移。

## 可选的本地接收验证

独立 override 会启动固定版本的 Collector，使用 HTTP receiver 和 basic debug exporter。它不映射宿主端口、不保存历史、不提供看板，也不是默认部署的强制依赖；不会修改用户 Computer 或其网络策略。

```bash
docker compose --env-file deploy/.env \
  -f deploy/docker-compose.yaml -f deploy/docker-compose.otel.yaml \
  up -d --build api sandbox-service artifact-host otel-collector
# 在 Lester 中运行一个小任务，等待指标导出周期后查看接收日志。
docker compose --env-file deploy/.env \
  -f deploy/docker-compose.yaml -f deploy/docker-compose.otel.yaml \
  logs --tail=100 otel-collector
```

Override 将三个 Go 服务指向 `http://otel-collector:4318`，但显式 `OTEL_SDK_DISABLED=true` 仍优先。它会清除单信号目的地址／协议覆盖与 Collector 认证，避免把生产凭证发给本地接收端；明确设为 `none` 的信号仍保持关闭。Collector 日志显示收到的 Trace、Metric 批次数量，只证明接收成功，不是持久追踪界面，也不证明真实模型／服务商已成功。生产环境应在自己的 Collector 中配置导出目标和保留策略。

切回已有 Collector 时，使用所需端点配置重新应用基础 Compose 文件。关闭采集时设置 `OTEL_SDK_DISABLED=true`，重新创建三个服务；不再需要的可选 Collector 单独停止，不删除应用数据卷。

## Kubernetes／Helm

在私有 values 中配置 `config.telemetry`：

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

Chart 为三个 Go 服务分别设置名称，并只选择相应遥测配置与可选 Collector 请求头。请求头保存在 Secret 的 `OTEL_EXPORTER_OTLP_HEADERS`、`OTEL_EXPORTER_OTLP_TRACES_HEADERS`、`OTEL_EXPORTER_OTLP_METRICS_HEADERS` 键，不进入 ConfigMap。使用 `secrets.existingSecret` 时这三个键可缺省，启用 Collector 认证时补充所需键。Chart 不部署 Collector，也不修改出网策略；请允许 Go 服务访问所选端点，并在外部管理的 Secret 改变后重启工作负载。详见 [Helm 参考](../../deploy/helm/lester/README.md)。
