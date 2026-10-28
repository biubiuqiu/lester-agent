# 应用连接

[文档目录](README.md) · [English](../app-connections.md)

从**设置 → 应用连接**或账户菜单连接 GitHub、Gmail、X、飞书和 Slack。点击连接，在当前标签页跳转服务商授权，完成后返回 Lester；保留原任务/项目返回入口和按账号保存的草稿。每个用户在每个工作区为每个应用连接一个账号，其他成员不能借用该授权。

普通连接支持任务 Agent **搜索和读取**，不会发邮件、发帖、发消息、标记已读、下载附件、跟随外部链接或自动导入整个邮箱。个人助理仍只负责记忆与委派。应用授权独立于 Lester 登录，不会绑定新的登录方式或合并账号。

## 部署

先备份、停止 API 写入与活跃任务，按编号执行所有缺失迁移，直到 `backend/migrations/000034_speech_input.up.sql`，然后一起重建 API、Web 和 Sandbox Service。新 Compose 初始化挂载 001–035；已有数据卷需手动升级。保留原 `MASTER_KEY_BASE64` 和数据卷。032 引入应用连接，033 增加独立授权的代码任务。回滚需倒序执行，并先检查[代码任务的回滚保护](github-code-tasks.md#升级与验证)；不会删除账号、登录会话、任务文件，也不撤销已执行操作。

分别创建服务商应用并填写以下可选凭据。仅 API 接收密钥，不得放入 `NEXT_PUBLIC_*`、Skill 或 Computer。它们与 Lester 登录使用的 `GOOGLE_OAUTH_*` / `GITHUB_OAUTH_*` 独立。

| 应用 ID | 环境变量 | 精确回调路径（接在 `WEB_ORIGIN` 后） |
| --- | --- | --- |
| `github` | `CONNECTOR_GITHUB_CLIENT_ID`、`CONNECTOR_GITHUB_CLIENT_SECRET` | `/api/v1/connectors/github/callback` |
| `gmail` | `CONNECTOR_GMAIL_CLIENT_ID`、`CONNECTOR_GMAIL_CLIENT_SECRET` | `/api/v1/connectors/gmail/callback` |
| `x` | `CONNECTOR_X_CLIENT_ID`、`CONNECTOR_X_CLIENT_SECRET` | `/api/v1/connectors/x/callback` |
| `feishu` | `CONNECTOR_FEISHU_CLIENT_ID`、`CONNECTOR_FEISHU_CLIENT_SECRET` | `/api/v1/connectors/feishu/callback` |
| `slack` | `CONNECTOR_SLACK_CLIENT_ID`、`CONNECTOR_SLACK_CLIENT_SECRET` | `/api/v1/connectors/slack/callback` |

生产使用同源网关/Ingress 的 HTTPS 地址作为 `WEB_ORIGIN`，设置 `SESSION_COOKIE_SECURE=true`，登记不含通配符的完整回调地址。仅本地开发允许 localhost HTTP。缺少任一凭据时明确显示尚未配置；显示已配置不代表测试过真实授权。iOS/Android App 暂需先在浏览器版完成授权，因为系统浏览器与 WebView 不共享本次 OAuth 浏览器 Cookie；连接后 App 内任务可使用同一服务端授权。

服务商控制台：[GitHub](https://github.com/settings/apps)、[Google Cloud](https://console.cloud.google.com/apis/credentials)、[X](https://developer.x.com/en/portal/dashboard)、[飞书](https://open.feishu.cn/app)、[Slack](https://api.slack.com/apps)。

### 各平台配置与范围

- **GitHub：**私有仓库推荐创建并安装 **GitHub App**，启用用户授权，仓库权限只开 **Metadata、Contents、Issues、Pull requests 的 read-only**。填写 OAuth client ID/secret，不是 App ID 或私钥。访问范围是用户权限和 App 安装范围的交集。不请求宽泛 `repo` OAuth scope，因此普通 OAuth App 仅支持公开资源；代码搜索等仍有平台限制。授权不会自动安装 GitHub App，安装需另行完成。平台返回 refresh token 时支持续期。
- **Gmail：**创建 Google OAuth **Web 应用**，启用 Gmail API、配置同意屏幕和测试用户。范围为 `openid email https://www.googleapis.com/auth/gmail.readonly`，请求离线授权。公开上线可能需要 Google 验证及受限范围安全审核；测试模式授权可能提前过期。没有可用刷新授权时需重新连接。使用不可变 Google subject 标识账号，不合并 Lester 账号。
- **X：**创建支持 OAuth 2.0 用户认证的机密 Web App，范围 `tweet.read users.read offline.access`。近期搜索与帖子查询要求匹配的 API 套餐；OAuth 成功不代表套餐允许搜索。不请求写权限。
- **飞书：中国区端点。**创建支持用户 OAuth v2 的应用并登记回调，启用 `offline_access contact:user.base:readonly im:chat:readonly im:message:readonly docx:document:readonly`，按平台要求发布或配置测试用户。仍受租户管理员审批、群聊和文档可见范围限制。支持**按名称搜索群聊**、读取群/单条消息和已知授权 **Docx 文档 ID**；不宣称支持全局消息/文档全文搜索、旧版 Docs、表格或国际版 Lark 端点。
- **Slack：**创建 OAuth v2 应用，在 **User Token Scopes** 添加 `search:read channels:history groups:history im:history mpim:history`，不是 Bot Scopes。在允许的工作区安装并授权，使用返回的 `authed_user` 用户令牌，可处理平台提供的轮换令牌。工作区政策、管理员审批、用户原有可见范围和限流仍有效；拒绝把 bot token 当成用户授权。

服务商可能要求管理员操作、审核或付费套餐。当前使用协议测试夹具和真实 PostgreSQL 验证代码，没有部署凭据时不宣称已完成真实账号联调。

## Agent 使用与数据边界

任务注册两个工具，在只读 Plan 中也可使用：

1. `list_app_connections {}` 返回**本次任务提交者**的安全连接元数据。
2. `read_app` 接收 `provider`、`action: search|read`，以及可选 `kind`、`query`、`resource`、`cursor`、`limit`（1–20；X 搜索至少 10）。不接受令牌、API 地址、用户或工作区 ID。下一页需显式传递返回的 `next_cursor`。

| 应用 | 搜索 | 读取 `resource` |
| --- | --- | --- |
| GitHub | `kind: issues`（默认）、`repositories`、`code`；使用平台查询语法 | `owner/repo`、`owner/repo/issues/N`、`owner/repo/pulls/N`、`owner/repo/contents/path` |
| Gmail | `kind: messages`；Gmail 查询语法 | 返回的邮件 ID |
| X | `kind: tweets`；近期搜索语法 | 返回的帖子 ID |
| 飞书 | `kind: chats`；群名称 | `chat:CHAT_ID`、`message:MESSAGE_ID`、`doc:DOCUMENT_ID` |
| Slack | `kind: messages`；Slack 搜索语法 | `CHANNEL_ID:message_timestamp`，读取线程 |

例如：“在已连接的 Gmail 中搜索 `from:vendor@example.com newer_than:7d`，读取相关邮件，并附来源 ID 总结交付时间。”只有具体任务调用才读取。读取后如需生成文件，仍经过原有任务权限机制。

外部内容只作为参考资料，不能成为系统指令。平台响应解析前限 2 MiB，标准化结果最多 20 条、正文合计 12,000 字符，并明确标记截断/分页。Gmail 搜索先读有限元数据，单封读取纯文本或去标签 HTML，不加载外部图片/附件，也不改未读状态。过大、格式错误或不可访问时明确报错，不能伪装成空结果；读到部分来源不代表读完全文或获得公开发布权。

GitHub 搜索在平台标记结果不完整，或查询超过其 1,000 条结果窗口时，明确返回 `truncated: true`。有 `next_cursor` 时可继续分页，窗口用尽后需缩小查询；游标为空不代表已读完所有搜索结果。限流会与缺少权限分别报错。Slack 搜索命中回复时，会保留父线程引用，供下一步继续读取该线程。

## 安全、续期与断开

浏览器变更使用现有登录与严格同源校验。OAuth 绑定十分钟单次哈希 state、HttpOnly 服务商 Cookie、原始有效登录族、当前浏览器访问凭证和连接代次。除 Slack 的机密 OAuth v2 流程外使用 PKCE S256。平台端点固定在服务端，不跟随重定向，不将原始平台错误或凭据交给界面、模型。

访问/刷新令牌与 PKCE verifier 使用现有主密钥进行 AES-GCM 加密，关联连接和用户信息。任务依据 `runs.submitted_by` 校验仍活跃的任务、账号和成员关系，而非会话最初创建者。凭据不进入 Computer、模型提示、浏览器存储或应用 URL；用户请求读取的资料正文会交给该任务选择的模型。

每个连接串行刷新。成功轮换必须**先提交再读取资料**，后续读取失败也不会重新消耗旧 refresh token。刷新结果不确定时要求重新授权，不自动重放。授权撤销提示重新连接；读取资料时的权限、套餐和限流错误保留授权。断开使用版本校验，删除本地加密凭据并阻止旧回调恢复连接；**不会撤销服务商侧的应用安装/授权**，彻底撤销请在服务商设置操作。已读取的任务记录仍保留。

## 验证

设置 `LESTER_TEST_DATABASE_URL` 指向可销毁 PostgreSQL 后运行后端检查。测试覆盖五种协议、只读 GET、加密、用户与任务隔离、授权过期/重放/取消、断开/退出/禁用/新授权后的旧回调、刷新并发和不确定响应、Origin 和回滚。前端单元与浏览器夹具覆盖授权地址验证、连接状态、取消授权、断开回执恢复、六种语言、响应式/键盘交互和原生浏览器提示。另有脚本模型集成测试通过实际任务执行器验证“检查连接 → 搜索 → 读取”，覆盖普通与只读 Plan，校验工具回执、来源正文、无需执行审批以及不访问 Computer API。这些检查不替代真实 OAuth 应用登记、平台审核或 API 套餐验证。

刷新请求先持久化意图，再联系服务商。若消耗令牌后崩溃或请求取消，后续会要求重新连接，不会重放旧刷新令牌；PostgreSQL 连接级 advisory guard 在多个 API 副本间串行处理同一连接的刷新。

GitHub 可单独开启[代码任务](github-code-tasks.md)，通过审批发布工作分支或 PR；其他应用连接仍只读。
