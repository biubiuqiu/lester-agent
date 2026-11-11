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

## Lester 2026-10-30 采用的设计

- **权威数据在 PostgreSQL。** 按用户、工作区和可选项目进行服务端隔离，不把个人记忆放进任务 Computer 的共享文件。
- **一处管理，两种入口。** 直接新增无需模型；聊天可以明确记住、更正、忘记，自动提炼可关闭。管理器支持主题、内容、类别和范围编辑，以及搜索、筛选、来源与撤销。
- **事实与来源分开。** 手工输入如实标记；模型提炼必须有最新用户消息原文依据。任务回复、文件和连接器内容不会自动晋升长期事实。
- **更正有版本边界。** 过期编辑不能覆盖新版本；忘记保留标记，旧模型输出或旧聊天不能自动恢复。变更后推进模型上下文边界。
- **召回有范围和预算。** 先选择最多 24 条、10,000 字符的相关事实；模型需要时，可从当前个人/选定项目的目录 ID 召回最多 12 条、10,000 字符，与其他只读检查共用每轮一次额度。完整请求预算可进一步缩小初始事实集合；目录长主题会标明缩略，保存正文不变。使用规范化词语与 CJK 匹配，不宣称 embedding 语义检索。
- **结果未知只读取。** 直接新增使用稳定 UUID 和原始请求身份，编辑校验 revision；丢失响应不自动重放。当前内容相同不能单独证明原保存请求成功。
- **遗忘语义明确。** 移出未来有效召回，不承诺删除原聊天、撤销历史或已接受任务快照。

2026-10-30 的管理更新复用当时已有记忆表与迁移 036，不引入新模型密钥、embedding 服务、向量库、后台巩固 Worker 或全历史摄入。自动结果转记忆、语义召回和主动巩固属于需要另行设计、授权与质量评测的后续方向。

操作与 API 见[个人助理文档](../zh-CN/personal-assistant.md) / [English](../personal-assistant.md)。

## 2026-11-11：记忆管理与使用边界

用户提出的参考链接为 [Anthropic · The components of agent memory](https://www.anthropic.com/research/components-of-agent-memory)，并于 2026-11-11 提供了该文的长篇解读。以下借鉴该解读中的四层组织、维护成本、并发保护和跨会话复用思路。当前环境直接访问原网页仍被代理白名单拒绝（HTTP 403），**未独立核实原文、模型版本、实验数字或基准结果**，因此不将解读中的厂商数据作为 Lester 的能力或收益承诺。实现判断同时依据上面已检查的公开实现与本仓库契约。

### 资料分层

| 解读中的层级 | Lester 当前的实现 | 用户怎样看见与管理 |
| --- | --- | --- |
| L1 · Transcripts | 个人对话、任务消息、工具结果和运行记录保留在各自的权威表，作为可追溯证据 | 原聊天与任务记录保持原貌；查看历史不会自动把旧说法恢复成有效记忆 |
| L2 · Memory Notes | `personal_memories` 保存有主题、范围、来源、修订与可用状态的稳定事实；用户纠错可以形成项目经验条目 | 在“设置 → 记忆”新增、修改、暂停或忘记；模型提炼需要当前用户原文依据 |
| L3 · Memory Representations | 当前采用按主题、类别与个人/项目范围组织的轻量目录，按需读取正文 | 搜索与筛选当前事实，在同一条目展开来源和修改历史；完整 Wiki 与知识图谱留待质量验证 |
| L4 · Injected Context | 每次请求从有效条目选出有界参考，正文可被预算进一步裁剪；明确委派时生成该任务的固定参考快照 | 助理回复可展开当次参考的条目与修订；暂停会影响未来选择，不改写已接受快照 |

这四层是不同职责，权威数据仍在 PostgreSQL。数据库的用户/工作区隔离、版本检查和变更事务适合 Lester 的多用户云端架构；不把可由任务命令随意改写的共享 `MEMORY.md` 当作个人记忆的安全边界。Markdown 可以作为以后可导出或可重建的阅读表示，避免出现数据库、文件两份同时可写的“真相”。

记忆选择优先稳定、难以从当前环境重建且可能复用的信息，例如用户确认的构建约束与故障教训。当前分支、文件字节、运行状态继续由工具实时读取；它们的某次历史快照不应冒充今天的环境。上下文压缩负责当次任务连续性，长期记忆负责跨会话复用，两者分别保留来源与有效范围。

本次增加 `/app/settings/memory` 作为常规设置入口，与助理对话框共用一个布局级常驻管理器。列表、编辑、搜索、范围/类别、自动提炼设置、来源与已有撤销流程保持一套行为，切换入口保留草稿与待确认请求。独立集合读取只请求记忆和自动提炼设置，不为管理记忆顺带加载聊天或任务。默认页面不增加仪表盘、批量控制或已忘记记录列表。

### 暂停、遗忘与历史

- `paused` 默认 `false`。用户可用当前 `revision` 暂停或恢复单条记忆；暂停保留内容、来源、可编辑性与历史，仍计入 128 条未忘记记录的容量。
- 暂停立即推进模型上下文边界，排除之后的目录、首次事实、按 ID 召回和委派快照。模型不能覆盖或恢复暂停项；手动改内容也不会隐式恢复。已接受任务的快照保留，不追溯改写。
- 忘记继续使用已有 tombstone 与最近变更撤销，释放有效容量；不宣称抹除原聊天、历史版本或已接受快照。
- “修改历史”只读，按修订倒序每页 30 条，展示当时记录的前后状态、来源和撤销关系；撤销仍通过当前最近一次已确认变更的回执入口，不提供任意旧版本恢复。新的撤销追加自己的变更并指向原变更；旧撤销存在的历史缺口不伪造补写。撤销仍检查当前结果修订、项目权限、主题与容量。
- 响应未知时只读取当前记录或历史，不重放新增、编辑、暂停/恢复、忘记或撤销；当前值相同不能单独证明原请求成功。

个人助理回复可提供本次实际送入模型的参考记忆 ID、修订、主题与项目，按预算裁剪后的请求记录、去重最多 36 条。这个回执帮助用户核对输入来源，不代表模型采纳了记忆或输出正确；委派仍使用独立保存的已接受快照。

### 迁移与未扩大的范围

迁移 `000038_memory_management` 为当前记忆增加 `paused`，为变更日志增加 `undo_of`。已有部署备份、停止 API 写入与活跃任务后，按序补齐 001–038，同步重建 API/Web，保留原密钥与数据卷。回滚 038 保留事实、变更和任务；存在未忘记的暂停项时拒绝回滚，须先由用户明确恢复，避免旧版本静默把它们用于模型。

本次没有引入新的语义检索、向量库、embedding 凭证、后台巩固、全历史摄入或普通任务的私有记忆自动注入。自动整理和检索质量仍需独立的数据集、可核实依据与评测，不能从存储结构或参考链接推导为已具备的能力。


### 跨新会话的工程验证与后续整理

系统回归使用真实 PostgreSQL 与固定模型响应，验证“用户明确纠正并记住一条项目经验 → 新的个人对话和明确委派的新任务获得该经验 → 暂停后另一新任务不再获得它”的数据闭环，同时检查其他项目事实与隐私边界。它验证持久化、范围、带入与暂停机制，不证明真实模型能独立避免同类错误，也不提供跨会话成功率或 Token 成本的统计结论。真实学习收益需要另行建立任务集，对照无记忆、即时记忆与整理后记忆，检查错误复发与成本。

解读所说的 Dreaming 可理解为跨层维护过程，不再增加一个用户必须理解的“记忆层”。后续适合从有界、用户触发的整理建议开始：选择范围和来源快照，提出纠错、合并或索引调整，展示证据和前后差异，经确认后按当前版本事务提交。任务完成并不自动授权读取全部私人历史，也不授权恢复暂停/已忘记的条目。后台定期维护需在这些边界与质量评测稳定后再加入；当前没有启用后台整理 Worker。
