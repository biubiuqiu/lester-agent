# Pi 上下文管理深研

固定快照：`b9fb2b854b7a19b28a68c3421365fee0a5342ce6`，获取地址 `badlogic/pi-mono`。当前包名与文档已有 `earendil-works/pi`，以下所有判断固定本commit，不凭当前名称推断其他版本。研究为静态源码追踪；未运行真实模型、未执行测试，也不声称压缩效果优于其他框架。

## 先从问题到抽象

假设会话已经184K token、模型窗口200K。用户把最后一次工具结果缩短成一句话后，磁盘还应能导出原始结果，但下一轮不能继续按旧的184K usage判断。与此同时，system prompt里的项目规则和tool schema不应随着旧前缀被摘要而丢失，失败的assistant若含toolCall则必须连配套结果一起退出下一请求。Pi用三个投影关系解决这些问题：

1. **原始树**：JSONL的追加entry，id/parentId决定分支，不因摘要删除原文。
2. **活动工作集**：当前leaf路径→最新checkpoint→firstKeptEntryId近期后缀→context_edit替换/省略；每个projected entry保留sourceEntry。
3. **provider请求**：工作集经extension transform、custom-role转换、system/tool状态重放，再按transport能力编码。这个请求不保证与UI可见历史或原始导出相等。

贯穿三层的两个预算门：先判断是否需要压缩工作集，再决定本次请求还可以要多少输出。它们的估算函数、常量和语义不同。窗口大小、reserve、keep也不是同一概念。

## 组装层：规则、Skill、工具不是一团字符串

规则发现从agentDir与cwd祖先链发生。每个目录只选优先候选中的第一份可读规则，global在前、祖先由远到近；嵌套linked worktree有去重同逻辑仓库范围处理。规则以带path的project_instructions装入system project_context。

system是结构化状态事件。默认包含preamble、tools、rules、docs，再加append配置、项目规则、Skill目录、cwd及自定义section。开始run和下一assistant前只把变动section作为patch持久化；null表示删section。tool schema也在system消息上用toolsAdded/toolsRemoved记录。当前state可以重放成完整head；不支持中途system的provider把中途patch折叠到头部。这样修改一个工具不必在持久日志复制全部system，但真实请求仍按provider能力计入有效指令与schema，绝非免费缓存。

Skill默认渐进披露：先发name/description/location，模型按任务读正文。/skill:name把去frontmatter正文装成带location的user块。disableModelInvocation只阻止自动目录，不阻止显式调用。没有read/bash能力则目录提示也不会照常存在。压缩时读入的正文通常已在消息层，并非所有Skill永远钉在system。

## 树与投影：持久“修改视图”而非改原文

CompactionEntry不只包含summary/firstKeptEntryId/tokensBefore。它还保存当前完整SystemMessage（包含tool状态），因为保留范围内旧system消息会跳过，不能靠随机幸存的历史patch恢复规则。模型重建时得到 `[checkpoint.systemMessage, summary-as-user, retained entries, later entries]`。

context_edit是当前分支的追加entry。它指向更早的可编辑entry，null让该entry贡献零条model message；非null只替换content。作用范围是当前path可见的edit，原始内容和metadata不变。plain custom entry、usage、labels等只存状态；custom_message即使display=false仍可进入上下文。UI隐藏与模型省略是两种规则。

## 预算与自动检查的真实时机

压缩设置默认enabled=true、reserveTokens=16,384、keepRecentTokens=20,000。严格条件是 `contextTokens > contextWindow - reserveTokens`。200K窗口对应183,616触发线；等于线不触发。exact provider/modelId可分别覆盖reserve和keep，enabled则是全局开关。

计数优先最近有效非零usage，totalTokens优先，否则input+output+cacheRead+cacheWrite；新追加消息用chars/4估算。error/aborted的usage不当有效基线。compaction/context_edit在usage之后发生会使旧计数失效，需按当前system重放态与非system消息估算，不能沿用旧大窗口usage而反复压缩。

检查发生在三个时点：新run准备时检查上一assistant；已有lastCompletedTurn后的工具循环下一assistant前，prepareNextTurnWithContext检查当前projection（已含工具结果）；agent_end之后处理overflow或threshold。关键时序：_prepareRun检查后才加本次user/handler消息；工具循环preflight之后才注入新的prepared/steering与system patch。故实体模型首请求的大user输入、preflight之后的新内容没有必然被完整预检。virtual模型则在prepareRequest中等实际route已知后拿routed window再判，消息已加入。

真正发包前AI simple-options还有输出clamp：`min(desired output, max(1, modelWindow - estimatedInput - 4096))`。这里AI估算是chars/3.5，既不是压缩层chars/4，也不是精确tokenizer。reserveTokens只是提前压缩预留；generateSummary的旧_reserveTokens参数已不使用，摘要用模型输出limit，再受该层clamp。输入如果本身太大，这个最少1输出机制无法保证请求成功。

## 压缩：规划→生成→提交三步不可混画

**规划**：在canonical projection上，从后向前累计到keep，找user-like/assistant有效切点。toolResult从来不是切点；assistant call+后续result必须保持结构。巨大单用户跨度可在assistant中途切，之前的原始用户要求变为turn-prefix摘要。因工具原子组和边界，20K只是近似近期选择目标，不是严格上界。

**生成**：旧历史和previousSummary合并成结构化摘要。split-turn另外概括被切开的当前跨度前缀，必要时总共两次摘要调用。摘要请求是独立system+一个序列化user payload，不直接把主system/tools都带过去；thinking丢弃，tool result仅前2,000字符并注明省略，路径和tool arguments则进入记录。缓存写保留为none，摘要LLM调用按配置重试瞬时断流，但错误、长度打满与toolCall结果不得成为checkpoint。

**提交**：只有生成成功且signal未取消，才appendCompaction并刷新projection。原树保留，新增checkpoint持有system/tool快照。estimatedTokensAfter在结果中报告，但此处没有强制“必须减少多少且小于线”的校验；所以不能将图画成摘要成功必然恢复安全。扩展可在session_before_compact取消或提供自定义summary和boundary，已不属于默认摘要质量保证。

## 超限、length与取消：状态机比公式更关键

识别包括provider错误字符串、successful stop但usage.input+cacheRead超过window的silent overflow、length且零输出/接近满输入等。一般recoverable length要求输出少于原始model desired maxTokens，而不是少于已被context clamp后的request max。length带toolCalls时不执行可能截断参数，先生成失败tool results保持配对。

失败attempt先持久化，turn_end、agent_end生命周期完成后再post-run恢复。_omitRecoveryAttempt把该assistant及callId匹配的结果各写一条null context_edit。随后尝试overflow compaction，成功后以fresh run retry一次；第二次仍失败就报告并停止。成功stop的silent overflow只压缩、不重新回答已完成响应。

重要例外：如果omission已经提交，后面的summary失败或取消，omission仍在历史中；不会追加compaction，也不调度内部retry。不能为了动画好看而把它画成整个恢复事务全部回滚。manual compact先abort当前run，完成后也不自动续跑该run。压缩期间到达的steering/follow-up按正常队列规则投递；代码在prepare耗时后会重新poll steering，又避免one-at-a-time一次取两条。

## 分支摘要是另一种预算产品

/tree显式summarize离开分支时，先找old leaf与target共同祖先，收集离开路径。分支摘要默认window-reserve(16384)由新到旧取消息；未知window暂用128K。与正常compaction不同，它跳过toolResult，读取raw entry路径，context_edit并不作为canonical projection套用。旧summary有重要性例外，在已取量低于90%预算时可勉强纳入。最后在目标leaf追加BranchSummaryEntry；旧分支仍保留。它解决知识迁移，不是固定窗口滑动的同一个函数。

## 动画应分成三个场景

1. 正常增长：持久树在底部累积；上方projection只显示当前工作集；中间画切点，旧prefix流入摘要器，工具call/result成对留在近期区；checkpoint生成后system/tool快照与summary+suffix成为新发包。
2. Provider拒绝：先展示实际失败记录，再出现两条context_edit把assistant与结果从视图划掉（底部原文不消失）；摘要成功才进入新run重试；失败分支留下omission而无checkpoint、无retry。
3. 分支返回：树上old leaf与target定位共同祖先；仅离开路径进入branch摘要；摘要箭头落在目标新分支，不把所有其他分支混进模型上下文。

所有token物件大小应显式标“教学设定”，只有阈值公式是代码数值推导；原始日志/工作集/provider请求必须三个独立容器，防止把删视图画成删数据。

## 逐条规则与源码证据

### PI01 仓库规则按目录范围加载，并非简单拼入全部同名文件

- 条件：资源加载 / reload 时
- 动作：先读 agentDir 的一份规则，再从文件系统祖先到 cwd；每目录按 AGENTS.override.md、AGENTS.md、AGENTS.MD、CLAUDE.md、CLAUDE.MD 找到首个可读普通文件；路径去重，嵌套 linked worktree 避免主库同范围规则重复
- 保留：选中的规则正文与文件路径
- 可能丢失 / 边界：同目录低优先级候选不会同时加入；不是递归加载 cwd 下所有子目录规则
- 默认与开关：默认资源加载行为；自定义 resourceLoader / noContextFiles 等设置可改变
- 证据：[function loadContextFileFromDir](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/resource-loader.ts#L184-L210)；[export function loadProjectContextFiles](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/resource-loader.ts#L232-L277)

### PI02 system 是可重放的 section patch，tool schema 也是状态事件

- 条件：开始 run 或下一 assistant 响应前，配置/工具集有变化
- 动作：buildSystemPromptSections 组装 preamble/tools/rules/docs/addendum/project_context/skills/cwd；diffSystemPromptSections 仅返回改变或删除的 section；工具声明用 toolsAdded/toolsRemoved；provider 不支持中途 system 时折叠为一个当前 head
- 保留：当前有效指令、工具声明和顺序化变化
- 可能丢失 / 边界：hidden tools 的声明在请求投影中移除；历史差分不是每轮重复全文
- 默认与开关：默认 preamble 下分 section；customPrompt 替换默认前缀但不自动去掉项目/skill/cwd；before_agent_start 可修改 options，forceSystemPrompt 可替换本轮最终请求而不覆写持久结构
- 证据：[export function buildSystemPromptSections](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/system-prompt.ts#L128-L195)；[export function diffSystemPromptSections](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/system-prompt.ts#L217-L229)；[private _preparePromptAndToolLoadout](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L1758-L1778)；[export function getCurrentSystemMessage](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/ai/src/utils/transcript.ts#L73-L120)

### PI03 Skills 目录先提供目录索引，正文按需进入消息

- 条件：存在 read/bash（包括 hidden 间接 reader）并且发现 skills
- 动作：system 只列可模型调用 skill 的 name/description/location，提示按需读文件；显式 /skill:name 命令读 SKILL.md、去 frontmatter，把完整正文包装为 user skill block
- 保留：skill 元信息；真正调用后正文进入相应 user/工具消息
- 可能丢失 / 边界：索引不包含所有正文；disableModelInvocation 的 skill 不出现在自动目录，但显式命令可用
- 默认与开关：默认渐进披露；不是所有 skill 全文常驻 system；reader 可用性、noSkills、自定义 resource loader、input handlers
- 证据：[export function formatSkillsForPrompt](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/skills.ts#L358-L400)；[private _expandSkillCommand](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L2211-L2244)；[const readers =](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/system-prompt.ts#L175-L185)

### PI04 原始 session 树、活动投影、provider 请求是三种不同对象

- 条件：每次 prepareRequest / reload / compaction
- 动作：从 leaf 沿 parentId 找活动分支；取最新 compaction 和它的 kept suffix；apply context_edit；convertToLlm 再把自定义消息映射成 provider roles；transformContext 可再变换
- 保留：原始 entry 与 provenance、模型/思考状态、当前投影
- 可能丢失 / 边界：其他分支不自动进工作集，metadata-only/custom state/usage entry 不自动发给模型；display=false 不等于排除上下文
- 默认与开关：append-only tree + canonical projection；扩展可用 context transform 修改当次发包
- 证据：[function buildSessionPath](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/session-manager.ts#L390-L424)；[export function buildSessionProjection](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/session-manager.ts#L543-L577)；[export function convertToLlm](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/messages.ts#L148-L195)；[async function streamAssistantResponse](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/agent/src/agent-loop.ts#L390-L416)

### PI05 context_edit 是分支局部的追加事件，不修改原文

- 条件：appendContextEdit(targetId,replacement)
- 动作：目标必须在当前分支且为可编辑 message/custom_message；null 令其模型贡献为空，非 null 只换内容而非身份/元信息；同一目标采用当前可见路径中最后 edit
- 保留：原始记录、ID、审计历史；其他分支不受本 edit 影响
- 可能丢失 / 边界：模型不再看 omitted 内容；replacement 不是原文
- 默认与开关：公开 API 与恢复逻辑都使用；不适用于 system/compaction/meta 等不可编辑 entry
- 证据：[appendContextEdit(targetId](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/session-manager.ts#L1360-L1413)；[function projectContextEntry](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/session-manager.ts#L519-L546)；[const edits = new Map](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/session-manager.ts#L551-L567)

### PI06 compaction 触发计数优先 usage，编辑后避免旧 usage 冒充当前量

- 条件：评估压缩时
- 动作：取最近非 error/aborted、非零 usage；用 totalTokens 或 input+output+cacheRead+cacheWrite；追加消息按 chars/4；若后面有 context_edit 或 compaction 使 usage 过时，则按当前重建 system + 非 system 消息重新估算
- 保留：来源是否 usage-backed 与 trailing estimate 的区分
- 可能丢失 / 边界：不是精确 tokenizer；没有覆盖 provider 序列化的数学保证；图片以4800估计字符计入
- 默认与开关：纯估算与 usage anchor 混用；与 provider 请求 clamp 的 chars/3.5 是另一层
- 证据：[export function calculateContextTokens](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/compaction.ts#L140-L164)；[export function estimateContextTokens](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/compaction.ts#L196-L236)；[export function estimateProjectedContextTokens](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/compaction.ts#L227-L273)；[export function estimateTokens](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/compaction.ts#L298-L351)

### PI07 触发公式与按模型覆盖

- 条件：compaction.enabled=true，current context > model.contextWindow-reserveTokens
- 动作：自动压缩；严格大于而非 >=；默认 reserve=16384、keepRecent=20000；每个 provider/modelId 精确键可分别覆盖两项，再回退普通配置、内置默认
- 保留：低于触发线时正常继续
- 可能丢失 / 边界：触发线不是压缩后的目标总量；keep 不是摘要输出上限
- 默认与开关：enabled=true，reserveTokens=16384，keepRecentTokens=20000；普通/override 只能非负安全整数；无效配置报错；enabled 全局
- 证据：[export const DEFAULT_COMPACTION_SETTINGS](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/compaction.ts#L126-L133)；[export function shouldCompact](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/compaction.ts#L267-L271)；[private getCompactionTokenSetting](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/settings-manager.ts#L833-L891)

### PI08 检查点不是只有 agent_end：请求前、run后都检查

- 条件：_prepareRun、prepareNextTurnWithContext 与 post-agent handling
- 动作：新run开始先检查上次assistant，随后才加入本次user/handler消息；low-level loop已有lastCompletedTurn后，下一assistant前针对当前projection做threshold检查（已含工具结果），然后才注入prepared/steering；virtual prepareRequest在消息加入后按实际route窗口再检查；agent_end后处理overflow/threshold
- 保留：已完成响应不被盲目重试；新用户消息/steering 在准备阶段照队列顺序注入
- 可能丢失 / 边界：实体模型首请求前并非对本次新user作完整重新预检；工具轮preflight后的steering、section patch与extension transform也可继续增大，所以preflight不是强保证
- 默认与开关：自动检查开启时；实体 model preflight window<=0 跳过；virtual 路径单独处理
- 证据：[private async _compactBeforeNextAssistantResponse](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L776-L791)；[private _installAgentNextTurnRefresh](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L898-L939)；[private async _prepareRun](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L1926-L1940)；[private async _handlePostAgentRun](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L1979-L2028)；[if (this._exceedsCompactionThreshold(route.model](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L838-L844)

### PI09 切点按估算近期量选，不能切到孤立 tool result

- 条件：prepareCompaction 对 canonical projection 规划
- 动作：从后向前累加估计，达到 keepRecentTokens 时选附近有效 user-like/assistant 切点；toolResult 不是切点；巨大单轮可切在 assistant，找到该用户跨度起点并标记 split
- 保留：assistant tool-call 与后续结果的结构连续性；近期后缀
- 可能丢失 / 边界：keep 是近似目标，原子工具组可使实际超过；用户输入在巨大单轮前缀时可能被概括，不能宣称无条件原样保留最新 user
- 默认与开关：默认 keep约20000；特殊闭合 recovery omission suffix 可推进边界；任意 metadata 不得让未发送输入消失
- 证据：[function isCutPointMessage](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/compaction.ts#L351-L376)；[function findProjectedCutPoint](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/compaction.ts#L789-L873)；[export function prepareCompaction](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/compaction.ts#L859-L922)

### PI10 连续压缩是在投影上更新上一份摘要

- 条件：已有最新 compaction
- 动作：projection 把最新 checkpoint 放首位，把上次 firstKeptEntryId 起的幸存消息放其后；preparation 单独拿 previousSummary，针对当前幸存前缀重新总结；更老 compaction 即使原始ID落在保留区内也不再次贡献摘要
- 保留：上一摘要 + 上次幸存但现在要概括的消息
- 可能丢失 / 边界：摘要反复总结可累积遗漏，原文还在 session；不会把全原始树每次重新喂入
- 默认与开关：默认摘要更新 prompt；扩展可替换 summary/boundary
- 证据：[export function buildContextEntries](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/session-manager.ts#L476-L520)；[sourceEntry.type === "compaction" && index > 0](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/session-manager.ts#L562-L568)；[export function prepareCompaction](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/compaction.ts#L859-L925)

### PI11 普通摘要和 split-turn 摘要的调用数不同

- 条件：isSplitTurn 且 turnPrefixMessages非空
- 动作：先概括历史（有历史需概括时），再单独概括当前用户跨度的前缀，两份文本合并；无新历史时复用 previousSummary / No prior history；普通情况只调用历史摘要
- 保留：当前单轮请求与进度在专用 turn context 区
- 可能丢失 / 边界：最多两次摘要请求（每次内部还可能 transient retry），不是永远一次；不包含 kept suffix 原文
- 默认与开关：内置 compaction；session_before_compact 可以提供自定义结果跳过默认推理
- 证据：[export async function compact(](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/compaction.ts#L952-L1067)；[async function generateTurnPrefixSummary](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/compaction.ts#L1062-L1104)

### PI12 摘要输入先做有损序列化，摘要输出不以 reserveTokens 封顶

- 条件：调用 generateSummaryWithUsage / turn prefix summary
- 动作：convertToLlm 后转带角色标签文本；丢 thinking，tool result 最多2000字符并附省略量；生成结构化 goal/constraints/progress/decisions/next/critical summary；_reserveTokens 参数不再使用
- 保留：可见文本、工具名称参数、前2000字符结果、上次摘要
- 可能丢失 / 边界：工具结果2000字符后半截与thinking不进摘要推理；不能说摘要保留全部事实
- 默认与开关：摘要用模型输出上限，不是16384固定输出；completeSummarization cacheRetention=none；独立请求不继承全主对话 system/tools
- 证据：[const TOOL_RESULT_MAX_CHARS](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/utils.ts#L94-L152)；[export async function generateSummaryWithUsage](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/compaction.ts#L696-L770)；[function buildSummarizationContext](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/compaction.ts#L682-L695)；[export async function completeSummarization](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/compaction.ts#L618-L645)

### PI13 输出预算另有 provider 层 clamp

- 条件：buildBaseOptions/simple API 发请求
- 动作：desired max=调用者maxTokens或model.maxTokens；maxTokens=min(desired,max(1,window-estimatedContext-4096))；AI estimator 用chars/3.5、tool schema与当前可用 usage，不同于compaction层chars/4
- 保留：至少1个输出token请求；非估计的usage优先
- 可能丢失 / 边界：是估算式输出预算，不会裁剪保护输入；因此输入本身过长仍会被拒
- 默认与开关：CONTEXT_SAFETY_TOKENS=4096、MIN_MAX_TOKENS=1；model.contextWindow<=0 时仅确保输出>=1；低层/自定义stream可能不同
- 证据：[export function clampMaxTokensToContext](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/ai/src/api/simple-options.ts#L18-L25)；[export function buildBaseOptions](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/ai/src/api/simple-options.ts#L36-L53)；[const CHARS_PER_TOKEN](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/ai/src/utils/estimate.ts#L15-L21)；[export function estimateContextTokens](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/ai/src/utils/estimate.ts#L97-L117)

### PI14 只在成功并未取消后追加 compaction，保存完整 system/tool 态

- 条件：摘要或扩展结果返回，abort signal 未取消
- 动作：appendCompaction(summary,firstKept,tokensBefore,details,fromHook,usage)，附当前 getCurrentSystemMessage 快照；firstKept为null时用自身ID表示不保留旧消息；刷新projection并计算estimatedTokensAfter
- 保留：原始日志不删除，当前指令与tool loadout不会随着旧system事件被丢弃
- 可能丢失 / 边界：tokensBefore/estimatedAfter 是报告估计；没有在此强制“压缩后必须低于阈值”的减量校验
- 默认与开关：自动与手动共用持久边界语义；手动compact先abort现有run，绝不自动继续旧run
- 证据：[appendCompaction<T](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/session-manager.ts#L1261-L1297)；[this.sessionManager.appendCompaction(summary](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L2911-L2924)；[async compact(customInstructions](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L2828-L2843)

### PI15 摘要长度耗尽或工具调用不能成为有效 checkpoint

- 条件：summary response error / length / toolCall
- 动作：getSummarizationFailure 返回错误，throw而不append；完整摘要与turn-prefix都拒绝toolCall；abort由controller检查；transient stream失败经retryAssistantCall，deterministic/abort立即返回
- 保留：此前有效checkpoint和原始消息
- 可能丢失 / 边界：不提供“失败就丢中段”的默认fallback；失败不能假装成功
- 默认与开关：默认安全失败；扩展自供summary不经过相同LLM输出校验，扩展承担格式语义
- 证据：[export function getSummarizationFailure](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/compaction.ts#L585-L597)；[export async function completeSummarization](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/compaction.ts#L618-L645)；[const failure = getSummarizationFailure(response, "Summarization")](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/compaction.ts#L742-L754)；[private async _runAutoCompaction](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L3157-L3298)

### PI16 overflow 与 recoverable length 不是普通网络重试

- 条件：同模型有效响应，显式错误模式或silent overflow，或length输出小于原始模型maxTokens
- 动作：成功 stop 的silent overflow只压缩不重答；错误/可恢复length压缩后最多一次新run重试；旧模型错误/旧checkpoint前usage不会触发当前模型恢复
- 保留：完整成功回答；错误历史仍审计可见
- 可能丢失 / 边界：第二次仍超限停止恢复并给出错误；length若已达到desiredMaxOutput不按这条恢复
- 默认与开关：bounded compact-and-retry一次；compaction.enabled=false跳过；新用户消息或非error/length成功assistant会重置恢复标记
- 证据：[private async _checkCompaction](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L3011-L3145)；[export function isContextOverflow](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/ai/src/utils/overflow.ts#L142-L180)；[export function isRecoverableLength](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/ai/src/utils/overflow.ts#L184-L187)；[this._overflowRecoveryAttempted = false;](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L1123-L1128)

### PI17 失败尝试和结果先成对持久省略，再压缩和重试

- 条件：overflow/length 恢复或一般可重试错误
- 动作：已完成attempt先经message_end持久化，turn_end/agent_end对外事件完成；_omitRecoveryAttempt找assistant及其callId匹配tool results，为每个追加null context_edit，再refresh；之后尝试摘要
- 保留：原始失败与token账单/导出；工具call/result不会孤立重放
- 可能丢失 / 边界：省略后摘要失败或取消：omissions仍保存，但无新compaction、无内部retry；不回滚成重新喂入失败内容
- 默认与开关：默认持久恢复路径；若目标source ID无法解析则抛错，不能悄悄仅内存删除
- 证据：[private _omitRecoveryAttempt](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L1244-L1287)；[this._omitRecoveryAttempt(assistantMessage)](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L3103-L3109)；[private async _handlePostAgentRun](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L1979-L2028)；[await emit({ type: "agent_end", messages: newMessages })](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/agent/src/agent-loop.ts#L263-L267)

### PI18 取消和队列属于压缩正确性的一部分

- 条件：用户abort、扩展cancel、压缩期间steering/follow-up到达
- 动作：检查signal后再append；auto成功且willRetry则continue；非恢复压缩也可continue一次投递队列；manual永不重启打断的run；prepare期间重新取steering但避免one-at-a-time重复取两条；compaction期间普通prompt报错要求等完成
- 保留：已接受队列顺序、Stop效力、工具批边界
- 可能丢失 / 边界：取消不等于auth错误：只有实际controller aborted或扩展cancel才标aborted；其他同名错误仍为failed
- 默认与开关：默认cancel lifecycle；扩展可取消session_before_compact或提供替代summary
- 证据：[abortCompaction(): void](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L2978-L2982)；[if (willRetry) return true;](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L3267-L3302)；[Cannot submit a prompt while compaction](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L2112-L2116)；[Preparation can be long-running](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/agent/src/agent-loop.ts#L210-L218)

### PI19 分支摘要是导航知识迁移，不是同一份compaction算法

- 条件：navigateTree(target,{summarize:true})
- 动作：找旧leaf与target最深共同祖先，收集离开路径；以window-reserve(默认16384)从新到旧挑消息；toolResult在分支摘要输入中直接跳过；已有summary较重要可在总量<90%预算时超额加入；最后在目标新leaf追加BranchSummaryEntry
- 保留：所离开分支的进度摘要与文件线索
- 可能丢失 / 边界：过旧消息可因预算不入摘要；原始context_edit在此 raw-entry 路径不按 canonical projection套用；不应宣称所有摘要采用同一套过滤
- 默认与开关：显式summarize；window未知按128000做branch summary预算；session_before_tree可取消/改说明/自供摘要；未请求summarize不生成
- 证据：[export function collectEntriesForBranchSummary](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/branch-summarization.ts#L108-L153)；[function getMessageFromEntry](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/branch-summarization.ts#L156-L187)；[export function prepareBranchEntries](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/branch-summarization.ts#L195-L255)；[export async function generateBranchSummary](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/compaction/branch-summarization.ts#L293-L328)；[async navigateTree(](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/agent-session.ts#L4024-L4049)

### PI20 工具边界先控制单次膨胀，和窗口摘要是不同层

- 条件：read/bash产生大结果
- 动作：文本read默认截头，2000行或50KiB先到者，提供offset继续；bash默认保留末2000行/50KiB并保存fullOutputPath；read图像走原生image与resize；主循环对length中工具调用生成失败结果而不执行可能截断的参数
- 保留：读取原文件和命令fullOutputPath供后续恢复；tool call/result结构
- 可能丢失 / 边界：单次read响应只保留页面，摘要输入还会再限2000字符；read实现仍可整文件读入内存，不能称streaming bounded IO
- 默认与开关：DEFAULT_MAX_LINES=2000、DEFAULT_MAX_BYTES=51200；自定义tool和extension result hooks可改变返回内容
- 证据：[description: `Read the contents](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/tools/read.ts#L96-L103)；[const truncation = truncateHead](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/tools/read.ts#L177-L205)；[export const DEFAULT_MAX_LINES](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/tools/truncate.ts#L11-L14)；[description: `Execute a](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/coding-agent/src/core/tools/bash.ts#L255-L260)；[A "length" stop means](https://github.com/badlogic/pi-mono/blob/b9fb2b854b7a19b28a68c3421365fee0a5342ce6/packages/agent/src/agent-loop.ts#L273-L283)

## 验证边界

已静态阅读取消回归测试 `9340-9777-auto-compaction-cancellation.test.ts`：覆盖abort后不开始post-run压缩、鉴权阶段可取消、compaction_start同步取消不调LLM、仅相同错误文字或无关AbortError不应冒充已取消。未执行这些测试；不能把静态测试存在说成本次运行通过。
