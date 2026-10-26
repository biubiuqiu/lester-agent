# Lester 中文文档

[项目首页](../../README.zh-CN.md) · [English](../README.md)

先完成安装，再按需要阅读对应章节。除非代码块切换了目录，命令均从仓库根目录运行。这些 Markdown 文档介绍项目使用、部署与开发；部署内的 `/docs` 继续提供六语言站内帮助。

## 章节

| 章节 | 内容 |
| --- | --- |
| [快速开始](getting-started.md) | 安装 Lester 并开始第一个任务。 |
| [使用指南](usage.md) | 个人助理、任务交互、权限、定时任务与会话回顾。 |
| [项目、文件与发布](projects-and-files.md) | 项目工作台、成果、文件预览与站点发布。 |
| [Agent、上下文与 Skill](agents-and-context.md) | 自定义 Agent、@ 引用、Skill 与附件机制。 |
| [应用连接](app-connections.md) | 五个平台 OAuth 配置、只读工具与隐私边界。 |
| [架构与 Computer](architecture.md) | 服务边界、沙箱、存储、事件与模型上下文。 |
| [部署与升级](deployment.md) | 升级检查、统一网关、Docker/ACS 与 Kubernetes/Helm。 |
| [账号、管理与安全](accounts-and-security.md) | 注册、单点登录、令牌、头像、管理员与安全配置。 |
| [开发与 CI](development.md) | 仓库结构、本地检查与 GitHub Actions。 |
| [移动客户端](mobile.md) | iPhone、iPad、Android 的安装入口与支持范围。 |
| [操作演示](walkthrough.md) | 演示视频、时间章节、示例边界与录制方法。 |

## 推荐阅读顺序

- **初次使用：** [快速开始](getting-started.md) → [使用指南](usage.md) → [项目与文件](projects-and-files.md)。
- **部署维护：** [升级检查](deployment.md#已有部署升级检查) → [账号与安全](accounts-and-security.md) → [Helm 参考](../../deploy/helm/lester/README.md)。
- **参与开发：** [系统架构](architecture.md) → [开发与 CI](development.md) → [后端扩展说明](../../backend/ARCHITECTURE.md)。

## 专题说明与研究

原有专题文档保留各自的语言与范围，与分章指南互相补充。

- [个人助理的记忆与委派机制](../personal-assistant.md)
- [自动任务、入站 Webhook 与结构化提问](../automations-and-questions.md)
- [Claude Code 对标](../claude-code-alignment.md)
- [云端 Agent 优先方向](../cloud-agent-priorities.md)
- [2026-10-10 安全审计](../security-audit-2026-10-10.md)
- [上下文管理研究：从抽象到实现](../research/agent-context-management.html)
- [演示录制与复现](../media/README.md)
- [iPhone/iPad 构建指南](../../ios/README.md) · [Android 构建指南](../../android/README.md)

## 维护约定

根目录 README 保持简洁。行为或配置的详细修改写入所属章节，同步维护两种语言的目录与对应章节；产品行为变化还要更新站内帮助。拆分或移动文档时校验相对路径、媒体与标题锚点，不再把完整功能规格和历史迁移命令追加回项目首页。
