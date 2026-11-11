import type { SiteDocSection } from "./types";
import { T } from "@/components/i18n";
import Link from "next/link";
import { CodeBlock } from "@/components/site/code-block";
import { Note } from "./shared";
import { repositoryURL } from "../site";

export const sections: SiteDocSection[] = [
{ id: "compose", title: "Docker Compose 部署", content: <>
        <p><T>{"首次部署请先完成"}</T><Link href="/docs"><T>{"快速开始"}</T></Link><T>{"。Compose 会启动 Gateway、Web、API、PostgreSQL、Redis、对象存储、Sandbox Service 与独立 Artifact Host。"}</T></p>
        <CodeBlock label="查看服务状态">{"docker compose --env-file deploy/.env -f deploy/docker-compose.yaml ps"}</CodeBlock>
        <p><T>{"日常重启可以使用"}</T><code>up -d</code><T>{"。拉取代码并完成升级步骤后，用"}</T><code>up -d --build</code> <T>{"构建新版服务。只更新源码不会更新已有运行容器。"}</T></p>
      </> },
{ id: "runtime", title: "沙盒开发环境", content: <>
        <p><T>{"Compose 会先构建完整沙盒镜像，供新建的 Computer 使用。镜像预装 Node.js、npm、pnpm、Python、pip、venv、Go 和常用编译工具。Node.js 与 Python 的 Playwright 共用预下载的 Chromium，并包含中日韩字体。"}</T></p>
        <p><T>{"Agent 可通过命令工具直接运行 Node.js 或 Python 的 Playwright 脚本，默认无头运行，无需安装浏览器 Skill。截图和下载文件保存在当前会话目录，操作结果以实际页面为准；旧 Computer 或自定义镜像需先检查环境。"}</T></p>
        <CodeBlock label="运行环境检查">{"make sandbox-check\n# deploy/.env\nSANDBOX_IMAGE=lester-sandbox-runtime:local"}</CodeBlock>
        <p><T>{"Docker Computer 默认禁用外网。预装浏览器可以测试本地页面；安装新依赖或访问外部网站仍需要部署侧的网络配置。"}</T></p>
        <Note><T>{"重建镜像不会升级已有 Computer，也不会删除工作区。旧配置需更新 SANDBOX_IMAGE；现有 Computer 请由管理员保留卷并安排迁移，检查非 root 用户的文件权限。ACS 需要推送新镜像并更新模板。"}</T></Note>
      </> },
{ id: "address", title: "配置应用访问地址", content: <>
        <p><T>{"默认应用端口为"}</T><code>13000</code><T>{"。端口占用或 Windows 保留端口导致启动失败时，选择可用端口，并同时修改两个变量："}</T></p>
        <CodeBlock label="应用地址示例">{"GATEWAY_PORT=13280\nWEB_ORIGIN=http://localhost:13280"}</CodeBlock>
        <p><T>{"远程访问时，设置为实际公开地址，并使用 HTTPS 入口。更新配置后重新启动完整 Compose 栈。不要把 Sandbox Service 或 Docker Socket 暴露为公网服务。"}</T></p>
      </> },
{ id: "authentication", title: "配置第三方登录与邮件", content: <>
        <p><T>{"在"}</T><code>deploy/.env</code> <T>{"成对填写"}</T><code>GOOGLE_OAUTH_CLIENT_ID</code> / <code>GOOGLE_OAUTH_CLIENT_SECRET</code><T>{"，或"}</T><code>GITHUB_OAUTH_CLIENT_ID</code> / <code>GITHUB_OAUTH_CLIENT_SECRET</code><T>{"。空的变量对隐藏对应按钮；配置只填一项会拒绝启动。"}</T></p>
        <CodeBlock label="OAuth 回调地址">{"<WEB_ORIGIN>/api/v1/auth/oauth/google/callback\n<WEB_ORIGIN>/api/v1/auth/oauth/github/callback"}</CodeBlock>
        <p><T>{"将"}</T><code>&lt;WEB_ORIGIN&gt;</code> <T>{"替换为真实应用地址。Google 创建 Web application 客户端并配置授权页面、测试用户或正式发布；GitHub 创建对应环境的 OAuth App。授权回调需与配置完全一致，并经同源 Gateway / Ingress 转发到 API。"}</T></p>
        <p><T>{"生产环境使用 HTTPS，并设置"}</T><code>SESSION_COOKIE_SECURE=true</code><T>{"；HTTP 仅限 localhost 开发。密钥保存在服务端，API 需能访问 Google / GitHub 的 HTTPS 接口。"}</T></p>
        <p><T>{"代理部署设置"}</T><code>AUTH_TRUSTED_PROXY_CIDRS</code> <T>{"为实际网关 / Ingress 的可信 IP 范围（逗号分隔 CIDR，禁止 /0）；Helm 对应"}</T><code>config.auth.trustedProxyCIDRs</code><T>{"。默认留空会忽略转发头，同一网关后的用户共用网关 IP 的限流桶。Redis 故障时暂停受限认证操作，恢复后重试。"}</T></p>
        <CodeBlock label="SMTP 配置示例">{"SMTP_HOST=smtp.example.com\nSMTP_PORT=587\nSMTP_TLS_MODE=starttls\nSMTP_USERNAME=your-mail-account\nSMTP_PASSWORD=your-mail-password\nSMTP_FROM=no-reply@example.com"}</CodeBlock>
        <p><T>{"替换为邮件服务实际参数并使用已授权的发件地址。465 端口可用"}</T><code>SMTP_TLS_MODE=tls</code><T>{"，STARTTLS 要求服务器支持 TLS 且证书有效。明文 plain 仅允许 localhost 测试。"}</T></p>
        <p><T>{"SMTP 启用后，新邮箱注册需验证，支持“忘记密码”；旧账号可从资料页补充验证。关闭 SMTP 不会让待验证账号自动获得访问权。设置"}</T><code>AUTH_REGISTRATION_ENABLED=false</code> <T>{"可关闭所有新账号注册，同时保留已有账号登录和绑定。"}</T></p>
        <p><T>{"Helm 对应参数在"}</T><code>config.auth</code><T>{"，OAuth/SMTP 密钥在"}</T><code>secrets</code> <T>{"或引用的 existingSecret。完整字段见"}</T><a href={`${repositoryURL}/blob/main/docs/accounts-and-security.md#accounts-and-sign-in`} target="_blank" rel="noopener noreferrer"><T>{"仓库账号配置文档"}</T></a><T>{"。"}</T></p>
      </> },
{ id: "artifacts", title: "配置公开站点地址", content: <>
        <p><T>{"Artifact Host 默认在"}</T><code>http://127.0.0.1:13181</code><T>{"。外部分享需配置可访问的站点地址；应用与 Artifact Host 必须使用不同主机名，只有端口不同是不够的。"}</T></p>
        <CodeBlock label="独立域名示例">{"WEB_ORIGIN=https://lester.example.com\nARTIFACT_PUBLIC_URL=https://sites.example.net\nARTIFACT_PORT=13181"}</CodeBlock>
        <p><T>{"示例域名需要替换并配置 DNS、TLS 和反向代理。通过公开链接可访问已发布内容；管理、更新和下线需要登录。"}</T></p>
      </> },
{ id: "backup", title: "保留配置与持久数据", content: <>
        <p><T>{"备份 PostgreSQL、对象存储以及用户 Computer 的持久工作区。保留原"}</T><code>MASTER_KEY_BASE64</code><T>{"，否则已有加密凭证无法正常解密。"}</T></p>
        <p><T>{"已有部署不要重新覆盖"}</T><code>deploy/.env</code><T>{"，也不要删除 Volume 来升级。切换 Docker 与 ACS Provider 不会自动迁移用户文件，应另行规划。"}</T></p>
      </> },
{ id: "upgrade", title: "已有数据库升级", content: <>
        <p><T>{"037 新增桌面设备、共享模型路由和仅含元数据的本地任务目录。先备份数据库、对象存储和 Computer 数据，停止 API 写入与任务，补齐全部缺失迁移后同步重建 API/Web。回滚 037 不删除设备本地 SQLite、项目文件或云端历史消息，也不会撤销任务操作。"}</T></p>
        <p><T>{"当前版本需要迁移 001–037。全新 PostgreSQL 数据目录会按序执行初始化 SQL，已有 Volume 不会自动重新执行这些文件。"}</T></p>
        <p><T>{"已完成 001–022 时依次执行 023（file_changes）和 024（project_rules）；更早版本补齐所有缺失迁移。先备份、停止 API 写入并保留密钥与 Volume。回滚前停止任务并配套兼容服务；024 删除项目设置规则，已受理消息快照保留；023 删除审阅及恢复记录，不会撤销实际文件操作。"}</T></p>
        <p><T>{"先备份并停止 API 写入，确认数据库已应用哪些迁移，再按编号执行尚未应用的"}</T><code>backend/migrations/*.up.sql</code><T>{"。不要重复执行已经完成的迁移。"}</T></p>
        <p><T>{"例如，数据库已完成 001–012 时，先执行一次账号机制的 013，再按编号补齐下面的迁移，最后重建服务："}</T></p>
        <CodeBlock label="迁移 013（仅适用于已完成 001–012）">{"docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api\ndocker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U \"$POSTGRES_USER\" -d \"$POSTGRES_DB\"' < backend/migrations/000013_account_identity.up.sql"}</CodeBlock>
        <p><T>{"这段重定向命令适用于 Bash。PowerShell 可使用"}</T><code>Get-Content -Raw</code> <T>{"读取迁移文件，再通过管道传入同一"}</T><code>exec -T postgres</code> <T>{"命令。"}</T></p>
        <p><T>{"数据库已完成 001–013 时，执行一次新手引导的 014。新数据库会自动初始化；已有卷不会自动升级："}</T></p>
        <CodeBlock label="迁移 014（仅适用于已完成 001–013）">{"docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api\ndocker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U \"$POSTGRES_USER\" -d \"$POSTGRES_DB\"' < backend/migrations/000014_user_guides.up.sql"}</CodeBlock>
        <p><T>{"014 回滚只删除新手引导进度；使用兼容的 API/Web 版本。迁移时已有账户默认跳过自动欢迎，新账户首次进入会看到引导。"}</T></p>
        <p><T>{"数据库已完成 001–014 时，先备份、停止 API 写入并执行一次 015，然后继续执行下方的 016–037，最后启动新版服务："}</T></p>
        <CodeBlock label="迁移 015（仅适用于已完成 001–014）">{"docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api\ndocker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U \"$POSTGRES_USER\" -d \"$POSTGRES_DB\"' < backend/migrations/000015_rotating_tokens.up.sql"}</CodeBlock>
        <p><T>{"015 使旧登录失效，升级后需重新登录一次，账号、项目与文件保留。015 回滚也撤销登录，需配套兼容的 API/Web。双令牌有效期为固定默认值，无需新增环境变量。"}</T></p>
        <p><T>{"已完成 001–015 的部署继续按顺序执行 016–037：提交去重、模型预算、命令任务和请求用量。停止 API 写入后，每项只执行一次。保留密钥、对象存储和 Computer 文件；回滚 018 前先停止命令任务，回滚不会撤销文件修改或外部操作。"}</T></p>
        <CodeBlock label="迁移 016–037">{"docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api\nfor migration in 000016_run_submissions 000017_model_budget 000018_command_tasks 000019_model_request_usage 000020_context_checkpoints 000021_run_interactions 000022_run_inputs 000023_file_changes 000024_project_rules 000025_run_permissions 000026_cloud_task_indexes 000027_automations 000028_permission_reviews 000029_conversational_schedules 000030_native_signin 000031_personal_assistant 000032_app_connections 000033_github_code_workflows 000034_speech_input 000035_speech_providers 000036_conversation_reads 000037_desktop_devices; do\n  docker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U \"$POSTGRES_USER\" -d \"$POSTGRES_DB\"' < \"backend/migrations/${migration}.up.sql\" || exit 1\ndone\ndocker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --build api web sandbox-service"}</CodeBlock>
        <p><T>{"Sandbox Service 需带新版 lester-toolbox：文件协议为 2，命令任务协议为 1。ACS 需重建模板；旧 Computer 不会被删除或重建。新 Docker Computer 使用进程回收器；既有容器不会自动获得它。任务启动响应丢失时，已保存的日志仍可读取，但进程身份未确认，平台不会盲目重启或停止进程。"}</T></p>
        <Note><T>{"回滚应配套恢复兼容的应用版本。迁移 013 会删除身份、令牌和头像引用，存在无密码账号时会拒绝回滚，应先安排密码恢复。工作区与文件不会迁移。迁移 012 回滚会删除成果登记和待投递记录。"}</T></Note>
      </> },
{ id: "helm", title: "Kubernetes 与 ACS", content: <>
        <p><T>{"Helm Chart 位于"}</T><code>deploy/helm/lester</code><T>{"。PostgreSQL、Redis 和兼容 S3 的对象存储由外部提供，安装前需按编号完成数据库迁移。"}</T></p>
        <p><T>{"Docker Provider 需要提供 Docker Engine 的专用节点。ACS Provider 使用 E2B-compatible Sandbox Manager，通过 Provider 接口管理 Computer，支持 Native 和 Private 路由。"}</T></p>
        <p><T>{"完整参数和示例见"}</T><a href={`${repositoryURL}/blob/main/docs/deployment.md#kubernetes-and-helm`} target="_blank" rel="noopener noreferrer"><T>{"仓库部署文档"}</T></a><T>{"。"}</T></p>
      </> }
];
