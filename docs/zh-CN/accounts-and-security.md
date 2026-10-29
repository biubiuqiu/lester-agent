# 账号、管理与安全

[文档目录](README.md) · [项目首页](../../README.zh-CN.md) · [English](../accounts-and-security.md)

注册、单点登录、令牌、头像、管理员与安全配置。

> 除非代码块另有说明，命令均从仓库根目录运行。已有数据库先看[完整升级检查](deployment.md#已有部署升级检查)，功能章节中的迁移示例只是部分步骤。

## 本章目录

- [账号、第三方登录与头像](#账号第三方登录与头像)
- [双令牌登录、头像裁剪与独立 HTML 预览](#双令牌登录头像裁剪与独立-html-预览)
- [安全提示](#安全提示)

## 账号、第三方登录与头像

登录页支持邮箱密码以及部署方配置的 Google / GitHub 登录。首次注册会创建一个 Personal Workspace 和默认项目，新账号始终是普通成员。`AUTH_REGISTRATION_ENABLED=false` 关闭邮箱和第三方的**新账号注册**，已有账号仍可登录、绑定身份。注册校验有效邮箱、1–60 字称呼和至少 10 字符、不超过 1024 字节的密码；界面要求确认密码。

在 `deploy/.env` 配置：

| 服务 | 变量 | 授权回调地址 |
| --- | --- | --- |
| Google | `GOOGLE_OAUTH_CLIENT_ID`、`GOOGLE_OAUTH_CLIENT_SECRET` | `<WEB_ORIGIN>/api/v1/auth/oauth/google/callback` |
| GitHub | `GITHUB_OAUTH_CLIENT_ID`、`GITHUB_OAUTH_CLIENT_SECRET` | `<WEB_ORIGIN>/api/v1/auth/oauth/github/callback` |

Google 在 Cloud Console 的 Google Auth Platform 创建 Web application 客户端，配置授权页面、测试用户或正式发布状态，并添加精确回调地址。GitHub 在 Developer settings 创建 OAuth App，每个部署环境使用对应回调的应用。回调应经同源 Gateway / Ingress 转发到 API。生产环境必须 HTTPS，并设置 `SESSION_COOKIE_SECURE=true`；HTTP 仅限 localhost 开发。未配置的完整变量对会隐藏对应按钮；只填一项会拒绝启动。密钥只放 `.env` 或 Kubernetes Secret。API 需能访问第三方 HTTPS 接口；授权范围为 Google 的 `openid email profile` 和 GitHub 的 `read:user user:email`。

身份按第三方稳定用户 ID 识别，邮箱必须由服务商确认已验证，GitHub 私有邮箱也支持。回调使用单次、10 分钟有效、绑定浏览器的状态和 PKCE S256；Access Token 不持久化。相同邮箱**不会自动合并账户**：先用原方式登录，再到“个人资料 → 登录方式”绑定。绑定不会改写原邮箱、称呼、工作区或文件。解除绑定需保留其他已配置的登录方式或密码，并退出其他设备、轮换当前会话。停用账号仍无法登录。

配置 SMTP 后，新邮箱注册必须验证，登录页提供“忘记密码”和重新发送验证邮件；旧账号保持可用，可从资料页补充验证。不配置 SMTP 时保留自部署的即时注册方式，邮箱显示未验证，邮件操作隐藏。验证链接有效 24 小时，找回密码链接有效 30 分钟，数据库仅保存摘要且只能使用一次；令牌放在 URL fragment，页面读入后立即移除，操作需明确提交。找回密码撤销所有会话和待处理安全令牌；设置页修改密码保留轮换后的当前会话、退出其他设备。

```dotenv
SMTP_HOST=smtp.example.com
SMTP_PORT=587
SMTP_TLS_MODE=starttls
SMTP_USERNAME=your-mail-account
SMTP_PASSWORD=your-mail-password
SMTP_FROM=no-reply@example.com
```

发件地址需获得邮件服务授权。465 端口可用 `SMTP_TLS_MODE=tls`；`starttls` 要求服务端支持 TLS 且证书有效。明文 `plain` 仅限 localhost 测试。申请邮件对未知、停用和符合条件的账户返回相同信息；邮件失败可检查 API 的 `account mail request failed` 日志。注册邮件发送失败时账户仍需验证，可重新发送；关闭 SMTP 不会让待验证账号自动获得访问权。Helm 的 ID/邮件参数位于 `config.auth`，密钥位于 `secrets.googleOAuthClientSecret`、`secrets.githubOAuthClientSecret`、`secrets.smtpPassword`；使用 `existingSecret` 时按需加入三个对应环境变量密钥。

头像支持小于 2 MiB 的 PNG/JPEG/GIF，服务端居中裁剪并转为 256×256 PNG，GIF 使用首帧且移除元数据；拒绝单边超过 4096 像素或总量超过 1200 万像素的图片。照片存入现有私有对象存储，仅通过已登录的 `/api/v1/me/avatar` 读取，失败回退到首字头像。第三方首次注册尝试导入头像，失败不阻止登录；资料页支持上传、恢复主题、明确使用已绑定账号的照片。远程获取仅允许指定 Google/GitHub HTTPS 图片主机，不跟随跳转、限定时间/大小且不携带凭证。替换或删除的旧对象会尽力清理；备份对象存储时包含头像。

已有部署先备份、停止 API 写入；数据库在 001–012 时依次执行一次 013、014、015。已完成 013 时执行下方“逐步新手引导”中的 014、015：

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres \
  sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
  < backend/migrations/000013_account_identity.up.sql
# 最新服务需先补齐 014–021，再启动。
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres \
  sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
  < backend/migrations/000014_user_guides.up.sql
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres \
  sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
  < backend/migrations/000015_rotating_tokens.up.sql
for migration in 000016_run_submissions 000017_model_budget 000018_command_tasks 000019_model_request_usage 000020_context_checkpoints 000021_run_interactions 000022_run_inputs 000023_file_changes 000024_project_rules 000025_run_permissions 000026_cloud_task_indexes 000027_automations 000028_permission_reviews 000029_conversational_schedules 000030_native_signin; do
  docker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < "backend/migrations/${migration}.up.sql" || exit 1
done
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --build api web
```

保留 `.env`、原密钥和数据卷。全新卷自动初始化 001–036，已有卷不会自动升级。PowerShell 用 `Get-Content -Raw` 管道传入 `exec -T postgres`。013 回滚会删除身份、令牌和头像引用，存在无密码账号时会拒绝回滚；降级前先安排密码恢复并配套兼容版本。工作区、项目和文件不迁移。实际 OAuth 授权与邮件投递需使用部署方真实配置验收。

## 双令牌登录、头像裁剪与独立 HTML 预览

邮箱、Google 和 GitHub 登录统一签发两个 HttpOnly/SameSite Cookie，数据库只存摘要：**access token 两小时过期**；**refresh token 30 天过期**，每次续期都会轮换并重新计算 30 天。持续使用没有累计登录时长上限；30 天未续期则需要重新登录。浏览器在到期前、受保护请求被认证中间件拒绝后自动续期，并协调多个请求和标签页。仅重试尚未进入业务处理的认证拒绝，网络故障不会清空草稿。退出登录及密码、账号安全变更撤销整个令牌组；超过 5 秒并发宽限后重用已消费的 refresh token 会撤销该设备的令牌组。

上传头像后，可拖动或用方向键调整位置、缩放、手机双指缩放、旋转、重置，并查看圆形头像效果。点击“保存头像”才上传裁剪后的 256px PNG；取消不更改原头像。已有图片或第三方头像可点击“调整裁剪”。HTML 的“在新页面打开”仅显示私有、隔离的 HTML 及本地资源，可跳转同一会话内已验证的 HTML 文件，不再带入侧栏、终端、文件面板，也不会发布网站。

数据库已完成 **001–014** 时，先备份、停止 API 写入，执行一次 **015** 后重建 API/Web。更早版本先按编号补齐缺失迁移：

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres \
  sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
  < backend/migrations/000015_rotating_tokens.up.sql
for migration in 000016_run_submissions 000017_model_budget 000018_command_tasks 000019_model_request_usage 000020_context_checkpoints 000021_run_interactions 000022_run_inputs 000023_file_changes 000024_project_rules 000025_run_permissions 000026_cloud_task_indexes 000027_automations 000028_permission_reviews 000029_conversational_schedules 000030_native_signin; do
  docker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < "backend/migrations/${migration}.up.sql" || exit 1
done
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --build api web
```

015 会使旧登录失效，升级后需重新登录一次，账号、个人资料、项目与文件不受影响。保留原 `.env`、加密密钥与数据卷。全新 Compose 卷初始化 001–036；Helm 部署前手动执行 SQL。015 回滚也会撤销登录，需配套兼容的 API/Web。有效期为应用固定默认值，无需新增环境变量。

## 安全提示

历史扫描见 [2026-10-10 安全报告](../security-audit-2026-10-10.md)，最新依赖修复见 [2026-10-29 CI 排查](../security-ci-2026-10-29.md)。当前 Go 构建使用 1.26.10 安全补丁版本。

网关 / Ingress 部署需将实际可信代理的 CIDR 填入 `AUTH_TRUSTED_PROXY_CIDRS`（逗号分隔），Helm 对应 `config.auth.trustedProxyCIDRs`。用 `docker network inspect <网络名称>` 查看网关 IP / 子网，优先信任专用代理的 `/32` 或 `/128`，或其受控子网；地址变化时更新，并限制 API 只接受该代理。留空会忽略所有转发头，同一网关后的用户共用网关 IP 的限流桶。禁止使用 `/0` 或信任无关服务。只读取可信连接提供的 `X-Forwarded-For`；默认网关覆盖该头并移除 `True-Client-IP`。

模型接口须直接响应 API 请求，不再跟随重定向，避免密钥和对话内容外泄。终端单条消息限制 1 MiB。已建立的终端 / 事件连接每 30 秒复核会话，在退出登录、禁用账号或移除工作区成员后关闭（数据库检查最多另需 5 秒）。

- 不要在生产环境使用示例 `MASTER_KEY_BASE64`
- 不要在生产环境使用示例 `SANDBOX_SERVICE_TOKEN`；Sandbox Service 不应公开暴露
- Provider 密钥使用 AES-GCM 加密后存储
- HTTPS 部署会默认使用 Secure 会话 Cookie；登录和注册按身份与客户端 IP 做分钟级限流，Redis 故障时拒绝受限操作，默认客户端 IP 为实际 TCP 连接地址；续期按账号与 IP 限制每分钟 60 次
- Sandbox Service 拥有 Docker Socket 权限，生产部署时应运行在隔离的专用 Worker
- User Computer 默认禁用网络，并限制 CPU、内存和 PID 数量
- 文件 API 与终端默认进入 `/workspace/conversations/{conversationId}`，不会把其他会话目录展示为当前会话文件

---

[← 部署与升级](deployment.md) · [开发与 CI →](development.md)
