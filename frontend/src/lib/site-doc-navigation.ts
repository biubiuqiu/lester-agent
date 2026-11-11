import type { ProductGlyphKind } from "@/components/product-glyph";

export type SiteDocGroup = "start" | "work" | "maintain";
export type SiteDocMetadata = {
  slug: string;
  navTitle: string;
  title: string;
  description: string;
  group: SiteDocGroup;
  glyph: ProductGlyphKind | null;
};

export const siteDocGroups: { id: SiteDocGroup; title: string }[] = [
  { id: "start", title: "开始使用" },
  { id: "work", title: "日常使用" },
  { id: "maintain", title: "部署与维护" },
];

/** Navigation metadata stays separate from article bodies and their client bundles. */
export const siteDocNavigation: SiteDocMetadata[] = [
  { slug: "", navTitle: "快速开始", title: "开始使用 Lester", description: "部署你的工作区，连接模型，完成第一项任务。", group: "start", glyph: "onboarding" },
  { slug: "models", navTitle: "模型配置", title: "连接你选择的模型", description: "管理服务连接与 Model ID，并为任务选择合适的模型。", group: "start", glyph: "model" },
  { slug: "usage", navTitle: "使用指南", title: "和 Lester 一起完成工作", description: "从描述目标，到查看成果，再继续把它做好。", group: "work", glyph: "conversation" },
  { slug: "personal-assistant", navTitle: "个人助理与记忆", title: "个人助理与记忆", description: "记住重要信息，安排任务，查看未读回复。", group: "work", glyph: "memory" },
  { slug: "projects-and-files", navTitle: "项目、文件与成果", title: "管理项目，查看工作成果", description: "组织项目与文件，预览成果，再继续修改或发布。", group: "work", glyph: "project" },
  { slug: "agents-and-context", navTitle: "Agent、上下文与 Skill", title: "让 Agent 理解你的工作", description: "选择 Agent、引用背景，并按需安装 Skills。", group: "work", glyph: "skill" },
  { slug: "connections", navTitle: "应用连接", title: "连接你的工作资料", description: "授权应用读取资料，或单独开启 GitHub 代码任务。", group: "work", glyph: "connections" },
  { slug: "automations", navTitle: "自动任务", title: "安排定时任务与外部触发", description: "通过会话、定时规则或 Webhook 发起任务。", group: "work", glyph: "automation" },
  { slug: "speech", navTitle: "语音输入", title: "把语音变成可编辑的草稿", description: "录音、转写、检查，再明确发送。", group: "work", glyph: "conversation" },
  { slug: "mobile", navTitle: "客户端", title: "在桌面、手机与平板上使用 Lester", description: "在桌面处理本地任务，或在各平台连接自己的云端部署。", group: "work", glyph: "computer" },
  { slug: "deployment", navTitle: "部署与升级", title: "部署在自己的环境里", description: "管理访问地址、持久数据与版本升级。", group: "maintain", glyph: "computer" },
  { slug: "troubleshooting", navTitle: "常见问题", title: "遇到问题时，从这里排查", description: "配置、执行和成果预览的常见问题。", group: "maintain", glyph: null },
];

export function docURL(slug: string) { return slug ? `/docs/${slug}` : "/docs"; }

/** Existing shared help links retain their original section IDs. */
export const legacyUsageLinks = [
  { id: "personal-assistant", title: "个人助理与记忆", href: "/docs/personal-assistant#personal-assistant" },
  { id: "project-workbench", title: "项目工作台", href: "/docs/projects-and-files#project-workbench" },
  { id: "computer", title: "使用你的 Computer", href: "/docs/projects-and-files#computer" },
  { id: "deliverables", title: "查看成果，继续修改", href: "/docs/projects-and-files#deliverables" },
  { id: "publish", title: "明确发布 HTML", href: "/docs/projects-and-files#publish" },
  { id: "skills", title: "安装会话 Skills", href: "/docs/agents-and-context#skills" },
  { id: "agents", title: "定义适合你的 Agent", href: "/docs/agents-and-context#agents" },
  { id: "context", title: "用上下文库提供背景", href: "/docs/agents-and-context#context" },
  { id: "app-connections", title: "应用连接", href: "/docs/connections#app-connections" },
  { id: "github-code", title: "GitHub 代码任务", href: "/docs/connections#github-code" },
  { id: "automations", title: "自动任务", href: "/docs/automations#automations" },
  { id: "speech", title: "语音输入", href: "/docs/speech#speech" },
  { id: "ios", title: "iPhone 与 iPad", href: "/docs/mobile#ios" },
  { id: "android", title: "Android 手机与平板", href: "/docs/mobile#android" },
];
