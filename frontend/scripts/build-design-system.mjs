import { readFileSync, writeFileSync, mkdirSync, existsSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";
import ts from "typescript";
import { createElement } from "react";
import { Plus, Send, Square, AtSign, Paperclip, TerminalSquare, Folder, PanelRight, Ellipsis, Search } from "lucide-react";
import { renderToStaticMarkup } from "react-dom/server";
import { readTokens, tokenCSS } from "./design-token-core.mjs";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const require = createRequire(import.meta.url);
const postcss = createRequire(require.resolve("next/package.json"))("postcss");
const modules = new Map();
// Render the production TSX components, so the reference cannot drift into a second icon set.
function loadTSX(path) {
  const requested = resolve(root, path);
  const absolute = [requested, `${requested}.tsx`, `${requested}.ts`].find(candidate => existsSync(candidate));
  if (!absolute) throw new Error(`Missing design component: ${path}`);
  if (modules.has(absolute)) return modules.get(absolute).exports;
  const compiledModule = { exports: {} };
  modules.set(absolute, compiledModule);
  const code = ts.transpileModule(readFileSync(absolute, "utf8"), { compilerOptions: { jsx: ts.JsxEmit.ReactJSX, module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
  const localRequire = name => name.startsWith(".") ? loadTSX(resolve(dirname(absolute), name)) : require(name);
  new Function("require", "module", "exports", code)(localRequire, compiledModule, compiledModule.exports);
  return compiledModule.exports;
}
const data = readTokens();
const css = tokenCSS(data);
const { ProductGlyph } = loadTSX("src/components/product-glyph.tsx");
const { BrandMarkArt } = loadTSX("src/components/brand-mark-art.tsx");
const escape = value => String(value).replace(/[&<>"']/g, char => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char]);
const controls = new Set([".primary-button", ".secondary-button", ".primary-button svg", ".secondary-button svg", ".field", ".field input", ".field textarea", ".field select"]);
const controlRules = [];
postcss.parse(readFileSync(resolve(root, "src/app/globals.css"), "utf8")).walkRules(rule => {
  if (rule.parent.type !== "root") return;
  const selectors = postcss.list.comma(rule.selector).filter(selector => controls.has(selector.replace(/:(?:hover|focus-visible|focus|disabled|active|not\(:disabled\))/g, "")));
  if (selectors.length) { const copy = rule.clone(); copy.selector = selectors.join(","); controlRules.push(copy.toString()); }
});
if (!controlRules.length) throw new Error("No shared production controls found");
const glyphs = [
  ["personal-project", "Personal project · 个人项目", "个人工作项目"], ["artifact", "Results · 成果", "会话交付与成果集合"],
  ["document", "Document · 文档", "文件 / 内容"], ["analysis", "Analysis · 分析", "图表 / 数据"], ["web", "Web · 网页", "站点 / 页面"], ["code", "Code · 代码", "源代码"], ["review", "Review · 审查", "检查，不代表已通过"], ["project", "Project · 项目", "文件夹 / 项目"], ["conversation", "Conversation · 会话", "消息 / 对话"], ["skill", "Skill · 能力", "可复用能力"], ["agent", "Agent · 助理", "自定义 Agent"], ["designer", "Designer · 设计", "编辑 / 创作"], ["compass", "Compass · 探索", "方向 / 探索"], ["strategy", "Strategy · 策略", "层次 / 规划"], ["bolt", "Bolt · 执行", "动作 / 活力"], ["model", "Model · 模型", "模型 / 芯片"], ["computer", "Computer · 计算机", "计算环境"], ["context", "Context · 上下文", "参考资料"], ["profile", "Profile · 个人资料", "身份，无在线状态"], ["automation", "Automation · 自动任务", "时钟 / 触发"], ["connections", "Connections · 连接", "应用授权"], ["publish", "Publish · 发布", "显式发布动作"], ["memory", "Memory · 记忆", "笔记 / 书签"], ["onboarding", "Guide · 引导", "入门 / 路标"],
];
const categories = glyphs.map(([kind, name, usage]) => `<button type="button" class="glyph-tile" data-kind="${kind}" data-name="${escape(name)}" disabled aria-label="复制 ${escape(name)} SVG"><span class="glyph-stage">${[16, 20, 24, 32].map(size => renderToStaticMarkup(createElement(ProductGlyph, { kind, size }), { identifierPrefix: `reference-${kind}-${size}-` }).replace("<svg ", `<svg data-size="${size}" `)).join("")}</span><strong>${escape(name)}</strong><small>${escape(usage)}</small></button>`).join("\n");
const actions = [["add", "Add · 添加", Plus], ["send", "Send · 发送", Send], ["stop", "Stop · 停止", Square], ["mention", "Mention · 引用", AtSign], ["attach", "Attach · 附件", Paperclip], ["terminal", "Terminal · 终端", TerminalSquare], ["folder", "Folder · 文件夹", Folder], ["workspace", "Workspace · 工作区", PanelRight], ["more", "More · 更多", Ellipsis], ["search", "Search · 搜索", Search]];
const actionGallery = actions.map(([kind, name, Icon]) => `<button type="button" class="glyph-tile" data-kind="action-${kind}" data-name="${escape(name)}" disabled aria-label="复制 ${escape(name)} SVG"><span class="glyph-stage">${[16,20,24,32].map(size => renderToStaticMarkup(createElement(Icon, { size, "data-size": size, "aria-hidden": true, focusable: false, style: { strokeWidth: size <= 20 ? "var(--lester-icon-stroke-width-small)" : "var(--lester-icon-stroke-width)" } }))).join("")}</span><strong>${escape(name)}</strong><small>Action · Lucide / 保持单色</small></button>`).join("\n");
const gallery = categories + "\n" + actionGallery;
const mark = (size, prefix, colors) => renderToStaticMarkup(createElement(BrandMarkArt, { size, prefix, colors }));
const logos = `<div class="logo-samples"><figure class="logo-primary">${mark(88, "reference-primary")}<figcaption>Lester<small>Make it happen.</small></figcaption></figure><div class="logo-size-row">${[16, 24, 32, 48].map(size => `<figure>${mark(size, `reference-${size}`)}<figcaption>${size}px</figcaption></figure>`).join("")}<figure><span style="display:inline-flex;padding:8px;border-radius:var(--lester-radius-control);background:var(--lester-color-text)">${mark(32, "reference-dark")}</span><figcaption>Dark surface</figcaption></figure></div></div>`;
const defaultValues = data.themes[data.defaultTheme].values;
const row = (key, value, description, group, variable) => `<tr data-token-key="${escape(key)}" data-token-group="${group}"><td>${escape(variable)}</td><td${group === "role" ? ` data-token-value="${escape(key)}"` : ""}>${escape(value)}</td><td>${escape(description)}</td></tr>`;
const rows = [
  ...Object.entries(data.tokens).map(([key, token]) => row(key, token.value, token.description, "foundation", `--lester-${key}`)),
  ...Object.entries(defaultValues).map(([key, value]) => row(key, value, data.roles[key], "role", `--lester-${key}`)),
  ...Object.entries(data.aliases).map(([key, target]) => row(key, `var(--lester-${target})`, "Compatibility alias · 旧样式的语义映射", "alias", `--${key}`)),
].join("\n");
const references = `<h3>单一来源，实际复用</h3><p>Tokens 来自 <code>src/design/tokens.json</code>；本页控件提取自生产 <code>globals.css</code>，图标和 Logo 直接渲染项目 TSX。使用 <code>pnpm design:build</code> 生成，<code>pnpm design:check</code> 检测源与产物是否一致。</p><h3>覆盖边界</h3><p>已接入官网、工作区、账户页的基础语义与共享控件；并非所有业务样式都完成迁移。终端、代码高亮、用户 HTML、第三方品牌、旗帜和原生端保持独立。这里的主题切换仅用于比较方案，不会修改应用或保存偏好。对比度展示只检验所列颜色对。</p><h3>参考依据 · 官方源码与规范</h3><ul><li><a href="https://github.com/lucide-icons/lucide/tree/7bb15b80a3c885c68d2c65a11381f25920bbf1ef">Lucide</a>：统一网格、端点与描边。</li><li><a href="https://github.com/phosphor-icons/core/tree/2b75f3ad12b420c9504ef05df8d2564a28f8500e">Phosphor</a>：相同语义骨架上的克制双色。</li><li><a href="https://github.com/microsoft/fluentui-system-icons/tree/9bfae3d782fda4c8764f2128cec4d0dda160b283">Fluent System Icons</a>：按尺寸做光学校正。</li><li><a href="https://github.com/radix-ui/website/tree/402e749613ab2e7ab7139c65b0029842b0360f07">Radix Colors</a>：按背景、交互、边界与文字划分角色。</li><li><a href="https://github.com/carbon-design-system/carbon/tree/487b24869143fe35599118760ef39d17d8696c65">Carbon</a>：源文件与生成产物分离。当前 JSON 是项目自有简化格式，不宣称完整 DTCG 兼容。</li></ul><p>研究记录：仓库 <code>research/lester-design-system/README.md</code>。以上引用的是设计原则，Lester 图形为自行绘制。</p>`;
const themeOptions = Object.entries(data.themes).map(([key, theme]) => `<label class="theme-option"><input data-enhance disabled type="radio" name="design-theme" value="${escape(key)}"${key === data.defaultTheme ? " checked" : ""}><span><strong>${escape(theme.name)}</strong><small>${escape(theme.label)}</small></span><span class="palette-dots" data-theme="${escape(key)}" aria-hidden="true">${["color-brand", "logo-fold-end", "logo-arrow"].map(role => `<i style="background:${escape(theme.values[role])}"></i>`).join("")}</span></label>`).join("\n");
const slots = { DEFAULT_THEME: escape(data.defaultTheme), THEME_OPTIONS: themeOptions, DEFAULT_THEME_DESCRIPTION: escape(data.themes[data.defaultTheme].description), TOKEN_CSS: css, CONTROL_CSS: controlRules.join("\n"), TOKEN_JSON: JSON.stringify(data).replace(/</g, "\\u003c"), ICON_GALLERY: gallery, LOGO_SAMPLE: logos, TOKEN_ROWS: rows, REFERENCE_LINKS: references };
let html = readFileSync(resolve(root, "src/design/workbench.html"), "utf8");
for (const [key, value] of Object.entries(slots)) {
  const placeholder = `__${key}__`;
  if (html.split(placeholder).length !== 2) throw new Error(`Template must contain ${placeholder} exactly once`);
  html = html.replace(placeholder, () => value);
}
if (/__[A-Z_]+__/.test(html)) throw new Error("Unresolved workbench placeholder");
const favicon = `<!-- Generated by pnpm design:build from BrandMarkArt and default theme tokens. -->\n${mark(64, "lester-icon", defaultValues)}\n`;
const outputs = { "src/app/design-tokens.css": css, "public/design-system.html": html, "src/app/icon.svg": favicon };
let stale = false;
for (const [path, body] of Object.entries(outputs)) {
  const absolute = resolve(root, path);
  if (process.argv.includes("--check")) {
    if (!existsSync(absolute) || readFileSync(absolute, "utf8") !== body) { process.stderr.write(`Stale design artifact: ${path}. Run pnpm design:build.\n`); stale = true; }
  } else { mkdirSync(dirname(absolute), { recursive: true }); writeFileSync(absolute, body); }
}
if (stale) process.exitCode = 1;
else process.stdout.write(`Design ${process.argv.includes("--check") ? "checked" : "built"}: ${Object.keys(outputs).length} artifacts, ${glyphs.length + actions.length} glyphs, ${Object.keys(data.themes).length} palettes.\n`);
