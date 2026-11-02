import { test as base, expect, type Locator, type Page, type WebSocketRoute } from "@playwright/test";
import { WorkspaceApi, conversationId } from "./workspace-api";

const source = `/app/c/${conversationId}`;
const viewKey = `lester.view.v1.22222222-2222-4222-8222-222222222222.fixture-workspace.${conversationId}`;
const test = base.extend<{ api: WorkspaceApi }>({
  api: [async ({ context, page }, runFixture) => {
    const api = new WorkspaceApi(), errors: string[] = [];
    api.conversation = { id: conversationId, workspace_id: "fixture-workspace", created_by: "22222222-2222-4222-8222-222222222222", project_id: "fixture-project", agent_slug: "lester", agent_name: "Lester", model_deployment_id: "fixture-model", title: "自由组合工作区标签", pinned: false, run_status: "running", created_at: "2026-11-02T00:00:00Z", updated_at: "2026-11-02T00:00:00Z" };
    page.on("pageerror", error => errors.push(error.message));
    page.on("console", message => { if (message.type() === "error") errors.push(message.text()); });
    await api.install(context);
    await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", url: "http://127.0.0.1:13020" }, { name: "lester_locale", value: "zh-CN", url: "http://127.0.0.1:13020" }]);
    try { await runFixture(api); expect(api.unexpected).toEqual([]); expect(api.mutations).toEqual([]); expect(errors).toEqual([]); }
    finally { await api.close(); }
  }, { auto: true }],
});

const panel = (page: Page) => page.locator(".computer-panel");
const tabs = (page: Page) => panel(page).getByRole("tablist", { name: "工作区标签", exact: true });
async function openWorkspace(page: Page) {
  const toggle = page.locator(".workspace-panel-button");
  if ((page.viewportSize()?.width ?? 1440) > 1000) await toggle.click();
  else await page.locator(".conversation-header").getByRole("button", { name: "成果", exact: true }).click();
  await expect(panel(page)).toBeVisible();
}
async function add(page: Page, label: string) {
  await panel(page).getByLabel("添加工作区标签", { exact: true }).first().click();
  await panel(page).locator(".workspace-add-menu").getByRole("button", { name: label, exact: true }).click();
}
async function visibleTerminal(page: Page) {
  const input = panel(page).getByRole("textbox", { name: "交互式终端输入", exact: true });
  await expect(input).toBeVisible();
  await expect(panel(page).locator(".terminal-connection:visible")).toHaveText("已连接");
  return input;
}
function mockTerminals(sockets: WebSocketRoute[], inputs: string[][], closed: boolean[]) {
  return (socket: WebSocketRoute) => {
    const index = sockets.push(socket) - 1;
    inputs[index] = []; closed[index] = false;
    socket.send(JSON.stringify({ Type: "output", Data: `terminal-${index + 1} fixture@computer:~$ ` }));
    socket.onMessage(raw => { const message = JSON.parse(raw.toString()); if (message.Type === "input") inputs[index].push(message.Data); });
    socket.onClose(() => { closed[index] = true; });
  };
}
async function noOverflow(page: Page, controls: Locator) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)).toBe(false);
  const width = page.viewportSize()!.width;
  for (const control of await controls.all()) {
    if (!await control.isVisible()) continue;
    const bounds = await control.boundingBox();
    expect(bounds!.x).toBeGreaterThanOrEqual(-1); expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width + 1);
  }
}

test("one tab strip mixes files and optional tools, with singleton tools and keyboard close recovery", async ({ page, api }, info) => {
  await page.goto(source); await expect(page).toHaveTitle(/Lester/);
  await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeVisible();
  await expect(panel(page)).toBeHidden();
  await openWorkspace(page);
  // Compact navigation opens Results explicitly; closing it still exposes the same empty workspace.
  const results = tabs(page).getByRole("tab", { name: "成果", exact: true });
  if (await results.count()) await panel(page).getByRole("button", { name: "关闭 成果", exact: true }).click();
  await expect(tabs(page).getByRole("tab")).toHaveCount(0);
  const addControl = panel(page).getByLabel("添加工作区标签", { exact: true }).first();
  await expect(addControl).toBeVisible();
  await panel(page).getByRole("button", { name: "添加标签", exact: true }).focus(); await page.keyboard.press("Enter");
  await expect(addControl).toBeFocused(); await expect(panel(page).locator(".workspace-add-menu")).toBeVisible();
  await page.keyboard.press("Escape"); await expect(panel(page).locator(".workspace-add-menu")).toBeHidden(); await expect(panel(page)).toBeVisible();
  await add(page, "文件");
  await expect(tabs(page).getByRole("tab", { name: "文件", exact: true })).toHaveAttribute("aria-selected", "true");
  await add(page, "技能"); await add(page, "技能");
  await expect(tabs(page).getByRole("tab", { name: "技能", exact: true })).toHaveCount(1);
  await expect(panel(page).locator(".conversation-skills")).toBeVisible();
  api.writeFile("notes.txt", "保留原文的文件标签");
  await expect(tabs(page).getByRole("tab", { name: "notes.txt", exact: true })).toBeVisible();
  await expect(tabs(page).getByRole("tab", { name: "技能", exact: true })).toHaveAttribute("aria-selected", "true");
  await tabs(page).getByRole("tab", { name: "notes.txt", exact: true }).click();
  await expect(panel(page).getByLabel("notes.txt 源代码", { exact: true })).toContainText("保留原文");
  await panel(page).getByRole("button", { name: "关闭 notes.txt", exact: true }).click();
  await add(page, "文件");
  const treeFile = panel(page).getByRole("treeitem", { name: /notes.txt/ });
  await treeFile.focus(); await treeFile.press("Enter");
  await expect(tabs(page).getByRole("tab", { name: "notes.txt", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(tabs(page).getByRole("tab", { name: "notes.txt", exact: true })).toBeFocused();
  await add(page, "成果");
  await panel(page).getByRole("button", { name: "关闭 文件", exact: true }).click();
  await expect(tabs(page).getByRole("tab", { name: "成果", exact: true })).toHaveAttribute("aria-selected", "true");
  const all = tabs(page).getByRole("tab"), first = all.first(), last = all.last();
  await last.focus(); await page.keyboard.press("Home"); await expect(first).toBeFocused();
  await page.keyboard.press("ArrowLeft"); await expect(last).toBeFocused();
  await page.keyboard.press("ArrowRight"); await expect(first).toBeFocused();
  await page.keyboard.press("End"); await expect(last).toBeFocused();
  const before = await all.count(); await page.keyboard.press("Delete"); await expect(all).toHaveCount(before - 1);
  await expect(all.last()).toBeFocused();
  await noOverflow(page, panel(page).locator(".computer-panel-header > button, #workspace-tab-add"));
  await page.screenshot({ path: info.outputPath("mixed-workspace-tabs.png") });
});

test("terminal tabs keep independent live shells through switching and panel hiding", async ({ page, context }, info) => {
  const sockets: WebSocketRoute[] = [], inputs: string[][] = [], closed: boolean[] = [];
  await context.routeWebSocket(`**/api/v1/conversations/${conversationId}/terminal`, mockTerminals(sockets, inputs, closed));
  await page.goto(source); await openWorkspace(page); expect(sockets).toHaveLength(0); await add(page, "终端");
  let input = await visibleTerminal(page); await input.focus(); await page.keyboard.type("first-shell");
  await expect.poll(() => inputs[0]?.join("")).toContain("first-shell");
  await add(page, "终端");
  await expect(tabs(page).getByRole("tab", { name: "终端 2", exact: true })).toHaveAttribute("aria-selected", "true");
  input = await visibleTerminal(page); await input.focus(); await page.keyboard.type("second-shell");
  await expect.poll(() => inputs[1]?.join("")).toContain("second-shell");
  await tabs(page).getByRole("tab", { name: "终端", exact: true }).click();
  input = await visibleTerminal(page); await input.focus(); await page.keyboard.type("-still-first");
  await expect.poll(() => inputs[0]?.join("")).toContain("-still-first");
  expect(inputs[1].join("")).not.toContain("-still-first"); expect(sockets).toHaveLength(2); expect(closed).toEqual([false, false]);
  await panel(page).getByRole("button", { name: info.project.name === "mobile" ? "返回对话" : "关闭工作区面板", exact: true }).click();
  await expect(panel(page)).toBeHidden(); expect(closed).toEqual([false, false]);
  await openWorkspace(page);
  await tabs(page).getByRole("tab", { name: "终端", exact: true }).click(); await visibleTerminal(page);
  expect(sockets).toHaveLength(2);
  await panel(page).getByRole("button", { name: "关闭 终端", exact: true }).click();
  await expect.poll(() => closed[0]).toBe(true); expect(closed[1]).toBe(false);
  await tabs(page).getByRole("tab", { name: "终端 2", exact: true }).click();
  input = await visibleTerminal(page); await input.focus(); await page.keyboard.type("-second-alive");
  await expect.poll(() => inputs[1]?.join("")).toContain("-second-alive"); expect(sockets).toHaveLength(2);
  await page.screenshot({ path: info.outputPath("independent-terminal-tabs.png") });
});

test("background files leave the terminal selected and preserve the draft and its focus", async ({ page, context, api }, info) => {
  const sockets: WebSocketRoute[] = [], inputs: string[][] = [], closed: boolean[] = [];
  await context.routeWebSocket(`**/api/v1/conversations/${conversationId}/terminal`, mockTerminals(sockets, inputs, closed));
  api.files.push({ path: "other.txt", name: "other.txt", is_dir: false, size: 100, modified_at: "2026-11-01T00:00:00Z" });
  await page.goto(source);
  const draft = page.getByRole("textbox", { name: "消息输入框", exact: true }); await draft.fill("没有发送的任务补充");
  await openWorkspace(page); await add(page, "终端"); await visibleTerminal(page);
  if (info.project.name === "desktop") await draft.focus();
  api.writeFile("report.html", "<!doctype html><h1>后台成果</h1>");
  await expect(tabs(page).getByRole("tab", { name: "report.html", exact: true })).toBeVisible();
  await expect(tabs(page).getByRole("tab", { name: "终端", exact: true })).toHaveAttribute("aria-selected", "true");
  if (info.project.name === "desktop") await expect(draft).toBeFocused();
  else { await panel(page).getByRole("button", { name: "返回对话", exact: true }).click(); await expect(draft).toHaveValue("没有发送的任务补充"); await openWorkspace(page); await tabs(page).getByRole("tab", { name: "终端", exact: true }).click(); }
  expect(sockets).toHaveLength(1);
  await tabs(page).getByRole("tab", { name: "report.html", exact: true }).click();
  await expect(page.frameLocator('iframe[title="report.html preview"]').getByRole("heading", { name: "后台成果", exact: true })).toBeVisible();
  await panel(page).getByRole("button", { name: "源码", exact: true }).click();
  await expect(panel(page).getByLabel("report.html 源代码", { exact: true })).toContainText("后台成果");
  await page.screenshot({ path: info.outputPath("file-and-terminal-tabs.png") });
  await panel(page).getByRole("button", { name: "预览", exact: true }).click();
  await add(page, "文件"); await add(page, "终端"); await visibleTerminal(page);
  await tabs(page).getByRole("tab", { name: "report.html", exact: true }).click();
  await expect(page.frameLocator('iframe[title="report.html preview"]').getByRole("heading", { name: "后台成果", exact: true })).toBeVisible();
  if (info.project.name === "desktop") await panel(page).getByLabel("添加工作区标签", { exact: true }).first().click();
  await page.screenshot({ path: info.outputPath("workspace-tabs-preview.png") });
  if (info.project.name === "desktop") await page.keyboard.press("Escape");
  await tabs(page).getByRole("tab", { name: "终端", exact: true }).click(); await visibleTerminal(page);
  const focus = info.project.name === "desktop" ? draft : tabs(page).getByRole("tab", { name: "终端", exact: true });
  await focus.focus();
  await page.evaluate(() => {
    document.documentElement.dataset.testPreviewMessage = "waiting";
    window.addEventListener("message", event => {
      if (event.data?.type === "lester:preview:navigate" && event.data.path === "other.txt") document.documentElement.dataset.testPreviewMessage = "received";
    });
  });
  const report = page.frameLocator('iframe[title="report.html preview"]').locator("body");
  await report.evaluate(() => parent.postMessage({ type: "lester:preview:navigate", path: "other.txt" }, "*"));
  await expect(page.locator("html")).toHaveAttribute("data-test-preview-message", "received");
  await expect(tabs(page).getByRole("tab", { name: "终端", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(tabs(page).getByRole("tab", { name: "other.txt", exact: true })).toHaveCount(0); await expect(focus).toBeFocused();
  await tabs(page).getByRole("tab", { name: "report.html", exact: true }).click();
  await page.locator('iframe[title="report.html preview"]').focus();
  await report.evaluate(() => parent.postMessage({ type: "lester:preview:navigate", path: "other.txt" }, "*"));
  await expect(tabs(page).getByRole("tab", { name: "other.txt", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(tabs(page).getByRole("tab", { name: "other.txt", exact: true })).toBeFocused();
});

test("reload restores chosen layout but terminal reconnection remains explicit", async ({ page, context }) => {
  const sockets: WebSocketRoute[] = [], inputs: string[][] = [], closed: boolean[] = [];
  await context.routeWebSocket(`**/api/v1/conversations/${conversationId}/terminal`, mockTerminals(sockets, inputs, closed));
  await page.goto(source); await openWorkspace(page); await add(page, "文件"); await add(page, "终端"); await visibleTerminal(page);
  expect(sockets).toHaveLength(1);
  await page.reload();
  await expect(tabs(page).getByRole("tab", { name: "文件", exact: true })).toHaveCount(1);
  await expect(tabs(page).getByRole("tab", { name: "终端", exact: true })).toHaveAttribute("aria-selected", "true");
  const reconnect = panel(page).getByRole("button", { name: "连接终端", exact: true }); await expect(reconnect).toBeVisible();
  expect(sockets).toHaveLength(1);
  await reconnect.click(); await visibleTerminal(page); expect(sockets).toHaveLength(2);
});

test("a narrow workspace keeps adding and closing mixed tabs reachable", async ({ page, context, api }, info) => {
  const sockets: WebSocketRoute[] = [], inputs: string[][] = [], closed: boolean[] = [];
  await context.routeWebSocket(`**/api/v1/conversations/${conversationId}/terminal`, mockTerminals(sockets, inputs, closed));
  await page.setViewportSize({ width: 320, height: 640 });
  await page.goto(source); await openWorkspace(page);
  const addControl = panel(page).getByLabel("添加工作区标签", { exact: true }).first();
  await addControl.click(); await noOverflow(page, panel(page).locator(".workspace-add-menu"));
  await addControl.press("Tab"); await expect(panel(page).locator(".workspace-add-menu")).toBeVisible();
  await page.keyboard.press("Escape"); await expect(panel(page).locator(".workspace-add-menu")).toBeHidden(); await expect(panel(page)).toBeVisible();
  await add(page, "文件"); await add(page, "终端"); await visibleTerminal(page);
  api.writeFile("a-very-long-generated-report-name.html", "<!doctype html><h1>窄屏成果</h1>");
  const file = tabs(page).getByRole("tab", { name: "a-very-long-generated-report-name.html", exact: true });
  await file.click(); await expect(file).toBeVisible();
  await noOverflow(page, panel(page).locator(".computer-panel-header > button, #workspace-tab-add"));
  await add(page, "技能"); await expect(tabs(page).getByRole("tab", { name: "技能", exact: true })).toBeVisible();
  await panel(page).getByRole("button", { name: "关闭 技能", exact: true }).click();
  await expect(file).toHaveAttribute("aria-selected", "true");
  await page.screenshot({ path: info.outputPath("workspace-tabs-320px.png") });
});

test("terminal capacity is explicit, closing frees a slot and an empty layout stays empty after reload", async ({ page, context }, info) => {
  const sockets: WebSocketRoute[] = [], inputs: string[][] = [], closed: boolean[] = [];
  await context.routeWebSocket(`**/api/v1/conversations/${conversationId}/terminal`, mockTerminals(sockets, inputs, closed));
  await page.goto(source); await openWorkspace(page);
  for (let i = 0; i < 4; i++) { await add(page, "终端"); await visibleTerminal(page); }
  expect(sockets).toHaveLength(4);
  const addControl = panel(page).getByLabel("添加工作区标签", { exact: true }).first();
  await addControl.click();
  const addTerminal = panel(page).locator(".workspace-add-menu").getByRole("button", { name: "终端", exact: true });
  await expect(addTerminal).toBeDisabled(); await expect(addTerminal).toHaveAttribute("title", "最多同时打开 4 个终端");
  await page.keyboard.press("Escape");
  await panel(page).getByRole("button", { name: "关闭 终端 2", exact: true }).click();
  await expect.poll(() => closed[1]).toBe(true); expect(closed).toEqual([false, true, false, false]);
  await add(page, "终端"); await visibleTerminal(page);
  await expect(tabs(page).getByRole("tab", { name: "终端 5", exact: true })).toHaveAttribute("aria-selected", "true");
  expect(sockets).toHaveLength(5); expect(closed[0]).toBe(false); expect(closed[2]).toBe(false); expect(closed[3]).toBe(false);
  while (await tabs(page).getByRole("tab").count()) {
    await tabs(page).getByRole("tab").last().focus(); await page.keyboard.press("Delete");
  }
  await expect.poll(() => closed.every(Boolean)).toBe(true);
  await expect(panel(page).locator(".workspace-tabs-empty")).toBeVisible();
  await expect(addControl).toBeFocused();
  await page.reload();
  await expect(panel(page).locator(".workspace-tabs-empty")).toBeVisible();
  await expect(tabs(page).getByRole("tab")).toHaveCount(0); expect(sockets).toHaveLength(5);
  await page.screenshot({ path: info.outputPath("empty-workspace-restored.png") });
});

test("source and Markdown reading positions survive hidden tabs, file browsing and reload", async ({ page, context, api }, info) => {
  const sockets: WebSocketRoute[] = [], inputs: string[][] = [], closed: boolean[] = [];
  await context.routeWebSocket(`**/api/v1/conversations/${conversationId}/terminal`, mockTerminals(sockets, inputs, closed));
  await page.goto(source); await expect.poll(() => api.streamCount).toBeGreaterThan(0);
  api.writeFile("long.txt", Array.from({ length: 200 }, (_, index) => `第 ${index + 1} 行：保留源码阅读位置`).join("\n"));
  await expect(tabs(page).getByRole("tab", { name: "long.txt", exact: true })).toBeVisible();
  const text = panel(page).locator(".file-source-scroll:visible");
  await expect(text).toContainText("第 200 行");
  await text.evaluate(node => { node.scrollTop = 420; });
  const saved = (resource: string) => page.evaluate(({ key, resource }) => JSON.parse(sessionStorage.getItem(key) || "{}").positions?.[resource]?.top, { key: viewKey, resource });
  await expect.poll(() => saved("source:long.txt")).toBe(420);
  await add(page, "终端"); await visibleTerminal(page);
  await tabs(page).getByRole("tab", { name: "long.txt", exact: true }).click();
  await expect.poll(() => text.evaluate(node => node.scrollTop)).toBe(420);
  await add(page, "文件"); await expect(panel(page).getByRole("treeitem", { name: /long.txt/ })).toBeVisible();
  await tabs(page).getByRole("tab", { name: "long.txt", exact: true }).click();
  await expect.poll(() => text.evaluate(node => node.scrollTop)).toBe(420);
  api.writeFile("guide.md", Array.from({ length: 120 }, (_, index) => `## 章节 ${index + 1}\n\n这是一段用于验证 Markdown 阅读位置的完整段落。`).join("\n\n"));
  await tabs(page).getByRole("tab", { name: "guide.md", exact: true }).click();
  const markdown = panel(page).locator(".file-preview-body.markdown:visible");
  await expect(markdown).toContainText("章节 120");
  await markdown.evaluate(node => { node.scrollTop = 620; });
  await expect.poll(() => saved("markdown:guide.md")).toBe(620);
  await tabs(page).getByRole("tab", { name: "终端", exact: true }).click(); await visibleTerminal(page);
  await tabs(page).getByRole("tab", { name: "guide.md", exact: true }).click();
  await expect.poll(() => markdown.evaluate(node => node.scrollTop)).toBe(620);
  await tabs(page).getByRole("tab", { name: "终端", exact: true }).click();
  await page.reload();
  await expect(tabs(page).getByRole("tab", { name: "终端", exact: true })).toHaveAttribute("aria-selected", "true");
  expect(sockets).toHaveLength(1);
  await tabs(page).getByRole("tab", { name: "long.txt", exact: true }).click();
  await expect.poll(() => text.evaluate(node => node.scrollTop)).toBe(420);
  await tabs(page).getByRole("tab", { name: "guide.md", exact: true }).click();
  await expect.poll(() => markdown.evaluate(node => node.scrollTop)).toBe(620);
  await page.screenshot({ path: info.outputPath("markdown-position-restored.png") });
});

test("native restore hides old terminal tabs and selects an available file without opening sockets", async ({ page, context, api }, info) => {
  const sockets: WebSocketRoute[] = [], inputs: string[][] = [], closed: boolean[] = [];
  await context.routeWebSocket(`**/api/v1/conversations/${conversationId}/terminal`, mockTerminals(sockets, inputs, closed));
  api.files.push({ path: "notes.txt", name: "notes.txt", is_dir: false, size: 100, modified_at: "2026-11-01T00:00:00Z" });
  await context.addInitScript(({ key, platform }) => {
    if (window.top !== window) return;
    Object.defineProperty(window, "__LESTER_NATIVE__", { value: Object.freeze({ version: 1, platform }) });
    if (platform === "android") Object.defineProperty(window, "LesterAndroid", { value: { postMessage: () => {} } });
    else Object.defineProperty(window, "webkit", { value: { messageHandlers: { lester: { postMessage: () => {} } } } });
    if (!sessionStorage.getItem(key)) sessionStorage.setItem(key, JSON.stringify({ version: 1, tabs: ["notes.txt"], selected: "notes.txt", workspaceTabs: [{ id: "terminal:old-shell", kind: "terminal", serial: 1 }, { id: "file:notes.txt", kind: "file", path: "notes.txt" }], activeWorkspaceTab: "terminal:old-shell", workspacePanelOpen: true, workspacePanelIntent: true }));
  }, { key: viewKey, platform: info.project.name === "mobile" ? "android" : "ios" });
  await page.goto(source);
  await expect(page.locator("html")).toHaveAttribute("data-lester-native", info.project.name === "mobile" ? "android" : "ios");
  const file = tabs(page).getByRole("tab", { name: "notes.txt", exact: true });
  await expect(file).toHaveAttribute("aria-selected", "true");
  await expect(tabs(page).getByRole("tab", { name: /^终端/ })).toHaveCount(0);
  await expect(panel(page).getByLabel("notes.txt 源代码", { exact: true })).toContainText("请人工验收");
  await file.focus(); await page.keyboard.press("Home"); await expect(file).toBeFocused();
  await panel(page).getByLabel("添加工作区标签", { exact: true }).first().click();
  await expect(panel(page).locator(".workspace-add-menu").getByRole("button", { name: "终端", exact: true })).toHaveCount(0);
  expect(sockets).toHaveLength(0);
  await page.screenshot({ path: info.outputPath("native-restored-visible-tab.png") });
});

test("native created-Agent configuration stays reachable and keeps unsaved edits when switching tabs", async ({ page, context, api }, info) => {
  const sockets: WebSocketRoute[] = [], inputs: string[][] = [], closed: boolean[] = [];
  await context.routeWebSocket(`**/api/v1/conversations/${conversationId}/terminal`, mockTerminals(sockets, inputs, closed));
  const agentId = "44444444-4444-4444-8444-444444444444";
  api.conversation!.agent_slug = "agent-designer"; api.conversation!.agent_name = "智能体设计师"; api.conversation!.created_agent_id = agentId;
  await context.route(`**/api/v1/agents/custom-${agentId}`, route => route.fulfill({ json: { id: agentId, slug: `custom-${agentId}`, name: "原生端配置回归", description: "固定配置样例，不调用真实模型。", instructions: "这是已保存的 Agent 提示词。", skill_slugs: [], version: 1, builtin: false, updated_at: "2026-11-02T00:00:00Z" } }));
  await context.route(`**/api/v1/agents/${agentId}/files`, route => route.fulfill({ json: { files: [] } }));
  await context.addInitScript(platform => {
    if (window.top !== window) return;
    Object.defineProperty(window, "__LESTER_NATIVE__", { value: Object.freeze({ version: 1, platform }) });
    if (platform === "android") Object.defineProperty(window, "LesterAndroid", { value: { postMessage: () => {} } });
    else Object.defineProperty(window, "webkit", { value: { messageHandlers: { lester: { postMessage: () => {} } } } });
  }, info.project.name === "mobile" ? "android" : "ios");
  await page.goto(source);
  if ((page.viewportSize()?.width ?? 1440) > 1000) { await openWorkspace(page); await add(page, "智能体"); }
  else await page.locator(".conversation-header").getByRole("button", { name: "智能体", exact: true }).click();
  await expect(tabs(page).getByRole("tab", { name: "智能体", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(panel(page).locator(".designer-agent-panel")).toBeVisible();
  await expect(panel(page).getByRole("textbox", { name: "名称", exact: true })).toHaveValue("原生端配置回归");
  const prompt = panel(page).getByRole("textbox", { name: "系统提示词", exact: true }); await expect(prompt).toHaveValue("这是已保存的 Agent 提示词。");
  await prompt.fill("切换标签期间保留尚未保存的配置。");
  await add(page, "文件"); await add(page, "智能体");
  await expect(tabs(page).getByRole("tab", { name: "智能体", exact: true })).toHaveCount(1);
  await expect(prompt).toHaveValue("切换标签期间保留尚未保存的配置。");
  await panel(page).getByLabel("添加工作区标签", { exact: true }).first().click();
  await expect(panel(page).locator(".workspace-add-menu").getByRole("button", { name: "终端", exact: true })).toHaveCount(0);
  expect(sockets).toHaveLength(0);
  await page.screenshot({ path: info.outputPath("native-agent-configuration.png") });
});
