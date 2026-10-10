
import { T } from "@/components/i18n";
import type { ReactNode } from "react";
import Link from "next/link";
import { CodeBlock } from "@/components/site/code-block";
import { cloneCommand, repositoryURL, startCommand } from "./site";

export type SiteDoc = {
  slug: string;
  navTitle: string;
  title: string;
  description: string;
  note?: string;
  sections: { id: string; title: string; content: ReactNode }[];
};

function Note({ children }: { children: ReactNode }) {
  return <aside className="site-doc-note">{children}</aside>;
}

export const siteDocs: SiteDoc[] = [
  {
    slug: "", navTitle: "快速开始", title: "开始使用 Lester",
    description: "部署你的工作区，连接模型，完成第一项任务。",
    note: "适用于首次 Docker Compose 部署。",
    sections: [
      { id: "prerequisites", title: "准备好这些", content: <ul><li><T>{"Docker 与 Docker Compose v2。"}</T></li><li><T>{"支持的模型服务及其访问凭证。"}</T></li><li><T>{"Git，用于获取项目源码。"}</T></li></ul> },
      { id: "get-project", title: "1. 获取项目", content: <CodeBlock label="获取项目">{cloneCommand}</CodeBlock> },
      { id: "environment", title: "2. 配置环境", content: <>
        <CodeBlock label="创建环境配置">{"cp deploy/.env.example deploy/.env"}</CodeBlock>
        <p><T>{"编辑"}</T><code>deploy/.env</code><T>{"，填写数据库密码、加密密钥、Sandbox Service Token 和对象存储密码。下面的命令会生成四个独立的值，把每个输出填写到对应变量中："}</T></p>
        <CodeBlock label="生成独立密钥">{"openssl rand -hex 24     # POSTGRES_PASSWORD\nopenssl rand -base64 32  # MASTER_KEY_BASE64\nopenssl rand -hex 32     # SANDBOX_SERVICE_TOKEN\nopenssl rand -hex 24     # MINIO_ROOT_PASSWORD"}</CodeBlock>
        <p><T>{"这些命令适用于提供 OpenSSL 的终端。Windows 用户可使用 Git Bash 或 WSL 来生成密钥，再通过 PowerShell 运行 Docker Compose。"}</T></p>
        <Note><T>{"环境配置保存在"}</T><code>deploy/.env</code><T>{"，请勿提交到 Git。已有部署请保留原配置、加密密钥和数据卷，并阅读"}</T><Link href="/docs/deployment#upgrade"><T>{"升级说明"}</T></Link><T>{"。"}</T></Note>
      </> },
      { id: "start", title: "3. 启动服务", content: <>
        <CodeBlock label="启动 Docker Compose">{startCommand}</CodeBlock>
        <p><T>{"从仓库根目录运行。构建和启动完成后，打开"}</T><a href="http://localhost:13000" target="_blank" rel="noopener noreferrer">http://localhost:13000</a><T>{"。官网位于根路径，点击“进入工作区”前往"}</T><code>/app</code><T>{"。"}</T></p>
        <p><T>{"部署在远程主机时，请使用该主机配置的访问地址，而不是浏览器所在电脑的 localhost。"}</T></p>
      </> },
      { id: "first-task", title: "4. 完成第一项任务", content: <>
        <ol><li><T>{"进入工作区，使用邮箱或已配置的 Google / GitHub 注册、登录。启用邮件服务时先验证邮箱。Lester 会创建你的 Personal Workspace 和默认项目。详见"}</T><Link href="/docs/usage#account"><T>{"账号与登录"}</T></Link><T>{"。"}</T></li><li><T>{"点击“配置第一个模型”，保存服务连接，再添加对应的 Model ID。更多说明见"}</T><Link href="/docs/models"><T>{"模型配置"}</T></Link><T>{"。"}</T></li><li><T>{"输入一个目标，例如“制作一个产品介绍网页，保存为 index.html”。需要时上传材料，再明确发送。"}</T></li><li><T>{"跟随执行过程。文件写入或编辑后会自动打开预览；任务结束后还可从成果卡片下载文件或点击“继续修改”。"}</T></li></ol>
        <Note><T>{"保存模型配置不代表连接已经验证。第一次实际任务会确认模型是否能被调用；示例任务只会填写输入框，不会自动发送。"}</T></Note>
        <p><Link href="/app" prefetch={false}><T>{"进入当前部署的工作区"}</T></Link><T>{"，或继续阅读"}</T><Link href="/docs/usage"><T>{"使用指南"}</T></Link><T>{"。"}</T></p>
      </> },
    ],
  },
  {
    slug: "usage", navTitle: "使用指南", title: "和 Lester 一起完成工作",
    description: "从描述目标，到查看成果，再继续把它做好。",
    sections: [
      { id: "account", title: "注册、登录与个人资料", content: <>
        <p><T>{"使用邮箱和密码，或部署方已配置的 Google / GitHub 登录。启用邮件服务时，新邮箱注册需验证；登录页可重新发送验证邮件或找回密码。未配置邮件服务时，保留自部署即时注册方式。"}</T></p>
        <p><T>{"access token 有效 2 小时，refresh token 有效 30 天。持续使用会自动轮换并重新计算 30 天，可持续续期；30 天未续期需重新登录。退出登录或重设密码会撤销令牌。"}</T></p>
        <p><T>{"相同邮箱不会自动合并账号。已有账号请先用原方式登录，再到“个人资料 → 登录方式”绑定第三方身份。绑定不会更改账号邮箱、项目或文件。至少保留一种可用登录方式；解除绑定会退出其他设备。"}</T></p>
        <p><T>{"资料页可修改称呼、设置或更换密码、上传照片或恢复内置头像。照片支持小于 2 MB 的 PNG/JPEG/GIF，可拖动、缩放、旋转、重置，并预览圆形效果，GIF 使用首帧。确认裁剪才保存，取消不修改头像；已有照片可调整裁剪。头像恢复即时保存；称呼与主题选择需点击“保存资料”。更换密码会保留当前登录，退出其他设备。"}</T></p>
        <p><T>{"找回密码链接在 30 分钟后过期，验证链接在 24 小时后过期，均只能使用一次。密码重设后所有设备需重新登录。第三方按钮未出现时，请联系部署负责人配置，参见"}</T><Link href="/docs/deployment#authentication"><T>{"登录与邮件服务配置"}</T></Link><T>{"。"}</T></p>
      </> },
      { id: "guides", title: "逐步认识工作区", content: <>
        <p><T>{"可在官网导航、登录页、账户菜单或设置侧栏切换界面语言。首次访问跟随浏览器偏好，支持中文、英文、日文、韩文、法文和西班牙文；手动选择会在当前浏览器记忆一年。切换时保留草稿和本地附件，用户消息与生成文件保持原样。"}</T></p>
        <p><T>{"新账户首次进入会看到可跳过的 7 步教学，介绍选择模型、描述目标、提供材料、查看执行和文件成果。点击“新手引导”打开教学中心，每个功能都有“上一步 / 下一步”的说明。"}</T></p>
        <p><T>{"模型、项目、文件、Agent、上下文库、Computer、Skill、个人资料与发布各有独立教学。功能页首次访问的提示可关闭；进度跟随账户保存，完成后也可重新看一遍。已有账户升级后不会被强制欢迎弹窗打断。"}</T></p>
        <p><T>{"教学不会自动发送任务或发布内容；跳转模型配置时草稿保留。阅读模型教学不等于模型调用已验证。"}</T></p>
      </> },
      { id: "conversation", title: "描述目标，开始会话", content: <>
        <p><T>{"在工作区首页选择模型，按需展开输入框下方的 Agent 选项，写下目标、约束和想要的交付物。展开“试试一个任务”可以选择示例，只填入草稿；发送第一条消息时才会创建会话。顶部的书本图标可重放新手引导，产物图标可进入已发布产物管理。"}</T></p>
        <p><T>{"你可以上传文件或粘贴图片。附件存入当前会话目录，模型默认接收文件路径提示，按需要使用工具读取内容。"}</T></p>
        <p><T>{"任务运行中可以准备下一条草稿，也可以点击停止。一个会话同时只执行一个任务。工具详情可展开查看，运行状态来自真实事件。"}</T></p>
      </> },
      { id: "computer", title: "使用你的 Computer", content: <>
        <p><T>{"每个用户拥有一个逻辑 Computer，每个会话有独立的目录。Agent 可以在目录内运行命令、读取、创建和编辑文件。桌面右侧提供文件列表、预览和终端。"}</T></p>
        <p><T>{"终端使用交互式 Bash：Tab 补全命令和路径，↑↓ 查看历史（输入前缀后查找匹配命令），Ctrl+R 搜索历史，Ctrl+C 中断，Ctrl+L 清屏。多行粘贴后按 Enter 执行。选中文字可用“复制”、Ctrl+Shift+C / ⌘C；Shift+Esc 离开终端焦点。手机提供补全、历史和控制键按钮。断线可重新连接，会启动新的 Shell。"}</T></p>
        <p><T>{"历史保存在当前会话的"}</T><code>.agent/terminal/bash_history</code><T>{"，敏感命令以空格开头可避免记录。自定义镜像需要 Bash；未安装时提示并使用基础 sh。提供的运行时镜像包含 bash-completion，具体命令参数补全取决于该工具的补全脚本；新增镜像包需重建镜像及 ACS 模板，不要删除已有工作区来升级。"}</T></p>
        <p><T>{"Agent 写入、编辑或登记文件后，确认文件实际存在再自动打开；运行中同步发现的命令生成文件也会打开。多个文件放在独立标签中（最多 8 个），同一路径复用标签，支持切换、关闭并保留查看方式。HTML 默认使用隔离 iframe 预览，可以切换源码；修改后同步最新内容。点击“在新页面打开”只显示私有 HTML，不带工作区侧栏、终端或文件面板，也不会自动发布。左右方向键、Home/End 切换标签，Delete 关闭标签。历史或重复事件不会反复弹出文件。"}</T></p>
        <p><T>{"Computer 的工作区会保留；空闲时暂停，下一次访问时唤醒。当前支持 Docker 与 Alibaba Cloud ACS Provider。两者之间不会自动迁移文件。"}</T></p>
        <p><T>{"手机端会自动打开文件面板，可关闭后继续对话。顶部“成果”直接打开成果集合；需要浏览其他文件时，切换面板的“文件”标签，或选择“更多 → 查看会话文件”。桌面端可以拖动调整右侧宽度、放大预览和收起会话栏。"}</T></p>
      </> },
      { id: "deliverables", title: "查看成果，继续修改", content: <>
        <p><T>{"任务结束、失败或停止后，成果区集中展示当前文件清单中确认存在的 HTML 网页与 Markdown 文档。Agent 可以登记标题、摘要和来源任务，旧文件仍支持按类型发现。"}</T></p>
        <p><T>{"点击成果卡片打开预览，目录会收起以留出阅读空间；点击“目录”可重新展开。卡片上的“待验收”保持可见，文件路径、大小、完整摘要、下载与 HTML 部署放在卡片的“详情与更多操作”中。"}</T></p>
        <p><T>{"点击预览可查看页面或文档，切换源码、下载文件。“继续修改”会引用文件并回到输入框，保留已有草稿；只有你发送后才开始下一项任务。"}</T></p>
        <Note><T>{"成果登记与运行结束均不代表内容验收通过。预览显示当前文件，尚不提供历史内容版本恢复。失败或停止后的文件可能不完整，需要检查。"}</T></Note>
      </> },
      { id: "publish", title: "明确发布 HTML", content: <>
        <p><T>{"需要分享时，从 HTML 文件的部署按钮打开发布对话框，或明确要求 Agent 使用"}</T><code>deploy_html</code><T>{"。支持 HTML 文件及静态站点目录，包含受支持的本地资源。"}</T></p>
        <p><T>{"发布会把文件保存为对象存储中的快照，由独立 Artifact Host 提供访问。后续修改工作区文件不会自动更新已发布版本；更新发布需要明确操作。"}</T></p>
        <p><T>{"框架项目需要先构建为静态文件。Artifact Host 不运行服务端程序；应用与公开站点应使用不同主机名，详见"}</T><Link href="/docs/deployment#artifacts"><T>{"发布域名配置"}</T></Link><T>{"。"}</T></p>
      </> },
      { id: "skills", title: "安装会话 Skills", content: <>
        <p><T>{"从 Skill 广场选择并安装到当前会话。安装的技能位于"}</T><code>.agent/skills</code><T>{"，Agent 可按需要加载说明。Skill 提供可复用的方法和工具使用指导。"}</T></p>
        <p><T>{"安装前了解包的来源和内容。一个 Skill 的安装不会自动开始任务，也不代表相关外部服务已经完成配置。"}</T></p>
      </> },
      { id: "agents", title: "定义适合你的 Agent", content: <>
        <p><T>{"在 Agent 管理中创建 Agent。Agent Designer 会与你讨论目标、指令和技能，在你同意后保存定义；右侧配置面板可继续调整。"}</T></p>
        <p><T>{"新会话默认使用 Lester，也可以明确选择自己的 Agent。会话创建时会保存 Agent 的定义与资源引用；之后修改 Agent，不会改写已有会话的历史设置。"}</T></p>
      </> },
      { id: "context", title: "用上下文库提供背景", content: <>
        <p><T>{"在上下文库维护项目背景、术语或要求。输入"}</T><code>@</code> <T>{"选择条目，发送时会保存当时的内容快照，后续编辑原条目不会改写历史消息。"}</T></p>
        <p><T>{"只有明确选择的条目才会被加入请求。当前上下文库不提供自动检索、RAG 或跨会话记忆。"}</T></p>
      </> },
    ],
  },
  {
    slug: "models", navTitle: "模型配置", title: "连接你选择的模型",
    description: "管理服务连接与 Model ID，并为任务选择合适的模型。",
    sections: [
      { id: "connection", title: "添加服务连接", content: <>
        <p><T>{"在“设置 → 模型”或首页的“配置第一个模型”中，选择 Provider 并填写访问凭证。已有可用模型时先展示模型列表，点击“添加模型”展开表单；新增服务连接可展开“服务商连接”，再点“添加连接”。连接名称可以留空；自定义服务可按需填写 Endpoint 和高级 JSON 配置。"}</T></p>
        <p><T>{"当前集成包含 OpenAI-compatible、Anthropic-compatible、Azure OpenAI、Vertex Anthropic、Foundry Anthropic 与 Bedrock。具体字段以界面和对应服务要求为准。"}</T></p>
        <Note><T>{"凭证在 Lester 数据库中加密保存。模型请求会发往你选择的服务；自托管 Lester 不意味着外部模型也在你的机器上运行。"}</T></Note>
      </> },
      { id: "deployment", title: "添加模型并设置默认", content: <>
        <p><T>{"保存连接后，添加服务支持的准确 Model ID。显示名称可以留空。首个模型默认作为个人默认模型，你也可以取消该选项或之后修改。"}</T></p>
        <p><T>{"模型名称与实际 Model ID 是不同字段。无法确定 ID 时，查看服务商的模型列表或部署配置，避免仅填写产品显示名称。"}</T></p>
      </> },
      { id: "verify", title: "用真实任务验证", content: <>
        <p><T>{"保存配置只验证本地配置规则，不会调用 Provider。当前连接测试也不代表真实模型调用成功。"}</T></p>
        <p><T>{"回到原项目发送一个小任务，例如“创建一个简洁的 HTML 产品介绍页”。如果返回错误，检查凭证权限、Endpoint、Model ID 和网络连通性。"}</T></p>
      </> },
      { id: "shared", title: "管理员共享模型", content: <>
        <p><T>{"管理员可在独立管理后台配置共享模型。成员可以选择启用的共享模型，但无法读取其连接配置和凭证。个人默认模型优先于系统默认模型。"}</T></p>
        <p><T>{"注册账号默认是普通成员，首个注册账号不会自动成为管理员。首次管理员配置由部署负责人通过授权数据库会话完成，参见"}</T><a href={`${repositoryURL}#administration`} target="_blank" rel="noopener noreferrer"><T>{"仓库管理说明"}</T></a><T>{"。"}</T></p>
      </> },
    ],
  },
  {
    slug: "deployment", navTitle: "部署与升级", title: "部署在自己的环境里",
    description: "管理访问地址、持久数据与版本升级。",
    sections: [
      { id: "compose", title: "Docker Compose 部署", content: <>
        <p><T>{"首次部署请先完成"}</T><Link href="/docs"><T>{"快速开始"}</T></Link><T>{"。Compose 会启动 Gateway、Web、API、PostgreSQL、Redis、对象存储、Sandbox Service 与独立 Artifact Host。"}</T></p>
        <CodeBlock label="查看服务状态">{"docker compose --env-file deploy/.env -f deploy/docker-compose.yaml ps"}</CodeBlock>
        <p><T>{"日常重启可以使用"}</T><code>up -d</code><T>{"。拉取代码并完成升级步骤后，用"}</T><code>up -d --build</code> <T>{"构建新版服务。只更新源码不会更新已有运行容器。"}</T></p>
      </> },
      { id: "runtime", title: "沙盒开发环境", content: <>
        <p><T>{"Compose 会先构建完整沙盒镜像，供新建的 Computer 使用。镜像预装 Node.js、npm、pnpm、Python、pip、venv、Go 和常用编译工具。Node.js 与 Python 的 Playwright 共用预下载的 Chromium，并包含中日韩字体。"}</T></p>
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
        <p><T>{"Helm 对应参数在"}</T><code>config.auth</code><T>{"，OAuth/SMTP 密钥在"}</T><code>secrets</code> <T>{"或引用的 existingSecret。完整字段见"}</T><a href={`${repositoryURL}#accounts-and-sign-in`} target="_blank" rel="noopener noreferrer"><T>{"仓库账号配置文档"}</T></a><T>{"。"}</T></p>
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
        <p><T>{"当前版本需要迁移 001–015。全新 PostgreSQL 数据目录会按序执行初始化 SQL，已有 Volume 不会自动重新执行这些文件。"}</T></p>
        <p><T>{"先备份并停止 API 写入，确认数据库已应用哪些迁移，再按编号执行尚未应用的"}</T><code>backend/migrations/*.up.sql</code><T>{"。不要重复执行已经完成的迁移。"}</T></p>
        <p><T>{"例如，数据库已完成 001–012 时，执行一次账号机制的 013，并继续执行下面的 014、015，再重建 API 和 Web："}</T></p>
        <CodeBlock label="迁移 013（仅适用于已完成 001–012）">{"docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api\ndocker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U \"$POSTGRES_USER\" -d \"$POSTGRES_DB\"' < backend/migrations/000013_account_identity.up.sql"}</CodeBlock>
        <p><T>{"这段重定向命令适用于 Bash。PowerShell 可使用"}</T><code>Get-Content -Raw</code> <T>{"读取迁移文件，再通过管道传入同一"}</T><code>exec -T postgres</code> <T>{"命令。"}</T></p>
        <p><T>{"数据库已完成 001–013 时，执行一次新手引导的 014。新数据库会自动初始化；已有卷不会自动升级："}</T></p>
        <CodeBlock label="迁移 014（仅适用于已完成 001–013）">{"docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api\ndocker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U \"$POSTGRES_USER\" -d \"$POSTGRES_DB\"' < backend/migrations/000014_user_guides.up.sql"}</CodeBlock>
        <p><T>{"014 回滚只删除新手引导进度；使用兼容的 API/Web 版本。迁移时已有账户默认跳过自动欢迎，新账户首次进入会看到引导。"}</T></p>
        <p><T>{"以上旧版本升级还需继续补齐 015。数据库已完成 001–014 时，备份、停止 API 写入，执行一次 015，再启动新版 API/Web："}</T></p>
        <CodeBlock label="迁移 015（仅适用于已完成 001–014）">{"docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api\ndocker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U \"$POSTGRES_USER\" -d \"$POSTGRES_DB\"' < backend/migrations/000015_rotating_tokens.up.sql\ndocker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --build api web"}</CodeBlock>
        <p><T>{"015 使旧登录失效，升级后需重新登录一次，账号、项目与文件保留。015 回滚也撤销登录，需配套兼容的 API/Web。双令牌有效期为固定默认值，无需新增环境变量。"}</T></p>
        <Note><T>{"回滚应配套恢复兼容的应用版本。迁移 013 会删除身份、令牌和头像引用，存在无密码账号时会拒绝回滚，应先安排密码恢复。工作区与文件不会迁移。迁移 012 回滚会删除成果登记和待投递记录。"}</T></Note>
      </> },
      { id: "helm", title: "Kubernetes 与 ACS", content: <>
        <p><T>{"Helm Chart 位于"}</T><code>deploy/helm/lester</code><T>{"。PostgreSQL、Redis 和兼容 S3 的对象存储由外部提供，安装前需按编号完成数据库迁移。"}</T></p>
        <p><T>{"Docker Provider 需要提供 Docker Engine 的专用节点。ACS Provider 使用 E2B-compatible Sandbox Manager，通过 Provider 接口管理 Computer，支持 Native 和 Private 路由。"}</T></p>
        <p><T>{"完整参数和示例见"}</T><a href={`${repositoryURL}#kubernetes-and-helm`} target="_blank" rel="noopener noreferrer"><T>{"仓库部署文档"}</T></a><T>{"。"}</T></p>
      </> },
    ],
  },
  {
    slug: "troubleshooting", navTitle: "常见问题", title: "遇到问题时，从这里排查",
    description: "配置、执行和成果预览的常见问题。",
    sections: [
      { id: "cannot-start", title: "页面打不开或服务启动失败", content: <>
        <p><T>{"先检查服务状态和日志，确认四个必填密钥已配置。端口不可用时，同时更新"}</T><code>GATEWAY_PORT</code> <T>{"与"}</T><code>WEB_ORIGIN</code><T>{"。"}</T></p>
        <CodeBlock label="服务状态与日志">{"docker compose --env-file deploy/.env -f deploy/docker-compose.yaml ps\ndocker compose --env-file deploy/.env -f deploy/docker-compose.yaml logs --tail=100 api web gateway"}</CodeBlock>
        <p><T>{"新版源码需要重新构建运行容器；已有数据库需要显式补齐迁移，见"}</T><Link href="/docs/deployment#upgrade"><T>{"部署与升级"}</T></Link><T>{"。"}</T></p>
      </> },
      { id: "model-error", title: "模型已保存，任务仍报错", content: <>
        <p><T>{"配置保存和当前连接测试不会确认真实 Provider 调用。检查 Model ID、凭证权限、Endpoint 及部署环境的网络，再发一个小任务验证。"}</T></p>
        <p><T>{"没有可用模型时，先添加个人模型，或让管理员配置启用的共享模型。配置页返回原项目时会保留任务草稿。"}</T></p>
      </> },
      { id: "missing-file", title: "文件已生成，成果卡片没有出现", content: <>
        <p><T>{"运行中成果区会等待任务结束。结束后可刷新文件清单与成果；成果区只显示已同步、确认存在的 HTML/Markdown 文件。"}</T></p>
        <p><T>{"大目录或部分读取失败会限制同步范围。依赖、缓存和隐藏目录不会自动遍历，辅助文件也可能被类型发现规则排除。可以在文件面板手动查找，或让 Agent 明确登记真正的主成果。"}</T></p>
      </> },
      { id: "preview", title: "HTML 预览与普通浏览器行为不同", content: <>
        <p><T>{"私有 HTML 在受限 iframe 中执行。页面不能访问 Lester 的应用存储，网络请求、弹窗、表单和部分浏览器功能会受限制。相对链接只会打开同一会话清单中确认存在的文件。"}</T></p>
        <p><T>{"检查源码、依赖文件和生成页面自身的脚本错误。需要外部分享时，明确发布静态站点，而不是发送私有预览地址。"}</T></p>
      </> },
      { id: "capabilities", title: "现在支持记忆或定时任务吗？", content: <>
        <p><T>{"当前支持个人工作区、模型与工具调用、持久 Computer、Skills、自定义 Agent、成果预览及明确发布。上下文库需要手动选择条目。"}</T></p>
        <p><T>{"当前尚未实现跨会话 Memory、浏览器自动化、外部应用连接器、周期任务调度或独立 Worker。执行器仍运行在 API 内；中断后不会自动重放工具。"}</T></p>
      </> },
      { id: "report", title: "如何反馈问题或参与开发？", content: <>
        <p><T>{"前往"}</T><a href={`${repositoryURL}/issues`} target="_blank" rel="noopener noreferrer">GitHub Issues</a><T>{"，附上版本或提交号、复现步骤、预期行为和实际结果。日志请移除凭证及个人敏感内容。"}</T></p>
        <p><T>{"源码、架构说明和开发检查命令都在"}</T><a href={repositoryURL} target="_blank" rel="noopener noreferrer"><T>{"项目仓库"}</T></a><T>{"。欢迎提交问题和改进建议。"}</T></p>
      </> },
    ],
  },
];

export function docURL(slug: string) { return slug ? `/docs/${slug}` : "/docs"; }
export function getSiteDoc(slug: string) { return siteDocs.find((doc) => doc.slug === slug); }
