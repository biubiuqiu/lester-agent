# Agent 上下文管理研究

这是一份可独立阅读、离线重建的研究包，比较 Claude Code、Codex、OpenCode、Hermes 与 Pi 如何装配上下文、计量容量、整理历史及恢复执行。报告包含 **116 条规则、320 个去重源码定位、21 个教学动画场景**，供理解实现、核查证据和讨论 Lester 工程约束使用。

直接打开 [index.html](index.html) 即可阅读。页面将样式、数据与脚本全部内嵌，保留动画播放、逐步操作、框架切换、规则搜索、证据展开、键盘导航、减少动态效果适配和打印功能，无需启动 Lester、安装前端依赖或连接模型服务。

## 版本与研究边界

- 研究源码快照日期：**2026-10-22**（Asia/Shanghai）；报告版式更新：**2026-10-25**。这是固定版本研究，不代表各项目最新源码或当前产品行为。
- Claude Code 来自社区公开的泄露镜像，**未认证原始 sourcemap 来源**；缺失模块、生产开关和内部服务算法仍有明确的证据边界。
- 本包保留研究笔记、结构化结论、教学数据、报告资产与固定 commit 的源码链接；**不再分发第三方项目源码或源码 checkout**。笔记中的函数名与有限引用用于定位证据，不构成完整源码副本。
- 动画中的 token、容量与过程数字是教学设定，展示确定性的状态变化，**不是实际模型运行、性能测试或效果排名**。本研究未运行五套 Agent 的真实模型 benchmark。

| 框架 | 固定仓库 | commit |
| --- | --- | --- |
| Claude Code | `alex000kim/claude-code` | `6aa7e7f8789014d42247a0545ab4e9d4e531b0f2` |
| Codex | `openai/codex` | `61f34c35af77777db4d77d0ea2aeb8547a6b4523` |
| OpenCode | `anomalyco/opencode` | `f7d5b6ffc48223e654b20043a500d315e4bc8053` |
| Hermes | `NousResearch/hermes-agent` | `944c3a434071f43e64b443d033f3eba30c64434c` |
| Pi | `badlogic/pi-mono` | `b9fb2b854b7a19b28a68c3421365fee0a5342ce6` |

## 包内文件

```text
agent-context-management/
├── index.html                    # 最终单文件报告，也是静态站点入口
├── README.md
├── notes/                        # claude / codex / opencode / hermes / pi 研究笔记
├── data/
│   ├── claude.json               # 五份框架规则、对象与调用链数据
│   ├── codex.json
│   ├── opencode.json
│   ├── hermes.json
│   ├── pi.json
│   ├── scenes.json               # 五框架、21 场景、132 步骤
│   ├── framework-learning.json   # 框架导读与学习要点
│   └── evidence-index.json       # 生成的 320 个去重源码定位
├── assets/                       # 最终 CSS、JS 与总览 HTML 片段
└── scripts/
    └── build.py                  # 可移植的单文件生成器
```

`assets/` 中保留 `report.css`、`animation.css/js`、`memory.css/js`、`memory-content.js`、`capacity.js`、`textbook-overview.html/css` 与 `textbook.css`。这些是可编辑的研究源，构建时会内嵌到 `index.html`。

## 离线重建与检查

使用 **Python 3.9+ 标准库**，无 pip/npm 依赖。以下命令从仓库根目录运行；也可在任意工作目录以脚本路径调用，生成器始终相对自身定位文件。

```sh
python3 research/agent-context-management/scripts/build.py
python3 research/agent-context-management/scripts/build.py --check
```

普通构建仅写包内的 `index.html` 和 `data/evidence-index.json`。`--check` 在内存中重建这两份产物并逐字节比较；缺失或过期时以非零状态退出，**不写文件**，适合用于 CI。生成顺序固定，不包含当前时间、绝对路径或机器信息，因此同一份研究源会得到相同产物。

默认构建不会联网、下载、克隆或读取第三方源码。它验证包内的框架与固定 commit、一致的源码相对路径、正整数行号及范围、GitHub blob URL 结构、规则关联和场景数量。这只能证明研究数据与引用结构一致，**不能证明未提供的源码文件存在、行数正确或远端链接可访问**；命令输出会明确标注未执行源码实体验证。

修改研究源后运行构建，再运行 `--check`。`data/evidence-index.json` 应由生成器更新：它按框架、路径与行范围去重，与报告中的 320 处证据保持一致。笔记和场景中的额外来源继续保留原始固定版本链接。

## 可选的本地源码校验

若已经另行取得相应固定版本的 Git checkout，可额外提供其父目录；该模式需要本机安装 Git，也不会下载或克隆代码。

```text
/path/to/source-checkouts/
├── claude-full/
├── codex/
├── opencode/
├── hermes/
└── pi/
```

```sh
python3 research/agent-context-management/scripts/build.py \
  --check --source-root /path/to/source-checkouts
```

生成器会核对五个 checkout 的 HEAD 与固定 commit、确认被引用文件未偏离该 commit，并检查规则、笔记和场景引用的文件存在及实际行数。校验不会修改源码 checkout。该检查仍不验证 Claude 镜像来源真实性、缺失内部模块、服务端算法或真实运行效果。
