# Codex 深入上下文管理研究

固定快照：`61f34c35af77777db4d77d0ea2aeb8547a6b4523`。

以持久rollout保存可恢复事件，以world-state full/diff维持权威指令，以ContextManager保存有界活动工作集；按provider能力和运行特性选择local文本摘要、remote V2 opaque checkpoint或TokenBudget新window。压缩不是单个阈值函数，而是计量、策略、保留、安装、恢复的协议。

## 研究边界

读取固定commit静态调用链和配置/测试定位；未运行模型请求、未验证远端压缩语义和线上服务端开关。

## 先纠正上版简化

- 补上AGENTS发现与字节预算、world-state full/diff替换语义。
- 区分active usage与累计消费账单；actual+增量并非所有内容精确tokenizer。
- remote V2原文保留64K，与local20K不同；是客户端可见代码，不需猜服务端。
- remote预裁剪从尾部连续outputs，不是最老消息。local摘要超限才删最老item。
- TokenBudget table默认false，但模型元数据/合格实验路径可运行时启用，不能绝对声称off。
- pre-turn旧history检查未含新input（TODO）；普通sampling超限直接返回而非普遍同轮自动压缩重试。

## 层与状态所有权

### 持久事件/原始记录（rollout）

来源：SessionMeta、ResponseItem、TokenUsageRecord、WorldState、TurnContext、Compacted等rollout项。生命周期：跨进程；resume/fork读取；历史记录不是全部逐轮重放到provider。预算：磁盘/存储策略，不是模型window预算。
- [codex-rs/core/src/session/mod.rs:4472 let rollout_items = [](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/mod.rs#L4472-L4490)

### 权威上下文状态（world state）

来源：当前模型、权限、AGENTS、环境、skills目录、tools能力与extension贡献。生命周期：首次full；之后diff；压缩后full重建；snapshot单独保存。预算：分组件预算，如AGENTS32KiB、namespace简述4KiB；不代表总请求限额。
- [codex-rs/core/src/session/mod.rs:4996 pub(crate) async fn record_context_updates_and_set_reference_context_item](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/mod.rs#L4996-L5076)
- [codex-rs/core/src/session/world_state.rs:59 pub(crate) async fn build_world_state_for_step](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/world_state.rs#L59-L94)

### 活动会话工作集（ContextManager）

来源：已记录并经工具截断的ResponseItemEnvelope数组，含用户、assistant、tools和context diff。生命周期：每步增长；合法配对/模态归一化；压缩时替换或前缀替换。预算：actual anchor + local增量；full与scope双检查。
- [codex-rs/core/src/context_manager/history.rs:603 pub(crate) fn for_prompt](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/context_manager/history.rs#L603-L626)
- [codex-rs/core/src/context_manager/history.rs:939 pub(crate) fn get_total_token_usage](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/context_manager/history.rs#L939-L967)

### 真正模型请求（Prompt）

来源：normalized history + 当前tool specs/base instructions/output schema，或incremental history中的base/tools。生命周期：每次sampling/compaction构造；网络缓存优化不等于内容免占window。预算：provider语义约束；本地估计不能精确计opaque项。
- [codex-rs/core/src/session/turn.rs:1672 pub(crate) fn build_prompt](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/turn.rs#L1672-L1703)

### 压缩表示/原文保留区

来源：local:20K user文本+summary；V2:64K筛选原消息+opaque compaction；TokenBudget:new-window权威context+可选client items。生命周期：替换history并重置world-state reference；之后增长至下一次压缩。预算：20K与64K为特定保留策略预算，不是整轮总量/summary上限。
- [codex-rs/core/src/compact.rs:63 const COMPACT_USER_MESSAGE_MAX_TOKENS](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact.rs#L63-L66)
- [codex-rs/core/src/compact_remote_v2.rs:87 pub(crate) const RETAINED_MESSAGE_TOKEN_BUDGET](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact_remote_v2.rs#L87-L92)
- [codex-rs/core/src/session/mod.rs:4919 pub(crate) async fn start_new_context_window](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/mod.rs#L4919-L4977)

## 实际调用链

### 启动/恢复

`Session::spawn/config resolution → initial_history.get_base_instructions → load_project_instructions`

输入：config；model catalog；rollout；filesystem。输出：base指令；技能快照；指令来源；恢复活动history。规则：DX01, DX02, DX22。

### turn前检查

`run_turn → run_pre_sampling_compact → maybe_run_previous_model_inline_compact`

输入：旧working set；当前/前一model；上次usage。输出：保持旧window或完成压缩；新输入尚未入working set。规则：DX08, DX09, DX10, DX11。

### 当前上下文与输入注入

`record_context_updates_and_set_reference_context_item → build_skills_and_plugins → run_hooks_and_record_inputs`

输入：current world state；user input；explicit skills。输出：full或diff items；skill body fragments；用户输入记录。规则：DX03, DX04。

### 请求投影与采样

`ContextManager::for_prompt → build_prompt → try_run_sampling_request`

输入：active items；input modalities；model_visible_specs。输出：合法工具配对/模态的Prompt；assistant/tool calls/usage。规则：DX05, DX06, DX07, DX23。

### 工具结果回写与续跑

`record_item_with_metadata → context_window_token_status → run_auto_compact`

输入：tool results；实际usage+增量；needs_follow_up。输出：被限长的工具项；normal/local/remote/new-window分派。规则：DX06, DX08, DX10, DX12。

### 压缩/安装

`run_compact_task_inner_impl → run_remote_compact_v2_attempt → build_v2_compacted_history → replace_compacted_history`

输入：captured prefix/current working set；world state；feature gates。输出：替换工作集；retained originals；summary/opaque项；持久checkpoint。规则：DX13, DX14, DX15, DX16, DX17, DX19, DX21, DX22。

### 下轮验证

`recompute_token_usage → record_token_usage_info → context_window_token_status`

输入：replacement history；next provider response。输出：局部估计重置→新的actual观测；后续正常增长。规则：DX07, DX08, DX21。

## 逐条执行规则

### DX01 base instructions 在启动时选定来源优先级

- 触发条件：新建/恢复会话
- 动作：优先 config.base_instructions，其次历史 session_meta.base_instructions，否则渲染当前模型 instructions_template；动态 tools 定义也能从 session metadata 恢复
- 保留：启动时选中的指令来源和 provenance
- 丢失/限制：不是每轮随机重拼一个独立 system prompt；模型切换可通过 world state 差异补充
- 默认：正常启动规则
- Gate：自定义配置/恢复历史/模型元数据共同决定
- 证据等级：源码直接可见
- [codex-rs/core/src/session/mod.rs:808 // Resolve base instructions for the session.](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/mod.rs#L808-L854)

### DX02 AGENTS.md 发现有目录、信任与字节预算边界

- 触发条件：可信项目且 project_doc_max_bytes > 0
- 动作：从项目根到 cwd 搜集；默认 .git 定位根；同层优先 override，再 AGENTS，再配置 fallback；跨环境共享 remaining byte budget；读超预算则截断
- 保留：host-provided user instructions 与预算内仓库规则；根到叶顺序
- 丢失/限制：超预算尾部不可见；untrusted project 不加载项目 docs；这不是 tokens 预算
- 默认：project_doc_max_bytes 默认 32 KiB；LOCAL_AGENTS_MD_FILENAME 为 AGENTS.override.md
- Gate：project_root_markers 可配置；空 markers 禁用上溯
- 证据等级：源码直接可见
- [codex-rs/core/src/agents_md.rs:58 pub(crate) async fn load_project_instructions](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/agents_md.rs#L58-L121)
- [codex-rs/core/src/agents_md.rs:125 async fn read_agents_md](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/agents_md.rs#L125-L205)
- [codex-rs/config/src/config_toml.rs:75 pub const DEFAULT_PROJECT_DOC_MAX_BYTES](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/config/src/config_toml.rs#L75-L85)
- [codex-rs/core/src/agents_md.rs:272 fn candidate_filenames](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/agents_md.rs#L272-L322)

### DX03 规则不是每轮追加全量：world state 通过 snapshot/diff 更新

- 触发条件：首次 real turn 无 reference_context_item，或之后 world state 变化
- 动作：首次渲染 full initial context 并建立 baseline；后续只 emit diff，记录 WorldState 与 TurnContext 供恢复；压缩安装后重新建立 full baseline
- 保留：有效当前环境/权限/模型/规则/能力信息
- 丢失/限制：若把所有历史规则都当永久有效会误读；AGENTS 变化时新消息明确 replace/remove
- 默认：稳定轮不重复无变化段落
- Gate：WorldStateSection 各段 render_diff
- 证据等级：源码直接可见
- [codex-rs/core/src/session/mod.rs:4996 pub(crate) async fn record_context_updates_and_set_reference_context_item](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/mod.rs#L4996-L5091)
- [codex-rs/core/src/context/world_state/agents_md.rs:44 fn render_diff](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/context/world_state/agents_md.rs#L44-L85)

### DX04 Skill 目录元数据与被显式调用正文分开注入

- 触发条件：真实用户输入命中 explicit skill mentions
- 动作：读取当前 skills_snapshot，load_skill_prompts 只为 mentioned skills 生成 fragments；避免同一已注入路径重复；Guardian 不把父 transcript 里的 mention 当指令加载
- 保留：当前明确选择的 Skill instructions 和错误/警告
- 丢失/限制：不能从“catalog 中有 skill”推断它的全文已进入请求
- 默认：host skills snapshot 每 turn 记录；正文按 mentions 加载
- Gate：guardian session 特殊 skip；plugin dependency/access 规则仍独立
- 证据等级：源码直接可见
- [codex-rs/core/src/session/turn.rs:1104 async fn build_skills_and_plugins](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/turn.rs#L1104-L1239)
- [codex-rs/core/src/session/turn_context.rs:1323 let skills_snapshot = if](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/turn_context.rs#L1323-L1361)

### DX05 模型请求 = 归一化活动历史 + 当前工具 specs + base + output schema

- 触发条件：每次 sampling 前
- 动作：history.for_prompt 按模型输入模态归一化；build_prompt 单独加入 tool_router.model_visible_specs 和 final output schema；incremental_tools 模式则把 base/tools 放进 history，不再在顶层重复
- 保留：工具调用配对；当前可用 schema；声明的输出schema
- 丢失/限制：unsupported images/audio 被剥离；工具 namespace 4KiB 简述限额不等于完整工具schema总预算
- 默认：普通模式单独发 specs；responses-lite incremental 模式有条件
- Gate：use_responses_lite + incremental tools settings
- 证据等级：源码直接可见
- [codex-rs/core/src/session/turn.rs:1672 pub(crate) fn build_prompt](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/turn.rs#L1672-L1703)
- [codex-rs/core/src/context_manager/history.rs:968 fn normalize_history](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/context_manager/history.rs#L968-L991)
- [codex-rs/core/src/session/world_state.rs:44 pub(crate) async fn current_window_uses_incremental_tools](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/world_state.rs#L44-L63)
- [codex-rs/core/src/context/world_state/tools.rs:29 const MAX_RENDERED_FRAGMENT_BYTES](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/context/world_state/tools.rs#L29-L35)

### DX06 进入工作集时先对工具输出做局部截断

- 触发条件：record_item_with_metadata 收到 FunctionCallOutput/CustomToolCallOutput
- 动作：使用 metadata.history_truncation_token_limit 覆盖，或模型 truncation_policy + 序列化 allowance 截断 payload，再记录 processed item
- 保留：调用ID、工具语义和预算内输出
- 丢失/限制：大输出在进入后续请求前已可能丢部分正文；不能认为摘要能补回工作集中从未保留的字节；record_annotated_items明确只截断live history，完整rollout payload保留
- 默认：模型元数据可为 byte/token policy；未知slug fallback descriptor 是10,000 bytes，不应宣称所有模型都10K tokens
- Gate：可由tool_output_token_limit / 每结果 metadata override
- 证据等级：源码直接可见
- [codex-rs/core/src/context_manager/history.rs:555 fn record_item_with_metadata](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/context_manager/history.rs#L555-L598)
- [codex-rs/core/src/context_manager/history.rs:518 /// Tool output truncation applies only to live history](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/context_manager/history.rs#L518-L535)
- [codex-rs/core/src/session/step_settings.rs:203 tool_output_token_limit: self.tool_output_token_limit](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/step_settings.rs#L203-L213)
- [codex-rs/models-manager/src/model_info.rs:98 pub fn model_info_from_slug](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/models-manager/src/model_info.rs#L98-L144)
- [codex-rs/models-manager/src/model_info.rs:32 if let Some(token_limit) = config.tool_output_token_limit](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/models-manager/src/model_info.rs#L32-L48)

### DX07 active usage 是上次请求实际观测 + 新增项估算，不是生命周期总账

- 触发条件：采样后或新增本地上下文后判断预算
- 动作：last_token_usage.total_tokens 加最后 model-generated item 之后新增项的估算；服务端未含旧 reasoning 时再加相关旧reasoning估计
- 保留：provider 已知基线与新tool/user/diff增长
- 丢失/限制：估计是 coarse lower bound；不是精确tokenizer；billing total 不能用作当前窗口大小
- 默认：Sampling usage 更新 last；SpendingOnly 只累计消费，不替换当前context测量
- Gate：server_reasoning_included 决定是否补估旧reasoning
- 证据等级：源码直接可见
- [codex-rs/core/src/context_manager/history.rs:939 pub(crate) fn get_total_token_usage](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/context_manager/history.rs#L939-L967)
- [codex-rs/core/src/context_manager/history.rs:661 pub(crate) fn estimate_token_count_with_base_instructions](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/context_manager/history.rs#L661-L688)
- [codex-rs/core/src/session/mod.rs:5178 pub(crate) async fn record_token_usage_info](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/mod.rs#L5178-L5256)

### DX08 同时检查 auto scope 与整个模型窗口

- 触发条件：计算 ContextWindowTokenStatus
- 动作：Total 直接对 active；BodyAfterPrefix 扣当前window prefill baseline；full limit = resolved window × effective percent；任一硬线触发；剩余量取两者较小
- 保留：完整context硬约束独立于body预算
- 丢失/限制：BodyAfterPrefix 不意味着前缀不占模型window；baseline未知时先以active作baseline，之后实测替代
- 默认：scope 默认 Total；effective_context_window_percent 默认95
- Gate：body scope可配置；fallback buffer仅有token_budget fallback prompt时适用
- 证据等级：源码直接可见
- [codex-rs/core/src/session/context_window.rs:53 async fn context_window_token_status_with_config](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/context_window.rs#L53-L130)
- [codex-rs/protocol/src/openai_models.rs:529 pub fn auto_compact_token_limit](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/protocol/src/openai_models.rs#L529-L545)
- [codex-rs/protocol/src/openai_models.rs:393 const fn default_effective_context_window_percent](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/protocol/src/openai_models.rs#L393-L397)
- [codex-rs/protocol/src/config_types.rs:49 pub enum AutoCompactTokenLimitScope](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/protocol/src/config_types.rs#L49-L57)

### DX09 90% 只是模型基础自动阈值，不是所有触发规则

- 触发条件：resolved_context_window 已知
- 动作：auto_compact_token_limit=min(显式模型limit, floor(window×0.9))；无显式limit使用90%；full/分域/备用buffer/手动/模型降档另判断
- 保留：当前模型真实catalog/config约束
- 丢失/限制：window未知时不生成90%假限制；不是任何情况下input+output精确安全保证
- 默认：基础90%；有效window默认95%是另一线
- Gate：用户覆盖model/window metadata与scope可变
- 证据等级：源码直接可见
- [codex-rs/protocol/src/openai_models.rs:529 pub fn auto_compact_token_limit](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/protocol/src/openai_models.rs#L529-L546)
- [codex-rs/core/src/session/context_window.rs:98 let auto_compact_fallback_buffer_tokens](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/context_window.rs#L98-L127)

### DX10 触发分布在 turn 前、需要续跑时的 turn 中、可选 turn 后

- 触发条件：turn 开始 / 采样完成还有工具或输入续跑 / turn真正结束
- 动作：Pre-turn先看旧history；Mid-turn needs_follow_up 且token limit/new_window请求/async candidate时切换；Post-turn只在配置percent>0且无pending input且未取消时尝试
- 保留：已完成答复在post-turn压缩失败时保留
- 丢失/限制：源码TODO：最前面的pre-turn检查发生在新user/context diffs入history之前，无法声称总能提前阻断巨大新输入
- 默认：post_turn_percent 默认0；AutoCompactionDisabled线程扩展可停自动压缩
- Gate：TokenBudget与普通summarization不同；guardians另有特殊检查
- 证据等级：源码直接可见
- [codex-rs/core/src/session/turn.rs:173 pub(crate) async fn run_turn](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/turn.rs#L173-L216)
- [codex-rs/core/src/session/turn.rs:664 let should_roll_over =](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/turn.rs#L664-L717)
- [codex-rs/core/src/session/turn.rs:783 // Token-budget resets do not summarize](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/turn.rs#L783-L837)
- [codex-rs/core/src/config/mod.rs:4463 model_post_turn_compact_threshold_percent: cfg](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/config/mod.rs#L4463-L4469)

### DX11 模型降档或 comp_hash 不兼容可以在阈值前先压缩

- 触发条件：两端comp_hash均存在且不同，或切换到更小window且旧context超过新限制
- 动作：用previous model/previous access program总结旧window，但replacement step使用current model；单纯换model未满足条件不一定触发
- 保留：旧window模型兼容性与当前新模型状态
- 丢失/限制：未知hash不等于不兼容；远端fallback仅特定错误条件
- 默认：按真实switch状态判断
- Gate：Total scope与BodyAfterPrefix降档判断不同
- 证据等级：源码直接可见
- [codex-rs/core/src/session/turn.rs:1412 fn comp_hash_changed](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/turn.rs#L1412-L1423)
- [codex-rs/core/src/session/turn.rs:1422 async fn maybe_run_previous_model_inline_compact](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/turn.rs#L1422-L1518)

### DX12 先按模式分派，不是所有压缩都做文本摘要

- 触发条件：run_auto_compact
- 动作：TokenBudget优先开始新window；否则provider能力V2走remote；Unsupported走客户端编排local LLM摘要
- 保留：当前选择路径的生命周期/hooks/事件
- 丢失/限制：local不代表离线确定性算法；remote算法不在仓库内
- 默认：provider OpenAI/Azure配置默认V2，通用provider默认Unsupported，可override
- Gate：model/provider能力与features先决
- 证据等级：源码直接可见
- [codex-rs/core/src/session/turn.rs:1513 async fn run_auto_compact](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/turn.rs#L1513-L1633)
- [codex-rs/model-provider/src/capabilities.rs:31 pub(crate) fn from_config](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/model-provider/src/capabilities.rs#L31-L57)

### DX13 remote请求发送前仅对连续新工具输出应急减量

- 触发条件：remote前 history粗估 > model_context_window
- 动作：从尾部反向walk，FunctionCallOutput/CustomOutput变超窗stub，ToolSearchOutput清空tools；直到估计合窗或遇user/developer/不可重写item/受保护结果就停
- 保留：call_id及工具事件形状；边界前历史
- 丢失/限制：这是最新连续尾部工具结果裁剪，不是删除最老message，也不是无条件剪全部tool结果
- 默认：只在窗口已知且估计超窗时
- Gate：RetainClientToolOutputs启用时被标记结果成为不可跨越边界
- 证据等级：源码直接可见
- [codex-rs/core/src/compact_remote_history.rs:70 pub(crate) fn trim_function_call_history_to_fit_context_window](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact_remote_history.rs#L70-L153)
- [codex-rs/core/src/compact_remote_history.rs:138 fn rewritten_output_for_context_window](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact_remote_history.rs#L138-L202)
- [codex-rs/core/src/compact_remote_v2_attempt.rs:104 trim_function_call_history_to_fit_context_window(](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact_remote_v2_attempt.rs#L104-L133)

### DX14 remote压缩请求是特殊协议：追加CompactionTrigger，期望恰好一个Compaction item

- 触发条件：V2路径
- 动作：构造带base/specs的Prompt，附CompactionTrigger，发送流；必须收到completed且恰好1个Compaction输出；丢掉trigger后保留input metadata供重建
- 保留：明确的replacement checkpoint和source metadata
- 丢失/限制：客户端不能验证encrypted_content语义保真，不能臆测远端摘要模板/抽取算法
- 默认：remote transport retries最多2且不超过provider stream retry上限
- Gate：opaque provider contract；无Completed或错误数量判失败
- 证据等级：源码直接可见
- [codex-rs/core/src/compact_remote_v2_attempt.rs:172 input.push(ResponseItem::CompactionTrigger](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact_remote_v2_attempt.rs#L172-L203)
- [codex-rs/core/src/compact_remote_v2.rs:464 if compaction_count != 1](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact_remote_v2.rs#L464-L486)
- [codex-rs/core/src/compact_remote_v2.rs:91 MAX_REMOTE_COMPACTION_V2_STREAM_RETRIES](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact_remote_v2.rs#L91-L98)

### DX15 remote客户端还会自己保留原始消息，不只收一个黑盒

- 触发条件：remote输出成功后 build_v2_compacted_history
- 动作：先筛真实user/hook，合资格AgentMessage；client developer和标记tool outputs依features；筛完再按64,000 tokens从新到旧截断；最后追加opaque compaction item
- 保留：预算内真实用户原文/必要注释/可选client结果
- 丢失/限制：合成环境/仓库规则不是普通user对话；assistant旧输出默认不逐条保留；图片按默认开启的image budget原子保留/丢弃
- 默认：RETAINED_MESSAGE_TOKEN_BUDGET=64,000；agent单条最多10,000；CompactionImageBudget table true
- Gate：RetainClientDeveloperMessages/RetainClientToolOutputs table false，运行可配置
- 证据等级：源码直接可见
- [codex-rs/core/src/compact_remote_v2.rs:480 fn build_v2_compacted_history](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact_remote_v2.rs#L480-L518)
- [codex-rs/core/src/compact_remote_v2.rs:529 fn is_retained_for_remote_compaction_v2](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact_remote_v2.rs#L529-L590)
- [codex-rs/core/src/compact_remote_v2.rs:87 pub(crate) const RETAINED_MESSAGE_TOKEN_BUDGET](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact_remote_v2.rs#L87-L95)
- [codex-rs/core/src/compact_remote_v2_images.rs:31 pub(super) fn truncate_message_to_token_budget](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact_remote_v2_images.rs#L31-L103)
- [codex-rs/features/src/lib.rs:2142 id: Feature::CompactionImageBudget](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/features/src/lib.rs#L2142-L2165)

### DX16 local摘要：用户原文20K预算 + 文本摘要 + 当前权威context

- 触发条件：provider Unsupported且TokenBudget未开启
- 动作：克隆history追加summary prompt并调模型；选用户消息从新到旧最多20,000 tokens；超额边界截断文本，媒体不复制；加CompactionSummary，再重建当前world state
- 保留：预算内真实user文本、摘要、权威初始context
- 丢失/限制：旧assistant和工具链主要依赖摘要；文本摘要也有损；media不等于完整保留
- 默认：COMPACT_USER_MESSAGE_MAX_TOKENS=20,000
- Gate：不是remote64K策略；完整返回须通过hooks与安装
- 证据等级：源码直接可见
- [codex-rs/core/src/compact.rs:63 const COMPACT_USER_MESSAGE_MAX_TOKENS](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact.rs#L63-L69)
- [codex-rs/core/src/compact.rs:263 async fn run_compact_task_inner_impl](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact.rs#L263-L295)
- [codex-rs/core/src/compact.rs:710 fn build_compacted_history_with_limit](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact.rs#L710-L815)
- [codex-rs/core/src/compact.rs:84 pub(crate) async fn build_compaction_replacement_history](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact.rs#L84-L119)

### DX17 摘要请求本身过长时，local路径从最老项开始逐项缩小

- 触发条件：local summary调用被ContextWindowExceeded拒绝且输入多于1 item
- 动作：history.remove_first_item并重试；若被移除项是call/output会同步移除配对项；只剩1 item仍失败则markfull并报错；Interrupted/TurnAborted/预算耗尽/不完整响应不作为可无限重连
- 保留：较近上下文与合法工具配对
- 丢失/限制：最早信息可能在摘要生成前就不在输入；该规则与remote最新连续output stub不是一个策略
- 默认：每次过长删除一个合法项组，直到可发或1项失败
- Gate：一般transport重试受provider.max_retries约束
- 证据等级：源码直接可见
- [codex-rs/core/src/compact.rs:358 Err(e) if matches!(e.details(), CodexErrorDetails::ContextWindowExceeded)](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact.rs#L358-L392)
- [codex-rs/core/src/context_manager/history.rs:685 pub(crate) fn remove_first_item](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/context_manager/history.rs#L685-L699)

### DX18 TokenBudget table默认false，但模型默认与实验条件可开启

- 触发条件：token_budget feature / model_messages.token_budget / ContextManagement
- 动作：显式配置优先；无显式偏好且模型defaults enabled=true可开启；实验路径还校验ChatGPT套餐、模型支持、provider/auth，启用history notes extension
- 保留：预算提示、新window ID、可选历史notes提示
- 丢失/限制：不能由feature table false推断所有线上模型都关闭；也不能由代码存在推断所有人开启
- 默认：TokenBudget与ContextManagement table false；模型defaults可覆盖实际启动激活
- Gate：managed requirements仍可pin off
- 证据等级：源码直接可见
- [codex-rs/features/src/lib.rs:1980 id: Feature::TokenBudget](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/features/src/lib.rs#L1980-L1992)
- [codex-rs/core/src/session/token_budget.rs:123 pub(super) fn apply_model_defaults](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/token_budget.rs#L123-L166)
- [codex-rs/core/src/session/token_budget.rs:21 pub(super) fn apply_experimental_context](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/token_budget.rs#L21-L71)

### DX19 TokenBudget新window不生成隐式摘要

- 触发条件：start_new_context_window
- 动作：重新渲染initial context，仅保留feature允许的client tool/developer项（同64K选择器），其余旧workset清除；替换checkpoint、recompute usage；显式notes/外部记忆必须由对应机制承接
- 保留：当前权威context、window lineage和可选client items
- 丢失/限制：普通user历史/assistant/tools不自动变为summary；这是语义上重要的窗口重置
- 默认：仅TokenBudget路径生效
- Gate：保留client features表默认false；notes extension另有gate
- 证据等级：源码直接可见
- [codex-rs/core/src/session/mod.rs:4919 pub(crate) async fn start_new_context_window](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/mod.rs#L4919-L4983)
- [codex-rs/core/src/compact_token_budget.rs:42 async fn run_compact_task_inner](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact_token_budget.rs#L42-L69)
- [codex-rs/core/src/session/mod.rs:4674 // Keep the legacy bridge hint when native Notes is disabled.](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/mod.rs#L4674-L4721)

### DX20 异步压缩是对前缀做投机，正常append不作废，替换历史会作废

- 触发条件：AsyncCompaction=true，TokenBudget=false，provider V2，窗口达到async百分比且还没硬超
- 动作：捕获history generation/len、model/hash/auth generation和world state；后台请求，正常前台可继续append后缀；重复相同attempt不再发
- 保留：前台live suffix；截止时点world state
- 丢失/限制：请求失败或被废弃依然可能收费；stop不再启动新candidate，session shutdown取消并drain usage
- 默认：AsyncCompaction table false；async默认阈值65%，允许feature config覆盖
- Gate：必须provider V2；pre-turn未有turn_state不启动
- 证据等级：源码直接可见
- [codex-rs/core/src/session/async_compaction.rs:40 pub(super) fn enabled](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/async_compaction.rs#L40-L55)
- [codex-rs/core/src/session/async_compaction.rs:52 pub(super) async fn maybe_start](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/async_compaction.rs#L52-L152)
- [codex-rs/core/src/context_manager/history.rs:715 self.history_version = self.history_version.saturating_add(1)](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/context_manager/history.rs#L715-L723)
- [codex-rs/core/src/config/mod.rs:226 const DEFAULT_ASYNC_COMPACTION_THRESHOLD_PERCENT](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/config/mod.rs#L226-L229)

### DX21 采用后台结果要再次校验，并保留live suffix与新目标编辑

- 触发条件：安装Prefix/Complete replacement
- 动作：Prefix在同一state lock核验generation、长度、auth generation；检查model/hash；拼上未压缩suffix，刷新world-state baseline；Complete保留快照之后新接受的goal edits
- 保留：压缩启动后新输入/结果/目标更新
- 丢失/限制：过期结果不能覆盖新历史；candidate成功也不代表工作集已足够小
- 默认：stale候选discard；采用后仍超budget最多再尝试一次foreground
- Gate：fallback为Skip或Foreground由触发现场决定
- 证据等级：源码直接可见
- [codex-rs/core/src/session/mod.rs:4359 // Validate immediately before slicing and replacing the captured prefix.](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/mod.rs#L4359-L4439)
- [codex-rs/core/src/compact_remote_v2_installation.rs:85 if let Some(candidate) = &candidate](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact_remote_v2_installation.rs#L85-L95)
- [codex-rs/core/src/compact_remote_v2_installation.rs:29 if let Some(candidate) = candidate](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/compact_remote_v2_installation.rs#L29-L72)

### DX22 checkpoint写入rollout后，resume按replacement重建工作集

- 触发条件：local/remote/TokenBudget成功安装
- 动作：统一replace_compacted_history写Compacted replacement_history + WorldState + TurnContext + applied thread settings；记录window lineage、producer hash、usage与资源来源；保持原始日志作为历史，而不是把旧全文继续发模型
- 保留：恢复基线、opaque producer兼容信息、retained metadata
- 丢失/限制：完整rollout不等于provider可见history；malformed opaque checkpoint保留为未知/不可用而非退回旧授权
- 默认：同一checkpoint机制跨路径复用
- Gate：持久化成功与usage重估/服务端下次usage不同
- 证据等级：源码直接可见
- [codex-rs/core/src/session/mod.rs:4430 let replacement_history = items.clone()](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/mod.rs#L4430-L4505)
- [codex-rs/history/src/compaction_checkpoint.rs:36 pub fn is_usable](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/history/src/compaction_checkpoint.rs#L36-L61)

### DX23 正常sampling被provider拒绝不等于本轮总会自动compact重试

- 触发条件：普通run_sampling_request返回ContextWindowExceeded
- 动作：mark当前context full，返回error；下一轮precheck因full可进入压缩。Guardians exhausted review budget有独立受限压缩分支，不能泛化给普通会话
- 保留：已经记录的输入与执行证据
- 丢失/限制：巨大新user输入可越过旧history precheck；不能声称token策略数学保证从不拒绝
- 默认：普通错误分支直接返回；不是普遍无限自动compact/retry
- Gate：guardian特殊分支另有gate；manual compact也可由用户触发
- 证据等级：源码直接可见
- [codex-rs/core/src/session/turn.rs:1807 CodexErrorDetails::ContextWindowExceeded => {](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/turn.rs#L1807-L1813)
- [codex-rs/core/src/session/turn.rs:196 // TODO(ccunningham): Pre-turn compaction runs](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/turn.rs#L196-L204)
- [codex-rs/core/src/session/turn.rs:837 && !guardian_budget_compacted](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/core/src/session/turn.rs#L837-L854)

### DX24 软件回退窗口不是provider已经确认的能力

- 触发条件：未知model slug或用户覆盖metadata
- 动作：未知slug使用最小fallback descriptor：context_window/max_context_window=272,000且标used_fallback_model_metadata；配置window override被max_context_window向下夹住；预算使用resolved元数据但不能将fallback当实测
- 保留：软件能够继续构建请求；来源标记存在
- 丢失/限制：fallback仍可能与真实provider窗口不一致；手填1M也不能绕过catalog max clamp
- 默认：unknown slug fallback272K；不是所有unknown都保持None
- Gate：models_manager config override；具体catalog可能有不同最大窗口
- 证据等级：源码直接可见
- [codex-rs/models-manager/src/model_info.rs:98 pub fn model_info_from_slug](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/models-manager/src/model_info.rs#L98-L153)
- [codex-rs/models-manager/src/model_info.rs:19 pub fn with_config_overrides](https://github.com/openai/codex/blob/61f34c35af77777db4d77d0ea2aeb8547a6b4523/codex-rs/models-manager/src/model_info.rs#L19-L48)

## 动画脚本（演示数字，不是实测）

### 同一个任务从增长到remote V2压缩

前提：说明性数字，不是实测或压缩率保证；W=200,000，Total scope，无fallback buffer；基础自动线180,000；full线默认190,000；provider V2；TokenBudget=false；Async=false。

0. **权威状态与历史分居两个容器**
   - Before：{"active": 40000, "rawLog": "保留全部已有事件"}
   - After：{"active": 40000}
   - 画面：world-state蓝色8K；真实user黄色12K；assistant/tools灰色20K
   - 规则：DX01, DX03, DX08。日志大容器与模型工作集分开；缓存不能擦掉蓝黄灰占用

1. **工具循环增长，不重复注入全部规则**
   - Before：{"active": 40000}
   - After：{"active": 175000}
   - 画面：无变化world-state保持；assistant/tool组连续追加；usage观测刷新
   - 规则：DX03, DX06, DX07。每次工具输出先过局部限长；当前用量基于观测与增量，不是从第一轮账单累计

2. **达到auto线且还要继续工具任务**
   - Before：{"active": 175000, "autoLine": 180000}
   - After：{"active": 185000, "needs_follow_up": true}
   - 画面：最近工具组追加10K；180K触发线亮起
   - 规则：DX08, DX10。needs_follow_up=true才在该处走mid-turn rollover；完成答复且post_turn=0不会在该分支强制摘要

3. **发送特殊compaction请求**
   - Before：{"active": 185000}
   - After：{"request": "history+base+tools+CompactionTrigger"}
   - 画面：opaque远端处理盒；触发器小卡片
   - 规则：DX12, DX14。不画成已知的“服务端总结8个字段”；本仓库只证明协议和输出形状

4. **客户端筛原文并安装replacement**
   - Before：{"raw": "真实user24K + old assistant/tools153K + world8K"}
   - After：{"activeIllustrative": 44000, "rawLog": "旧记录仍在"}
   - 画面：蓝world-state8K重新渲染；黄user24K保留；紫opaque12K（演示）；灰assistant/tools移出工作集但留日志
   - 规则：DX15, DX21, DX22。64K是原文保留区预算；opaque大小12K仅示意，不是读取encrypted字符得出的值

5. **下一轮用新观测校准**
   - Before：{"activeEstimate": 44000}
   - After：{"active": "以新provider usage更新"}
   - 画面：绿色actual anchor；之后新增tool增量
   - 规则：DX07, DX22。客户端先recompute估计，下次actual才提供新测量；完整请求仍需服从硬窗口

### remote压缩输入已经太大：只裁最新连续输出

前提：W=200K；远端请求预估214K；先前user/developer内容受保留边界保护；TokenBudget=false；V2；不假定任何服务端摘要率。

0. **倒序检查末尾**
   - Before：{"items": ["user", "assistant(call)", "tool A 30K", "tool B 18K"], "estimate": 214000}
   - After：{"cursor": "tool B"}
   - 画面：两张同批tool卡片；user/assistant边界
   - 规则：DX13。不是从最老message开始

1. **工具B替换为stub**
   - Before：{"estimate": 214000}
   - After：{"estimateIllustrative": 196100}
   - 画面：tool B正文淡出；call_id与超窗提示保留；tool A仍在
   - 规则：DX13。估计合窗即停；此处只损失B正文，仍保留调用/输出形状

2. **若仍超窗继续A，遇非output就停止**
   - Before：{"estimateCase2": 230000}
   - After：{"stopAt": "assistant call/user boundary"}
   - 画面：不可跨越边界的红线；RetainClientToolOutputs保护标志
   - 规则：DX13。不能保证能缩到window，尤其巨大user本身不可由此裁剪

3. **只有provider有效结果才安装**
   - Before：{"candidate": "未完成"}
   - After：{"install": "response.completed且1个Compaction"}
   - 画面：协议校验闸门；旧history保持直到成功结果
   - 规则：DX14, DX21。无completed/0或2个compaction都报错，不把部分流当有效checkpoint

### local摘要自己的输入超限

前提：provider Unsupported；普通summary路径；已完成原始工具副作用不能重放；以下删除只作用于当前摘要请求的history副本。

0. **摘要请求被400 ContextWindowExceeded拒绝**
   - Before：{"summaryRequest": "old history+summary prompt"}
   - After：{"error": "ContextWindowExceeded"}
   - 画面：最老user U1；call/result配对；近期U3；摘要指令
   - 规则：DX16, DX17。不要动画成摘要模型可以吃下任意大的历史

1. **移除最老item及必要配对再发**
   - Before：{"oldest": "U1"}
   - After：{"history": "去掉U1后的副本"}
   - 画面：U1从摘要输入移出；原始rollout保留
   - 规则：DX17。若删除call则同时删配对output，避免孤立结果

2. **若摘要成功，构造20K用户文本保留区**
   - Before：{"summaryInput": "可行大小"}
   - After：{"replacement": "近期用户文本≤20K + summary + world-state"}
   - 画面：用户原文20K标尺；summary紫卡；当前蓝规则
   - 规则：DX16, DX22。用户原文保留区从实际活跃history选，不等于“只总结前20K”

3. **只剩一项仍过长则明确失败**
   - Before：{"items": 1, "error": "ContextWindowExceeded"}
   - After：{"outcome": "markfull + 返回error"}
   - 画面：无法裁剪的巨大单项红卡；失败终止
   - 规则：DX17。失败不生成假的summary，也不无限同样请求

### 后台前缀压缩与前台新后缀如何共存

前提：显式AsyncCompaction=true；TokenBudget=false且provider V2；generation只在history replacement等改变，普通append不改变该generation。

0. **捕获前缀边界**
   - Before：{"generation": 7, "len": 40}
   - After：{"capture": ["generation7", "len40", "model/hash", "auth-owner", "cutoff world-state"]}
   - 画面：前40项蓝框；背景请求紫盒
   - 规则：DX20。捕获在锁内完成

1. **前台继续append**
   - Before：{"len": 40}
   - After：{"len": 44, "generation": 7}
   - 画面：40项前缀保持；新增4项橙色suffix
   - 规则：DX20。普通append不让候选失效，正是并发节省等待的目的

2. **采用时再校验**
   - Before：{"candidate": "generation7/len40"}
   - After：{"replacement": "新prefix+权威state+原4项suffix"}
   - 画面：前缀收缩；橙suffix不变；auth/model gate
   - 规则：DX21。若generation变8、auth owner改变或model/hash不匹配，候选discard

3. **加上suffix后仍超预算?**
   - Before：{"prefixPlusSuffix": "仍可能过大"}
   - After：{"fallback": "最多再试一次foreground路径"}
   - 画面：suffix撑满新window；一次foreground标记
   - 规则：DX21。后台成功不等于最终工作集一定合窗；废弃的完成请求usage仍计费

### 巨大新输入为何可能仍被provider拒绝

前提：普通非Guardian turn；旧history40K；新用户输入230K；本地pre-turn TODO未纳入全部incoming items。

0. **pre-turn只检查旧工作集**
   - Before：{"active": 40000}
   - After：{"precheck": "未触发"}
   - 画面：旧40K条；新user230K仍在待注入区
   - 规则：DX10。不能声称有总请求精确发前安全保证

1. **注入后请求达到270K**
   - Before：{"active": 40000}
   - After：{"activeIllustrative": 270000}
   - 画面：巨大用户卡加入；模型窗口200K红线
   - 规则：DX05, DX23。新input能越过precheck

2. **普通sampling context error**
   - Before：{"provider": "ContextWindowExceeded"}
   - After：{"state": "markfull；本轮返回错误"}
   - 画面：红色拒绝；已存用户输入不丢
   - 规则：DX23。一般分支不自动在同轮compact-and-retry；Guardian另有专门分支

3. **后续显式或下一轮触发压缩**
   - Before：{"active": "标记full"}
   - After：{"nextPrecheck": "可进入compaction"}
   - 画面：用户可见失败状态；后续压缩动作
   - 规则：DX10, DX17。输入无法缩小且保护集本身太大仍可能无法恢复

## 不可推断

- 服务端V2怎样编码/抽取/保留语义不可由此仓库证明。
- 模型catalog远端当前值、运行时feature activation会随配置与模型变更；固定快照不等于所有用户当前行为。
- 本地token估算是下界/近似，图片audio/opaque内容可能与provider定价不同。
- 本次没有实际模型压缩率、召回/延迟/成本benchmark。
