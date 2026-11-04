# Lester：图标、品牌色与 Design Token 参考

调研日期：2026-11-03。只读核对官方 GitHub 仓库源码与文档；未操作这些产品的桌面客户端，没有将其图形复制进 Lester。本笔记把**可验证事实**和**给 Lester 的建议**分开。

## 1. 可核实来源

### Lucide：统一基本语法

固定版本 `7bb15b80a3c885c68d2c65a11381f25920bbf1ef`。

- [file-text.svg](https://github.com/lucide-icons/lucide/blob/7bb15b80a3c885c68d2c65a11381f25920bbf1ef/icons/file-text.svg)：明确为 24×24、`viewBox="0 0 24 24"`、`stroke-width="2"`、`stroke="currentColor"`、round linecap/linejoin。
- [贡献指南](https://github.com/lucide-icons/lucide/blob/7bb15b80a3c885c68d2c65a11381f25920bbf1ef/CONTRIBUTING.md)：要求遵守单独的图标设计规则、提交截图、只提交相关变更。

能借鉴的是一套稳定的网格、描边、端点和外轮廓语法。不要把库的任意一枚 SVG 当成对所有品牌插画的强制规范。

### Phosphor：同一语义骨架，有控制地改变风格

固定版本 `2b75f3ad12b420c9504ef05df8d2564a28f8500e`。

- [仓库说明](https://github.com/phosphor-icons/core/blob/2b75f3ad12b420c9504ef05df8d2564a28f8500e/README.md)：资产按 weight 分组，包含标签、类别与版本的目录数据；强调资产是各框架实现的来源。
- [regular folder](https://github.com/phosphor-icons/core/blob/2b75f3ad12b420c9504ef05df8d2564a28f8500e/assets/regular/folder.svg) 与 [duotone folder](https://github.com/phosphor-icons/core/blob/2b75f3ad12b420c9504ef05df8d2564a28f8500e/assets/duotone/folder-duotone.svg)：都使用 256 坐标及 `currentColor`。duotone 示例只在同一骨架上加了 `opacity="0.2"` 的背景层，不需要给每个部件分配不同色相。

建议：同类图标先保证轮廓和语义一致，默认采用一个品牌色＋低透明底色；把三色插画限定在更大的展示用途。上述规律是基于这两个实际资产的观察，不声称所有 Phosphor 图标都使用完全相同实现。

### Fluent System Icons：尺寸需要光学校正

固定版本 `9bfae3d782fda4c8764f2128cec4d0dda160b283`。

- [Document metadata](https://github.com/microsoft/fluentui-system-icons/blob/9bfae3d782fda4c8764f2128cec4d0dda160b283/assets/Document/metadata.json)：列出 16、20、24、28、32、48 尺寸，以及 Regular、Filled、Color、Light 风格；描述隐喻为 paper、work、file、analog、vertical。
- [20px Document Regular](https://github.com/microsoft/fluentui-system-icons/blob/9bfae3d782fda4c8764f2128cec4d0dda160b283/assets/Document/SVG/ic_fluent_document_20_regular.svg) 与 [24px 版本](https://github.com/microsoft/fluentui-system-icons/blob/9bfae3d782fda4c8764f2128cec4d0dda160b283/assets/Document/SVG/ic_fluent_document_24_regular.svg)：有各自的路径与几何，不只是同一个 viewBox 放缩。20px 纸张直边约 1px，24px 约 1.5px。
- [README](https://github.com/microsoft/fluentui-system-icons/blob/9bfae3d782fda4c8764f2128cec4d0dda160b283/README.md) 明确区分 Regular/Filled 列表，以及镜像、独立 RTL 图形等方向元数据。

建议：Lester 必须在真正的 16、20、24px 下看轮廓与线重，不应只看 42px 图标墙。小尺寸移除次级线、节点、额外勾号。Regular、展示插画、品牌 Logo 应当是清楚的使用层级。

### Radix Colors：颜色按用途和交互状态组织

颜色源码固定版本 `dbdb85470547c7d34b9001f48fddb08ded335979`；官方文档仓库固定版本 `402e749613ab2e7ab7139c65b0029842b0360f07`。

- [light.ts](https://github.com/radix-ui/colors/blob/dbdb85470547c7d34b9001f48fddb08ded335979/src/light.ts)：每色系有 1–12 阶，并有 alpha/P3 变体。
- [Understanding the scale](https://github.com/radix-ui/website/blob/402e749613ab2e7ab7139c65b0029842b0360f07/data/colors/docs/palette-composition/understanding-the-scale.mdx)：1–2 背景，3–5 组件背景（normal/hover/selected），6–8 边框，9–10 实色背景（normal/hover），11–12 文字。文档明确指出这些是常用用途，存在例外；还建议以 mutable alias 映射 AppBg 等用途。
- 同文档明确说明 Mint、Sky、Lime、Yellow、Amber 的 9–10 阶适合深色文字；不能统一给明亮品牌底色叠白字。
- [Composing a palette](https://github.com/radix-ui/website/blob/402e749613ab2e7ab7139c65b0029842b0360f07/data/colors/docs/palette-composition/composing-a-palette.mdx) 可作为品牌色、中性色、交互和状态使用的补充。

建议：规范页展示 `surface / text / border / accent / status` 角色及 normal/hover/pressed/disabled/focus 实例，而不只展示孤立 hex 值。对比度保证必须基于 Lester 的实际前景/背景对计算，不可移植 Radix 的保证到自定义颜色上。

### IBM Carbon：Token 是生成物的唯一来源

固定版本 `487b24869143fe35599118760ef39d17d8696c65`。

- [Theme Tokens README](https://github.com/carbon-design-system/carbon/blob/487b24869143fe35599118760ef39d17d8696c65/packages/themes/tokens/README.md)：明确使用 DTCG 2025.10；JSON 是生成 Sass/JavaScript 的来源；禁止直接编辑生成文件。
- 同一文档描述 `$type`、`$value`、`$description`，原始色板别名、主题 modifier、组件 token、state 扩展及多主题一致性测试。
- [white/theme.tokens.json](https://github.com/carbon-design-system/carbon/blob/487b24869143fe35599118760ef39d17d8696c65/packages/themes/tokens/themes/white/theme.tokens.json)：实际语义角色包括 background、layer、text 等。
- [g10/status.tokens.json](https://github.com/carbon-design-system/carbon/blob/487b24869143fe35599118760ef39d17d8696c65/packages/themes/tokens/themes/g10/status.tokens.json)：状态色独立命名，用于错误、警告、需关注等真实状态。
- [layout.tokens.json](https://github.com/carbon-design-system/carbon/blob/487b24869143fe35599118760ef39d17d8696c65/packages/layout/tokens/layout.tokens.json)：间距也来自 tokens。
- [仓库总览](https://github.com/carbon-design-system/carbon/blob/487b24869143fe35599118760ef39d17d8696c65/README.md) 把 icons 与 pictograms 列为不同包，适合建立操作图标与展示图形的层级。

建议：Lester 使用 Primitive → Semantic → Component 的可追踪关系，并由同一份数据生成 CSS 与规范页。若实现只支持简化 schema，应明确称项目 token schema，不宜宣称完整支持 DTCG 标准。

## 2. Lester 调研基线问题的具体证据

以调研开始时 Git HEAD `3233ef4` 为基线：

- Logo 有 19 个独立 hex、6 个 gradient。覆盖青、蓝、紫、粉、黄、薄荷多个色相；工作区主色却是偏深绿/鼠尾草色，尚无明确的品牌角色映射。
- ProductGlyph 另写了 7 个颜色常量，UI 的 CSS 中又有独立硬编码。图标、Logo 和控件不能从同一处一起调整。
- ProductGlyph 使用 32 坐标网格和 1.65 单位描边：缩成 16px 后约 0.825px，和 Lucide 24/2 缩到 16px 的 1.33px 重量不同；在操作栏混排时更像较轻的插画。
- 部分普通实体图形包含勾号、外部小圆点等状态形状。例如普通 document 包含勾号，skill、model、profile 包含独立金色点。图标本身不应让未知状态看起来已经通过或有通知；Code Review 等明确能力语义可使用审阅符号，但实际安装/批准状态仍需独立。
- memory 目前更接近带顶部环的日历，和自动任务的隐喻不够清楚。可以改为记事/书签或记忆节点，避免与计划类混淆。

按 WCAG sRGB 相对亮度公式计算的白底对比度（并非所有地方都使用这些配对）：

| 色值 | 白底对比度 | 对实施的含义 |
| --- | ---: | --- |
| `#438F7C` | 3.85:1 | 可作装饰或部分图形；不能直接当作白字小按钮底色并声称达到 4.5:1 |
| `#C9954B` | 2.66:1 | 可作装饰点缀；不能作为白底小字/必要状态轮廓的唯一色 |
| `#425F59` | 6.97:1 | 正文/轮廓与白底配对已有足够数值余量 |
| `#1C2920` | 15.14:1 | 当前深色主要按钮与白字具有高对比 |

这些数值不构成全产品可访问性审计；必须根据每个真实状态的配对计算。

## 3. 原始设计建议（2026-11-03）

以下保留首次调研时的建议，包括以 Jade 为起点的配色探索；当前采用方向见第 5 节。

1. **先定角色，再定具体颜色。** 默认雾白＋深青可以沿用既有安静工作区气质；Logo 用同一品牌色的明暗变化，另一个色相最多承担小面积强调。靛蓝、暖白墨黑可放到规范页作局部比较，不能只换 Logo 而保留按钮/图标中的旧色。
2. **三种图形层级。** 操作图标：单色、24 网格、约 1.6–1.8px 基准、round cap/join；类别图形：同骨架、一个品牌色＋10–20% 底层；品牌标记：保留折叠 L 与终端箭头的识别，不添加每个图形都重复的节点。
3. **小尺寸单独审视。** 16/20px 用简化细节与光学校正；24px 规范版；32/48px 展示版。每个分类都有准确文字名称，装饰 SVG 不进入可访问名称，方向图形遵守明确方向语义。
4. **状态从图形中解耦。** 安装、通过、需审批、出错使用真实状态的独立标记；普通文档、模型、资料图形保持中性。不要让不同 Agent 的配色暗示权限或验证程度。
5. **单源 token。** 原始 palette、语义 surface/text/border/accent/status、组件状态与 spacing/radius/type/motion 进入统一数据。CSS、HTML 规范页和可下载 JSON 从该数据构建；校验 alias 循环、缺失引用、状态缺项、生成物过期及实际对比度。
6. **HTML 是可评审的说明。** 应显示 Logo 在浅/深底与 16/24/48px 的效果、图标网格、控件全部状态、真实 CSS 变量名与角色、主题切换、复制/导出、参考来源及“哪些旧样式尚未迁移”。局部预览不能隐式改应用主题、草稿或权限。

## 4. 边界

- 直接读取了以上 GitHub 官方资料与具体资源；没有独立浏览器操作 Lucide、Radix 或 Fluent 官网，也没有做这些产品完整交互审计。
- Adobe `spectrum-design-tokens` 的 Git 查询失败，未使用它作为论据。若后续引用 Spectrum，应另行核验可访问的官方来源。
- Carbon 图标源此次核实的是 32px `document.svg`，未据此声称所有 Carbon 尺寸采用同一固定笔画。关于按尺寸分别设计的结论依据 Fluent 的具体 20/24 源文件。
- 第 1–4 节保留首次调研的事实、建议及证据边界，不代表当前实现的测试结果；后续配色决定另记于第 5 节。

## 5. 本项目采用的实现契约

2026-11-03 的首版规范以 Jade 深青绿为默认品牌方向，开发规范页同时提供 Indigo、Graphite 的局部预览。2026-11-04，用户在配色对比后选定**首轮第 4 张 Graphite 暖石墨**，而非后续三张石墨细化方案。该方向成为当前默认配色，Jade、Indigo 继续作为规范页中的本地比较方案。

| 角色 | 采用方向 |
| --- | --- |
| 页面底色 | 暖白 `#FAF9F6` |
| 次级表面 | 暖灰 `#F0EFEB` |
| 品牌操作 | 石墨 `#373735` |
| 选中浅底 | 暖石色 `#ECEBE5` |
| 正文／辅助文字 | `#292927`／`#68665F` |
| Logo | 深石墨底、暖石色／象牙色折叠 L，小面积香槟金箭头 `#D9BC78` |

图标继续采用 24 网格、1.7 单位描边；16/20px 简化次级细节，普通实体不附带无状态依据的勾号或通知点。类别图形使用共享石墨色轮廓和浅底层，状态提示继续来自真实任务／安装记录。Logo 保留折叠 L 与终端箭头的几何，用同一套品牌 token 生成。配色选择应用到品牌与普通界面角色；成功、风险、错误和权限状态保留各自含义。

维护入口：

- 单一数据源：[`frontend/src/design/tokens.json`](../../frontend/src/design/tokens.json)。
- 解析与生成基础：[`frontend/scripts/design-token-core.mjs`](../../frontend/scripts/design-token-core.mjs)。
- 构建脚本：[`frontend/scripts/build-design-system.mjs`](../../frontend/scripts/build-design-system.mjs)。
- 生成 CSS：[`frontend/src/app/design-tokens.css`](../../frontend/src/app/design-tokens.css)。
- 单文件规范页：[`frontend/public/design-system.html`](../../frontend/public/design-system.html)，部署内访问 `/design-system.html`，也可以下载后离线打开。
- 操作与范围说明：[English](../../docs/design-system.md) / [中文](../../docs/zh-CN/design-system.md)。

规范页中的色板切换和组件示例只用于本地设计评审，不为产品增加主题设置，也不调用任务、权限或模型 API。本轮覆盖基础样式、共享控件和 SVG，不能把它描述为所有领域 CSS、终端/代码高亮、布局尺寸或第三方标识都已迁移完成。
