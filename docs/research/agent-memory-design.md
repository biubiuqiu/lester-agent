# Agent 记忆：公开实现与 Lester 的取舍

2026-10-30。研究对象为官方公开源码和文档，以下链接固定到已检查的 commit。未调用这些产品的线上模型，未验证其检索质量、延迟或成本；设计描述不等于运行评测。

## 三种公开实现

| 项目与版本 | 已核实的实现 | 对 Lester 的启发 |
| --- | --- | --- |
| [OpenClaw · 60fbd0f](https://github.com/openclaw/openclaw/tree/60fbd0f4fff0692daa8194829e929a2a8880f550) | 稳定用户偏好、长期事实、日期日志分层；派生索引支持关键词检索和可配置的 embedding 混合检索；具有来源记录、修改与忘记流程 | 记忆要有来源、有限召回和更新关系；用户必须能直接查看、修改 |
| [Letta Code · ebc496e](https://github.com/letta-ai/letta-code/tree/ebc496e532f1a34da9ac8d7e2da41c5920ea77e6) | 核心 Markdown 进入提示词，详细记忆沿目录索引按需读取；Git 保存变化和来源；更新侧重精炼、合并和替换旧结论 | 长期理解需要维护已有主题，不能把所有事件不断追加到上下文 |
| [Mem0 · 26cdefb](https://github.com/mem0ai/mem0/tree/26cdefb46600c0fa5953c4b5ee5003c125539fc4) | 用户/Agent/运行范围、手动保存、模型提取、向量与可选关键词排序、直接更新/删除及历史记录 | 手工管理不应依赖模型；范围不可被自由 metadata 改写；有效记忆与历史分开 |

### OpenClaw

[分层说明](https://github.com/openclaw/openclaw/blob/60fbd0f4fff0692daa8194829e929a2a8880f550/docs/concepts/memory.md#how-it-works)区分 `USER.md`、`MEMORY.md`、`memory/YYYY-MM-DD.md` 和供人审阅的巩固报告。文件可以手动编辑；[共享 Gateway 的个人指引](https://github.com/openclaw/openclaw/blob/60fbd0f4fff0692daa8194829e929a2a8880f550/docs/concepts/user-model.md#personal-user-files-on-a-shared-gateway)按认证身份选择，并用内容哈希检查编辑版本。官方也明确这类文件选择不是文件系统保密边界，不能直接替代多租户权限。

[内置索引](https://github.com/openclaw/openclaw/blob/60fbd0f4fff0692daa8194829e929a2a8880f550/docs/concepts/memory-builtin.md#how-indexing-works)和[来源设计](https://github.com/openclaw/openclaw/blob/60fbd0f4fff0692daa8194829e929a2a8880f550/docs/concepts/memory-architecture.md#provenance-every-memory-knows-where-it-came-from)将记录来源与正文分开；正文自称“可信”不改变信任。没有配置 embeddings 时可使用关键词检索，不能把所有安装都称为语义记忆。

压缩前的 [memory flush](https://github.com/openclaw/openclaw/blob/60fbd0f4fff0692daa8194829e929a2a8880f550/docs/concepts/memory.md#automatic-memory-flush)是独立维护步骤，使用受限写入工具，每个压缩周期最多一次。[来源删除](https://github.com/openclaw/openclaw/blob/60fbd0f4fff0692daa8194829e929a2a8880f550/docs/concepts/memory-provenance.md)保留 tombstone 防止重新摄入，但不保证原始聊天及任意外部副本一并抹除。可选 LanceDB 插件与默认 memory-core 的工具、自动捕获默认值不能混为一谈。

### Letta

[Letta 主仓 README](https://github.com/letta-ai/letta/blob/8816d3a8516893bfdc0fa14bfb1235226012e295/README.md)已将活跃实现指向 `letta-ai/letta-code`；退休 V1 API 在 archive 分支。旧文章中的 V1 设计不代表当前 main。

当前[核心记忆维护](https://github.com/letta-ai/letta-code/blob/ebc496e532f1a34da9ac8d7e2da41c5920ea77e6/src/skills/builtin/maintaining-core-memory/SKILL.md)说明根目录 Markdown 如何编入上下文，并要求解决冲突、合并重复原则、删除过时内容。来源放在 Git 变更说明中，已开始的上下文不因后续编辑自动改写。[详细记忆维护](https://github.com/letta-ai/letta-code/blob/ebc496e532f1a34da9ac8d7e2da41c5920ea77e6/src/skills/builtin/maintaining-deferred-memory/SKILL.md)用 `MEMORY.md` 索引递进发现主题，区分理解与逐字日志。Lester 借鉴分层和来源，不复制其文件执行权限或将记忆提升为平台指令。

### Mem0

当前 [`_add_to_vector_store`](https://github.com/mem0ai/mem0/blob/26cdefb46600c0fa5953c4b5ee5003c125539fc4/mem0/memory/main.py#L881)采用 V3 additive 提取：读取有界上下文，生成新事实，按内容去重后保存。**不能根据旧提示词断言当前 `add` 会自动执行 ADD/UPDATE/DELETE/NONE 四选一。** 直接 `update`、`delete` 和 `history` 仍是独立接口；`infer=False` 则跳过提取模型直接保存。

[`update` 与内部更新](https://github.com/mem0ai/mem0/blob/26cdefb46600c0fa5953c4b5ee5003c125539fc4/mem0/memory/main.py#L1829)重新计算文本索引，并保持用户/Agent/运行身份不变。[删除与历史](https://github.com/mem0ai/mem0/blob/26cdefb46600c0fa5953c4b5ee5003c125539fc4/mem0/memory/main.py#L2114)会移除有效向量记录，但历史仍可能保留旧正文。SDK 的范围过滤不是服务端身份认证，多租户系统还需自行实施授权。

## Lester 本次采用的设计

- **权威数据在 PostgreSQL。** 按用户、工作区和可选项目进行服务端隔离，不把个人记忆放进任务 Computer 的共享文件。
- **一处管理，两种入口。** 直接新增无需模型；聊天可以明确记住、更正、忘记，自动提炼可关闭。管理器支持主题、内容、类别和范围编辑，以及搜索、筛选、来源与撤销。
- **事实与来源分开。** 手工输入如实标记；模型提炼必须有最新用户消息原文依据。任务回复、文件和连接器内容不会自动晋升长期事实。
- **更正有版本边界。** 过期编辑不能覆盖新版本；忘记保留标记，旧模型输出或旧聊天不能自动恢复。变更后推进模型上下文边界。
- **召回有范围和预算。** 先选择最多 24 条、10,000 字符的相关事实；模型需要时，可从当前个人/选定项目的目录 ID 召回最多 12 条、10,000 字符，与其他只读检查共用每轮一次额度。完整请求预算可进一步缩小初始事实集合；目录长主题会标明缩略，保存正文不变。使用规范化词语与 CJK 匹配，不宣称 embedding 语义检索。
- **结果未知只读取。** 直接新增使用稳定 UUID 和原始请求身份，编辑校验 revision；丢失响应不自动重放。当前内容相同不能单独证明原保存请求成功。
- **遗忘语义明确。** 移出未来有效召回，不承诺删除原聊天、撤销历史或已接受任务快照。

本次复用已有记忆表与迁移 036，不引入新模型密钥、embedding 服务、向量库、后台巩固 Worker 或全历史摄入。自动结果转记忆、语义召回和主动巩固属于需要另行设计、授权与质量评测的后续方向。

操作与 API 见[个人助理文档](../zh-CN/personal-assistant.md) / [English](../personal-assistant.md)。
