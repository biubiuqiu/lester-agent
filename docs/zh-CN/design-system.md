# 设计规范与 Token

[文档目录](README.md) · [项目首页](../../README.zh-CN.md) · [English](../design-system.md)

Lester 将品牌、语义颜色、字体、间距、圆角、动效、图标和共享控件状态集中到一份可浏览的 HTML 中。调整风格时，先在规范页评审，再验证实际界面。

默认方向是 **Graphite 暖石墨**：用户于 2026-11-04 选定首轮配色中的第 4 张，以暖白表面（`#FAF9F6`）、石墨色操作（`#373735`）、暖石色／象牙色折叠 L 和少量香槟金终端箭头（`#D9BC78`）组成。规范页中的 Jade、Indigo 保留为本地比较方案，不会新增产品主题偏好，也不会更改用户的任务、草稿、权限或模型配置。

## 打开规范页

- 在运行中的部署访问 `/design-system.html`，例如 [http://localhost:13000/design-system.html](http://localhost:13000/design-system.html)。
- 离线评审时，下载或用浏览器打开 [`frontend/public/design-system.html`](../../frontend/public/design-system.html)。SVG、样式和 Token 数据均已内嵌，不依赖 CDN 或应用 API。
- 页面包含 Token 角色与值、局部配色预览、Logo 和图标尺寸、图标搜索、共享组件状态、对比度信息、复制与导出、参考来源及本轮迁移范围。示例控件只执行本地设计评审操作。

这是开发设计参考，与 `/docs` 下的六语言产品帮助分开维护。

## 源文件与生成物

| 层级 | 维护位置 | 用途 |
| --- | --- | --- |
| Token 单源 | [`frontend/src/design/tokens.json`](../../frontend/src/design/tokens.json) | 共享取值及其设计角色；统一改风格时先修改这里。 |
| Token 核心 | [`frontend/scripts/design-token-core.mjs`](../../frontend/scripts/design-token-core.mjs) | 解析数据并提供共用的生成能力。 |
| 构建脚本 | [`frontend/scripts/build-design-system.mjs`](../../frontend/scripts/build-design-system.mjs) | 生成 CSS 与独立规范页。 |
| 生成的 CSS | [`frontend/src/app/design-tokens.css`](../../frontend/src/app/design-tokens.css) | 供前端使用的 CSS 自定义属性。 |
| 生成的规范页 | [`frontend/public/design-system.html`](../../frontend/public/design-system.html) | 可离线打开、方便评审的单文件 HTML。 |
| 产品图形 | [`frontend/src/components/product-glyph.tsx`](../../frontend/src/components/product-glyph.tsx) | 类别、Agent 与 Skill 的 SVG 家族。 |
| 品牌源图 | [`frontend/src/components/brand-mark-art.tsx`](../../frontend/src/components/brand-mark-art.tsx) | 产品、规范页与 favicon 共用的几何与颜色角色。 |
| 生成的 Logo | [`frontend/src/app/icon.svg`](../../frontend/src/app/icon.svg) | 由同一套品牌取值生成，供浏览器直接加载的 favicon；应用使用同源内嵌 SVG。 |

原始色板先映射到表面、文字、边框、强调色、状态色等语义角色，再由组件使用这些角色。这样换配色时能追踪来源，不需要在各页替换看起来相近的色值。规范页展示项目实际使用的 Token 名称，示例中不要另写一套独立数值。

不要直接手改生成的 CSS、HTML 或 Logo 颜色。应修改源数据或生成逻辑，再统一构建。

## 修改与验证

从仓库根目录运行：

```bash
cd frontend
pnpm design:build
pnpm design:check
pnpm lint
pnpm build
```

`design:build` 更新生成物；`design:check` 检查提交的生成物是否与当前输入一致。修改 Token、图形或规范页生成逻辑后，都要执行检查。

调整设计时：

1. 先明确要改的语义角色。品牌强调色、危险操作和错误状态各有自己的用途。
2. 修改源数据或对应 SVG 几何，然后执行上述命令。
3. 检查实际文字与背景组合、默认／悬停／按下／禁用／聚焦状态，以及 Logo 在浅底和深底上的表现。对比度应按真实取值计算。
4. 同时查看 16、20、24px 和大尺寸图标，并验证桌面页面与窄屏手机；大图标墙不能代替小尺寸检查。
5. 用已有浏览器 fixture 验证受影响控件、键盘焦点和草稿返回路径。规范页中的示例不代表真实模型或应用连接已经验证。

## 图标与品牌规则

- 图标采用 24 单位网格、1.7 单位基准描边，统一圆端点和转角。16/20px 简化次级细节，避免把复杂插画一直等比缩小。
- 个人项目、普通项目、会话和成果分别使用共享 SVG；个人项目以文件夹内的人物轮廓识别，普通目录仍使用文件类型图标。菜单、引用和项目页使用相同的图形来源，官网演示复用正式 Logo。
- 类别图形保持中性：普通文档、模型图标不表示任务已经验收。安装、审批等提示仍独立显示，并来自真实状态。
- 类别图形采用共享石墨色轮廓与安静的浅底层。操作和状态控件保持清晰，第三方提供商标记与用户身份内容保留自身含义。
- Logo 保留折叠 **L** 与终端箭头，深石墨色底配暖石色／象牙色折叠面，香槟金只用于小面积箭头。颜色来自与界面一致的品牌 Token；状态色继续保留独立含义。
- 图标旁保留真实文字标签；装饰性 SVG 不进入可访问名称，也不引入远程素材请求、任务执行或权限。

## 本轮覆盖范围

本轮抽取的是基础样式、共享控件样式和 SVG 表达。规范页会说明哪些值已连接产品，不能将它描述为全部历史 CSS 色值都已迁完。

会话和侧栏尺寸、各领域布局、终端与代码高亮配色、第三方标识、用户文档，以及隔离的 HTML 预览仍有自己的边界。继续迁移某个表面时，应先映射语义角色，再验证该表面，不做全局色值机械替换。

部署中的规范页用于设计评审，不新增工作区路由或全局主题开关。此类视觉更新需要重建 Web，不增加数据库迁移或后端配置。

## 调研依据

参考已核实的 Lucide、Phosphor、Fluent System Icons、Radix Colors 和 IBM Carbon 官方资料。固定版本、可观察规则、旧版 Lester 的问题及证据边界见[研究记录](../../research/lester-design-system/README.md)。记录明确区分来源事实与本项目设计决定，不将参考资料描述为已完整采用这些设计系统。
