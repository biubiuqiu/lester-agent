export type GuideTopic = "welcome" | "models" | "projects" | "files" | "agents" | "contexts" | "computer" | "skills" | "profile" | "publishing";
export type GuideProgress = { topic: GuideTopic; step: number; status: "in_progress" | "skipped" | "completed" };
export type GuideStep = { title: string; body: string; hint?: string; target?: string; visual?: "journey" | "files" };
export type Guide = { topic: GuideTopic; title: string; description: string; path?: string; steps: GuideStep[] };

export const guides: Guide[] = [
  { topic: "welcome", title: "第一次使用 Lester", description: "从一个目标，到一份看得见的成果", steps: [
    { title: "欢迎，把一个想法交给 Lester", body: "Lester 是你的 AI 工作区。描述目标、提供材料，让 Agent 帮你整理文档、分析数据或制作网页，再查看和继续修改成果。", visual: "journey", hint: "大约 1 分钟 · 可以随时退出，稍后继续" },
    { title: "先选择一个模型", body: "模型负责理解任务与生成内容。可以使用管理员提供的共享模型，也可以在「模型」设置中连接自己的服务。没有模型时，先保存草稿，再去配置。", target: ".model-selector", hint: "配置已保存 ≠ 模型已验证。首次实际运行才会确认服务是否可调用。" },
    { title: "告诉它：你想得到什么", body: "在输入框写下目标、受众和交付形式，例如「为我的咖啡店做一个介绍网页，保存为 index.html」。展开「试试一个任务」可选择示例，只会填写草稿；你明确发送后，才会创建新会话。", target: ".compose-box", hint: "Enter 发送 · Shift + Enter 换行" },
    { title: "把需要的材料一起交给它", body: "点回形针上传文件，也可以直接粘贴图片。在输入框输入 @，按需引用上下文库中的背景和要求。", target: ".compose-actions", hint: "只会把你主动引用的上下文提供给 Agent。" },
    { title: "看执行进度，必要时调整方向", body: "发送后，可以查看回复和工具执行详情。运行时你仍可以准备下一条草稿；需要中断时点「停止」，再告诉它新的要求。", target: ".conversation-header", hint: "一个会话同时执行一个任务。" },
    { title: "打开文件，看见真正的成果", body: "Agent 写入或编辑文件后，文件会在右侧自动打开，多份文件使用多个标签。HTML 默认显示网页预览，可以切换到源码；你也可以从成果卡片继续修改，展开详情下载文件。", target: ".open-deliverables-button", visual: "files", hint: "生成文件后才会出现文件预览。手机端关闭面板即可回到对话。" },
    { title: "其他功能，随用随学", body: "项目整理会话；Agent 保存专门的工作方式；上下文库保存常用背景。模型、Computer、Skill 和个人资料各有独立教学，点「新手引导」随时查看。", target: ".guide-launcher", hint: "现在可以开始自己的任务，也可以先看看某个功能的教学。" },
  ] },
  { topic: "models", title: "模型配置", description: "连接服务，添加模型，再开始任务", path: "/app/settings/models", steps: [
    { title: "模型是 Agent 的思考能力", body: "有共享模型时可以直接使用，凭证由管理员管理。使用自己的模型时，先选择服务商、填写 API Key；兼容接口还需要 Endpoint。", target: ".settings-main", hint: "个人凭证加密保存，不向其他用户公开。" },
    { title: "服务连接和模型，是两件事", body: "「连接服务商」保存端点与凭证；「添加模型」选择已保存的连接，填写服务商提供的 Model ID。显示名称只是你看到的名字，Azure OpenAI 的 Model ID 使用部署名称。", target: ".settings-grid", hint: "已有模型时先展示列表，点「添加模型」打开表单；表单顶部的步骤可切换。云服务参数按服务商要求填写。" },
    { title: "保存配置，回到你的任务", body: "设为默认模型后，新会话会优先选择它。保存完成，点击「返回工作区」继续编辑刚才的草稿。若首次运行报错，检查凭证、模型标识、服务权限和配额。", target: ".saved-section", hint: "教学完成只表示你看过说明，不代表模型服务已经验证。" },
  ] },
  { topic: "projects", title: "项目与会话", description: "按工作组织任务，快速找回历史", steps: [
    { title: "用项目装下同一类工作", body: "会话栏按项目组织。默认项目可以直接使用；点击项目区的 + 为不同产品、客户或主题创建新项目。", target: ".project-rail", hint: "手机端先点左上角菜单打开会话栏。" },
    { title: "开始会话，不需要先建空任务", body: "进入项目后写下目标，发送第一条消息才创建会话。用搜索找回历史任务，展开项目查看其中的会话。", target: ".project-rail" },
    { title: "把重要工作放在手边", body: "项目和会话的更多菜单支持置顶。会话菜单还可以把任务移动到另一个项目；任务的消息和文件仍然保留。", target: ".project-rail" },
  ] },
  { topic: "files", title: "文件与成果", description: "多标签预览、查看代码和下载", steps: [
    { title: "生成的文件，会自己打开", body: "Agent 写入、编辑或登记文件后，确认文件存在，再自动打开预览。手机顶部点「成果」打开面板，再切换「文件」；也可从更多菜单查看会话文件。", target: ".open-deliverables-button", hint: "还没有会话或生成文件时，可以先了解操作，实际预览会在任务中出现。" },
    { title: "一份文件，一个标签", body: "右侧最多同时保留 8 个文件标签。点击切换，点 × 关闭；再次修改同一文件会刷新它的标签。HTML 默认以隔离的 iframe 预览，也可以切换源码。", target: ".computer-panel", visual: "files", hint: "文件标签支持方向键、Home / End 切换，Delete 关闭。" },
    { title: "看完，继续把成果做好", body: "点击成果卡片预览，展开详情下载文件，或点击「继续修改」补充要求。关闭预览面板后可以继续对话。需要公开网址时，再明确选择发布。", target: ".computer-panel", hint: "工作区内的文件预览需要登录；公开发布是单独的操作。" },
  ] },
  { topic: "agents", title: "Agent 管理", description: "为不同工作准备专门的助手", path: "/app/agents", steps: [
    { title: "Agent 决定如何工作", body: "模型负责思考，Agent 定义工作方式和可用的 Skill。通用任务用 Lester 即可；经常重复的专业工作，可以创建自己的 Agent。", target: ".agent-page-title" },
    { title: "和设计师一起确定它的职责", body: "点击「与智能体设计师创建」，描述目标、边界、输出格式以及需要的材料，再在设计会话中调整提示词、Skill 和文件。", target: ".agent-page-title .primary-button", hint: "只有你实际发送需求，才会开始设计任务。" },
    { title: "选择 Agent，开始新的工作", body: "打开 Agent 卡片查看介绍和预设能力，再和它开始会话。已有会话保留原有设置；自定义 Agent 可以继续编辑，内置 Agent 提供通用起点。", target: ".agent-card-grid" },
  ] },
  { topic: "contexts", title: "上下文库", description: "保存背景，在对话里按需引用", path: "/app/contexts", steps: [
    { title: "把反复解释的背景存下来", body: "品牌语气、团队术语、产品介绍和输出规范都可以成为上下文词条。这些内容仅自己可见，不会自动加入每个任务。", target: ".settings-heading" },
    { title: "新建一个清晰的词条", body: "点击「新建词条」，填写名称、简介与正文，再保存。名称帮助你查找，简介帮助你判断何时引用，正文才是提供给 Agent 的内容。", target: ".context-library-grid" },
    { title: "在聊天中输入 @ 引用", body: "回到消息输入框，输入 @ 选择词条。发送时保存当前内容的快照；之后编辑或删除词条，不会改变历史消息的引用。", target: ".context-library-list", hint: "每次最多引用 8 条；只引用和当前任务有关的内容。" },
  ] },
  { topic: "computer", title: "Computer", description: "了解文件、命令和运行环境", path: "/app/settings/sandbox", steps: [
    { title: "这是 Agent 的工作环境", body: "Computer 提供运行命令和读写文件的环境。每个用户复用自己的 Computer；不同会话使用独立的工作目录。", target: ".settings-card" },
    { title: "在会话面板查看和操作", body: "会话右侧可以查看文件、成果、终端和 Computer 状态。终端中的命令会直接影响当前工作区，输入前先确认自己要做什么。", hint: "此设置页说明环境；实际操作入口在会话中。" },
    { title: "文件保留，环境按需恢复", body: "Computer 空闲后会暂停，下一次使用会检查并恢复。连接异常时查看真实状态和错误提示；切换不同部署 Provider 不会自动迁移工作区文件。", target: ".settings-card" },
  ] },
  { topic: "skills", title: "Skill 广场", description: "按任务安装可复用能力", path: "/app/settings/skills", steps: [
    { title: "Skill 是可复用的工作方法", body: "在广场浏览能力的名称、说明与版本，例如代码审查或项目规划。先选和当前任务有关的能力。", target: ".skill-market-grid" },
    { title: "在具体会话中安装", body: "浏览广场不会自动安装。打开会话的 Computer 面板，在 Skill 页按需安装；自定义 Agent 也可以预设 Skill。", target: ".settings-heading", hint: "安装范围是当前会话。" },
    { title: "给它明确的使用目标", body: "在任务中说明要审查哪些文件、规划哪个目标，或分析什么数据。Skill 提供方法，实际输出仍然取决于目标、材料与模型能力。", target: ".skill-market-grid" },
  ] },
  { topic: "profile", title: "个人资料与登录", description: "头像、密码和第三方登录", path: "/app/settings/profile", steps: [
    { title: "让工作区认识你", body: "修改称呼，选择内置头像主题后点击「保存资料」。上传头像会即时保存，支持小于 2 MB 的 PNG、JPEG 或 GIF。", target: ".profile-card" },
    { title: "管理你的登录方式", body: "部署启用 Google / GitHub 时，可在登录方式中绑定。相同邮箱不会自动合并账号；请先登录已有账号再绑定，至少保留一种可用的登录方式。", target: ".account-security" },
    { title: "保护账号，找回访问", body: "在个人资料中设置或更换密码。部署配置邮件服务后可以验证邮箱，并从登录页找回密码；重设密码后，其他设备需要重新登录。", target: ".account-security", hint: "未配置的第三方登录或邮件功能不会显示为可用。" },
  ] },
  { topic: "publishing", title: "产物发布", description: "把完成的成果变成可分享网址", path: "/app/artifacts", steps: [
    { title: "预览之后，再决定是否分享", body: "产物管理集中查看已经发布的内容。会话内的文件和成果默认留在工作区；自动预览不会自动公开文件。", target: ".artifact-manager", hint: "只在需要公开分享时进行发布。" },
    { title: "明确选择要发布的文件", body: "从会话中选择入口文件和需要的资源，检查发布范围，再提交发布。公开链接可以被拿到网址的人访问，先确认内容适合分享。", target: ".artifact-manager" },
    { title: "查看、更新或停止分享", body: "发布完成后在产物管理中打开网址。需要更新时重新发布；不再分享时取消发布。工作区的对话和原始文件仍然保留。", target: ".artifact-manager" },
  ] },
];

export function guideFor(topic: GuideTopic): Guide { return guides.find(guide => guide.topic === topic)!; }
export function resumeGuide(progress: GuideProgress | undefined, count: number): number {
  return !progress || progress.status === "completed" ? 0 : Math.max(0, Math.min(count - 1, progress.step));
}
export function pageGuide(path: string): GuideTopic | undefined {
  if (path === "/app/settings/models") return "models";
  if (path === "/app/settings/profile") return "profile";
  if (path === "/app/settings/sandbox") return "computer";
  if (path === "/app/settings/skills") return "skills";
  if (path === "/app/agents") return "agents";
  if (path === "/app/contexts") return "contexts";
  if (path === "/app/artifacts") return "publishing";
  return undefined;
}
