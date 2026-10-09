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
      { id: "prerequisites", title: "准备好这些", content: <ul><li>Docker 与 Docker Compose v2。</li><li>支持的模型服务及其访问凭证。</li><li>Git，用于获取项目源码。</li></ul> },
      { id: "get-project", title: "1. 获取项目", content: <CodeBlock label="获取项目">{cloneCommand}</CodeBlock> },
      { id: "environment", title: "2. 配置环境", content: <>
        <CodeBlock label="创建环境配置">{"cp deploy/.env.example deploy/.env"}</CodeBlock>
        <p>编辑 <code>deploy/.env</code>，填写数据库密码、加密密钥、Sandbox Service Token 和对象存储密码。下面的命令会生成四个独立的值，把每个输出填写到对应变量中：</p>
        <CodeBlock label="生成独立密钥">{"openssl rand -hex 24     # POSTGRES_PASSWORD\nopenssl rand -base64 32  # MASTER_KEY_BASE64\nopenssl rand -hex 32     # SANDBOX_SERVICE_TOKEN\nopenssl rand -hex 24     # MINIO_ROOT_PASSWORD"}</CodeBlock>
        <p>这些命令适用于提供 OpenSSL 的终端。Windows 用户可使用 Git Bash 或 WSL 来生成密钥，再通过 PowerShell 运行 Docker Compose。</p>
        <Note>环境配置保存在 <code>deploy/.env</code>，请勿提交到 Git。已有部署请保留原配置、加密密钥和数据卷，并阅读<Link href="/docs/deployment#upgrade">升级说明</Link>。</Note>
      </> },
      { id: "start", title: "3. 启动服务", content: <>
        <CodeBlock label="启动 Docker Compose">{startCommand}</CodeBlock>
        <p>从仓库根目录运行。构建和启动完成后，打开 <a href="http://localhost:13000" target="_blank" rel="noopener noreferrer">http://localhost:13000</a>。官网位于根路径，点击“进入工作区”前往 <code>/app</code>。</p>
        <p>部署在远程主机时，请使用该主机配置的访问地址，而不是浏览器所在电脑的 localhost。</p>
      </> },
      { id: "first-task", title: "4. 完成第一项任务", content: <>
        <ol><li>进入工作区，使用邮箱或已配置的 Google / GitHub 注册、登录。启用邮件服务时先验证邮箱。Lester 会创建你的 Personal Workspace 和默认项目。详见<Link href="/docs/usage#account">账号与登录</Link>。</li><li>点击“配置第一个模型”，保存服务连接，再添加对应的 Model ID。更多说明见<Link href="/docs/models">模型配置</Link>。</li><li>输入一个目标，例如“制作一个产品介绍网页，保存为 index.html”。需要时上传材料，再明确发送。</li><li>跟随执行过程。任务结束后，打开成果预览，也可以下载文件或点击“继续修改”。</li></ol>
        <Note>保存模型配置不代表连接已经验证。第一次实际任务会确认模型是否能被调用；示例任务只会填写输入框，不会自动发送。</Note>
        <p><Link href="/app" prefetch={false}>进入当前部署的工作区</Link>，或继续阅读<Link href="/docs/usage">使用指南</Link>。</p>
      </> },
    ],
  },
  {
    slug: "usage", navTitle: "使用指南", title: "和 Lester 一起完成工作",
    description: "从描述目标，到查看成果，再继续把它做好。",
    sections: [
      { id: "account", title: "注册、登录与个人资料", content: <>
        <p>使用邮箱和密码，或部署方已配置的 Google / GitHub 登录。启用邮件服务时，新邮箱注册需验证；登录页可重新发送验证邮件或找回密码。未配置邮件服务时，保留自部署即时注册方式。</p>
        <p>相同邮箱不会自动合并账号。已有账号请先用原方式登录，再到“个人资料 → 登录方式”绑定第三方身份。绑定不会更改账号邮箱、项目或文件。至少保留一种可用登录方式；解除绑定会退出其他设备。</p>
        <p>资料页可修改称呼、设置或更换密码、上传照片或恢复内置头像。照片支持小于 2 MB 的 PNG/JPEG/GIF，居中裁剪为方形，GIF 使用首帧。头像上传和恢复即时保存；称呼与主题选择需点击“保存资料”。更换密码会保留当前登录，退出其他设备。</p>
        <p>找回密码链接在 30 分钟后过期，验证链接在 24 小时后过期，均只能使用一次。密码重设后所有设备需重新登录。第三方按钮未出现时，请联系部署负责人配置，参见<Link href="/docs/deployment#authentication">登录与邮件服务配置</Link>。</p>
      </> },
      { id: "conversation", title: "描述目标，开始会话", content: <>
        <p>在工作区首页选择模型和 Agent，写下目标、约束和想要的交付物。发送第一条消息时才会创建会话；任务示例只填入草稿。</p>
        <p>你可以上传文件或粘贴图片。附件存入当前会话目录，模型默认接收文件路径提示，按需要使用工具读取内容。</p>
        <p>任务运行中可以准备下一条草稿，也可以点击停止。一个会话同时只执行一个任务。工具详情可展开查看，运行状态来自真实事件。</p>
      </> },
      { id: "computer", title: "使用你的 Computer", content: <>
        <p>每个用户拥有一个逻辑 Computer，每个会话有独立的目录。Agent 可以在目录内运行命令、读取、创建和编辑文件。桌面右侧提供文件列表、预览和终端。</p>
        <p>Computer 的工作区会保留；空闲时暂停，下一次访问时唤醒。当前支持 Docker 与 Alibaba Cloud ACS Provider。两者之间不会自动迁移文件。</p>
        <p>手机端可以通过会话菜单查看成果，或打开文件面板。桌面端可以拖动调整右侧宽度、放大预览和收起会话栏。</p>
      </> },
      { id: "deliverables", title: "查看成果，继续修改", content: <>
        <p>任务结束、失败或停止后，成果区集中展示当前文件清单中确认存在的 HTML 网页与 Markdown 文档。Agent 可以登记标题、摘要和来源任务，旧文件仍支持按类型发现。</p>
        <p>点击预览可查看页面或文档，切换源码、下载文件。“继续修改”会引用文件并回到输入框，保留已有草稿；只有你发送后才开始下一项任务。</p>
        <Note>成果登记与运行结束均不代表内容验收通过。预览显示当前文件，尚不提供历史内容版本恢复。失败或停止后的文件可能不完整，需要检查。</Note>
      </> },
      { id: "publish", title: "明确发布 HTML", content: <>
        <p>需要分享时，从 HTML 文件的部署按钮打开发布对话框，或明确要求 Agent 使用 <code>deploy_html</code>。支持 HTML 文件及静态站点目录，包含受支持的本地资源。</p>
        <p>发布会把文件保存为对象存储中的快照，由独立 Artifact Host 提供访问。后续修改工作区文件不会自动更新已发布版本；更新发布需要明确操作。</p>
        <p>框架项目需要先构建为静态文件。Artifact Host 不运行服务端程序；应用与公开站点应使用不同主机名，详见<Link href="/docs/deployment#artifacts">发布域名配置</Link>。</p>
      </> },
      { id: "skills", title: "安装会话 Skills", content: <>
        <p>从 Skill 广场选择并安装到当前会话。安装的技能位于 <code>.agent/skills</code>，Agent 可按需要加载说明。Skill 提供可复用的方法和工具使用指导。</p>
        <p>安装前了解包的来源和内容。一个 Skill 的安装不会自动开始任务，也不代表相关外部服务已经完成配置。</p>
      </> },
      { id: "agents", title: "定义适合你的 Agent", content: <>
        <p>在 Agent 管理中创建 Agent。Agent Designer 会与你讨论目标、指令和技能，在你同意后保存定义；右侧配置面板可继续调整。</p>
        <p>新会话默认使用 Lester，也可以明确选择自己的 Agent。会话创建时会保存 Agent 的定义与资源引用；之后修改 Agent，不会改写已有会话的历史设置。</p>
      </> },
      { id: "context", title: "用上下文库提供背景", content: <>
        <p>在上下文库维护项目背景、术语或要求。输入 <code>@</code> 选择条目，发送时会保存当时的内容快照，后续编辑原条目不会改写历史消息。</p>
        <p>只有明确选择的条目才会被加入请求。当前上下文库不提供自动检索、RAG 或跨会话记忆。</p>
      </> },
    ],
  },
  {
    slug: "models", navTitle: "模型配置", title: "连接你选择的模型",
    description: "管理服务连接与 Model ID，并为任务选择合适的模型。",
    sections: [
      { id: "connection", title: "添加服务连接", content: <>
        <p>在“设置 → 模型”或首页的“配置第一个模型”中，选择 Provider 并填写访问凭证。连接名称可以留空；自定义服务可按需填写 Endpoint 和高级 JSON 配置。</p>
        <p>当前集成包含 OpenAI-compatible、Anthropic-compatible、Azure OpenAI、Vertex Anthropic、Foundry Anthropic 与 Bedrock。具体字段以界面和对应服务要求为准。</p>
        <Note>凭证在 Lester 数据库中加密保存。模型请求会发往你选择的服务；自托管 Lester 不意味着外部模型也在你的机器上运行。</Note>
      </> },
      { id: "deployment", title: "添加模型并设置默认", content: <>
        <p>保存连接后，添加服务支持的准确 Model ID。显示名称可以留空。首个模型默认作为个人默认模型，你也可以取消该选项或之后修改。</p>
        <p>模型名称与实际 Model ID 是不同字段。无法确定 ID 时，查看服务商的模型列表或部署配置，避免仅填写产品显示名称。</p>
      </> },
      { id: "verify", title: "用真实任务验证", content: <>
        <p>保存配置只验证本地配置规则，不会调用 Provider。当前连接测试也不代表真实模型调用成功。</p>
        <p>回到原项目发送一个小任务，例如“创建一个简洁的 HTML 产品介绍页”。如果返回错误，检查凭证权限、Endpoint、Model ID 和网络连通性。</p>
      </> },
      { id: "shared", title: "管理员共享模型", content: <>
        <p>管理员可在独立管理后台配置共享模型。成员可以选择启用的共享模型，但无法读取其连接配置和凭证。个人默认模型优先于系统默认模型。</p>
        <p>注册账号默认是普通成员，首个注册账号不会自动成为管理员。首次管理员配置由部署负责人通过授权数据库会话完成，参见<a href={`${repositoryURL}#administration`} target="_blank" rel="noopener noreferrer">仓库管理说明</a>。</p>
      </> },
    ],
  },
  {
    slug: "deployment", navTitle: "部署与升级", title: "部署在自己的环境里",
    description: "管理访问地址、持久数据与版本升级。",
    sections: [
      { id: "compose", title: "Docker Compose 部署", content: <>
        <p>首次部署请先完成<Link href="/docs">快速开始</Link>。Compose 会启动 Gateway、Web、API、PostgreSQL、Redis、对象存储、Sandbox Service 与独立 Artifact Host。</p>
        <CodeBlock label="查看服务状态">{"docker compose --env-file deploy/.env -f deploy/docker-compose.yaml ps"}</CodeBlock>
        <p>日常重启可以使用 <code>up -d</code>。拉取代码并完成升级步骤后，用 <code>up -d --build</code> 构建新版服务。只更新源码不会更新已有运行容器。</p>
      </> },
      { id: "address", title: "配置应用访问地址", content: <>
        <p>默认应用端口为 <code>13000</code>。端口占用或 Windows 保留端口导致启动失败时，选择可用端口，并同时修改两个变量：</p>
        <CodeBlock label="应用地址示例">{"GATEWAY_PORT=13280\nWEB_ORIGIN=http://localhost:13280"}</CodeBlock>
        <p>远程访问时，设置为实际公开地址，并使用 HTTPS 入口。更新配置后重新启动完整 Compose 栈。不要把 Sandbox Service 或 Docker Socket 暴露为公网服务。</p>
      </> },
      { id: "authentication", title: "配置第三方登录与邮件", content: <>
        <p>在 <code>deploy/.env</code> 成对填写 <code>GOOGLE_OAUTH_CLIENT_ID</code> / <code>GOOGLE_OAUTH_CLIENT_SECRET</code>，或 <code>GITHUB_OAUTH_CLIENT_ID</code> / <code>GITHUB_OAUTH_CLIENT_SECRET</code>。空的变量对隐藏对应按钮；配置只填一项会拒绝启动。</p>
        <CodeBlock label="OAuth 回调地址">{"<WEB_ORIGIN>/api/v1/auth/oauth/google/callback\n<WEB_ORIGIN>/api/v1/auth/oauth/github/callback"}</CodeBlock>
        <p>将 <code>&lt;WEB_ORIGIN&gt;</code> 替换为真实应用地址。Google 创建 Web application 客户端并配置授权页面、测试用户或正式发布；GitHub 创建对应环境的 OAuth App。授权回调需与配置完全一致，并经同源 Gateway / Ingress 转发到 API。</p>
        <p>生产环境使用 HTTPS，并设置 <code>SESSION_COOKIE_SECURE=true</code>；HTTP 仅限 localhost 开发。密钥保存在服务端，API 需能访问 Google / GitHub 的 HTTPS 接口。</p>
        <CodeBlock label="SMTP 配置示例">{"SMTP_HOST=smtp.example.com\nSMTP_PORT=587\nSMTP_TLS_MODE=starttls\nSMTP_USERNAME=your-mail-account\nSMTP_PASSWORD=your-mail-password\nSMTP_FROM=no-reply@example.com"}</CodeBlock>
        <p>替换为邮件服务实际参数并使用已授权的发件地址。465 端口可用 <code>SMTP_TLS_MODE=tls</code>，STARTTLS 要求服务器支持 TLS 且证书有效。明文 plain 仅允许 localhost 测试。</p>
        <p>SMTP 启用后，新邮箱注册需验证，支持“忘记密码”；旧账号可从资料页补充验证。关闭 SMTP 不会让待验证账号自动获得访问权。设置 <code>AUTH_REGISTRATION_ENABLED=false</code> 可关闭所有新账号注册，同时保留已有账号登录和绑定。</p>
        <p>Helm 对应参数在 <code>config.auth</code>，OAuth/SMTP 密钥在 <code>secrets</code> 或引用的 existingSecret。完整字段见<a href={`${repositoryURL}#accounts-and-sign-in`} target="_blank" rel="noopener noreferrer">仓库账号配置文档</a>。</p>
      </> },
      { id: "artifacts", title: "配置公开站点地址", content: <>
        <p>Artifact Host 默认在 <code>http://127.0.0.1:13181</code>。外部分享需配置可访问的站点地址；应用与 Artifact Host 必须使用不同主机名，只有端口不同是不够的。</p>
        <CodeBlock label="独立域名示例">{"WEB_ORIGIN=https://lester.example.com\nARTIFACT_PUBLIC_URL=https://sites.example.net\nARTIFACT_PORT=13181"}</CodeBlock>
        <p>示例域名需要替换并配置 DNS、TLS 和反向代理。通过公开链接可访问已发布内容；管理、更新和下线需要登录。</p>
      </> },
      { id: "backup", title: "保留配置与持久数据", content: <>
        <p>备份 PostgreSQL、对象存储以及用户 Computer 的持久工作区。保留原 <code>MASTER_KEY_BASE64</code>，否则已有加密凭证无法正常解密。</p>
        <p>已有部署不要重新覆盖 <code>deploy/.env</code>，也不要删除 Volume 来升级。切换 Docker 与 ACS Provider 不会自动迁移用户文件，应另行规划。</p>
      </> },
      { id: "upgrade", title: "已有数据库升级", content: <>
        <p>当前版本需要迁移 001–013。全新 PostgreSQL 数据目录会按序执行初始化 SQL，已有 Volume 不会自动重新执行这些文件。</p>
        <p>先备份并停止 API 写入，确认数据库已应用哪些迁移，再按编号执行尚未应用的 <code>backend/migrations/*.up.sql</code>。不要重复执行已经完成的迁移。</p>
        <p>例如，数据库已完成 001–012 时，执行一次账号机制的 013，再重建 API 和 Web：</p>
        <CodeBlock label="迁移 013（仅适用于已完成 001–012）">{"docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api\ndocker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U \"$POSTGRES_USER\" -d \"$POSTGRES_DB\"' < backend/migrations/000013_account_identity.up.sql\ndocker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --build api web"}</CodeBlock>
        <p>这段重定向命令适用于 Bash。PowerShell 可使用 <code>Get-Content -Raw</code> 读取迁移文件，再通过管道传入同一 <code>exec -T postgres</code> 命令。</p>
        <Note>回滚应配套恢复兼容的应用版本。迁移 013 会删除身份、令牌和头像引用，存在无密码账号时会拒绝回滚，应先安排密码恢复。工作区与文件不会迁移。迁移 012 回滚会删除成果登记和待投递记录。</Note>
      </> },
      { id: "helm", title: "Kubernetes 与 ACS", content: <>
        <p>Helm Chart 位于 <code>deploy/helm/lester</code>。PostgreSQL、Redis 和兼容 S3 的对象存储由外部提供，安装前需按编号完成数据库迁移。</p>
        <p>Docker Provider 需要提供 Docker Engine 的专用节点。ACS Provider 使用 E2B-compatible Sandbox Manager，通过 Provider 接口管理 Computer，支持 Native 和 Private 路由。</p>
        <p>完整参数和示例见<a href={`${repositoryURL}#kubernetes-and-helm`} target="_blank" rel="noopener noreferrer">仓库部署文档</a>。</p>
      </> },
    ],
  },
  {
    slug: "troubleshooting", navTitle: "常见问题", title: "遇到问题时，从这里排查",
    description: "配置、执行和成果预览的常见问题。",
    sections: [
      { id: "cannot-start", title: "页面打不开或服务启动失败", content: <>
        <p>先检查服务状态和日志，确认四个必填密钥已配置。端口不可用时，同时更新 <code>GATEWAY_PORT</code> 与 <code>WEB_ORIGIN</code>。</p>
        <CodeBlock label="服务状态与日志">{"docker compose --env-file deploy/.env -f deploy/docker-compose.yaml ps\ndocker compose --env-file deploy/.env -f deploy/docker-compose.yaml logs --tail=100 api web gateway"}</CodeBlock>
        <p>新版源码需要重新构建运行容器；已有数据库需要显式补齐迁移，见<Link href="/docs/deployment#upgrade">部署与升级</Link>。</p>
      </> },
      { id: "model-error", title: "模型已保存，任务仍报错", content: <>
        <p>配置保存和当前连接测试不会确认真实 Provider 调用。检查 Model ID、凭证权限、Endpoint 及部署环境的网络，再发一个小任务验证。</p>
        <p>没有可用模型时，先添加个人模型，或让管理员配置启用的共享模型。配置页返回原项目时会保留任务草稿。</p>
      </> },
      { id: "missing-file", title: "文件已生成，成果卡片没有出现", content: <>
        <p>运行中成果区会等待任务结束。结束后可刷新文件清单与成果；成果区只显示已同步、确认存在的 HTML/Markdown 文件。</p>
        <p>大目录或部分读取失败会限制同步范围。依赖、缓存和隐藏目录不会自动遍历，辅助文件也可能被类型发现规则排除。可以在文件面板手动查找，或让 Agent 明确登记真正的主成果。</p>
      </> },
      { id: "preview", title: "HTML 预览与普通浏览器行为不同", content: <>
        <p>私有 HTML 在受限 iframe 中执行。页面不能访问 Lester 的应用存储，网络请求、弹窗、表单和部分浏览器功能会受限制。相对链接只会打开同一会话清单中确认存在的文件。</p>
        <p>检查源码、依赖文件和生成页面自身的脚本错误。需要外部分享时，明确发布静态站点，而不是发送私有预览地址。</p>
      </> },
      { id: "capabilities", title: "现在支持记忆或定时任务吗？", content: <>
        <p>当前支持个人工作区、模型与工具调用、持久 Computer、Skills、自定义 Agent、成果预览及明确发布。上下文库需要手动选择条目。</p>
        <p>当前尚未实现跨会话 Memory、浏览器自动化、外部应用连接器、周期任务调度或独立 Worker。执行器仍运行在 API 内；中断后不会自动重放工具。</p>
      </> },
      { id: "report", title: "如何反馈问题或参与开发？", content: <>
        <p>前往<a href={`${repositoryURL}/issues`} target="_blank" rel="noopener noreferrer">GitHub Issues</a>，附上版本或提交号、复现步骤、预期行为和实际结果。日志请移除凭证及个人敏感内容。</p>
        <p>源码、架构说明和开发检查命令都在<a href={repositoryURL} target="_blank" rel="noopener noreferrer">项目仓库</a>。欢迎提交问题和改进建议。</p>
      </> },
    ],
  },
];

export function docURL(slug: string) { return slug ? `/docs/${slug}` : "/docs"; }
export function getSiteDoc(slug: string) { return siteDocs.find((doc) => doc.slug === slug); }
