import { test, expect, type BrowserContext, type Page, type WebSocketRoute } from "@playwright/test";
import { WorkspaceApi, conversationId } from "./workspace-api";

const source = `/app/c/${conversationId}`, overview = "/app/p/fixture-project/overview";
async function fixture(context: BrowserContext, running = false) {
  const api = new WorkspaceApi();
  api.conversation = { id: conversationId, workspace_id: "fixture-workspace", created_by: "22222222-2222-4222-8222-222222222222", project_id: "fixture-project", agent_slug: "lester", agent_name: "Lester", model_deployment_id: "fixture-model", title: "官网迭代任务", pinned: false, run_status: running ? "running" : "completed", created_at: "2026-10-17T09:00:00Z", updated_at: "2026-10-17T09:00:00Z" };
  if (!running) api.finish();
  if (!running) await context.addInitScript(() => {
    // These files are existing history, rather than new live output arriving
    // while the user types. Start after their acknowledged fixture events.
    const key = "lester.workspace.event-cursor.v1.fixture-workspace";
    if (!sessionStorage.getItem(key)) sessionStorage.setItem(key, "7");
  });
  await api.install(context);
  await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", url: "http://127.0.0.1:13020" }, { name: "lester_locale", value: "zh-CN", url: "http://127.0.0.1:13020" }]);
  await context.route("**/api/v1/me/identities", route => route.fulfill({ json: { identities: [] } }));
  return api;
}
const result = (index = 0) => ({ id: `result-${index}`, conversation_id: conversationId, run_id: "fixture-run", title: `官网 ${index}`, summary: "固定样例，需人工验收。", entry_path: "index.html", kind: "html", content_sha256: "a".repeat(64), created_at: "2026-10-17T09:00:00Z", updated_at: "2026-10-17T09:00:00Z", conversation_title: "官网迭代任务", run_status: "completed" });
async function project(page: Page, compact: boolean) {
  if (compact) await page.getByRole("button", { name: "打开会话栏", exact: true }).click();
  await page.getByRole("button", { name: "打开项目 个人项目", exact: true }).click();
  await expect(page).toHaveURL(overview);
}

test("project preview and browser back retain filters, loaded pages and reading position", async ({ page, context }, info) => {
  const api = await fixture(context);
  await context.route("**/api/v1/projects/fixture-project/deliverables?*", route => {
    const more = new URL(route.request().url()).searchParams.get("cursor");
    return route.fulfill({ json: { deliverables: Array.from({ length: more ? 3 : 24 }, (_, index) => result(index + (more ? 24 : 0))), next_cursor: more ? "" : "next-fixture" } });
  });
  try {
    await page.goto(overview); await expect(page.locator(".project-result-card")).toHaveCount(24);
    const search = page.getByRole("textbox", { name: "搜索项目成果", exact: true }), kind = page.getByRole("combobox", { name: "成果类型", exact: true });
    const filtered = page.waitForResponse(response => { const url = new URL(response.url()); return url.pathname.endsWith("/deliverables") && url.searchParams.get("q") === "官网" && url.searchParams.get("kind") === "html"; });
    await search.fill("官网"); await kind.selectOption("html"); await filtered;
    await expect.poll(() => page.locator('[role="tabpanel"]').getAttribute("aria-busy")).toBe("false");
    await page.getByRole("button", { name: "加载更多成果", exact: true }).click(); await expect(page.locator(".project-result-card")).toHaveCount(27);
    const preview = page.getByRole("button", { name: "预览项目成果 官网 26", exact: true }); await preview.scrollIntoViewIfNeeded();
    const top = await page.locator(".project-workbench").evaluate(node => node.scrollTop); expect(top).toBeGreaterThan(100);
    await preview.click(); await expect(page.locator('iframe[title="index.html preview"]')).toBeVisible();
    await page.goBack(); await expect(search).toHaveValue("官网"); await expect(kind).toHaveValue("html");
    await expect(page.locator(".project-result-card")).toHaveCount(27);
    await expect.poll(() => page.locator(".project-workbench").evaluate(node => node.scrollTop)).toBeGreaterThan(top - 3);
    await page.screenshot({ path: info.outputPath("project-back-position.png") });
    await page.getByRole("tab", { name: /^会话/ }).click();
    await page.locator(".project-conversation-list button").click(); await expect(page).toHaveURL(source);
    await page.goBack(); await expect(page.getByRole("tab", { name: /^会话/ })).toHaveAttribute("aria-selected", "true");
    expect(api.unexpected).toEqual([]); expect(api.mutations).toEqual([]);
  } finally { await api.close(); }
});

test("continue modifying cannot overwrite a newer draft from another browser tab", async ({ page, context }, info) => {
  const api = await fixture(context);
  await context.route("**/api/v1/projects/fixture-project/deliverables?*", route => route.fulfill({ json: { deliverables: [result()], next_cursor: "" } }));
  try {
    await page.goto(source); const input = page.getByRole("textbox", { name: "消息输入框", exact: true }); await input.fill("旧草稿");
    const other = await context.newPage(); await other.goto(source); await other.getByRole("textbox", { name: "消息输入框", exact: true }).fill("另一个标签页里刚写好的新草稿");
    await page.bringToFront(); await project(page, info.project.name === "mobile");
    await page.getByRole("button", { name: "继续修改项目成果 官网 0", exact: true }).click();
    await expect(page).toHaveURL(source); await expect(input).toHaveValue("另一个标签页里刚写好的新草稿"); await expect(page.locator(".file-reference")).toContainText("index.html");
    await other.close(); await page.reload(); await expect(input).toHaveValue("另一个标签页里刚写好的新草稿");
    expect(api.unexpected).toEqual([]); expect(api.mutations).toEqual([]);
  } finally { await api.close(); }
});

test("failed conversation reads stop loading and retry only the read without losing drafts", async ({ page, context }, info) => {
  const api = await fixture(context); let fail = false, reads = 0;
  await context.route(`**/api/v1/conversations/${conversationId}`, route => {
    reads++;
    return route.fulfill(fail ? { status: 503, json: { error: "测试：会话暂时不可用" } } : { json: { conversation: api.conversation, messages: api.messages, active_run: null } });
  });
  try {
    await page.goto(source); const input = page.getByRole("textbox", { name: "消息输入框", exact: true }); await input.fill("重试后保留的草稿");
    fail = true; await page.reload(); await expect(page.locator(".workspace-error")).toContainText("测试：会话暂时不可用");
    await expect(page.getByText("正在载入对话…", { exact: true })).toHaveCount(0);
    const retry = page.locator(".workspace-error").getByRole("button", { name: "重试", exact: true }); await expect(retry).toBeEnabled();
    await page.screenshot({ path: info.outputPath("conversation-retry.png") });
    fail = false; const before = reads; await retry.click(); await expect(input).toHaveValue("重试后保留的草稿"); expect(reads).toBe(before + 1);
    await expect(page.locator(".workspace-error")).toHaveCount(0); expect(api.mutations).toEqual([]); expect(api.unexpected).toEqual([]);
  } finally { await api.close(); }
});

test("workspace bootstrap failures have a usable retry instead of a blank home", async ({ page, context }) => {
  const api = await fixture(context); let fail = true;
  await context.route("**/api/v1/projects", route => route.fulfill(fail ? { status: 503, json: { error: "测试：项目服务暂时不可用" } } : { json: { projects: [{ id: "fixture-project", name: "个人项目", is_default: true, pinned: false, conversation_count: 1, created_at: "2026-10-17T09:00:00Z" }] } }));
  try {
    await page.goto("/app/new"); await expect(page.locator(".workspace-error")).toContainText("测试：项目服务暂时不可用"); await expect(page.getByText("正在载入对话…", { exact: true })).toHaveCount(0);
    fail = false; await page.locator(".workspace-error").getByRole("button", { name: "重试", exact: true }).click(); await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeVisible();
    expect(api.mutations).toEqual([]); expect(api.unexpected).toEqual([]);
  } finally { await api.close(); }
});

test("switching settings sections retains the original project and its draft", async ({ page, context }) => {
  const api = await fixture(context);
  try {
    await page.goto("/app/p/fixture-project"); const input = page.getByRole("textbox", { name: "消息输入框", exact: true }); await input.fill("属于这个项目的任务草稿");
    await page.goto("/app/settings/models?returnTo=%2Fapp%2Fp%2Ffixture-project");
    await page.locator(".settings-sidebar nav").getByRole("button", { name: "Skill 广场", exact: true }).click();
    await expect(page).toHaveURL("/app/settings/skills?returnTo=%2Fapp%2Fp%2Ffixture-project");
    await page.locator(".settings-sidebar nav").getByRole("button", { name: "个人资料", exact: true }).click();
    await page.getByRole("button", { name: "返回工作区", exact: true }).click(); await expect(page).toHaveURL("/app/p/fixture-project"); await expect(input).toHaveValue("属于这个项目的任务草稿");
    expect(api.mutations).toEqual([]); expect(api.unexpected).toEqual([]);
  } finally { await api.close(); }
});

test("short viewport keeps question choices readable beside a queue and long message draft", async ({ page, context }, info) => {
  const api = await fixture(context, true); await page.setViewportSize({ width: 393, height: 520 });
  const id = "44444444-4444-4444-8444-444444444444", digest = "a".repeat(64);
  const item = { id, run_id: "fixture-run", kind: "question", request_digest: digest, status: "pending", request: { questions: [{ id: "features", prompt: "选择要完善的交互细节", multi_select: true, options: [{ id: "queue", label: "消息队列", description: "继续安排下一条任务" }, { id: "recap", label: "会话回顾", description: "回来继续工作" }, { id: "files", label: "文件预览", description: "查看成果" }] }] } };
  await context.route(`**/api/v1/conversations/${conversationId}/interactions`, route => route.fulfill({ json: { interactions: [item], execution_mode: "execute" } }));
  let answers = 0;
  await context.route(`**/api/v1/conversations/${conversationId}/interactions/${id}/respond`, route => { answers++; return route.fulfill({ json: { ...item, status: "answered", response: route.request().postDataJSON() } }); });
  await context.route(`**/api/v1/conversations/${conversationId}/permissions/inputs`, route => route.fulfill({ json: { inputs: Array.from({ length: 5 }, (_, index) => ({ id: `queue-${index}`, after_run_id: "fixture-run", kind: "next", status: "queued", revision: 1, content: "等当前工作完成后再继续处理这个任务", request: { permission_mode: "standard" }, accepted_order: index })) } }));
  try {
    await page.goto(source); const input = page.getByRole("textbox", { name: "消息输入框", exact: true }), draft = "回答时保留的很长的聊天草稿。".repeat(80);
    await expect(page.locator(".question-card")).toBeVisible(); await expect(page.locator(".composer-queue-item")).toHaveCount(5); await input.fill(draft);
    // Check actual readable content rather than an arbitrary container height:
    // the heading and a complete choice must fit before any manual scrolling.
    await expect.poll(() => page.locator(".question-body").evaluate(node => {
      const body = node.getBoundingClientRect(), heading = node.querySelector("h3")!.getBoundingClientRect(), choice = node.querySelector(".interaction-option")!.getBoundingClientRect();
      return heading.top >= body.top - 1 && choice.bottom <= body.bottom + 1;
    })).toBe(true);
    for (const selector of [".question-footer", ".compose-actions", ".stop-button"]) {
      const box = await page.locator(selector).boundingBox(); expect(box!.y).toBeGreaterThanOrEqual(0); expect(box!.y + box!.height).toBeLessThanOrEqual(521);
    }
    await page.screenshot({ path: info.outputPath("short-question-with-queue.png") });
    await page.getByRole("checkbox", { name: /消息队列/ }).check(); await page.getByRole("button", { name: "提交回答", exact: true }).click();
    await expect(page.locator(".question-card")).toHaveCount(0); await expect(input).toHaveValue(draft); expect(answers).toBe(1); expect(api.mutations).toEqual([]); expect(api.unexpected).toEqual([]);
  } finally { await api.close(); }
});

test("avatar cropping supports cancel, keyboard adjustments and one explicit upload", async ({ page, context }, info) => {
  const api = await fixture(context); let uploads = 0;
  await context.route("**/api/v1/me/avatar", route => { uploads++; expect(route.request().method()).toBe("POST"); return route.fulfill({ json: { user_id: "22222222-2222-4222-8222-222222222222", workspace_id: "fixture-workspace", email: "browser@example.test", display_name: "浏览器测试", avatar_key: "forest", role: "member", email_verified: true, has_password: true } }); });
  try {
    await page.goto("/app/settings/profile"); await expect(page.getByLabel("称呼", { exact: true })).toHaveValue("浏览器测试");
    const png = await page.evaluate(() => { const canvas = document.createElement("canvas"); canvas.width = 640; canvas.height = 320; const paint = canvas.getContext("2d")!; paint.fillStyle = "#34533c"; paint.fillRect(0, 0, 640, 320); paint.fillStyle = "#e4eadb"; paint.fillRect(320, 0, 320, 320); return canvas.toDataURL("image/png").split(",")[1]; });
    const upload = page.getByLabel("上传头像图片", { exact: true }), photo = { name: "test-avatar.png", mimeType: "image/png", buffer: Buffer.from(png, "base64") };
    await upload.setInputFiles(photo); const dialog = page.getByRole("dialog", { name: "裁剪头像", exact: true }); await expect(dialog.getByRole("button", { name: "保存头像", exact: true })).toBeEnabled();
    await page.keyboard.press("Escape"); await expect(dialog).toHaveCount(0); expect(uploads).toBe(0);
    await upload.setInputFiles(photo); await expect(dialog.getByRole("button", { name: "旋转", exact: true })).toBeEnabled();
    await dialog.getByRole("button", { name: "旋转", exact: true }).click();
    await dialog.getByRole("slider", { name: "缩放", exact: true }).fill("2"); await dialog.getByRole("img").focus(); await page.keyboard.press("ArrowRight");
    await page.screenshot({ path: info.outputPath("avatar-crop.png") });
    await dialog.getByRole("button", { name: "保存头像", exact: true }).click(); await expect(dialog).toHaveCount(0); await expect(page.getByRole("status")).toContainText("图片头像已保存"); expect(uploads).toBe(1); expect(api.unexpected).toEqual([]);
  } finally { await api.close(); }
});

test("terminal input, control keys, focus escape and reconnection work inside the workspace", async ({ page, context }, info) => {
  const api = await fixture(context), sockets: WebSocketRoute[] = [], inputs: string[] = [], sizes: { Cols: number; Rows: number }[] = [];
  await context.routeWebSocket(`**/api/v1/conversations/${conversationId}/terminal`, socket => {
    sockets.push(socket);
    // This validates rendered terminal transport, not a live Shell/readline.
    socket.send(JSON.stringify({ Type: "output", Data: "\u001b[32mfixture@computer\u001b[0m:~$ " }));
    socket.onMessage(raw => { const message = JSON.parse(raw.toString()); if (message.Type === "input") inputs.push(message.Data); if (message.Type === "resize") sizes.push(message); });
  });
  try {
    await page.goto(source); const draft = page.getByRole("textbox", { name: "消息输入框", exact: true }); await draft.fill("终端操作时保留的聊天草稿");
    if (info.project.name === "mobile") await page.getByRole("button", { name: "成果", exact: true }).click();
    else await page.locator(".workspace-panel-button").click();
    const panel = page.locator(".computer-panel"); await panel.locator(".workspace-tools>summary").click(); await panel.getByRole("button", { name: "终端", exact: true }).click();
    await expect(panel.locator(".terminal-connection")).toHaveText("已连接");
    const terminal = panel.getByRole("textbox", { name: "交互式终端输入", exact: true }); await terminal.focus();
    await page.keyboard.type("pwd"); await page.keyboard.press("Enter"); await page.keyboard.press("Tab"); await page.keyboard.press("Control+c");
    await expect.poll(() => inputs.join("")).toContain("pwd\r\t\x03");
    await page.keyboard.press("Escape"); await expect.poll(() => inputs.at(-1)).toBe("\x1b"); await expect(panel).toBeVisible();
    if (info.project.name === "mobile") await panel.getByRole("button", { name: "上一条历史命令", exact: true }).click();
    else await page.keyboard.press("ArrowUp");
    await expect.poll(() => inputs.at(-1)).toBe("\x1b[A");
    await terminal.focus(); await page.keyboard.press("Shift+Escape"); await expect(panel.getByRole("button", { name: "查看终端快捷键", exact: true })).toBeFocused();
    await expect.poll(() => sizes.some(size => size.Cols > 0 && size.Rows > 0)).toBe(true);
    await page.screenshot({ path: info.outputPath("terminal-connected.png") });
    sockets[0].close({ code: 1000, reason: "Fixture shell ended" }); await expect(panel.locator(".terminal-connection")).toHaveText("已断开");
    await panel.getByRole("button", { name: "重新连接", exact: true }).click(); await expect(panel.locator(".terminal-connection")).toHaveText("已连接"); expect(sockets).toHaveLength(2);
    await panel.getByRole("button", { name: info.project.name === "mobile" ? "返回对话" : "关闭工作区面板", exact: true }).click(); await expect(draft).toHaveValue("终端操作时保留的聊天草稿");
    expect(api.unexpected).toEqual([]); expect(api.mutations).toEqual([]);
  } finally { await api.close(); }
});

test("late context lists cannot hide saved entries; edit, dirty cancellation and delete remain explicit", async ({ page, context }, info) => {
  const api = await fixture(context); let release!: () => void; const gate = new Promise<void>(resolve => { release = resolve; });
  type Entry = { id: string; title: string; description: string; content: string; version: number; updated_at: string };
  let entries: Entry[] = [], reads = 0; const mutations: string[] = [];
  await context.route("**/api/v1/contexts", async route => {
    if (route.request().method() === "POST") { mutations.push("create"); entries = [{ ...route.request().postDataJSON(), id: "fixture-context", version: 1, updated_at: new Date().toISOString() }]; return route.fulfill({ json: entries[0] }); }
    const snapshot = [...entries], read = ++reads; if (read === 1) await gate;
    if (read === 3) return route.fulfill({ status: 503, json: { error: "测试：保存成功后的列表刷新暂时失败" } });
    return route.fulfill({ json: { entries: snapshot } });
  });
  await context.route("**/api/v1/contexts/fixture-context", route => {
    if (route.request().method() === "PATCH") { mutations.push("edit"); entries = [{ ...entries[0], ...route.request().postDataJSON(), version: entries[0].version + 1 }]; return route.fulfill({ json: entries[0] }); }
    if (route.request().method() === "DELETE") { mutations.push("delete"); entries = []; return route.fulfill({ status: 204 }); }
    return route.fulfill({ json: entries[0] });
  });
  try {
    await page.goto("/app/contexts"); await expect.poll(() => reads).toBe(1);
    await page.getByRole("button", { name: "新建词条", exact: true }).click(); await page.getByRole("textbox", { name: "名称", exact: true }).fill("品牌写作规范"); await page.getByRole("textbox", { name: "正文", exact: true }).fill("使用简洁、清晰的中文。"); await page.getByRole("button", { name: "保存词条", exact: true }).click();
    await expect(page.locator(".context-library-list button")).toHaveCount(1);
    const old = page.waitForResponse(response => new URL(response.url()).pathname === "/api/v1/contexts"); release(); await (await old).finished();
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
    await expect(page.locator(".context-library-list button")).toHaveCount(1); await expect(page.locator(".context-list-count")).toContainText("1 个词条");
    const content = page.getByRole("textbox", { name: "正文", exact: true }); await content.fill("新版本，保留尚未保存的编辑。");
    page.once("dialog", dialog => dialog.dismiss()); await page.getByRole("button", { name: "新建词条", exact: true }).click(); await expect(content).toHaveValue("新版本，保留尚未保存的编辑。");
    await page.getByRole("button", { name: "保存词条", exact: true }).click(); await expect(page.getByText("版本 2", { exact: true })).toBeVisible();
    await expect(page.locator(".success-banner")).toContainText("词条已保存"); await expect(page.locator(".context-list-error")).toContainText("保存成功后的列表刷新暂时失败");
    await page.locator(".context-list-error").getByRole("button", { name: "重试", exact: true }).click(); await expect(page.locator(".context-list-error")).toHaveCount(0); expect(mutations).toEqual(["create", "edit"]);
    await page.getByRole("button", { name: "删除词条", exact: true }).click(); await page.locator(".context-delete-confirm").getByRole("button", { name: "取消", exact: true }).click(); expect(mutations).toEqual(["create", "edit"]);
    await page.screenshot({ path: info.outputPath("context-saved-entry.png") });
    await page.getByRole("button", { name: "删除词条", exact: true }).click(); await page.getByRole("button", { name: "确认删除", exact: true }).click(); await expect(page.locator(".context-library-list button")).toHaveCount(0);
    expect(mutations).toEqual(["create", "edit", "delete"]); expect(api.unexpected).toEqual([]);
  } finally { release(); await api.close(); }
});

test("context list failure has a read-only retry and does not claim an empty library", async ({ page, context }) => {
  const api = await fixture(context); let fail = true;
  await context.route("**/api/v1/contexts", route => route.fulfill(fail ? { status: 503, json: { error: "测试：上下文服务暂时不可用" } } : { json: { entries: [] } }));
  try {
    await page.goto("/app/contexts"); await expect(page.locator(".context-list-error")).toContainText("测试：上下文服务暂时不可用"); await expect(page.getByText("还没有词条。可以从团队术语、产品介绍或输出格式开始。", { exact: true })).toHaveCount(0);
    fail = false; await page.locator(".context-list-error").getByRole("button", { name: "重试", exact: true }).click(); await expect(page.getByText("还没有词条。可以从团队术语、产品介绍或输出格式开始。", { exact: true })).toBeVisible(); await expect(page.locator(".context-list-error")).toHaveCount(0);
    expect(api.mutations).toEqual([]); expect(api.unexpected).toEqual([]);
  } finally { await api.close(); }
});

test("closing a file cancels its pending auto-open without losing explicit reopen", async ({ page, context }, info) => {
  const api = await fixture(context); let hold = false, waiting = false, release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  await context.route(`**/api/v1/conversations/${conversationId}/files?*`, async route => {
    const files = [...api.files];
    if (hold) { hold = false; waiting = true; await gate; }
    return route.fulfill({ json: { files } });
  });
  try {
    await page.goto(source);
    if (info.project.name === "mobile") await page.getByRole("button", { name: "成果", exact: true }).click();
    else await page.locator(".workspace-panel-button").click();
    const panel = page.locator(".computer-panel"); await panel.locator(".computer-tabs").getByRole("button", { name: "文件", exact: true }).click();
    await panel.getByRole("treeitem", { name: /notes.txt/ }).click(); await expect(panel.locator(".file-source-scroll")).toContainText("请人工验收");
    await expect.poll(() => api.streamCount).toBeGreaterThan(0);
    hold = true; api.writeFile("notes.txt", "这个更新会触发一次延迟的目录同步。"); await expect.poll(() => waiting).toBe(true);
    await panel.getByRole("button", { name: "关闭 notes.txt", exact: true }).click(); await expect(panel.getByRole("tab", { name: "notes.txt", exact: true })).toHaveCount(0);
    const response = page.waitForResponse(value => new URL(value.url()).pathname.endsWith("/files")); release(); await (await response).finished();
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
    await expect(panel.getByRole("tab", { name: "notes.txt", exact: true })).toHaveCount(0);
    await panel.getByRole("button", { name: "重新打开刚关闭的标签", exact: true }).click(); await expect(panel.locator(".file-source-scroll")).toContainText("这个更新会触发一次延迟的目录同步。");
    expect(api.unexpected).toEqual([]); expect(api.mutations).toEqual([]);
  } finally { release(); await api.close(); }
});
