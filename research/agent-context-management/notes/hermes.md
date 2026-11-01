# Hermes Agent 上下文管理：从缓存 epoch 到事务式投影替换

研究固定快照：`NousResearch/hermes-agent@944c3a434071f43e64b443d033f3eba30c64434c`。本次为静态源码追踪，未运行真实模型摘要或测量质量。图中的token数字只能作为示例账本，不能标成产品实际压缩率。

## 核心抽象

把 Hermes 理解成一个“有持久历史的缓存 epoch 状态机”：原始记录是证据层，当前 messages/system/tools 是工作集，provider usage 是计量锚点。正常调用追加历史、复用冻结前缀；压缩是允许重写工作集并刷新system状态的边界。生成候选不是提交，摘要成功也不能直接把DB历史覆盖掉；必须通过租约、取消fence、coverage/watermark与事务发布。

需要显式分开三种“压缩”：

1. full/batch compress 默认压力管理：先裁旧结果→选head/middle/tail→中段有界摘要→候选重建→原子发布。
2. proactive prune 默认关闭：不调用LLM，按配置trigger、回收量、rearm决定是否值得破缓存。
3. rolling micro 默认关闭：在turn完成后按cadence逐个吸收旧exchange，通过辅助LLM更新rolling summary；并非廉价prune，也不是full compress之前每轮必跑。

另外，Codex app-server拥有权威thread，Hermes自己的mirror变短不会缩小provider线程，必须调用Codex自己的compact_thread。原生Responses opaque checkpoint是另一条route，计量要等实际usage。

## 上一版需要纠正/补全的地方

- “全局默认50%”只是一项raw config。小于512000窗口先提升比例到至少75%；还要在有效输入预算上计算、处理64K trigger floor、85%退化保护、显式cap和aux ceiling。200K窗口预留20K输出时非Codex路由例子为135K，不是100K。
- `usage_anchor.py` 当前已哈希**整个已计价messages prefix**，不仅最后消息；维护者文档某段仍说不fingerprint whole prefix，已漂移。它仍不构成对独立system/tools/model配置的完整指纹。
- `volatile`是system层中“重建时可能变化”的位置，不表示每轮动态刷新。常规conversation内缓存固定；压缩后invalidate才reload memory/plugin sections。
- `protect_first_n=3` 是system以外额外行，且首次压缩后衰减为0；不能说永远保留前三条。
- `protect_last_n=20` 不是无条件尾20条。count floor最多8，能按token满足空行开销时walk可不带floor。最新行、必要user/assistant、tool group才是强结构约束，并有巨大active turn的有界例外。
- 尾部保留不等于全部正文保留。lean模式可把早于最新6个tool rounds且大于1500字符的旧工具输出降为带session_search指针的stub。
- proactive prune自己的文档字符串还声称会缩短调用参数，实际 `_prune_old_tool_results` 已明确保持 canonical arguments，不改执行记录；参数仅在送summary模型的序列化副本中限长。
- 摘要模型输入已受字符截断和160K字符aggregate均匀采样限制；“LLM看了全部中段”不普遍成立。单attempt lean常规是一份aux摘要，不是map-reduce多块总结。
- failure既不是“一律删中段”也不是“一律保原文”。按auth/network/empty/length/overload分类，存在一次main-model fallback、持久cooldown、强制abort与持续故障后deterministic degrade。
- 防无效压缩breaker只在后续provider实际usage仍不低于线时计strike，两次触发；不是看消息数减少就成功，也不是静态fallback累计直接触发。

## 一个数字例子：看清阈值计算

条件：raw threshold .50，W=200000，max_tokens=20000，非Codex特例、无显式cap和aux ceiling。

```
p = max(.50, .75) = .75     # W < 512000
B = W - 20000 = 180000
ratio = floor(B × p) = 135000
floor = max(ratio, 64000) = 135000  # 64K不绑定
T = 135000
```

若W=64000、R=0，比例值48000被64K最低值抬高；floor会吃掉窗口，所以退化保护把它压回85% B=54400。若W=1000000、R=20000，未触小窗口比例floor，T=.50×980000=490000。上面都是这条代码公式的算术结果，而非实测。

## 层：生命周期比角色名称更重要


### stable system tier

- 内容：identity、SOUL、guidance、pinned/auto-load skills、coding brief
- 生命周期：一个缓存epoch保持字节稳定；pinned skill按agent首次解析后复用
- 预算与限制：context文件另有动态字符cap；不是总窗口保障

[agent/system_prompt.py:736 · def build_system_prompt_parts](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/system_prompt.py#L736)

### project context tier

- 内容：caller system_message、CWD上下文文件、workspace snapshot
- 生命周期：组装时位于stable后、volatile前；不是每轮重读
- 预算与限制：context文件默认动态cap=max(20K,min(window×4×0.06,500K))字符，显式context_file_max_chars优先

[agent/system_prompt.py:715 · def _context_files_part](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/system_prompt.py#L715)；[agent/prompt_builder.py:1055 · def _dynamic_context_file_max_chars](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/prompt_builder.py#L1055)

### volatile system tier

- 内容：skills index、MEMORY/USER、外部memory provider、plugin sections、timestamp、environment
- 生命周期：volatile指重建时更可能变化；会话中仍冻结，压缩后invalidate重新装配
- 预算与限制：占输入不可随消息摘要消失；存储/插件可各有限制，不能当成免费token

[agent/system_prompt.py:785 · volatile_parts: list[str]](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/system_prompt.py#L785)；[agent/system_prompt.py:814 · def invalidate_system_prompt](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/system_prompt.py#L814)

### tool schema floor

- 内容：agent.tools与provider最终request assembly
- 生命周期：正常每次请求携带，工具能力列表保持稳定
- 预算与限制：计入prompt压力；system+schema可成为不可压缩floor

[agent/turn_request_assembly.py:106 · def assemble_api_request](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/turn_request_assembly.py#L106)；[agent/context_compressor.py:2925 · # Anti-thrashing verdict](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L2925)

### active messages

- 内容：用户输入、assistant、tool_calls与tool results
- 生命周期：正常追加；压缩候选替换活动序列
- 预算与限制：usage anchor + appended delta；无usage时启用明确heuristic

[agent/usage_anchor.py:108 · def anchored_context_tokens](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/usage_anchor.py#L108)

### loaded skill bodies / subdirectory hints

- 内容：skill_view结果、slash user消息、文件工具结果上的子目录规则提示
- 生命周期：进入历史而非改写system；被压缩后须显式reload
- 预算与限制：占tool/user历史；SKILL_PRUNED标记指出正文已失

[agent/context_compressor.py:852 · SKILL_PRUNED_MARKER_PREFIX](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L852)；[agent/prompt_builder.py:261 · A skill placeholder containing](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/prompt_builder.py#L261)

### head / summary / tail projection

- 内容：ContextCompressor的边界选择与summary placement
- 生命周期：head额外早期消息仅首次受保护；摘要替代middle；尾部可有stub
- 预算与限制：lean默认clamp(2.5%W,10K,25K)再受20%W cap；摘要单独预算

[agent/context_compressor.py:4578 · def _protect_head_size](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L4578)；[agent/context_compressor.py:2235 · def tail_token_budget](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L2235)；[agent/context_compressor.py:5559 · def _assemble_compressed](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L5559)

### durable original transcript + archive

- 内容：SQLite session/messages archive_and_compact
- 生命周期：活动行软归档，保留可恢复证据；并发tail重排至新projection后
- 预算与限制：存储长度不受模型窗口同一上限约束

[hermes_state_messages.py:901 · def archive_and_compact](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/hermes_state_messages.py#L901)


## 主调用链与每一步输入输出

### 0 会话装配

`build_system_prompt_parts → build_system_prompt`

输入：identity/context files/memory/skills/tools。输出：冻结的三段system + 稳定tool schema。规则：H01, H02, H03。

### 1 外层入站安全网

`GatewayTurnHygieneMixin._hmwa_hygiene_plan`

输入：last actual / persisted anchor / history。输出：hygiene plan 或跳过。规则：H08。

### 2 组装turn并初查

`build_turn_context → run_turn_start_compaction`

输入：冻结system + 历史 + 新user。输出：idle/preflight compressed history 或原工作集。规则：H06, H09。

### 3 最终请求压力检查

`run_preflight_compression`

输入：完整request压力与attempt ledger。输出：允许发送 / 压缩后重启iteration / defer失败。规则：H04, H05, H06, H07, H25。

### 4 接收usage与工具输出

`provider response → compress_after_tool_results`

输入：actual prompt/completion + appended results。输出：新anchor、压缩或proactive prune候选。规则：H05, H09, H10。

### 5 准备压缩

`_compress_context → compress_context`

输入：snapshot、session lease、cancel fence、watermark。输出：唯一current attempt及fenced candidate输入。规则：H20, H21。

### 6 有损候选制作

`ContextCompressor.compress`

输入：canonical原文 + pruned copy。输出：head/middle/tail三分区。规则：H11, H12, H13, H15, H16。

### 7 摘要

`_serialize_records_for_summary → _sample_summary_records → _generate_summary`

输入：有界middle + previous summary + memory context。输出：结构化summary或abort/fallback。规则：H17, H18, H24。

### 8 重建候选

`_assemble_compressed → _finalize_compressed`

输入：head + summary + tail/stubs。输出：role合法、工具配对、actionable task恢复后的candidate。规则：H03, H19, H20。

### 9 发布

`begin_commit → archive_and_compact`

输入：candidate + covered ids + watermark/holder。输出：原行软归档、新active投影与并发tail，system重建。规则：H20, H21, H01。

### 10 新provider观测

`update_from_response`

输入：压缩后真实prompt_tokens。输出：清latch，更新ineffective verdict。规则：H23, H27。

### 11 可选逐轮维护

`finalize_turn → _micro_compact`

输入：已完成回复、旧完整exchange。输出：rolling summary marker + DB同步。规则：H22。


## 规则账本

### H01 缓存epoch内冻结system，而非每轮刷新记忆

**何时：**build_system_prompt首次或压缩后重建

**执行：**按stable→context→volatile组装；invalidate后reload memory与plugin snapshot

**保留：**稳定prefix与先前指令字节

**会失去什么：**压缩前新写memory不会立即重进system

**默认值：**正常启用

**开关 / 例外：**显式invalidate / 新epoch

证据：[agent/system_prompt.py:736 · def build_system_prompt_parts](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/system_prompt.py#L736)；[agent/system_prompt.py:814 · def invalidate_system_prompt](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/system_prompt.py#L814)

### H02 context文件按字符cap，不是总token上限

**何时：**加载CWD项目上下文文件

**执行：**显式cap优先，否则window×4×6%，clamp到20K..500K字符

**保留：**项目指导前缀；截断警告显式输出

**会失去什么：**超出cap正文不进入prompt

**默认值：**动态cap，unknown回20K

**开关 / 例外：**skip_context_files可跳过

证据：[agent/prompt_builder.py:1051 · _CONTEXT_FILE_WINDOW_FRACTION](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/prompt_builder.py#L1051)；[agent/prompt_builder.py:1063 · def _get_context_file_max_chars](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/prompt_builder.py#L1063)

### H03 loaded skill失去正文后必须reload

**何时：**历史skill_view / slash inline body被裁剪或摘要

**执行：**加入SKILL_PRUNED与skill_view(name=...)恢复提示；重置读去重缓存

**保留：**skill名字与恢复路径

**会失去什么：**正文不再模型可见；摘要不能替代执行指导

**默认值：**默认压缩路径防护

**开关 / 例外：**系统预加载skill位于stable，语义不同

证据：[agent/context_compressor.py:852 · SKILL_PRUNED_MARKER_PREFIX](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L852)；[agent/turn_preflight.py:392 · # A committed prune is a content-loss boundary](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/turn_preflight.py#L392)

### H04 usage anchor绑定整个已计价prefix

**何时：**provider返回可用prompt_tokens且有messages

**执行：**保存prompt+completion、base_count、最后消息fp和base_prefix_fp

**保留：**已确认用量锚点，可跨DB重读与进程重启

**会失去什么：**不是新请求精确token计数；tools/system独立变更需各处失效机制

**默认值：**优先使用实际锚点

**开关 / 例外：**只有整个已计价message prefix仍匹配才复用

证据：[agent/usage_anchor.py:46 · def _priced_prefix_fingerprint](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/usage_anchor.py#L46)；[agent/usage_anchor.py:67 · def capture_usage_anchor](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/usage_anchor.py#L67)；[agent/usage_anchor.py:87 · def _anchor_matches](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/usage_anchor.py#L87)

### H05 只估算锚点之后的新增delta

**何时：**_anchor_matches为true

**执行：**total=prompt_tokens+completion_tokens+rough(new appended messages)，跳过已计入completion的首个assistant

**保留：**实际prefix计数避免全量字符估算偏高

**会失去什么：**追加delta仍是粗估；reasoning计价与实际context可能不同

**默认值：**anchored优先

**开关 / 例外：**无效锚点返回None

证据：[agent/usage_anchor.py:108 · def anchored_context_tokens](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/usage_anchor.py#L108)

### H06 无anchor的粗估过线可先等一次provider证据

**何时：**first request / rewind / edit-resend / 压缩后且rough>=threshold

**执行：**threshold gate推迟一次；usage-less provider不再无限等

**保留：**不因膨胀粗估立即有损压缩

**会失去什么：**可能先遭一次真实超限拒绝

**默认值：**built-in策略

**开关 / 例外：**真实overflow总有reactive path；anchor figures不defer

证据：[agent/context_compressor.py:2983 · def should_defer_preflight_to_real_usage](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L2983)；[agent/context_compressor.py:2964 · def note_usage_less_response](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L2964)

### H07 先算有效比例，再扣输出与floor/cap

**何时：**窗口已解析

**执行：**p=global或longest substring/provider scope override；W<512000时p>=.75；B=W-R(若<=0退W)；T=max(floor(Bp),64000)，floor绑定时cap到85%B，最终还有显式cap与aux ceiling

**保留：**输出余量、小模型可触发性

**会失去什么：**不是固定50%；特定Codex route会再抬阈值

**默认值：**global .50；max_tokens未设视无预留

**开关 / 例外：**provider/model override和feasibility影响实际阈值

证据：[agent/context_compressor.py:2763 · def _effective_threshold_percent](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L2763)；[agent/context_compressor.py:2777 · def _compute_threshold_tokens](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L2777)；[agent/context_compressor.py:2751 · def _apply_threshold_tokens_cap](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L2751)

### H08 网关hygiene是更外层安全网

**何时：**入站history>=最小条件，compression enabled；usage或5000消息到线

**执行：**默认85%W gate；actual→persisted anchor→rough；检查持久cooldown与in-flight

**保留：**跨turn/重启仍能发现压力

**会失去什么：**outer粗估仍可能提前

**默认值：**85%，hard message默认5000

**开关 / 例外：**配置可变hard limit，非agent50%比例

证据：[gateway/run_turn_hygiene.py:94 · async def _hmwa_hygiene_settings](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/gateway/run_turn_hygiene.py#L94)；[gateway/run_turn_hygiene.py:156 · async def _hmwa_hygiene_plan](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/gateway/run_turn_hygiene.py#L156)

### H09 turn-start、pre-API、post-tool都可压缩

**何时：**各entry point压力到阈值且attempt budget允许

**执行：**统一调用_compress_context，重建后重启当前iteration而非继续发旧request

**保留：**新用户task、原子工具组

**会失去什么：**不能声称只在用户turn末才检查

**默认值：**max attempts默认3，配置clamp最高10

**开关 / 例外：**cooldown/structural/no-progress/ownership gates

证据：[agent/turn_context_compaction.py:129 · def run_turn_start_compaction](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/turn_context_compaction.py#L129)；[agent/turn_preflight.py:57 · def run_preflight_compression](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/turn_preflight.py#L57)；[agent/turn_preflight.py:244 · def compress_after_tool_results](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/turn_preflight.py#L244)

### H10 proactive prune默认关闭且有收益门槛

**何时：**proactive_prune_tokens>0、压力到线、rearm满足

**执行：**确定性去重/缩短旧tool result bodies、退休旧图像；至少回收4096估算token才提交。tool_call arguments作为canonical execution records不改写；它们仅在送摘要模型的序列化副本中限长

**保留：**尾部、最后完整重复副本、明确stub与archive

**会失去什么：**非尾部大tool result细节与旧图像会失去模型当前可见性；执行参数原记录不改

**默认值：**trigger=0 disabled；min result chars=8000；min reclaim4096

**开关 / 例外：**该独立prune不等同full compress的必经phase1

证据：[agent/context_compressor_prune.py:28 · def prune_tool_results_only](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor_prune.py#L28)；[agent/agent_init.py:1565 · proactive_prune_tokens=max](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/agent_init.py#L1565)

### H11 full compress phase1先裁剪候选副本

**何时：**已进入compress且有足够消息

**执行：**复制canonical用于abort/no-op；对副本_prune_old_tool_results后选中段

**保留：**abort可返回canonical；调用参数full流程不改写

**会失去什么：**成功candidate里的旧结果可能已变stub

**默认值：**每次full compression都会经历

**开关 / 例外：**不需要启用proactive trigger

证据：[agent/context_compressor.py:5447 · def compress(](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L5447)；[agent/context_compressor.py:3285 · def _prune_old_tool_results](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L3285)

### H12 早期head保护会衰减

**何时：**首次压缩前 vs 已compression_count>=1/previous_summary/重启读到summary

**执行：**system若存在始终单独保护；protect_first_n额外行首次后降为0

**保留：**system权威指令

**会失去什么：**最初用户轮不永久原样存活

**默认值：**protect_first_n=3，额外于system

**开关 / 例外：**恢复的早期handoff也触发衰减

证据：[agent/context_compressor.py:4566 · def _effective_protect_first_n](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L4566)；[agent/context_compressor.py:4578 · def _protect_head_size](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L4578)

### H13 lean尾部为预算目标，20条不是硬承诺

**何时：**tail_mode=lean

**执行：**budget=max(10K,min(25K,floor(.025W)))，再min(.20W)；walk soft ceiling=min(1.5budget,.20W)

**保留：**最新row、必要user/assistant与原子工具组

**会失去什么：**可舍弃较老optional rows；实际tail可超预算

**默认值：**lean默认；protect_last_n20仅bounded floor最多8

**开关 / 例外：**count floor在可满足空行开销时甚至floorless，保证中段可压

证据：[agent/context_compressor.py:2235 · def tail_token_budget](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L2235)；[agent/context_compressor.py:3214 · def _tail_soft_ceiling](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L3214)；[agent/context_compressor.py:4980 · def _find_tail_cut_by_tokens](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L4980)

### H14 legacy与lean不能混同

**何时：**tail_mode=legacy

**执行：**tail budget=threshold_tokens×target_ratio，再受20%W cap

**保留：**更长近期原文

**会失去什么：**更多token持续占窗口

**默认值：**target_ratio=.20但只直接控制legacy

**开关 / 例外：**lean不按target_ratio算tail

证据：[agent/context_compressor.py:2235 · def tail_token_budget](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L2235)；[agent/agent_init.py:1560 · tail_mode=str](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/agent_init.py#L1560)

### H15 原子tool group与最新行不能切断

**何时：**候选cut落在tool结果组或最新row过大

**执行：**backward/forward align保留完整assistant(tool_calls)+tool results

**保留：**调用与结果对应

**会失去什么：**单组过大会使预算无法严格满足

**默认值：**always-on结构约束

**开关 / 例外：**最后sanitize还移除孤立结果/修复孤立调用

证据：[agent/context_compressor.py:4587 · def _align_boundary_backward](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L4587)；[agent/context_compressor.py:4513 · def _sanitize_tool_pairs](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L4513)；[agent/context_compressor.py:4980 · def _find_tail_cut_by_tokens](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L4980)

### H16 超大单轮允许有界拆分，但非任意删除最新请求

**何时：**文本opening user正常大小，后续tool growth使整轮超soft ceiling

**执行：**保留组对齐mid-turn cut，opening request进入handoff并verbatim restate；图片/音频opening不可此替换

**保留：**最新用户语义与最新tool组

**会失去什么：**旧单轮中段被摘要

**默认值：**batch/manual allow_split_turn=true

**开关 / 例外：**single oversized user本身不可拆；min_tail_user_messages>1承诺优先预算

证据：[agent/context_compressor.py:4980 · def _find_tail_cut_by_tokens](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L4980)；[agent/context_compressor.py:4772 · def _reappend_inflight_user_task](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L4772)；[agent/context_compressor.py:4916 · def _ensure_last_n_user_messages_in_tail](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L4916)

### H17 摘要输入本身先有损限流

**何时：**middle过大或单message过长

**执行：**每body总6000字符(head4000/tail1500)、args1500字符；aggregate160K字符，整段均匀sample记录覆盖而非多模型分块

**保留：**全时段抽样覆盖、精确标识符和省略说明

**会失去什么：**模型从未看到被裁/采样掉的正文

**默认值：**lean一次aux summary call/attempt

**开关 / 例外：**retry/main fallback是失败重试，不是常规chunk summary

证据：[agent/context_compressor.py:830 · _SUMMARY_INPUT_MAX_CHARS =](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L830)；[agent/context_compressor.py:3423 · def _serialize_records_for_summary](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L3423)；[agent/context_compressor.py:3651 · def _sample_summary_records](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L3651)

### H18 摘要输出预算与tail独立

**何时：**middle估算token为M

**执行：**summaryBudget=max(2000,min(.20M,min(.05W,10000)))；还受aux/output room处理

**保留：**结构化goal/constraints/workstate/IDs，lean recovery hints

**会失去什么：**模型摘要有损，无召回保证

**默认值：**code ceiling10000，非文档旧值12000

**开关 / 例外：**模型返回length判失败，不当完整摘要

证据：[agent/context_compressor.py:3395 · def _compute_summary_budget](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L3395)；[agent/context_compressor.py:823 · _SUMMARY_TOKENS_CEILING =](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L823)

### H19 lean尾部旧工具结果可降成恢复stub

**何时：**结果>1500chars且早于最新6个tool rounds

**执行：**一行说明+session_search(session_id)；skill正文损失附SKILL_PRUNED

**保留：**原archive恢复路径

**会失去什么：**tail保留不等于tool正文永远保留

**默认值：**lean路径

**开关 / 例外：**required pending / skill markers按专门规则保护

证据：[agent/context_compressor_tail.py:49 · def demote_stale_tail_tools](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor_tail.py#L49)

### H20 候选生成与持久发布是不同阶段

**何时：**LLM摘要完成但未begin_commit

**执行：**在session compression lease内生成candidate，校验progress与snapshot；cancel fence赢则不写；watermark/CAS写事务

**保留：**并发新消息、原history、唯一新generation

**会失去什么：**只有成功提交后活动窗口替换

**默认值：**in_place=True默认，保留同session id

**开关 / 例外：**legacy rotation另分支；lease-less prune/micro有CAS gate

证据：[agent/conversation_compression.py:3989 · def compress_context(](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/conversation_compression.py#L3989)；[agent/conversation_compression.py:578 · def begin_commit](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/conversation_compression.py#L578)；[hermes_state_messages.py:901 · def archive_and_compact](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/hermes_state_messages.py#L901)；[hermes_state_messages.py:968 · if held_for_cas:](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/hermes_state_messages.py#L968)

### H21 摘要期间新消息必须留在新工作集尾部

**何时：**新DB rows id>summary-start watermark

**执行：**archive只覆盖held范围，concurrent tail重新排到compacted projection后

**保留：**所有未被摘要worker看到的新用户/工具到达

**会失去什么：**不把迟到输入悄悄归档

**默认值：**事务内实现

**开关 / 例外：**coverage/holder/held_for_cas验证；旧generation writer拒绝

证据：[hermes_state_messages.py:901 · def archive_and_compact](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/hermes_state_messages.py#L901)

### H22 micro是默认关闭的辅助LLM rolling summary

**何时：**final_response存在且启用micro_compact，非checkpoint_required/非persist_disabled

**执行：**finalize后每N次吸收一个head-tail之间完整exchange，更新rolling summary；达到defrag门槛再压summary

**保留：**完整exchange边界；stale commit回滚cursor/summary

**会失去什么：**该exchange正文转入rolling marker

**默认值：**micro_compact=false；every_n=1；defrag2000tokens

**开关 / 例外：**只strict True built-in callable，watermark不可回答则skip

证据：[agent/agent_init.py:1586 · micro_compact=is_truthy_value](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/agent_init.py#L1586)；[agent/turn_finalizer.py:299 · and getattr(_compressor, '_micro_compact_enabled'](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/turn_finalizer.py#L299)；[agent/micro_compaction.py:205 · def _micro_compact](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/micro_compaction.py#L205)

### H23 压缩是否有效只由后续真实usage裁决

**何时：**压缩后第一份真实provider计数仍>=threshold

**执行：**ineffective strike+1；连续2次自动breaker；低于线清零

**保留：**防system+schema不可压缩floor引起每轮循环

**会失去什么：**breaker可能留上下文仍高压，须显式处理

**默认值：**strike>=2

**开关 / 例外：**static fallback streak不触发此breaker，它改为bench failing summary model

证据：[agent/context_compressor.py:2925 · # Anti-thrashing verdict](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L2925)；[agent/context_compressor.py:3027 · def _tripped](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L3027)

### H24 失败不应统称丢middle或全部保留

**何时：**辅助summary失败，distinct model可先一次main fallback

**执行：**network/auth/quota/empty/truncated等终态保canonical；timeout/truncation60→300→900 cooldown；JSON/closed/empty30s；持续overload3次可降deterministic

**保留：**原文在abort保留，fallback含旧summary快照与可抽取anchors

**会失去什么：**fallback不保证决策/活动状态召回，显式标Unknown

**默认值：**abort_on_summary_failure=false不代表所有错误都丢原文

**开关 / 例外：**force可清cooldown；真实overflow只bypass cooldown不清状态

证据：[agent/context_compressor.py:4087 · def _on_summary_failure](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L4087)；[agent/context_compressor.py:3513 · def _build_static_fallback_summary](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L3513)；[agent/context_compressor.py:5238 · def _abort_on_summary_failure](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L5238)

### H25 provider-proven overflow有界恢复

**何时：**provider明确context too long

**执行：**计attempt，bypass_cooldown=True压缩重建；耗尽/timeout不重复送同样超限请求

**保留：**原history与unknown/partial失败状态

**会失去什么：**无法保证所有输入都可压缩进窗口

**默认值：**max attempts默认3，配置最多10

**开关 / 例外：**lock/transient defer退还attempt，不误报exhaustion

证据：[agent/turn_overflow.py:128 · def count_attempt](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/turn_overflow.py#L128)；[agent/turn_overflow.py:149 · def compress(](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/turn_overflow.py#L149)；[agent/turn_preflight.py:221 · if provider_overflow_preflight:](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/turn_preflight.py#L221)

### H26 Codex-owned thread必须让Codex压缩

**何时：**api_mode=codex_app_server

**执行：**compact_thread()操控权威线程；Hermes本地transcript保持不变

**保留：**provider真实thread ownership

**会失去什么：**Hermes自己摘要mirror无法节省Codex线程token

**默认值：**auto_compaction native默认；hermes由Hermes gate initiate；off跳自动

**开关 / 例外：**force manual可发；checkpoint_required此route拒绝，因为无真实pre-boundary

证据：[agent/conversation_compression_codex.py:51 · def _compress_context_via_codex_app_server](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/conversation_compression_codex.py#L51)；[agent/conversation_compression.py:3962 · if getattr(agent, "api_mode", None)](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/conversation_compression.py#L3962)

### H27 native opaque checkpoint不按密文字符定价

**何时：**provider原生Responses压缩已capture

**执行：**note_native_compaction_checkpoint打开await-real-usage latch，清旧rough baseline

**保留：**原生ownership与真实计量

**会失去什么：**本地看似0或密文很长都不代表真实token

**默认值：**条件route

**开关 / 例外：**与Codex app-server本地mirror不变不同

证据：[agent/context_compressor.py:2969 · def note_native_compaction_checkpoint](https://github.com/NousResearch/hermes-agent/blob/944c3a434071f43e64b443d033f3eba30c64434c/agent/context_compressor.py#L2969)


## 为什么需要事务式发布：这是上下文管理的一部分

慢summary期间可能有新的消息进入，甚至另一条compression完成。先把内存list变短再盲目写DB，会出现“新消息被归档掉”“两个generation并存”“取消后迟到worker覆盖新结果”等实际一致性问题。

Hermes将三个边界分开：

1. **计算边界**：held snapshot + 初始watermark。摘要只覆盖worker当时实际持有的消息。
2. **所有权边界**：session compression lease + attempt generation + commit fence。取消若在begin_commit前赢，则候选不能发布；提交已开始则等待提交完成，不能释放lease让第二writer中途进入。
3. **存储边界**：archive_and_compact在一个SQLite事务中软归档covered原行，插入新active projection，将未覆盖/迟到tail重排至新序列之后；lease holder和无lease writer的held_for_cas都在事务内验证。

这解释了为什么动画不应只画“旧消息缩成小块”：必须同时画原历史层、工作集层、provider计量层，另有candidate未提交状态。摘要成功、工作集重建成功、DB发布成功、后续真实usage确认有效，是四个不同事件。

## 动画分镜：每一帧都有规则


### 普通lean压缩：从缓存epoch到新的工作集

假设：W=200000，max_tokens=20000，非Codex特例、无绝对/aux cap、有效p=.75，阈值135000；示例账本起170000。proactive prune与micro都未启用；phase1 prune仍在full compress内发生。

1. **冻结提示词与schema** — 三个system tier堆入固定前缀，工具schema独立固定

   before: 原始会话存储 → after: 稳定前缀 / 活动历史

   可视对象：stable, context, volatile, tools；规则：H01, H02。

2. **工具增长与计量** — provider anchor已计价prefix，新的tool结果只加delta

   before: 用量锚点158000 → after: 示例pressure170000 > threshold135000

   可视对象：anchor, new-tool-result；规则：H04, H05, H07。

3. **拿到租约并保存原文快照** — 慢summary前capture watermark，尚未改变模型工作集

   before: DB active generation A → after: candidate generation B being prepared

   可视对象：lease, fence, watermark, canonical；规则：H20, H21。

4. **候选副本先清旧结果** — 旧大tool body变短stub；调用组边界保留

   before: candidate170000 → after: candidate150000 illustrative

   可视对象：old-tool→stub, latest-tool-group；规则：H11, H15。

5. **画出三个区域** — head首次system+额外3rows；中段待总结；lean尾部原文目标10K但anchors可超

   before: pruned history → after: head / middle / tail

   可视对象：head-lock, middle-bracket, tail-budget-marker；规则：H12, H13, H16。

6. **给summary模型的也不是全量原文** — 超大记录head/tail截断并均匀采样，保留省略标记

   before: middle full candidate → after: bounded summary input

   可视对象：sampled records, elision markers；规则：H17, H18。

7. **生成summary候选** — 目标、约束、进度、路径、identifiers与恢复指针进入摘要

   before: middle → after: summary + head + tail

   可视对象：summary, archive-pointer, SKILL_PRUNED；规则：H18, H19, H03。

8. **提交前并发输入** — 另一surface追加新user，不属于摘要快照

   before: watermark N → after: new user row N+1

   可视对象：concurrent-user-outside-coverage；规则：H21。

9. **fence赢得commit，原子替换** — 只归档covered原行；新active为candidate+N+1，旧正文仍archive

   before: generation A active → after: generation B active + concurrent tail

   可视对象：archived old-middle, active summary, active latest；规则：H20, H21。

10. **新epoch重组并再次观测** — 重读memory/plugin，工具读去重重置；下一actual裁决是否真回到阈值内

   before: old cached system → after: rebuilt system + candidate + next usage

   可视对象：new-epoch, actual-usage-verdict；规则：H01, H23。

### 真实超限与不能压缩的保护集合

假设：provider明确拒绝，先前rough deferred一次或新工具突然变大；示例只演示控制流。

1. **粗估未获证实** — unanchored estimate超阈值先等一次provider

   before: rough only → after: one request allowed

   可视对象：estimate-dashed；规则：H06。

2. **provider报context too long** — 真实拒绝取代heuristic猜测

   before: request sent → after: provider overflow

   可视对象：rejection-red；规则：H25。

3. **花一次恢复预算** — bypass cooldown一次，不清durable cooldown；走同一租约与候选流程

   before: attempt0 → after: attempt1/3

   可视对象：attempt-ledger, cooldown-bypass；规则：H25, H20。

4. **发现保护集合本身巨大** — 单个用户图片或单tool组不可分，summary不能让总request可靠进窗口

   before: oversized protected set → after: no adequate shrink

   可视对象：unbreakable group, system/schema floor；规则：H15, H16, H23。

5. **停止而非无尽重试** — 耗尽或无进展返回typed partial/error；不重复工具与同样超限request

   before: attempt budget spent → after: explicit exhaustion / deferred

   可视对象：stop, original archive；规则：H25。

### 摘要生成失败：保留原文还是显式降级

假设：区别可恢复provider失败、持续overload、默认false abort配置，不能以一个动画把所有failure画成删中段。

1. **候选未发布** — 原DB generation仍活动

   before: LLM pending → after: candidate only

   可视对象：DB-lock, uncommitted-summary；规则：H20。

2. **distinct auxiliary model失败** — 允许一次main model摘要fallback

   before: aux failed → after: one main retry

   可视对象：aux-route, main-route；规则：H24。

3. **auth/network/empty/length等终态** — abort，canonical原文保持；cooldown持久化

   before: failed candidate → after: unchanged active transcript

   可视对象：red failed candidate, unchanged DB；规则：H24。

4. **或持续故障达降级策略** — 如overload连续3次、重复stall等条件可确定性fallback，明确Active State Unknown

   before: no usable model summary → after: local extracted anchors + warning

   可视对象：fallback-symbol, unknown-state；规则：H24。

5. **仍须统一事务提交** — fallback也经过fence/lease/watermark，不能绕过一致性

   before: fallback candidate → after: commit or abort

   可视对象：same commit gate；规则：H20, H21。

### 可选micro：逐个旧exchange吸收

假设：micro_compact明确开启；默认关闭，final_response存在，非checkpoint_required。

1. **完成当前turn后** — 不是请求前：finalizer启动cadence check

   before: final response persisted → after: eligible old exchange

   可视对象：finished-turn, cadence；规则：H22。

2. **吸收一个完整exchange** — 辅助LLM把一个旧exchange合入rolling summary

   before: exchange + rolling summary → after: updated rolling summary

   可视对象：old-user+assistant+tools→summary；规则：H22。

3. **watermark/CAS检查** — 摘要期间别的compression提交则还原cursor+summary，不发布旧候选

   before: candidate rolling summary → after: accepted or true no-op

   可视对象：generation-check；规则：H20, H22。


## 不应在动画中暗示的东西

- 阈值线不是“保证不超限线”。token估算、瞬时大工具输出、保护集合不可拆、provider窗口变化都可能导致真实拒绝。
- fallback小块不能画成与LLM summary同等保真。应显式标“确定性提取 / Active State Unknown / 需要恢复检索”。
- storage archive灰化不是删除；模型工作集消失也不是agent已经知道原文。恢复必须有session_search或重新读工具行动。
- system/schema floor不应随着消息summary一起缩小；只有压缩边界重组system，且新memory可令前缀变大。
- opaque provider item可画为封装黑盒，但不要按密文字符长度算token，不能假设内部算法与本地摘要相同。
- micro动画需要“已开启”标记，单独时间位置放在finalizer之后；不能让默认normal流程每轮必跑micro。
