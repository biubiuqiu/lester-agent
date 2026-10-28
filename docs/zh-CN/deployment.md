# 部署与升级

[文档目录](README.md) · [项目首页](../../README.zh-CN.md) · [English](../deployment.md)

升级检查、统一网关、Docker/ACS 与 Kubernetes/Helm。

## 已有部署升级检查

1. 备份 PostgreSQL、对象存储和 Computer 数据，保留 `.env`、加密密钥与持久化数据卷。
2. 停止活跃任务和 API 写入，确认已经应用的迁移编号。
3. 按编号依次执行**尚未应用的全部迁移，直到 034**，每个迁移只执行一次。新数据库初始化不会自动升级旧数据卷；其他章节按功能提供的 SQL 示例只是历史步骤，不能代替完整升级。
4. 同步构建和部署匹配版本的 API、Web 与 Sandbox Service，不能通过删除数据卷升级。

全新安装请看[快速开始](getting-started.md)；事务执行 SQL 的方法见[账号升级](accounts-and-security.md#账号第三方登录与头像)和[上下文升级](architecture.md#已有部署升级)。完成全部缺失迁移后再重启最新服务。

## 本章目录

- [统一网关与部署配置](#统一网关与部署配置)
- [Kubernetes / Helm 部署](#kubernetes--helm-部署)
- [运行可靠性升级](#运行可靠性升级)

## 统一网关与部署配置

Compose 使用轻量 Nginx 网关：`/api` 和 `/api/*` 保留原路径转发到 API，其余请求转发到 Web。浏览器只访问一个地址，登录 Cookie、SSE、终端 WebSocket、附件和 HTML 预览均经过该入口。网关不实现业务鉴权、不直接读取用户文件，也不公开 Sandbox Service；预览仍由 API 鉴权并返回原有 CSP。

- 默认入口为 `http://localhost:13000`。如果端口被占用或被 Windows 保留，可在 `deploy/.env` 同时设置 `GATEWAY_PORT=13080` 和 `WEB_ORIGIN=http://localhost:13080`。远程访问时将 `WEB_ORIGIN` 改为用户实际访问的源（协议、域名、端口），否则终端的 Origin 校验会拒绝连接。
- Web 镜像在构建时固定使用空 `NEXT_PUBLIC_API_URL`（同源）；旧 `.env` 中该变量不再影响 Compose。升级必须重新构建 Web，单改运行时环境变量不能修改已经打包的浏览器代码。独立前端开发仍可自行设置该构建变量。
- SSE 禁用代理缓冲和缓存；终端支持 WebSocket Upgrade。API 代理读超时为 **1 小时空闲时间**，不是 Agent 的运行时限，SSE 心跳会保持连接。请求失败时网关不会自动重放 API 请求。
- 网关允许最多 26 MiB 请求体；API 继续限制单文件 25 MiB（余量用于 multipart 封装）。`/healthz` 只检查网关存活，不代表数据库或上游服务就绪。
- 默认仅公开网关端口。临时排查可追加 `-f deploy/docker-compose.debug.yaml`（或 `make dev-debug`），将 API `18080`、MinIO `9000/9001` 仅绑定到 `127.0.0.1`；浏览器仍走网关。排查结束用默认 Compose 重新 `up -d` 收回这些端口。
- 默认入口为 HTTP，未内置证书申请。公网部署应配置 HTTPS 并设置 `SESSION_COOKIE_SECURE=true`。可在 Nginx 配置中增加 TLS listener、挂载证书并发布对应端口；如果由外层负载均衡终止 TLS，应让网关只接受该可信代理的访问，再配置正确的外部协议转发，不能直接信任公网传入的 `X-Forwarded-Proto`。当前配置主动覆盖该头为本层协议。

已有部署升级网关无需新数据库迁移；保留原 `.env` 密钥和数据卷，更新 `WEB_ORIGIN` 后运行完整的 `docker compose ... up -d --build`，不要只重启 API，也不要删除数据卷。移除旧覆盖文件中 Web/API 的端口映射；自定义入口端口应配置在 gateway，而不是 web。

代理回归测试使用独立 Compose 项目，不访问应用数据、不开宿主机端口：

```bash
make gateway-check
# 如果测试失败，清理测试容器（不涉及 Lester 的数据卷）：
docker compose -p lester-gateway-test -f deploy/gateway/compose.test.yaml down
```

测试覆盖路由与转义路径、Cookie/请求头、私有预览 CSP、25 MiB 上传、SSE 首帧和双向 WebSocket；CI 自动执行同一测试。

## Kubernetes / Helm 部署

Helm Chart 位于 `deploy/helm/lester`，部署 Web、API、Sandbox Service、ClusterIP Service、可选 Ingress 和 NetworkPolicy。PostgreSQL、Redis、S3 兼容对象存储由集群外部提供；安装前需按编号执行 `backend/migrations/*.up.sql`。

先构建并推送 Web、API 和 Sandbox Service 三个镜像，然后准备私有 values（不要提交真实密钥）。ACS 若不使用已有运行镜像，还需用 `backend/Dockerfile.sandbox-runtime` 构建并推送 Sandbox 运行镜像：

```bash
docker build -f backend/Dockerfile.sandbox-runtime -t registry.example.com/lester-sandbox-runtime:v1 backend
docker push registry.example.com/lester-sandbox-runtime:v1
```

该镜像提供上述完整开发环境、Playwright / Chromium 及 `lester-toolbox`，并满足 ACS Agent Runtime 对 `/bin/bash`、`cp`、`mv`、`mkdir` 的要求。Docker 专用节点也使用此镜像；Compose 在启动 Sandbox Service 前自动构建它。

基础 values 示例：

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

Ingress 使用同源路由：`/api` 转发到 API，其余请求转发到 Web，因此 Helm 镜像构建时无需设置 `NEXT_PUBLIC_API_URL`。Kubernetes 继续直接使用 Ingress，不重复部署 Compose 的 Nginx。按所选 Ingress Controller 的配置方式关闭 SSE 缓冲、支持 WebSocket、放宽空闲超时和上传大小，并正确转发外部协议；具体参数不跨 Controller 通用。

Docker 是默认 Provider。Sandbox Service 固定一个副本，并通过 `sandbox.nodeSelector` 放到提供 Docker Engine 和 `/var/run/docker.sock` 的专用 Worker；Docker Socket 等同于很高的节点权限，不符合 Restricted Pod Security，且用户 Volume 属于该节点。

在 ACS 集群上可以切换为 Agent Sandbox Provider：

```yaml
secrets:
  # 与 ack-sandbox-manager 的 adminApiKey 一致
  acsSandboxAPIKey: ...

sandbox:
  provider: acs
  replicas: 2
  acs:
    domain: sandbox.example.com
    protocol: native # 生产推荐；private 适合单域名内网/测试
    template: lester-agent
    secure: true
    autoPause: true
    sandboxSet:
      enabled: true
      replicas: 4
      image: registry.example.com/lester-sandbox-runtime:v1
```

ACS 模式不挂载 Docker Socket，Sandbox Service 可以多副本运行。Chart 可选创建与 `sandbox.acs.template` 同名的 `SandboxSet` 预热池；也可以关闭 `sandboxSet.enabled` 并使用集群中已有模板。运行镜像至少需要 `/bin/bash` 以及 `cp`、`mv`、`mkdir`；仓库自带镜像默认使用无特权 `sandbox` 用户。部署前按[阿里云 E2B 接入文档](https://help.aliyun.com/zh/cs/user-guide/connect-to-agent-sandbox-using-the-e2b-sdk)安装/升级 `ack-agent-sandbox-controller` 与 `ack-sandbox-manager` 并配置域名、TLS 和 API Key。Native 需要泛域名 DNS/TLS；Private 使用单域名 `/kruise` 路由。

若使用 `secrets.existingSecret`，它必须包含 `DATABASE_URL`、`REDIS_URL`、`MASTER_KEY_BASE64`、`SANDBOX_SERVICE_TOKEN`、`OBJECT_STORE_ACCESS_KEY`、`OBJECT_STORE_SECRET_KEY`；ACS 模式还必须包含 `ACS_SANDBOX_API_KEY`。切换 Provider 会为用户创建目标 Provider 的新 Computer，当前不会自动迁移旧 Provider 中的文件，正式切换前应另行备份或迁移工作区。

## 运行可靠性升级

发送使用稳定的提交 ID，避免响应丢失后重复创建任务。消息或停止请求已受理时，后续刷新失败只提示同步异常；附件重试会复用已成功的上传。运行中的任务固定使用已选模型。覆盖文件需完整读取，局部编辑需读取未截断的目标范围，原生写入检查读取时的 SHA-256；历史正文移出上下文后需重读。原生 helper 写操作共用锁，任意 Bash 写入不参与该锁。

命令最多保留 64 MiB 原始合流日志，并明确标注超出的字节。非零退出、超时、取消后的局部输出和未确认清理都有实际状态。后台命令独立于 Agent 结束持续核对；活跃或未知任务阻止空闲暂停。启动响应丢失后只检查原任务，进程身份未知时不盲目重启或发送停止信号。新 Docker Computer 使用 `--init`，已有容器不会自动替换。ACS 需重建包含文件协议 2、任务协议 1 的模板，并部署匹配的 Sandbox Service helper；本地 ACS 契约测试不代表真实云端验收。

模型列表支持明确发起一次短文本验证；保存配置不等于 Provider 调用可用，文本成功也不代表工具与附件验证通过。共享模型允许成员验证，预算与凭证仍由管理员管理。展开“上下文预算”配置已知窗口、输出预留及本地策略；0 表示未知/自动/默认 64k 策略，不虚构模型上限，也不限制普通回复长度。每次请求记录完整请求估算和供应商实际/缺失用量，缓存只计算一次。

长会话现在会保存当前会话的历史检查点，完整有序记录保留。最新用户输入、最新助手消息、整批未观察工具结果、未解决的失败和有效 Skill 正文保持原文。Skill 带来源路径与 SHA-256 版本，旧加载正文成为可回读的历史引用。文件正文不再可见时必须重新读取才能编辑；后台任务事实从平台记录补回，明确属于缓存观察，而不是摘要推测。

摘要是独立的无工具请求，有单独的用量记录与输出限制；校验通过且整个请求确实缩小后，才原子保存检查点和事件。格式错误、取消或无效缩小保留旧检查点，并停止本轮自动整理。每轮最多尝试整理两次；Provider 返回上下文过长时，只在尚无输出的情况下恢复一次。普通模型/工具循环仍不限制次数。受保护内容无法安全容纳时明确停止，不重放工具、不截断最新输入；下一次任务会恢复检查点，切换模型后重新估算完整请求。过程协作和文件审阅的剩余目标见[对标实施记录](../claude-code-alignment.md)。

已有数据库完成 001–015 后，先备份、停止 API 写入，按顺序执行一次 016–021，再启动最新版服务；更早的数据库先补齐缺失迁移。保留密钥、文件与数据卷。回滚 018 前先停止命令任务；数据库回滚不会撤销文件修改或外部操作。

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api
for migration in 000016_run_submissions 000017_model_budget 000018_command_tasks 000019_model_request_usage 000020_context_checkpoints 000021_run_interactions 000022_run_inputs 000023_file_changes 000024_project_rules 000025_run_permissions 000026_cloud_task_indexes 000027_automations 000028_permission_reviews 000029_conversational_schedules 000030_native_signin 000031_personal_assistant 000032_app_connections 000033_github_code_workflows 000034_speech_input; do
  docker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < "backend/migrations/${migration}.up.sql" || exit 1
done
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --build api web sandbox-service
```

已完成 001–019 的部署补齐 020、021，勿重复旧迁移。回滚 020 只删除摘要检查点，原始消息和文件保留。

---

[← 架构与 Computer](architecture.md) · [账号、管理与安全 →](accounts-and-security.md)

应用 OAuth 配置与迁移 032：[应用连接](app-connections.md)。
