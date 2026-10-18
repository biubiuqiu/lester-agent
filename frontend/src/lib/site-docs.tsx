
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
      { id: "permissions", title: "权限设置", content: <><ul><li><strong><T>{"默认权限"}</T></strong><p><T>{"读取普通文件无需确认；修改文件和执行命令前确认。"}</T></p></li><li><strong><T>{"自动审批"}</T></strong><p><T>{"普通文件自动处理；命令由独立 AI 审批，不确定时请你确认。"}</T></p></li><li><strong><T>{"完全访问权限"}</T></strong><p><T>{"在已授权的 Computer 内执行，无需逐次确认。"}</T></p></li></ul><p><T>{"自动审批会使用当前任务的模型进行独立审查；允许后执行，拒绝则停止该操作，不确定或审查失败时交给你确认。敏感文件仍需确认，审批不会改变 Plan 或 Computer 的范围。"}</T></p><p><T>{"运行中修改权限会立即作用于当前任务尚未开始的操作，已排队任务保留发送时的权限。命令审批卡片也可选择“本次任务完全访问”。"}</T></p><p><T>{"已有部署需备份并停止 API 写入与活跃任务，按顺序补齐迁移至 000028_permission_reviews.up.sql，再同步重建 API 与 Web。新 Compose 数据库初始化 001–028；不要删除数据卷。"}</T></p><Note><T>{"审批仅适用于当前任务和这次操作，不会扩大 Computer 范围。"}</T></Note></> },
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
      { id: "project-workbench", title: "项目工作台", content: <>
        <p><T>{"点击侧栏的项目名称进入项目工作台，箭头负责展开或收起会话，＋负责在项目中新建任务。工作台默认展示成果，也可切换到会话；项目规则仍可在右上角编辑。原有新任务地址和草稿继续保留。"}</T></p>
        <p><T>{"成果页汇总项目内各会话已登记的 HTML 网页和 Markdown 文档，支持搜索名称、文件路径或来源会话，并按类型筛选、分批加载。登记记录不代表文件仍存在或内容已验收；预览和继续修改前会检查来源文件。继续修改会回到来源会话、引用该文件并保留已有草稿，不会自动发送。"}</T></p>
        <p><T>{"已发布站点单独管理公开发布记录；从项目进入时会带上项目筛选，返回时回到该项目工作台。工作台仅汇总记录，文件仍在原会话目录，不会自动复制或发布。此功能无需新增数据库迁移，已有部署需同步重建 API 和 Web。"}</T></p>
      </> },
      { id: "conversation", title: "描述目标，开始会话", content: <>
        <p><T>{"在工作区首页选择模型和 Agent，写下目标、约束和想要的交付物。输入为空时显示文档、表格、网页三个示例入口；有草稿或附件时，示例收进“试试一个任务”。示例只填入草稿，发送第一条消息时才会创建会话。顶部书本图标可重放新手引导，站点图标可进入已发布站点。"}</T></p>
        <p><T>{"发送首条消息时，输入框会平滑移到底部；创建失败会回到居中位置并保留草稿，系统的减少动态效果设置会跳过动画。"}</T></p>
        <p><T>{"你可以上传文件或粘贴图片。附件存入当前会话目录，模型默认接收文件路径提示，按需要使用工具读取内容。"}</T></p>
        <p><T>{"点击输入框的“＋”上传附件、添加 @ 引用或选择 Agent；也可直接输入 @ 或粘贴图片。模型在右侧选择，任务运行中暂不可切换。"}</T></p>
        <p><T>{"任务运行中可以准备下一条草稿，也可以点击停止。一个会话同时只执行一个任务。工具详情可展开查看，运行状态来自真实事件。"}</T></p>
        <p><T>{"消息受理后，即使刷新会话失败，也会保留任务和停止入口，点击“重新同步状态”即可，不必重复发送。同一次重试使用提交 ID 去重；已上传的附件会复用。模型在任务开始时固定，运行中不能切换。停止请求受理后会保留“正在停止”状态。"}</T></p>
        <p><T>{"复杂任务由 Agent 主动进入 Plan，先只读检查并提出计划；简单任务可直接执行。Plan 中不能运行命令、修改文件或发布。计划可批准、提出调整或暂不执行，批准仅绑定本次任务与当前版本，权限设置保持独立。"}</T></p>
        <p><T>{"Agent 需要关键选择时，会在对话中显示问题和选项，也可补充自由回答。回答草稿随会话保存，刷新后可继续。问题回答不是执行批准；提交结果不确定时只检查原请求，不会自动重复提交。停止或中断任务会撤销待答请求，旧回答和批准不能重新启动任务。"}</T></p>
        <p><T>{"待答问题固定在聊天框上方，逐题显示单选、多选和推荐项，多题可核对后统一提交。推荐不会默认选中；补充文字与多选同时保留。聚焦问题卡后可用数字选择，方向键切换问题标签，回答中按 Enter 保留换行。停止键始终可用，已提交回答可在对话回执中查看。"}</T></p>
        <p><T>{"运行中点击“运行中输入”，可选择调整当前任务或排队下一任务。调整只接受文字，在完整工具批次后加入，不打断或重放命令，也不批准 Plan。排队可包含附件与 @ 引用，并保存受理时的模型、权限与材料；新任务由 Agent 判断是否先规划。"}</T></p>
        <p><T>{"当前任务成功完成才会开始队列。失败或停止会暂停后续任务；会话空闲时可明确继续排队或取消，未加入的文字调整可放回草稿。输入、离开页面和刷新不会自动发送。受理结果不确定时保留草稿并检查原回执，不自动重复提交。"}</T></p>
        <p><T>{"草稿保存在当前浏览器，关闭标签页或窗口后再打开也会恢复；按用户、工作区与会话隔离。退出登录会清除本地草稿。附件原文件不会保存到浏览器，重新打开后需要重新选择。任务运行中按发送或 Enter 会明确排队，完成后开始；也可选择调整当前任务，在工具步骤安全结束后补充文字。"}</T></p>
        <p><T>{"会话中的“文件修改审阅”默认收起。展开后可查看 Agent 写入、编辑工具实际保留的修改前后内容并导出记录。每侧最多保留 512 KiB 文本，每个会话最多 64 MiB；大文件、二进制或未确认操作不会伪造差异。超过 1000 行时显示原始文本对照。"}</T></p>
        <p><T>{"撤回只支持已确认、保留完整文本的既有文件修改，需结束当前任务并明确确认。当前内容必须仍匹配修改后的 SHA-256，否则拒绝恢复。新文件删除、命令、网络和其他文件不在撤回范围；底层回执丢失时保持未确认，不重复执行，也不根据相同内容推断成功。"}</T></p>
        <p><T>{"项目菜单中的“项目规则”可保存明确编写的项目约定，显示来源、版本和 SHA-256。任务和队列受理时固定规则快照；之后编辑规则或移动会话只影响未来受理的任务。过期版本不能覆盖新规则，编辑草稿保留。规则不授予工具权限，不替代平台限制或最新用户要求。"}</T></p>
        <p><T>{"回答支持普通复制与保留格式复制，代码块可单独复制。成功后显示勾选反馈；剪贴板不可用时可手动选择复制。选中回答、Markdown 文件或源码后，可点击“引用并追问”追加到草稿，保留已有文字并回到输入框；源码引用包含文件路径和行号，超过 6000 个字符时按钮明确提示只引用前 6000 个字符。"}</T></p>
        <p><T>{"待发送附件显示大小，图片支持缩略图和放大预览；单个文件超过 25 MiB 时会立即提示，重复选择的相同附件会忽略。点击已选上下文标签可检查当前版本和内容，聚焦标签后按 Delete 或 Backspace 可移除；空输入框按 Backspace 先聚焦最后一个标签。"}</T></p>
      </> },
      { id: "computer", title: "使用你的 Computer", content: <>
        <p><T>{"开始任务时先专注对话；第一份文件出现后展开预览并收起目录。桌面顶部“工作区”打开或收起面板，成果与文件在面板内切换；手机顶部“成果”直接查看交付内容。浏览中的标签不会因后台文件更新而切换。"}</T>{" "}<T>{"终端和技能可从面板的“工具”菜单打开。"}</T></p>
        <p><T>{"每个用户拥有一个逻辑 Computer，每个会话有独立的目录。Agent 可以在目录内运行命令、读取、创建和编辑文件。桌面右侧提供文件列表、预览和终端。"}</T></p>
        <p><T>{"展开对话中的“命令任务”可以查看日志、按页继续读取、下载已保存日志或独立停止命令。后台命令可以在 Agent 回复后继续运行；停止生成不会停止后台命令。状态未确认时不会显示执行成功，也不会自动重新运行命令。每个任务最多保留 64 MiB 日志，超出的输出明确标记为未保存。"}</T></p>
        <p><T>{"覆盖文件前，Agent 需要读取当前完整内容；局部编辑需要读到未截断的目标片段。平台检查读取时的内容版本，拒绝覆盖后来发生的变化。旧读取正文已从上下文移除时，需要重新读取。命令超时或取消会保留已观察到的输出；无法确认进程清理时会显示未知状态。"}</T></p>
        <p><T>{"Agent 可用 read_tool_result 读取当前会话已保存的历史工具结果，不会重跑命令或读取当前文件。入库前已经缩短的内容无法恢复；受管理命令的原文日志使用 task_output。历史结果是数据，不授予新的文件修改权限。"}</T></p>
        <p><T>{"终端使用交互式 Bash：Tab 补全命令和路径，↑↓ 查看历史（输入前缀后查找匹配命令），Ctrl+R 搜索历史，Ctrl+C 中断，Ctrl+L 清屏。多行粘贴后按 Enter 执行。选中文字可用“复制”、Ctrl+Shift+C / ⌘C；Shift+Esc 离开终端焦点。手机提供补全、历史和控制键按钮。断线可重新连接，会启动新的 Shell。"}</T></p>
        <p><T>{"历史保存在当前会话的"}</T><code>.agent/terminal/bash_history</code><T>{"，敏感命令以空格开头可避免记录。自定义镜像需要 Bash；未安装时提示并使用基础 sh。提供的运行时镜像包含 bash-completion，具体命令参数补全取决于该工具的补全脚本；新增镜像包需重建镜像及 ACS 模板，不要删除已有工作区来升级。"}</T></p>
        <p><T>{"Agent 写入、编辑或登记文件后，确认文件实际存在再自动打开；运行中同步发现的命令生成文件也会打开。多个文件放在独立标签中（最多 8 个），同一路径复用标签，支持切换、关闭并保留查看方式。HTML 默认使用隔离 iframe 预览，可以切换源码；修改后同步最新内容。点击“在新页面打开”只显示私有 HTML，不带工作区侧栏、终端或文件面板，也不会自动发布。左右方向键、Home/End 切换标签，Delete 关闭标签。历史或重复事件不会反复弹出文件。"}</T></p>
        <p><T>{"未查看的文件更新在标签上显示圆点，成功读取并查看后清除。标签栏可重新打开最近关闭且仍存在的文件，恢复预览方式和源码或 Markdown 阅读位置；最多记住 8 个关闭标签，只恢复界面状态，不撤销文件修改。"}</T></p>
        <p><T>{"Computer 的工作区会保留；空闲时暂停，下一次访问时唤醒。当前支持 Docker 与 Alibaba Cloud ACS Provider。两者之间不会自动迁移文件。"}</T></p>
        <p><T>{"手机端会自动打开文件面板，可关闭后继续对话。顶部“成果”直接打开成果集合；需要浏览其他文件时，切换面板的“文件”标签，或选择“更多 → 查看会话文件”。桌面端可以拖动调整右侧宽度、放大预览和收起会话栏。"}</T></p>
      </> },
      { id: "cloud-tasks", title: "云端任务", content: <>
        <p><T>{"从侧栏打开“云端任务”，可查看每个会话的最近任务，筛选进行中、需处理与已结束状态。等待计划、操作确认或回答时，打开原会话处理；列表不会批准或重新执行任务。成果数量仅表示已登记，不代表内容验收通过。Agent 回复结束后，后台命令仍可能运行，面板会单独列出；状态未确认的命令仍需要检查。"}</T></p>
        <p><T>{"关闭浏览器不会停止正常的云端执行。API 定期核对执行锁，确认执行者已消失后，将任务标为中断并撤销待答请求，不自动重跑工具。执行器仍在 API 内；服务重启后请先检查当前成果，再明确发送恢复指令。"}</T></p>
        <p><T>{"展开“耗时与模型用量”查看含等待的总耗时、请求数、已报告输入输出及独立估算。缓存输入只计一次，包含上下文整理请求；缺失的输出用量保持未知，不显示为零，也不推算费用。"}</T></p>
        <p><T>{"已有部署请先备份并停止 API 写入，执行 000026_cloud_task_indexes.up.sql，再同步升级 API 与 Web。迁移只增加索引，不删除数据；请保留数据卷。"}</T></p>
      </> },
      { id: "automations", title: "自动任务", content: <>
        <p><T>{"从账户菜单或云端任务面板进入“自动任务”。选择项目、模型和任务内容，设置单次、每天、每周或五段 Cron。重复任务使用指定 IANA 时区并随夏令时变化；单次时间按当前浏览器本地时间保存。保存默认不启用，勾选启用后才会定时触发。"}</T></p>
        <p><T>{"每次触发创建独立会话，约每十秒检查一次。前一次任务、待答请求或后台命令尚未结束时，新的定时触发会跳过并留记录；Webhook 和手动触发按接收顺序等待。停机期间错过的周期合并为一次，恢复后不会补发全部历史周期。暂停或删除会取消未开始的触发，已进入会话的任务需在会话中停止。"}</T></p>
        <p><T>{"Webhook 使用独立 Bearer 密钥和 X-Lester-Event-ID 请求头，接收最多 32 KiB 的 JSON 对象。密钥仅创建或重置时显示，服务端只保存哈希；请通过 HTTPS 发送并存入发送方密钥配置。同一事件 ID 和相同数据重发返回原记录，数据不同则拒绝。事件内容不能更改模型、项目或权限。这是通用入站接口，不包含外发回调或特定应用的原生适配器。"}</T></p>
        <p><T>{"默认权限会等待操作确认，自动审批和完全访问需主动选择，范围始终限于已授权 Computer。回答问题不会批准执行。最近二十次触发保留状态和会话入口；未确认的启动只查询原回执，不自动重跑。每个账户最多一百个自动任务，每个入口每分钟最多接受三十个新事件、保留二十个待触发事件。重复投递不占新事件额度。"}</T></p>
        <p><T>{"已有部署需备份并停止 API 写入与活跃任务，按顺序补齐迁移至 000028_permission_reviews.up.sql，再同步重建 API 与 Web。新 Compose 数据库初始化 001–028；不要删除数据卷。"}</T></p>
      </> },
      { id: "recap", title: "会话回顾", content: <>
        <p><T>{"在已有会话点击“会话回顾”或输入 /recap，查看最近目标、回复和文件更新。回顾只整理已加载的记录，不调用模型，不自动执行或批准操作。"}</T></p>
        <p><T>{"离开至少两分钟且有新进展时可自动展开；未发送草稿会阻止自动展开。在回顾中可关闭自动回顾。恢复操作仅将指令加入草稿，确认发送后才开始新任务。"}</T></p>
        <p><T>{"上下文概览区分最近用户消息的材料快照与待发送材料；不代表完整模型请求。简洁过程合并连续完成的工具记录，详细过程保留逐条记录，异常不会被当作成功隐藏。"}</T></p>
      </> },
      { id: "deliverables", title: "查看成果，继续修改", content: <>
        <p><T>{"聊天区保留简短的成果入口；右侧成果面板提供完整摘要、待验收状态和修改操作。手机面板支持“返回对话”与 Escape 关闭，并恢复原来的键盘焦点。"}</T></p>
        <p><T>{"任务结束、失败或停止后，成果区集中展示当前文件清单中确认存在的 HTML 网页与 Markdown 文档。Agent 可以登记标题、摘要和来源任务，旧文件仍支持按类型发现。"}</T></p>
        <p><T>{"成果卡片显示标题、入口文件和“待验收”状态。点击主按钮“预览”查看内容，目录会收起以留出阅读空间；点击“目录”可重新展开。“继续修改”是单独的操作，大小、完整摘要、下载与 HTML 部署放在“详情与更多操作”中。"}</T></p>
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
        <p><T>{"输入 @ 或点击引用按钮，可搜索并按会话、上下文、Skill 分类选择；也可输入 @会话 名称、@上下文 名称或 @skill 名称。↑↓ 选择、Enter 引用、Esc 关闭；选择只添加草稿标签，发送仍需明确操作。三类对象合计最多 8 个，标签可以预览或移除，草稿会随会话切换和刷新保留。"}</T></p>
        <p><T>{"引用会话会保存名称、项目和发送时的消息截止位置，Agent 通过 read_conversation 按需分页读取可见的用户与助手文字；每页最多 20 条消息、12000 个字符，长消息可用返回的序号与字符游标继续读取。原会话后来新增的消息不会混入旧引用，文件和内部工具记录不会自动读取；跨工作区、未引用和已删除的会话均不能读取。"}</T></p>
        <p><T>{"上下文在发送时保存正文快照，历史内容不受词条后续修改影响。引用 Skill 会在发送后将尚未安装的包添加到当前会话，已有安装保留当前副本，再通过 load_skill 加载说明；历史选择不会自动为新任务安装 Skill。内置 Conversation Reader 可整理引用会话的背景、决定和待办，引用不等于记忆、全局搜索、自动发布或重新执行旧操作。"}</T></p>
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
        <p><T>{"保存配置只检查本地规则。模型列表的“验证连接”会发送一次短文本请求，可能产生费用；成功只代表文本调用可用，工具、附件和完整任务仍需单独验证。共享模型也可验证，成员无法读取凭证或修改其预算。"}</T></p>
        <p><T>{"展开“上下文预算”可设置已知的模型窗口、输出预留和本地策略。0 表示窗口未知、自动预留或默认 64k 策略。每次请求统计系统提示词、工具定义、消息、参数和图片；供应商用量与估算分开保存，缓存只计算一次。预算不会限制普通回复长度，无法容纳的输入会明确报错。"}</T></p>
        <p><T>{"长会话接近预算时会整理历史，完整聊天记录仍保留。最新指令、未观察的结果和未解决的失败保留原文；后台任务从平台记录补回。Skill 正文按来源和版本去重，文件正文移出请求后必须重读。"}</T></p>
        <p><T>{"摘要是独立的无工具请求，有单独的用量记录；校验失败、取消或没有缩小请求时不会替换旧检查点。每轮最多整理两次，上下文过长只重试一次；无法安全容纳的内容会明确停止，不会自动重放工具或截断最新输入。"}</T></p>
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
        <p><T>{"当前版本需要迁移 001–024。全新 PostgreSQL 数据目录会按序执行初始化 SQL，已有 Volume 不会自动重新执行这些文件。"}</T></p>
        <p><T>{"已完成 001–022 时依次执行 023（file_changes）和 024（project_rules）；更早版本补齐所有缺失迁移。先备份、停止 API 写入并保留密钥与 Volume。回滚前停止任务并配套兼容服务；024 删除项目设置规则，已受理消息快照保留；023 删除审阅及恢复记录，不会撤销实际文件操作。"}</T></p>
        <p><T>{"先备份并停止 API 写入，确认数据库已应用哪些迁移，再按编号执行尚未应用的"}</T><code>backend/migrations/*.up.sql</code><T>{"。不要重复执行已经完成的迁移。"}</T></p>
        <p><T>{"例如，数据库已完成 001–012 时，先执行一次账号机制的 013，再按编号补齐下面的迁移，最后重建服务："}</T></p>
        <CodeBlock label="迁移 013（仅适用于已完成 001–012）">{"docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api\ndocker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U \"$POSTGRES_USER\" -d \"$POSTGRES_DB\"' < backend/migrations/000013_account_identity.up.sql"}</CodeBlock>
        <p><T>{"这段重定向命令适用于 Bash。PowerShell 可使用"}</T><code>Get-Content -Raw</code> <T>{"读取迁移文件，再通过管道传入同一"}</T><code>exec -T postgres</code> <T>{"命令。"}</T></p>
        <p><T>{"数据库已完成 001–013 时，执行一次新手引导的 014。新数据库会自动初始化；已有卷不会自动升级："}</T></p>
        <CodeBlock label="迁移 014（仅适用于已完成 001–013）">{"docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api\ndocker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U \"$POSTGRES_USER\" -d \"$POSTGRES_DB\"' < backend/migrations/000014_user_guides.up.sql"}</CodeBlock>
        <p><T>{"014 回滚只删除新手引导进度；使用兼容的 API/Web 版本。迁移时已有账户默认跳过自动欢迎，新账户首次进入会看到引导。"}</T></p>
        <p><T>{"数据库已完成 001–014 时，先备份、停止 API 写入并执行一次 015，然后继续执行下方的 016–024，最后启动新版服务："}</T></p>
        <CodeBlock label="迁移 015（仅适用于已完成 001–014）">{"docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api\ndocker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U \"$POSTGRES_USER\" -d \"$POSTGRES_DB\"' < backend/migrations/000015_rotating_tokens.up.sql"}</CodeBlock>
        <p><T>{"015 使旧登录失效，升级后需重新登录一次，账号、项目与文件保留。015 回滚也撤销登录，需配套兼容的 API/Web。双令牌有效期为固定默认值，无需新增环境变量。"}</T></p>
        <p><T>{"已完成 001–015 的部署继续按顺序执行 016–024：提交去重、模型预算、命令任务和请求用量。停止 API 写入后，每项只执行一次。保留密钥、对象存储和 Computer 文件；回滚 018 前先停止命令任务，回滚不会撤销文件修改或外部操作。"}</T></p>
        <CodeBlock label="迁移 016–024">{"docker compose --env-file deploy/.env -f deploy/docker-compose.yaml stop api\nfor migration in 000016_run_submissions 000017_model_budget 000018_command_tasks 000019_model_request_usage 000020_context_checkpoints 000021_run_interactions 000022_run_inputs 000023_file_changes 000024_project_rules; do\n  docker compose --env-file deploy/.env -f deploy/docker-compose.yaml exec -T postgres sh -c 'psql -v ON_ERROR_STOP=1 --single-transaction -U \"$POSTGRES_USER\" -d \"$POSTGRES_DB\"' < \"backend/migrations/${migration}.up.sql\" || exit 1\ndone\ndocker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --build api web sandbox-service"}</CodeBlock>
        <p><T>{"Sandbox Service 需带新版 lester-toolbox：文件协议为 2，命令任务协议为 1。ACS 需重建模板；旧 Computer 不会被删除或重建。新 Docker Computer 使用进程回收器；既有容器不会自动获得它。任务启动响应丢失时，已保存的日志仍可读取，但进程身份未确认，平台不会盲目重启或停止进程。"}</T></p>
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
        <p><T>{"当前支持定时任务和通用入站 Webhook。跨会话 Memory、外部应用原生连接器及独立 Worker 尚未实现。浏览器自动化可由 Agent 在 Computer 中使用已安装的 Playwright 执行；没有专用浏览器界面。执行器仍运行在 API 内，中断后不会自动重放工具。"}</T></p>
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
