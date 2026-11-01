# OpenCode 深入源码研究

固定 commit `f7d5b6ffc48223e654b20043a500d315e4bc8053`。

持久事件与DB投影驱动、instruction epoch与活动checkpoint双时钟、usage anchor+delta估算、summary/native双路径；入口工具/图片边界先控制体积，超限时有界压缩请求退阶。

## 抽象

OpenCode 不是一条messages数组无限增长再截断，而是两条有边界的状态流：instruction epoch把稳定规则与chronological delta分开；conversation checkpoint把历史与活动窗口分开。每个durable Step从DB重新折叠两者，转换为canonical AI messages，最后经插件和媒体边界成为wire request。持久DB、模型工作集、provider cache、外置结果文件必须分开画。

## 深层规则

### O01 每个 Step 从持久状态重新装配，不维护第二份内存工具循环

触发：一个 Step 完成或重试后继续执行。

动作：select agent/instructions/tools，prepare instruction state，load DB active history，然后先 auto compaction 再 primary 请求。

保留：有序持久历史、当前 agent/model/权限与指令状态。

损失/边界：不把完整 DB 历史自动全部发送；active history 被最近可重放 checkpoint 截断。

默认：每步执行。条件：SessionRunner.runStep。

证据：[prepareContext/runStep](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/runner/llm.ts#L195-L240)；[select/load](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/context.ts#L126-L184)。

### O02 权限先影响可发现内容，再影响工具快照

触发：选择当前 Session agent。

动作：合并 agent.permissions 与 session.permissions；同一规则同时筛选 tool snapshot、skill catalogue、MCP instructions。

保留：权限允许的工具和 Skill 描述；同请求广告工具与 execute 映射。

损失/边界：被 deny 的工具/Skill 不出现在相应清单；清单不是完整 Skill 正文。

默认：每次 select。条件：Permission.merge、registry.snapshot。

证据：[select](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/context.ts#L132-L164)；[SkillInstructions.load](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/skill/instructions.ts#L79-L95)。

### O03 指令按共享程度排序，稳定前缀与变化尾部相分离

触发：准备 instruction sources。

动作：合并 CodeMode、MCP、reference、skills、discovery、builtins、entries；AGENT 自定义 system 或默认系统 prompt 在 initial 前。

保留：公共目录/规则的可复用 cache prefix。

损失/边界：排序不减少 token；不是规则优先级的完整规范。

默认：sources 固定合并顺序。条件：agent.system 可覆盖默认系统 prompt。

证据：[Instructions.combine](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/context.ts#L152-L163)；[baseTranscript](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/model-request.ts#L92-L111)。

### O04 Instruction epoch：基线固定，变化以 delta 入历史

触发：规则首次发现或之后发生新增/修改/移除。

动作：首次存 initial_values/current_values 的 hash 引用；之后冻结 renderUpdate 到 durable event；文件内容变化用较短 diff 或全量 replacement；压缩完成提升 current 为新 epoch baseline。

保留：最新规则值、历史规则变化事实、跨请求稳定初始前缀。

损失/边界：摘要不负责保存规则；压缩后旧 delta 不需要重复带入。

默认：每次 prepare observes；compaction ended advanceEpoch。条件：initial initialization 可被不可用来源阻塞。

证据：[observe/commit](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/instruction-state.ts#L21-L60)；[advanceEpoch/initial](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/instruction-state.ts#L139-L181)；[Compaction.Ended](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/projector.ts#L756-L760)；[renderUpdate](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/instruction-discovery.ts#L127-L148)。

### O05 仓库规则发现与按读懒加载是不同路径

触发：启动 Location 或成功读取子目录文件。

动作：配置插件读全局 config/AGENTS.md 和当前位置向上的项目 AGENTS；子目录 read 后向上发现 Location root 以下 AGENTS，作为 durable synthetic 注入。

保留：当前可用规则正文、已注入路径 metadata。

损失/边界：不预读所有子目录；synthetic 被压缩/revert 移出可见历史后会重新发现重注入。

默认：global/project discovery 默认 true；显式配置 instructions 独立于发现开关。条件：外部目录 read 不自动发现；发现失败不使 read 失败。

证据：[discovery candidates](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/config/plugin/instruction.ts#L24-L56)；[globalSource/projectSource](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/config/plugin/instruction.ts#L78-L109)；[read instruction discovery](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/tool/plugin/read.ts#L82-L106)；[visible ledger](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/instructions.ts#L35-L43)；[previouslyInjected](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/instructions.ts#L99-L110)。

### O06 Skill 目录与正文分层

触发：选择可用 Skills 或用户显式激活 Skill。

动作：system baseline 放 id/name/description 目录；权限允许且 autoinvoke 不为 false 的条目可自动发现；用户激活正文附在 user content 前或独立 skill message。

保留：被使用 Skill 的正文可进入会话；未使用 Skill 仅描述。

损失/边界：不是所有 Skill 全文常驻；原生保留 real user 时主动去 skills。

默认：目录按 ID 排序；未描述/禁自动 invoke 条目不列。条件：Skill tool/用户引用触发正文加载。

证据：[render](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/skill/instructions.ts#L16-L33)；[load](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/skill/instructions.ts#L79-L95)；[user/skill](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/runner/to-llm-message.ts#L256-L277)；[recentUserMessages](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L799-L811)。

### O07 工具输出入口边界：先截断，再入持久消息

触发：工具结果未自报 metadata.truncated，且文本超过 2,000 行或 50 KiB。

动作：保留完整前缀行到双重上限；原全文保存 tool-output 文件，返回显示范围和 outputPath。

保留：完整结果外置文件、头部文本、非文本 file 内容。

损失/边界：中后段不进本轮 prompt；文件保留期默认 7 天，不是永久档案。

默认：2,000 lines/50 KiB；7 days retention。条件：结果已带 truncated metadata 时跳过通用裁剪。

证据：[MAX_LINES/MAX_BYTES/RETENTION](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/tool-output.ts#L13-L15)；[truncate](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/tool-output.ts#L69-L119)；[toolOutput.truncate](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/runner/step.ts#L123-L128)。

### O08 图片是 byte 高低水位，而非 token compaction

触发：inline image 累计 payload 超过 25 MiB。

动作：按原顺序重放尺寸；每次超 trigger 丢最旧图到 15 MiB target，以明确不可见占位替换；远程/引用图不计 inline byte。

保留：近期 inline images、明确的丢失标记、稳定切点以减少 cache prefix 改写。

损失/边界：旧图片视觉信息丢失；不能凭记忆描述；与图像 token 预算是另一维度。

默认：25 MiB trigger / 15 MiB target。条件：boundImages 始终在请求 hook 后执行。

证据：[image bounds constants](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/model-request.ts#L42-L45)；[mediaBytes](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/model-request.ts#L141-L145)；[boundImages](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/model-request.ts#L181-L208)；[post-hook shaping](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/model-request.ts#L289-L291)。

### O09 usage anchor = 最近同 provider 成功请求的实测总量 + 未见新内容估算

触发：已有 input/cache tokens 非零、无 error 的 assistant。

动作：measured=input+cache.read+cache.write+output+reasoning；estimated 为 anchor 后 canonical 消息（排除 anchor assistant 自身，包含本地 tool results）；无 anchor 则全请求 system/tools/messages估算。

保留：缓存输入计入窗口；本地尚未发给 provider 的 tool results 纳入 delta。

损失/边界：4 chars/token 启发式非精确 tokenizer；同 provider 不同 model 的 usage 也可作 anchor；hook 修改的整体开销不保证精确覆盖。

默认：估算 round(string.length/4)；image 1500、PDF 2000。条件：hasMeasuredPrompt 只比较 providerID，不比较 model.id。

证据：[estimatePrompt](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L819-L843)；[hasMeasuredPrompt](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L868-L873)；[estimateRequest/estimatePart](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L899-L936)；[Token.estimate](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/util/token.ts#L3-L5)。

### O10 默认阈值保留 15%，中大窗口至少保留 16K

触发：auto=true，最新不是刚完成 compaction，且 native window 后已有 usage evidence。

动作：W=input limit || context；若 buffer 显式则 ceiling=W-buffer；否则 W-max(floor(.15W), W>=32000?16000:0)；estimateContext>=ceiling 触发。

保留：为下一回复与摘要留余量。

损失/边界：未知窗口 ceiling=Infinity 不会预触发；新工具输出跃迁仍可能超限。

默认：auto=true，keep=15000；unknown compaction planning window=200000。条件：auto=false 同时关闭 auto 和 overflow，只保留 manual；opaque native 等 usage latch。

证据：[constants](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L93-L108)；[state defaults](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L197-L202)；[compact/due](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L210-L255)；[calculateCeiling/summaryRoom](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L851-L862)。

### O11 输出请求上限动态适配剩余窗口

触发：primary 或 compaction 默认请求准备。

动作：requested=min(catalog.output 或 32000,256000)；room=context-measured-ceil(estimated*1.15)；maxTokens=min(requested,max(1024,room))；hook 可更改/移除此设置。

保留：默认输出空间估算，估算部分额外 15% margin。

损失/边界：1024 floor 在 auto off 极满时仍可能使总量过窗；未知 context 不适配。

默认：输出 fallback 32000，max 256000，min 1024。条件：主请求/压缩请求设默认，title/generate保留 provider 默认。

证据：[output constants](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/model-request.ts#L47-L54)；[outputLimit](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/model-request.ts#L84-L90)；[default hook options](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/model-request.ts#L244-L255)。

### O12 文本摘要不只是旧前缀：能装下时总结整个当前窗口

触发：选中的模型不是 native compaction。

动作：切 keepID，buildPrompt(update/legacy)，将当前请求尽可能保持原样 + summary prompt，重用 cache；不足时使用去掉 kept tail 的 prefix；输出写结构化 summary。

保留：目标/要求/决策/进度/阻塞/Next Move/最多15文件/不可恢复关键值；较新历史优先于旧摘要。

损失/边界：不能保证所有 template section：filled 实际只检查存在任一模板 heading；不保存细节保真。

默认：默认 keep15000；新 prompt 明确不复述单独重发的规则/AGENTS/environment。条件：可配置 compaction agent 的模型；resolve 失败 fallback 当前 model。

证据：[buildPrompt](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L111-L181)；[model selection](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L218-L238)；[summarize](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L278-L333)。

### O13 recent-context 是有损序列化快照，不是原消息 suffix

触发：摘要 keepID 被选择并加载下一请求。

动作：从 keepID 到 compaction seq 的记录即时 messageToText；包装单一 user conversation-checkpoint，summary 与 recent-context 两段；正文注明历史数据非新指令。

保留：user原文、Skill正文、assistant text和reasoning文本、tool调用参数、工具结果前1250 code points、附件名称/MIME。

损失/边界：原 tool role/配对被转换为文本；附件 bytes不保留；单结果超过1250被截断；不是原生tool消息重放。

默认：keep是选择目标而非总请求 hard cap；附件仅 metadata。条件：system/旧compaction不进 recent；messageToText 有多类过滤。

证据：[withRecentText](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/history.ts#L144-L181)；[messageToText/truncateToolOutput](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/recent-text.ts#L5-L81)；[checkpoint lowering](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/runner/to-llm-message.ts#L293-L312)。

### O14 keep 切点守 user exchange，不保证严格 <= keep

触发：消息文本可计算，准备 summary。

动作：从尾部往前累计预算，再向前对齐真实 user 边界；总量全 fit也只保留最新 exchange；仅一个 exchange可全总结且不保留；旧摘要已有 recent 时可保留全部 recent。

保留：tool calls/results 不切开；至少最新一条的选择机制。

损失/边界：对齐可能超 budget；无条件“最新用户永远原样保留”不成立：单 exchange路径可keep-none。

默认：keep=15000。条件：previous+recent 是单exchange特殊分支。

证据：[splitConversation/recentStart/oldestToDrop](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L716-L774)。

### O15 原生压缩两机制拥有不同保留方

触发：model.compaction.type===native。

动作：优先 trigger 机制：调用 llm.compact 后客户端补最近 whole real user messages + assistant checkpoint；否则 endpoint 返回 replacement 原样保存为native.messages。

保留：trigger补用户原文/附件整体；endpoint保留项由provider决定；native JSON与metadata。

损失/边界：provider不透明算法不可从客户端证明；trigger recentUserMessages 去掉skills、不保半个附件，可能无用户项fit。

默认：按 capability 检查 trigger 优先 endpoint 次之。条件：canCompact 均不支持则失败，非静默变文本。

证据：[compactNatively](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L336-L385)；[recentUserMessages](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L799-L811)。

### O16 原生窗口是 model+provider 绑定，切换可展开原历史

触发：选择 History boundary=target model。

动作：只有同providerID同model.id且window可decode才重放；不兼容则递归回溯更早checkpoint/原记录；不可重放native条目不当成文本摘要。

保留：原始DB transcript仍在；目标provider可接受的历史。

损失/边界：展开后可能比旧usage大很多；不能复用不可解码加密块；模型 switch可触发新一轮预算压力。

默认：Boundary latest对中立读者保留；local跳过native。条件：exact model+provider replay 检查。

证据：[Boundary/replayable](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/history.ts#L20-L46)；[messageEntries](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/history.ts#L103-L141)。

### O17 压缩请求自身也要能发送：保留checkpoint与最新exchange

触发：prepared request 不fit target，或provider证实too-long。

动作：优先原样全请求，再原样去tail前缀，再flatten文本；保留前checkpoint，省reasoning/media/system updates、工具结果截1250；按whole exchange删最旧，标注 omitted。

保留：前checkpoint、最新完整exchange不被进一步剪碎；system/tools仍计开销。

损失/边界：被drop exchange既不在摘要输入也不在tail的风险由withoutTail路径避免；被丢的历史细节不再可靠进入新summary。

默认：先估算，尚无provider拒绝时若无法fit仍尝试 whole；拒绝后不再硬送。条件：hook可供结果绕过LLM；hook改旧checkpoint在flatten存在TODO局限。

证据：[deliver](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L388-L477)；[flattenAndDropOldest](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L480-L526)；[flattenMessage](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L776-L796)。

### O18 context overflow 与 byte overflow 的恢复不同

触发：摘要/原生压缩请求被provider拒绝。

动作：payload-too-large且还是原请求时只切asText去媒体，暂不记rejection；context overflow/文本payload拒绝则目标依次首个被拒请求估计的70%、50%、35%；再失败退出。

保留：省略标记、最新exchange/checkpoint、每次尝试usage成本。

损失/边界：最多缩三阶仍不保证fit；不能再缩时显式失败；内容不可用Failure不按网络重试。

默认：SHRINK_STEPS=[.7,.5,.35]。条件：其他AIError走session重试策略；Failure不重试。

证据：[SHRINK_STEPS](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L93-L93)；[deliver retry loop](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L419-L477)。

### O19 主请求超限只在无已持久输出时恢复一次

触发：Step收到context overflow，publisher.outputStarted=false。

动作：调用 reason overflow 压缩；成功则本logical Step重新load和发新assistant；设recoverOverflow=false。

保留：已发生输出/工具副作用不被隐藏重放。

损失/边界：有部分输出后不走此自动压缩恢复；第二次overflow不再compact循环。

默认：每logical Step一次。条件：auto=false会导致overflow compaction skipped。

证据：[preoutput overflow capture](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/runner/step.ts#L113-L120)；[recoverOverflow](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/runner/step.ts#L155-L166)；[recovery flags](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/runner/llm.ts#L208-L209)；[recoverOverflow callback](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/runner/llm.ts#L259-L264)；[Compacted outcome](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/runner/llm.ts#L314-L317)。

### O20 只有可用结果才建立压缩边界；失败也计费

触发：摘要返回格式不符、空、length/content-filter/unknown结束，或取消。

动作：模板可补一次NUDGE（注意是原prompt+NUDGE新请求，并未拼回失败assistant）；仍无heading失败；length等失败事件，不推进Completed boundary；usage每次完成立即累计。

保留：旧历史、失败状态、实际花费；成功后才在Ended推进epoch。

损失/边界：格式检查弱：仅任一heading；不是schema严格验证。失败摘要花费不回滚。

默认：每个模板失败最多一次nudge；一个Session不并发compaction。条件：hook result可以直接提供summary/native/metadata。

证据：[filled/send/NUDGE](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L289-L317)；[stream](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L530-L585)；[fromHook/spend/publish](https://github.com/anomalyco/opencode/blob/f7d5b6ffc48223e654b20043a500d315e4bc8053/packages/core/src/session/compaction.ts#L615-L675)。

## 可执行动画分镜

数值是解释性场景，不是假装真实tokenizer测量。物件必须显示它在DB、活动prompt、provider opaque窗口还是外置文件，不能把从prompt消失画成永久删除。

### 200K窗口：一次正常文本压缩

1. **装配基线**：DB存全量E1–E8 → 工作集=[B0,E1…E8]；tools=T。选择工具与permission-filtered目录；读取固定instruction epoch。关联规则 O01, O02, O03, O04。

2. **观察usage与新结果**：anchor input/cache/output/reasoning=160K → 本地工具结果delta估计=12K；总估计172K。工具输出先过入口2000行/50KiB边界；anchor的assistant output不再重复估算。关联规则 O07, O09。

3. **比较触发线**：W=200K，无buffer → ceiling=200K−max30K,16K=170K；172K≥170K。进入summary路径；此步骤不是假定170K为模型hard window。关联规则 O10, O12。

4. **划keepID**：E1…E8 → 最新完整exchange E8被keepID引用（约12K）。倒数文本预算并向user boundary回退；工具result将在recent中截1250codepoints。关联规则 O13, O14。

5. **生成summary**：B0,T,E1…E8 → summary S1。能装时整个窗口原样+summary prompt发LLM以复用cache；格式至少一个heading才通过。关联规则 O12, O20。

6. **提交新边界**：DB旧记录仍在 → 新活动=[B1, user{summary S1,recent E8},新增内容]。Compaction.Ended -> advanceEpoch；B1取current规则；下一步DB重建recent。关联规则 O04, O13, O20。

### 主请求拒绝→一次恢复→摘要请求继续被拒

1. **估算未证明真实安全**：工作集估计低于threshold → provider返回context overflow，尚无输出。错误尚未产生durable assistant/tool output才可恢复。关联规则 O09, O19。

2. **启动一次overflow compact**：recoverOverflow=true → target=min(cap,estimatedContext×0.7)。压缩请求立即更保守，而非再发同一主请求。关联规则 O18, O19。

3. **文本化与整exchange裁剪**：完整历史太大 → checkpoint + 扁平文本 + omitted marker。去媒体/系统delta/reasoning，tool结果截1250；整组drop最早exchange。关联规则 O17, O18。

4. **提供者仍拒绝**：首个被拒压缩请求估计Q → 目标依次0.7Q→0.5Q→0.35Q。注意梯度基于首个被拒压缩请求，而非每轮乘前一轮0.7。关联规则 O18。

5. **停止而非破坏保护项**：最新exchange/checkpoint仍放不下 → Compaction.Failed；原历史与失败可见，Step终止。不能再缩时显式失败；summary length/empty亦失败；耗费usage仍记账。关联规则 O17, O20。

6. **若压缩成功的另一分支**：new checkpoint完成 → 重试主请求；recoverOverflow=false。若再次overflow不无限compact；部分输出后的overflow不走本恢复分支。关联规则 O19。

### 原生压缩与切模型后的展开

1. **选择原生能力**：model.compaction=native → 选择trigger或endpoint。trigger优先；两者客户端保留语义不同。关联规则 O15。

2. **保存不透明窗口**：原工作集 → native.messages=[whole users, encrypted checkpoint]（trigger示意）。endpoint则完全采用provider replacement；不绘制内部摘要算法。关联规则 O15。

3. **等待实测**：opaque checkpoint本地字符近零 → due先不触发，等后续成功response usage。不得因为本地密文长度小而宣称省到零token。关联规则 O09, O10。

4. **切到另一模型**：native绑定A/model1 → 跳过不兼容native boundary，回溯原历史。provider相同但model.id不同也不能重放native；展开后重估压力。关联规则 O16。

## 上一版需要纠正/补充

- recent-context虽注释称verbatim，实际不是原始message后缀：tool输出1250 codepoints、附件只metadata、包括可读reasoning，user与Skill正文可完整进入。
- hasMeasuredPrompt只同providerID而非exact model，native重放才要求exact provider+model。两者不可混说。
- instruction epoch持久initial/current hash baseline+chronological delta，压缩后advanceEpoch，规则不靠summary传承。
- 正常summary优先整个窗口总结以复用cache；仅room不足才排除kept tail，不是永远只总结旧前缀。
- overflow主请求恢复仅未产生durable输出时，且每logical Step仅一次。
- 格式检查filled只任一模板heading，不能描述成严格结构验证。
- 工具入口2K行/50KiB与compaction recent 1250codepoints是两层；7天外置文件retention不等于永久历史。

## 方法边界

- 固定2026-10-22源码快照，未运行真实provider模型或验证远端压缩内容
- 插件hooks可改system/messages/tools/options甚至直接提供压缩结果，默认规则不是所有部署的保证
- native server内部筛选/编码算法不在客户端可见范围
- 仅静态跟踪，不提供性能/质量排名