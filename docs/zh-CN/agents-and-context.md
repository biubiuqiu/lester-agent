# Agent、上下文与 Skill

[文档目录](README.md) · [项目首页](../../README.zh-CN.md) · [English](../agents-and-context.md)

自定义 Agent、@ 引用、Skill 与附件机制。

> 除非代码块另有说明，命令均从仓库根目录运行。已有数据库先看[完整升级检查](deployment.md#已有部署升级检查)，功能章节中的迁移示例只是部分步骤。

## 本章目录

- [自定义 Agent 与 @ 引用](#自定义-agent-与--引用)
- [Skill 与附件机制](#skill-与附件机制)

## 自定义 Agent 与 @ 引用

在账户菜单进入 Agent 管理。创建 Agent 会打开智能体设计师的普通项目会话，先沟通目标，再确认提示词、Skill 与资源文件。保存后的 Agent 可用于新任务；任务会保存受理时的 Agent 配置，后续编辑不会改写已有会话的快照。

在新任务或已有会话的输入框中输入 `@`，可以选择上下文词条、其他会话或 Skill；三类引用合计最多 8 项。上下文按发送时的内容保存快照；会话引用只允许读取明确选择的来源；Skill 在发送受理后按需安装。选择或预览引用不会自动发送任务，也不代表 Agent 已经读取材料。

完整的创建、资源文件限制、引用分页与授权规则见[英文机制说明](../agents-and-context.md)。

## Skill 与附件机制

Skill 广场的元数据保存在 PostgreSQL，版本化安装包保存在对象存储。`backend/internal/blob.Store` 是存储边界，当前实现连接 MinIO，也可以替换为 AWS S3 或其他兼容实现。服务启动时会写入三个默认 Skill：Code Review、Project Planner 和 Data Explorer。

Skill 是会话级能力：安装后解包到 `/workspace/conversations/{conversationId}/.agent/skills/{slug}`，数据库记录会话与 Skill 的关系。运行时 Prompt 只列出已安装 Skill 的名称、说明与路径；Agent 必须调用 `load_skill` 读取 `SKILL.md` 后才能使用它。卸载会同时清理当前会话目录和安装关系，不影响其他会话。

附件上传后写入 `/workspace/conversations/{conversationId}/.agent/upload`。消息只记录附件元数据，并向模型提供文件路径、原始名称、类型和大小，不会预先解析文件，也不会把文件内容直接塞入上下文。Agent 只有在任务确实需要时才使用 `read` 或 `bash` 检查附件。

---

[← 项目、文件与发布](projects-and-files.md) · [架构与 Computer →](architecture.md)
