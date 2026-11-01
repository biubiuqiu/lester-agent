# Claude Code 上下文管理：从材料生命周期到请求重建

固定快照：`6aa7e7f8789014d42247a0545ab4e9d4e531b0f2`；仓库 https://github.com/alex000kim/claude-code

社区公开泄露镜像，未对原始npm sourcemap逐字节认证；提交日2026-04-08。编译特性和远端配置未知，不推导2026-10-22生产默认。


## 研究方法与证据边界
静态追踪函数/分支/常量，未运行镜像（不完整且非官方开源），未调用真实模型；动画数量均教学示意。


## 统一抽象
由多寿命材料组装请求，以usage锚+增量估算测压；先条件结果减量，再按管理权与开关选择SM或完整摘要；压缩后恢复文件/Skill/模式/发现状态。它是多条门控路径，不是一条所有用户必经流水线。


## 材料分层


### 系统指令与环境

来源：query.systemPrompt + getSystemContext；appendSystemContext

生命周期：每次请求外层组装，稳定部分参与prompt cache

预算：不是摘要条目；首次无usage时不能由消息粗估精确覆盖


### 用户/项目指令上下文

来源：getUserContext→getMemoryFiles→getClaudeMds；prepend meta user reminder

生命周期：memoize；主线程compact清缓存重新发现；嵌套规则按工具路径注入

预算：CLAUDE.md 40K字符是警告阈值，不是统一硬裁剪


### 工具发现与schema

来源：filteredTools、toolToAPISchema、tool_reference与boundary metadata

生命周期：非deferred常驻，发现后加载deferred；schema会话稳定；compact重新公告已发现名

预算：必须计入完整窗口；未加载deferred schema可省输入


### Skill索引与已调用正文

来源：skill_listing vs invokedSkills及Skill tool返回

生命周期：索引只发送新增；正文按调用进入；compact恢复作用域内正文

预算：索引目标1%窗口折字符；正文恢复每项5K/总25K


### 当前活动历史

来源：getMessagesAfterCompactBoundary与query state.messages

生命周期：工具轮次累积；请求投影可落盘、snip、microclear、collapse

预算：usage锚+新增消息估算；工具结果先单项/聚合预算


### 替代摘要与保留suffix

来源：SM或full CompactionResult；boundary + summary + kept + attachments +hooks

生命周期：新boundary确定活动视图；SM保留suffix，full不默认保留普通最近消息

预算：SM负载阈值检查；full负载大小日志不等于完整请求验收


### 可恢复外部记录

来源：tool-results磁盘、transcriptPath、SessionMemory文件、计划文件

生命周期：原始内容按路径再读；并不自动全部回灌prompt

预算：路径/preview占少量窗口，恢复读取会再次占预算


## 源码规则（条件→动作→不变量→代价）


### CC01 规则文件分层发现，不是单个全局 system 字符串

**条件**：会话建立 / 用户上下文缓存失效；未禁用 CLAUDE.md 发现

**动作**：getMemoryFiles 先 Managed，再 User；项目目录从上层向 cwd 依次加入 CLAUDE.md、.claude/CLAUDE.md、无条件 rules 和 CLAUDE.local.md；嵌套 git worktree 避免重复加载父仓库已签入规则

**保留/不变量**：规则路径、类型与相对发现顺序

**损失/限制**：这只是组装次序，不是平台授权优先级；文件越晚不代表可覆盖系统权限

**默认**：User/Project/Local 由 settingSources 控制；Managed 总是处理

**门控**：CLAUDE_CODE_DISABLE_CLAUDE_MDS；bare 跳过自动发现但显式目录有例外

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/utils/claudemd.ts:790-945 / export const getMemoryFiles](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/utils/claudemd.ts#L790-L945)；[src/context.ts:155-189 / export const getUserContext](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/context.ts#L155-L189)


### CC02 CLAUDE.md 被包装为首部 meta user context

**条件**：query 真正调用模型前

**动作**：appendSystemContext 将环境字典追加到 system 数组；prependUserContext 将 CLAUDE.md / 日期包装到开头的 <system-reminder> meta user message，再接活动对话

**保留/不变量**：系统指令与用户上下文分属不同载体

**损失/限制**：把 CLAUDE.md 一律说成 API system message 会画错结构

**默认**：非测试环境且 userContext 非空才 prepend

**门控**：getUserContext memoize；完整压缩后清缓存重建

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/utils/api.ts:437-485 / export function appendSystemContext](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/utils/api.ts#L437-L485)；[src/query.ts:660-668 / messages: prependUserContext](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/query.ts#L660-L668)


### CC03 规则按路径懒加载，超长警告不是硬截断

**条件**：工具访问 cwd 以下文件；匹配 conditional rule 或嵌套目录规则

**动作**：发现目标目录的 CLAUDE.md / rules；loadedNestedMemoryPaths 去重；@include 跟踪 processedPaths 防循环并限制深度

**保留/不变量**：已加载的嵌套规则避免重复发送

**损失/限制**：MAX_MEMORY_CHARACTER_COUNT=40000 在此是警告筛选，不是所有 CLAUDE.md 的发送硬上限

**默认**：include 深度上限5；40,000字符筛选超长文件

**门控**：conditional globs 与 read 路径触发；外部 include 有独立许可条件

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/utils/claudemd.ts:537-545 / const MAX_INCLUDE_DEPTH](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/utils/claudemd.ts#L537-L545)；[src/utils/claudemd.ts:1132-1144 / export function getLargeMemoryFiles](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/utils/claudemd.ts#L1132-L1144)；[src/utils/attachments.ts:1722-1742 / toolUseContext.loadedNestedMemoryPaths?.has](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/utils/attachments.ts#L1722-L1742)


### CC04 工具 schema 按发现状态选择，并稳定缓存其字节

**条件**：API queryModel 准备 tools

**动作**：tool search 开启时，非 deferred 工具始终加入；deferred 工具只在 tool_reference 已发现后加入；schema 本体按名称+inputJSONSchema缓存以减少中途漂移

**保留/不变量**：非deferred工具、ToolSearch与已发现工具

**损失/限制**：未发现的完整 schema 不进入请求；不能据此说所有工具不占窗口

**默认**：工具搜索可因模型/可用工具关闭；没有 deferred 且无待连接MCP时关闭

**门控**：isToolSearchEnabled / provider能力 / defer_loading

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/services/api/claude.ts:1155-1182 / // Dynamic tool loading](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/api/claude.ts#L1155-L1182)；[src/utils/api.ts:119-164 / export async function toolToAPISchema](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/utils/api.ts#L119-L164)


### CC05 Skill发现清单与Skill正文分开预算

**条件**：工具上下文含 Skill 工具且发现新增 skill

**动作**：清单按agent记录已发送名字，只追加delta；预算默认按窗口1%×4字符/token换成字符目标；每项描述最多250字符，压力大缩短非bundled描述甚至仅名字；正文在调用Skill后进入对话

**保留/不变量**：bundled描述、所有条目名；已调用正文在独立invokedSkills表中存续

**损失/限制**：清单预算不是正文预算；极端 names-only 与 bundled保留使目标不是严格总上限

**默认**：无窗口 fallback8000字符；可用 SLASH_COMMAND_TOOL_CHAR_BUDGET覆盖

**门控**：EXPERIMENTAL_SKILL_SEARCH 开启时清单限 bundled+MCP；其他通过发现

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/tools/SkillTool/prompt.ts:21-144 / export const SKILL_BUDGET_CONTEXT_PERCENT](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/tools/SkillTool/prompt.ts#L21-L144)；[src/utils/attachments.ts:2661-2750 / async function getSkillListingAttachments](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/utils/attachments.ts#L2661-L2750)


### CC06 当前token不是把历史账单累加

**条件**：自动阈值 / SM 初始化等读取当前窗口

**动作**：tokenCountWithEstimation 找最近真实assistant usage，计 input+cache_creation+cache_read+output，再粗估此后消息；平行工具响应拆分共享message.id时向前找同响应首片，避免漏算交错tool_result

**保留/不变量**：最后API实际观测作为锚；新增内容估计delta

**损失/限制**：不是精确重算整个新请求；首次无usage时仅消息粗估；已有usage可能反映剪裁前旧前缀

**默认**：排除synthetic消息；cache token明确占窗口

**门控**：snip/collapse等改变旧前缀需要另行校正

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/utils/tokens.ts:46-56 / export function getTokenCountFromUsage](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/utils/tokens.ts#L46-L56)；[src/utils/tokens.ts:226-261 / export function tokenCountWithEstimation](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/utils/tokens.ts#L226-L261)


### CC07 大工具结果先落盘，聚合预算只处理首次新结果

**条件**：单结果过 persistence threshold；或启用每条user消息聚合工具结果预算

**动作**：大文本结果落盘替换为preview+路径；聚合时优先替换最大 fresh 结果；过去已经原样发出的frozen结果不再改写；恢复时重放持久替换决定保持相同字节

**保留/不变量**：旧决策、完整落盘文本、图片原样、Read自己的maxTokens边界

**损失/限制**：存储失败返回原结果；frozen本身超限允许超额；这不是全局窗口安全保证

**默认**：单工具默认阈值50,000字符；聚合默认200,000字符；聚合gate默认false

**门控**：tengu_hawthorn_steeple；Infinity硬退出；远端阈值可覆盖

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/constants/toolLimits.ts:13-55 / export const DEFAULT_MAX_RESULT_SIZE_CHARS](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/constants/toolLimits.ts#L13-L55)；[src/utils/toolResultStorage.ts:447-471 / export function provisionContentReplacementState](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/utils/toolResultStorage.ts#L447-L471)；[src/utils/toolResultStorage.ts:675-697 / function selectFreshToReplace](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/utils/toolResultStorage.ts#L675-L697)；[src/query.ts:369-400 / // Enforce per-message budget](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/query.ts#L369-L400)


### CC08 实际回收顺序比微压缩→自动压缩更长

**条件**：每轮query准备活动视图

**动作**：getMessagesAfterCompactBoundary → applyToolResultBudget → 条件HISTORY_SNIP → microcompact → 条件CONTEXT_COLLAPSE投影 → autoCompact →阻塞检查→组装请求

**保留/不变量**：完整会话状态与request projection区分；collapse摘要放commit log非REPL数组

**损失/限制**：Snip/collapse源码模块在镜像缺失，不能展开其内部算法或默认开启

**默认**：门控关闭时直接跳过对应项

**门控**：HISTORY_SNIP / CONTEXT_COLLAPSE是编译特性

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/query.ts:365-455 / let messagesForQuery = [...getMessagesAfterCompactBoundary](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/query.ts#L365-L455)


### CC09 冷缓存间隔微压缩默认关闭

**条件**：明确主线程source、配置enabled，存在可解析最后assistant时间，gap≥阈值，且有可清理的旧工具结果

**动作**：按compactable tool-use IDs选择，保留最近max(1,keepRecent)，旧tool_result替换成[Old tool result content cleared]；不调用LLM；清理cached MC状态

**保留/不变量**：tool_use和tool_result配对ID；非可清理工具；最近至少1个结果

**损失/限制**：删除结果正文；没有自动产生事实摘要或通用恢复指针

**默认**：源码配置 enabled=false、gap60分钟、keep5；远端可变

**门控**：tengu_slate_heron 配置；仅明确repl_main_thread前缀；不能把undefined视为本分支主线程

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/services/compact/timeBasedMCConfig.ts:30-43 / const TIME_BASED_MC_CONFIG_DEFAULTS](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/timeBasedMCConfig.ts#L30-L43)；[src/services/compact/microCompact.ts:422-505 / export function evaluateTimeBasedTrigger](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/microCompact.ts#L422-L505)


### CC10 暖缓存微压缩是API cache editing合同

**条件**：time-based未执行；CACHED_MICROCOMPACT编译特性存在，模型支持，cached模块开关开启，主线程来源

**动作**：登记工具结果ID及user消息分组；模块返回待删IDs后排队cache_edits；本地messages原样返回；API层注入cache_reference/cache_edits；边界事件等真实usage才记录节省量

**保留/不变量**：本地正文不改，已pin编辑在原位置重发维护cache前缀

**损失/限制**：服务端视图可能移除正文；无法从镜像确认缺失模块的triggerThreshold/keepRecent默认

**默认**：cachedMicrocompact.ts不在镜像；默认与内部选择不可核验

**门控**：CACHED_MICROCOMPACT；isCachedMicrocompactEnabled；isModelSupportedForCacheEditing

**可信度**：调用方合同可证实；被调用模块缺失，内部规则未知

证据：[src/services/compact/microCompact.ts:253-301 / export async function microcompactMessages](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/microCompact.ts#L253-L301)；[src/services/compact/microCompact.ts:305-397 / async function cachedMicrocompactPath](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/microCompact.ts#L305-L397)


### CC11 自动阈值是两次扣除而非固定比例

**条件**：auto允许且无更高层机制取得上下文管理权

**动作**：W可被环境值下调；E=W−min(模型maxOutput,20,000)；T=E−13,000；百分比override仅能进一步下调；usage≥T触发

**保留/不变量**：给摘要输出与执行增长留下提前量

**损失/限制**：阈值用估算/usage锚，不是每次API请求前精确硬证明

**默认**：摘要预留上限20K、auto buffer13K；manual blocking E−3K

**门控**：DISABLE_COMPACT/DISABLE_AUTO_COMPACT/userConfig.autoCompactEnabled；反应式/CONTEXT_COLLAPSE模式抑制主动压缩

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/services/compact/autoCompact.ts:30-95 / const MAX_OUTPUT_TOKENS_FOR_SUMMARY](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/autoCompact.ts#L30-L95)；[src/services/compact/autoCompact.ts:147-248 / export function isAutoCompactEnabled](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/autoCompact.ts#L147-L248)


### CC12 递归与管理权排他

**条件**：querySource=compact/session_memory；或内部context-agent；或反应式/上下文collapse拥有压力处理

**动作**：跳过主动autoCompact，避免摘要代理自己摘要自己或并行机制互相破坏；reactive/collapse抑制不等于isAutoCompactEnabled=false

**保留/不变量**：手动compact与独立恢复入口可继续

**损失/限制**：不能在动画中把所有机制串成必执行的一个流水线

**默认**：对应feature关闭时这些分支不存在

**门控**：REACTIVE_COMPACT / CONTEXT_COLLAPSE；内部被引用实现缺失

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/services/compact/autoCompact.ts:160-265 / export async function shouldAutoCompact](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/autoCompact.ts#L160-L265)


### CC13 SM压缩先复用已有记忆，不新调用摘要模型

**条件**：自动阈值已到且SM开关启用；记忆文件有实际内容；已知最后覆盖ID存在或恢复无ID场景

**动作**：最多等正在提取的记忆15秒；选择未被记忆覆盖尾部，必要时向前扩展；把sessionMemory文件包装成summary；SessionStart(compact) hooks与plan附件加入

**保留/不变量**：确定边界后的原样近期消息；旧工具调用配对及同响应thinking兄弟片段

**损失/限制**：记忆正文可能每节截断；缺失/空模板/ID失踪则返回null改走完整摘要

**默认**：SM远端两gate默认false；ENABLE_CLAUDE_CODE_SM_COMPACT可强开，DISABLE可关

**门控**：tengu_session_memory && tengu_sm_compact；ENABLE优先于DISABLE

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/services/compact/sessionMemoryCompact.ts:403-437 / export function shouldUseSessionMemoryCompaction](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/sessionMemoryCompact.ts#L403-L437)；[src/services/compact/sessionMemoryCompact.ts:514-627 / export async function trySessionMemoryCompaction](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/sessionMemoryCompact.ts#L514-L627)；[src/services/SessionMemory/sessionMemoryUtils.ts:89-108 / export async function waitForSessionMemoryExtraction](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/SessionMemory/sessionMemoryUtils.ts#L89-L108)


### CC14 SM的40K不是硬裁尾总上限

**条件**：calculateMessagesToKeepIndex选择边界

**动作**：从lastSummarizedID后起算，再向前扩到至少10K估算tokens和5个含text消息，达到40K停止扩展；最后为所有保留tool_results回找tool_use，并保留同message.id的assistant碎片

**保留/不变量**：未摘要尾部、tool调用/结果、同响应thinking拆分组

**损失/限制**：初始未摘要suffix已大于40K时并不会截掉它；配对回拉也可超过40K

**默认**：minTokens10,000/minText5/maxTokens40,000

**门控**：SM config远端可覆盖；选完组合消息估计≥auto阈值则拒绝SM结果

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/services/compact/sessionMemoryCompact.ts:57-64 / export const DEFAULT_SM_COMPACT_CONFIG](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/sessionMemoryCompact.ts#L57-L64)；[src/services/compact/sessionMemoryCompact.ts:324-400 / export function calculateMessagesToKeepIndex](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/sessionMemoryCompact.ts#L324-L400)；[src/services/compact/sessionMemoryCompact.ts:232-315 / export function adjustIndexToPreserveAPIInvariants](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/sessionMemoryCompact.ts#L232-L315)


### CC15 完整摘要是替换活动历史，不默认保留最近对话原文

**条件**：SM不可用或手动/自动调用compactConversation

**动作**：PreCompact hook可补充指令；getCompactPrompt组织摘要要求；得到summary后创建boundary+summary+恢复附件+SessionStart hook结果；full返回值没有messagesToKeep

**保留/不变量**：摘要表达的事实、恢复文件/Skill/plan、发现的工具名metadata

**损失/限制**：最新用户原话不保证逐字保留；旧消息不是普通recent suffix继续全放进prompt

**默认**：full与partial/SM路径不可混同

**门控**：自动调用suppressFollowUpQuestions=true；partial另有独立入口

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/services/compact/compact.ts:387-457 / export async function compactConversation](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/compact.ts#L387-L457)；[src/services/compact/compact.ts:741-750 / attachments: postCompactFileAttachments](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/compact.ts#L741-L750)；[src/services/compact/compact.ts:330-340 / export function buildPostCompactMessages](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/compact.ts#L330-L340)


### CC16 摘要尽量共享原请求前缀，但不是工具型新agent随意执行

**条件**：streamCompactSummary且cache-sharing gate启用

**动作**：runForkedAgent复用system/tools/model/messages前缀与thinking参数；maxTurns1、skipCacheWrite；createCompactCanUseTool拒绝全部工具执行，保留父工具schema仅为cache-key一致；不显式改maxOutputTokens避免thinking配置改变击穿cache

**保留/不变量**：相同prefix字节与已缓存推理前缀

**损失/限制**：仍需模型推理费用/时间；fork失败/无有效text降级普通stream

**默认**：tengu_compact_cache_prefix默认true；普通stream思考disabled、摘要system替换、图片与重复恢复附件剥离

**门控**：远端kill-switch；普通stream的可选重试gate默认false

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/services/compact/compact.ts:1136-1226 / async function streamCompactSummary](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/compact.ts#L1136-L1226)；[src/services/compact/compact.ts:1125-1145 / function createCompactCanUseTool](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/compact.ts#L1125-L1145)；[src/services/compact/compact.ts:1250-1348 / // Regular streaming path](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/compact.ts#L1250-L1348)


### CC17 恢复重新读取有限文件，并恢复状态，不是只塞一段摘要

**条件**：完整摘要生成成功

**动作**：保存readFileState快照后clear；按recency选最近最多5文件、每文件5K，FileRead重新读获取新鲜内容；文件附件累计粗估≤50K；追加后台agent、plan、plan_mode、已调用skills、deferred工具/agent/MCP delta、SessionStart结果

**保留/不变量**：当前权限/模式与schema发现状态、最近文件正文、已调用Skill作用域

**损失/限制**：未选文件和过预算内容不入模型；恢复附件本身重新占窗口

**默认**：文件5个×5K；文件附件总50K；并非整个postCompact窗口≤50K

**门控**：按agent作用域；某些文件排除；保留suffix已有Read路径可跳过重读

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/services/compact/compact.ts:518-634 / const preCompactReadFileState](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/compact.ts#L518-L634)；[src/services/compact/compact.ts:1415-1475 / export async function createPostCompactFileAttachments](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/compact.ts#L1415-L1475)


### CC18 已调用Skill正文独立恢复，清单不重复广播

**条件**：compaction后getInvokedSkillsForAgent非空

**动作**：已调用skills按最近调用排序；每项保留头部约5K token，累计25K；清理不删除invokedSkills也不reset sentSkillNames；避免每次重新列全部skill名称

**保留/不变量**：该agent曾调用Skill的头部正文及可重载路径

**损失/限制**：超出Skill总预算条目不选，单项尾部截断；250字发现描述与正文完全不同

**默认**：每Skill5,000；总25,000

**门控**：invokedSkills agent-scoped；缺Skill则无附件

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/services/compact/compact.ts:1494-1545 / export function createSkillAttachmentIfNeeded](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/compact.ts#L1494-L1545)；[src/services/compact/postCompactCleanup.ts:31-77 / export function runPostCompactCleanup](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/postCompactCleanup.ts#L31-L77)


### CC19 压缩指标不等于最终完整请求大小保证

**条件**：完整摘要完成计算遥测

**动作**：postCompactTokenCount是摘要API请求的input/cache/output总usage；truePostCompactTokenCount才是boundary+summary+attachments+hooks负载估计；下一轮还要加system/tools/userContext

**保留/不变量**：指标语义明确便于诊断反复压缩

**损失/限制**：full compact仅计算willRetriggerNextTurn日志，所读函数未对完整重组请求进行强制低于阈值验收；SM另有负载阈值检查

**默认**：system/tools/userContext未算在truePostCompact负载估计中

**门控**：第一次新请求usage将重新建立完整大小基准

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/services/compact/compact.ts:626-694 / // Previously "postCompactTokenCount"](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/compact.ts#L626-L694)；[src/services/compact/sessionMemoryCompact.ts:602-624 / const postCompactTokenCount = estimateMessageTokens](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/sessionMemoryCompact.ts#L602-L624)


### CC20 摘要请求本身过长：以API round为单元丢最旧部分再试

**条件**：summary文字以prompt-too-long标准前缀开头

**动作**：最多3次缩小重试；可解析tokenGap则累计丢头部API groups覆盖gap，不可解析则丢20% groups（至少1）；至少留1组；若第一条变assistant，补synthetic user marker

**保留/不变量**：剩余API round组与角色/工具配对合法性

**损失/限制**：被丢头部不进入摘要，信息真实丢失；不是保真压缩

**默认**：MAX_PTL_RETRIES3；需至少2组才能缩小

**门控**：forkContextMessages与普通messages同步替换，避免fork继续用旧输入

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/services/compact/compact.ts:227-300 / const MAX_PTL_RETRIES](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/compact.ts#L227-L300)；[src/services/compact/compact.ts:449-495 / let ptlAttempts = 0](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/compact.ts#L449-L495)


### CC21 局部重试与会话熔断是两层预算

**条件**：完整压缩失败或用户取消；自动路径catch

**动作**：普通stream可在gate开启时最多2 attempts；summary PTL最多3次缩小；auto consecutiveFailures累加，达到3后后续auto不再尝试；成功归零

**保留/不变量**：旧上下文不会仅因失败被替换为假摘要；用户取消signal传给fork

**损失/限制**：自动失败通常不弹手动通知，下一轮可再试；最终可能需要用户缩小输入

**默认**：streamingRetry gate默认false；auto失败3次熔断

**门控**：querySource递归保护；用户abort错误不打一般错误日志但仍计failure

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/services/compact/compact.ts:131-134 / const MAX_COMPACT_STREAMING_RETRIES](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/compact.ts#L131-L134)；[src/services/compact/autoCompact.ts:257-274 / // Circuit breaker: stop retrying](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/autoCompact.ts#L257-L274)；[src/services/compact/autoCompact.ts:334-351 / } catch (error) {](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/autoCompact.ts#L334-L351)


### CC22 阻塞检查为手动压缩留空间，但有恢复机制例外

**条件**：未刚成功compact；source非compact/session_memory；反应式/collapse不拥有压力处理

**动作**：按current估计−snipFreed与E−3K比较；达到阻塞线生成invalid_request并停止；不是一味继续调用API

**保留/不变量**：留出手动compact余量；避免摘要fork被自身阻塞

**损失/限制**：新工具结果仍可能越过provider窗口；上游注释说auto off，但实际if条件还检查恢复所有者，不能简化成单一if auto-off

**默认**：默认blocking E−3K，可测试override

**门控**：reactive/collapse + 自动允许时跳过synthetic preempt，让真实API错误驱动恢复

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/query.ts:592-670 / // Block if we've hit the hard blocking limit](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/query.ts#L592-L670)；[src/services/compact/autoCompact.ts:123-142 / const defaultBlockingLimit](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/autoCompact.ts#L123-L142)


### CC23 服务端context_management与客户端微压缩是独立机制

**条件**：API参数组装调用getAPIContextManagement

**动作**：thinking有内容时可提交clear_thinking保留all或冷缓存仅最近1轮；tool清理策略只有USER_TYPE=ant且显式env启用，声明input trigger与clear_at_least

**保留/不变量**：API保留规则由声明策略表达

**损失/限制**：服务端执行内部不可从客户端复原；默认180K→40K不是cached MC缺失模块默认

**默认**：tool清理env未设则不加；thinking策略可能单独存在

**门控**：USER_TYPE ant / USE_API_CLEAR_TOOL_RESULTS / USE_API_CLEAR_TOOL_USES

**可信度**：源码直接确认；只适用于该镜像快照

证据：[src/services/compact/apiMicrocompact.ts:64-153 / export function getAPIContextManagement](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/compact/apiMicrocompact.ts#L64-L153)


## 请求调用链


### 准备稳定上下文

getUserContext → getMemoryFiles → getClaudeMds → getSystemContext

输入：settingsSources / cwd / instruction files / environment

输出：systemContext / userContext

规则：CC01,CC02,CC03


### 活动视图与结果入口控制

getMessagesAfterCompactBoundary → applyToolResultBudget → snipCompactIfNeeded

输入：full transcript / contentReplacementState / feature gates

输出：messagesForQuery / snipTokensFreed

规则：CC07,CC08


### 局部回收

microcompactMessages → evaluateTimeBasedTrigger → cachedMicrocompactPath → applyCollapsesIfNeeded

输入：messages / timestamps / gates / tool IDs

输出：possibly edited view or pendingCacheEdits

规则：CC09,CC10,CC12


### 压力计量与策略选择

tokenCountWithEstimation → shouldAutoCompact → autoCompactIfNeeded

输入：last usage / delta messages / window/model output / tracking

输出：no-op OR SM/full attempt

规则：CC06,CC11,CC12,CC21


### SM快速路径

shouldUseSessionMemoryCompaction → waitForSessionMemoryExtraction → calculateMessagesToKeepIndex → adjustIndexToPreserveAPIInvariants

输入：memory content / last covered UUID / active history

输出：summary+kept suffix+plan+hooks OR null

规则：CC13,CC14


### 完整模型摘要路径

compactConversation → streamCompactSummary → runForkedAgent → truncateHeadForPTLRetry

输入：history / PreCompact instructions / cacheSafeParams

输出：valid text summary OR failure

规则：CC15,CC16,CC20,CC21


### 恢复与装配

createPostCompactFileAttachments → createSkillAttachmentIfNeeded → processSessionStartHooks → buildPostCompactMessages → runPostCompactCleanup

输入：summary / readFileState / invoked skills / current platform state

输出：new boundary+summary+kept?+attachments+hooks

规则：CC17,CC18,CC19


### 最终请求

calculateTokenWarningState → prependUserContext → appendSystemContext → queryModel → toolToAPISchema → normalizeMessagesForAPI

输入：working view / system / user context / tools

输出：API request and new actual usage

规则：CC02,CC04,CC22,CC23


## 动画脚本：具象物件，门控可见，数量只教学示意


### 普通自动完整压缩

1. **把不同寿命的材料装成一次请求**：本场景数字均为教学示意，非真实压缩测量。W=200K，输出预留按20K；稳定材料24K，加历史/近期/新输入150K，总174K。（规则CC01,CC02,CC04,CC06）

2. **低成本步骤不一定执行**：time-based微压缩默认gate关闭；cached内部条件未证实；本场景均跳过，避免把条件机制演成必定回收。活动视图仍174K。（规则CC08,CC09,CC10）

3. **对照触发线而非等待200K满**：E=200K−20K=180K；自动触发线167K。174K≥167K，且未禁用、未递归、无collapse/反应式所有者。（规则CC11,CC12）

4. **尝试SM：此场景不可用**：SM两个远端gate默认false；本场景无可用SM摘要，因此返回null。不是固定先发一次SM模型请求。（规则CC13,CC14）

5. **发起一个受约束的摘要分叉**：共享主请求缓存前缀，追加摘要请求；拒绝全部工具执行、最多1 turn；保留工具schema仅为缓存key匹配；缓存命中只是省计算，174K内容仍需模型理解。（规则CC15,CC16）

6. **模型输出新的事实摘要**：示意生成7K摘要。原始文本不是无损编码；最新要求可能由摘要承载，full不附加普通recent suffix。（规则CC15）

7. **从权威状态恢复文件、Skill与计划**：先清FileState后经FileRead重读最近文件；按scope恢复已调用Skill正文，重建plan/mode、工具发现delta及hook结果。示意恢复18K。（规则CC17,CC18）

8. **安装边界并重新计量**：本场景最终示意49K。实际full路径只日志估算负载；system/tools/userContext必须下轮请求重组并以usage重新锚定，不能把负载数当精确总量保证。（规则CC19,CC02）

9. **继续工具循环，不无限回填旧结果**：本轮以新的活动视图调用API，真实usage建立基准；外部旧文本只有主动读取才重新进入窗口。（规则CC06,CC07,CC22）


### 开启后的Session Memory快速路径

1. **选择有条件SM路径**：假定已显式开启SM，文件有内容且覆盖UUID存在。复用已有记忆，不新增一次摘要LLM调用。（规则CC13）

2. **尾部选择而不是绝对40K裁剪**：从覆盖ID后取未摘要suffix；若只有6K/2text消息则向前扩到≥10K且≥5text，达到40K停止扩展；如果原始未摘要suffix已50K仍先保留50K。（规则CC14）

3. **配对回拉可能越过预算目标**：扫描整个suffix所有tool_result，回找缺失tool_use；同message.id的thinking拆片也纳入。保留API合法性优先于粗略上限。（规则CC14）

4. **组合摘要、尾部、plan与hooks再验收**：SM记忆每section可能截断并附完整路径；组合消息估计≥autoThreshold则拒绝该SM结果，转完整摘要。该检查仍不含完整system/schema重新计价。（规则CC13,CC14,CC19）


### 摘要也超限、缩减与熔断

1. **大批新工具结果使上下文突然膨胀**：示意已观察usage180K再加入35K结果，超出200K；估算或工具突增使提前阈值也不能绝对防止超限。（规则CC06,CC07,CC11）

2. **压缩请求本身被provider拒绝**：如果摘要请求返回prompt-too-long标准错误，不能把错误文字安装成摘要；进入缩小摘要输入流程。（规则CC20,CC21）

3. **按API-round删除最旧组**：可解析超额gap就累计删除覆盖gap；否则删最旧20%组，至少1组，至少保留1组；补synthetic user起始并保持工具配对。缩小普通messages与forkContextMessages两份输入。（规则CC20）

4. **最多三次缩小，失败保持旧活动状态**：重试仍长或剩不到2组就抛ERROR_MESSAGE_PROMPT_TOO_LONG；没有成功result不会安装新的boundary。不要演成每次失败都丢掉持久原文。（规则CC20,CC21）

5. **跨query迭代的熔断**：自动路径consecutiveFailures+1；达到3后后续autoCompact跳过。成功才清零。普通stream重试（可选2次）与PTL缩小（3次）是局部预算，不能混作一次总共3次。（规则CC21）

6. **停止还是reactive接手取决于管理权**：通常E−3K阻塞线为177K；未刚压缩、非compact/session_memory且无reactive/collapse接手时返回blocking_limit。内部reactive/collapse可绕过此synthetic阻塞，让真实API错误恢复；其实现模块不在镜像，动画到此明确边界。（规则CC12,CC22）


## 上一版必须纠正 / 补充

- time-based默认关闭，不是每次闲置都必定运行；60分钟/keep5是gate开启后的配置默认。

- 镜像缺cachedMicrocompact.ts、reactiveCompact.ts、contextCollapse等模块，内部规则无法核验。

- SM40K是停止反向扩展目标，初始suffix/配对可越界，不能画成硬裁剪到40K。

- full compact默认没有recent messagesToKeep；摘要替代全部活动对话再恢复附件。

- full truePostCompactTokenCount只计结果消息负载且不强制总量低于阈值，不是整个新请求的精确验收。

- CLAUDE.md40000字符是警告筛选，并非统一硬截断；规则在API中属于首部meta user reminder。

- tool result落盘与per-message聚合是单独入口层，聚合gate默认false；单结果50K字符/聚合200K字符，不是token。

- Skill索引预算与正文恢复预算完全不同；1%窗口是索引字符目标，正文恢复每项5K/总25K。

- 摘要fork保留schema但createCompactCanUseTool拒绝全部工具执行；schema可见性与执行授权必须分开。


## 更深一层：SM如何预先生成

### CC24 SM快速压缩背后有独立的增量记忆提取循环

**条件**：主REPL来源严格等于repl_main_thread，tengu_session_memory开启；用户上下文增长满足阈值

**动作**：初始化至少10K当前窗口；之后至少增长5K，并且自上次至少3次工具调用或最后assistant轮无tool调用；在fork隔离上下文中更新session memory文件，成功后记录覆盖UUID与提取时token基线。该fork仅允许精确memoryPath上的Edit，和完整摘要的deny-all不是同一权限策略。

**不变量**：memoryPath精确限制、覆盖UUID、当前状态与错误修正；不是每轮自动重总结

**限制**：门控默认false，源码钩子是严格repl_main_thread匹配，其他带输出style后缀来源未由此条件匹配；不能宣称所有会话默认有预先记忆

**默认**：init10000、update增长5000、toolCalls3；门控默认false

**门控**：tengu_session_memory；注册hook不等于gate开启

证据：[src/services/SessionMemory/sessionMemoryUtils.ts:32-39](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/SessionMemory/sessionMemoryUtils.ts#L32-L39)；[src/services/SessionMemory/sessionMemory.ts:134-178](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/SessionMemory/sessionMemory.ts#L134-L178)；[src/services/SessionMemory/sessionMemory.ts:277-352](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/SessionMemory/sessionMemory.ts#L277-L352)；[src/services/SessionMemory/sessionMemory.ts:460-490](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/SessionMemory/sessionMemory.ts#L460-L490)

### CC25 记忆摘要软预算、插入截断与长期AutoMem入口要分清

**条件**：更新sessionMemory prompt；或用SM作为compact结果；或读取AutoMem/TeamMem入口文件

**动作**：sessionMemory更新prompt要求每节约2K、全文件12K，并在超预算时要求condense；compact插入时对每个#一级节实际按8000字符截断；AutoMem/TeamMem入口在claudemd parse时另用line/byte cap截断。

**不变量**：被截断SM的完整memory文件路径；AutoMem入口可引导读其他文件

**限制**：prompt的12K是指令性目标，不是统一总文件硬验证；按节截断也不能保证总summary≤12K；不要把所有memory混成一个预算

**默认**：section2000×4字符；sessionMemory提示总12000

**门控**：AutoMem/TeamMem与SessionMemory是不同来源/生命周期；TEAMMEM另受构建门控

证据：[src/services/SessionMemory/prompts.ts:8-14](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/SessionMemory/prompts.ts#L8-L14)；[src/services/SessionMemory/prompts.ts:256-305](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/services/SessionMemory/prompts.ts#L256-L305)；[src/utils/claudemd.ts:381-391](https://github.com/alex000kim/claude-code/blob/6aa7e7f8789014d42247a0545ab4e9d4e531b0f2/src/utils/claudemd.ts#L381-L391)
