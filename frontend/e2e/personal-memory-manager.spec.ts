import { test as base, expect, type Page, type BrowserContext } from "@playwright/test";
import { WorkspaceApi } from "./workspace-api";
import type { PersonalMemory, PersonalMemoryRevision, PersonalState } from "../src/lib/personal";

const test = base.extend<{ api: WorkspaceApi }>({ api: [async ({ context }, run) => {
  const api = new WorkspaceApi(); await api.install(context);
  try { await run(api); expect(api.unexpected).toEqual([]); } finally { await api.close(); }
}, { auto: true }] });
const memory = (input: Partial<PersonalMemory> = {}): PersonalMemory => ({ id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", project_id: null, kind: "preference", topic: "界面风格", content: "喜欢极简设计", evidence: "", source_turn_id: null, automatic: false, deleted: false, revision: 1, updated_at: "2026-10-30T04:00:00Z", ...input });
async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.locator(".personal-dock-launcher")).toBeVisible();
  await page.locator(".personal-dock-launcher").click();
  await page.getByRole("button", { name: "管理记忆", exact: true }).click();
  return page.getByRole("dialog", { name: "Lester 的记忆", exact: true });
}
async function installMemory(context: BrowserContext, initial: PersonalMemory[] = []) {
  const state: PersonalState = { turns: [], memories: initial, tasks: [], auto_memory: true, has_more: false, inbox: { tasks: [], total: 0, next_cursor: "" } };
  const all = new Map(initial.map(item => [item.id, item]));
  const changes = new Map<string, PersonalMemory | null>();
  const history = new Map<string, PersonalMemoryRevision[]>();
  const fixture = { state, all, history, writes: [] as { method: string; path: string; body: Record<string, unknown> }[], reads: [] as string[], loseNext: false, failRefresh: false, conflictNext: false };
  await context.route("**/api/v1/personal**", async route => {
    const request = route.request(), path = new URL(request.url()).pathname, id = path.split("/").at(path.endsWith("/availability") || path.endsWith("/history") ? -2 : -1)!;
    if (request.method() === "GET") {
      fixture.reads.push(path);
      if (path.endsWith("/history")) {
        const before = Number(new URL(request.url()).searchParams.get("before_revision")) || Infinity;
        const matching = (history.get(id) ?? []).filter(change => change.revision < before);
        return route.fulfill({ json: { memory: all.get(id), changes: matching.slice(0, 30), has_more: matching.length > 30 } });
      }
      if (path.includes("/memories/")) return all.has(id) ? route.fulfill({ json: all.get(id) }) : route.fulfill({ status: 404, json: { error: "记录不存在或无法访问" } });
      if (path.includes("/messages/")) return route.fulfill({ json: state.turns.find(turn => turn.id === id) });
      if (path.endsWith("/tasks")) return route.fulfill({ json: { tasks: [], inbox: state.inbox } });
      if (path.endsWith("/memories") && fixture.failRefresh && fixture.writes.length) { fixture.failRefresh = false; return route.fulfill({ status: 503, json: { error: "fixture refresh failure" } }); }
      if (path.endsWith("/memories")) return route.fulfill({ json: { memories: state.memories, auto_memory: state.auto_memory, user_id: "22222222-2222-4222-8222-222222222222", workspace_id: "fixture-workspace" } });
      return route.fulfill({ json: state });
    }
    const body = request.postDataJSON();
    fixture.writes.push({ method: request.method(), path, body });
    if (path.includes("/changes/")) {
      const change = path.split("/").at(-2)!;
      const prior = changes.get(change);
      const target = [...all.values()].find(item => `change-${item.id}-${item.revision}` === change);
      if (target) {
        const restored = prior ? { ...prior, revision: target.revision + 1 } : { ...target, deleted: true, revision: target.revision + 1, content: "" };
        all.set(target.id, restored); state.memories = [...all.values()].filter(item => !item.deleted);
      }
      for (const turn of state.turns) turn.changes = turn.changes.map(item => item.id === change ? { ...item, undone: true } : item);
      if (fixture.loseNext) { fixture.loseNext = false; return route.abort("failed"); }
      return route.fulfill({ status: 204 });
    }
    if (path.endsWith("/settings")) { state.auto_memory = body.auto_memory; if (fixture.loseNext) { fixture.loseNext = false; return route.abort("failed"); } return route.fulfill({ status: 204 }); }
    expect(path).toMatch(/^\/api\/v1\/personal\/memories\//);
    const prior = all.get(id);
    if (fixture.conflictNext && prior) {
      fixture.conflictNext = false;
      all.set(id, { ...prior, content: "另一设备更新的偏好", revision: prior.revision + 1 });
      state.memories = [...all.values()].filter(item => !item.deleted);
      return route.fulfill({ status: 409, json: { error: "内容已更新，请检查最新状态后重试" } });
    }
    if (request.method() === "PATCH") expect(body.revision).toBe(prior?.revision);
    const next: PersonalMemory = request.method() === "PUT" ? memory({ ...body, id }) : path.endsWith("/availability") ? { ...prior!, paused: body.paused, revision: prior!.revision + 1 } : { ...prior!, ...body.details, content: body.forget ? "" : body.content, deleted: !!body.forget, evidence: "", source_turn_id: null, automatic: false, revision: prior!.revision + 1 };
    all.set(id, next); state.memories = [...all.values()].filter(item => !item.deleted);
    const change = `change-${id}-${next.revision}`; changes.set(change, prior ?? null);
    history.set(id, [{ id: change, memory_id: id, revision: next.revision, before: prior ?? null, after: next, undone: false, turn_id: null, created_at: next.updated_at }, ...(history.get(id) ?? [])]);
    if (fixture.loseNext) { fixture.loseNext = false; return route.abort("failed"); }
    return route.fulfill({ json: { memory: next, change_id: change } });
  });
  return fixture;
}

test("memory can be added without a configured model, and cancelled drafts survive closing", async ({ page, context, api }, info) => {
  api.hasModel = false;
  const fixture = await installMemory(context), errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  const dialog = await login(page);
  await dialog.getByRole("button", { name: "新增记忆", exact: true }).click();
  await dialog.getByRole("textbox", { name: "记忆主题", exact: true }).fill("沟通方式");
  await dialog.getByRole("textbox", { name: "记忆内容", exact: true }).fill("请使用简洁的中文回答");
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  expect(fixture.writes).toHaveLength(0);
  await dialog.getByRole("button", { name: "新增记忆", exact: true }).click();
  await expect(dialog.getByRole("textbox", { name: "记忆主题", exact: true })).toHaveValue("沟通方式");
  await dialog.getByRole("button", { name: "关闭记忆", exact: true }).click();
  await page.getByRole("button", { name: "管理记忆", exact: true }).click();
  await expect(dialog.getByRole("textbox", { name: "记忆内容", exact: true })).toHaveValue("请使用简洁的中文回答");
  const box = await dialog.boundingBox(); expect(box!.x).toBeGreaterThanOrEqual(0); expect(box!.y).toBeGreaterThanOrEqual(0); expect(box!.y + box!.height).toBeLessThanOrEqual(page.viewportSize()!.height + 1);
  await dialog.getByRole("button", { name: "保存", exact: true }).scrollIntoViewIfNeeded();
  await page.screenshot({ path: info.outputPath("personal-memory-create.png") });
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog.locator("article")).toContainText("请使用简洁的中文回答");
  await expect(dialog.getByRole("form", { name: "新增记忆", exact: true })).toHaveCount(0);
  expect(fixture.writes).toHaveLength(1); expect(fixture.writes[0].method).toBe("PUT");
  expect(fixture.writes[0].path).toMatch(/\/[0-9a-f-]{36}$/);
  await dialog.getByText("记忆来源", { exact: false }).click();
  await expect(dialog.getByText("手动添加或编辑", { exact: true })).toBeVisible();
  expect(fixture.writes.some(item => item.path.endsWith("/messages"))).toBe(false);
  expect(errors).toEqual([]);
  await page.screenshot({ path: info.outputPath("personal-memory-manual.png") });
});

test("memory metadata can be edited and personal/project filters remain read-only", async ({ page, context }) => {
  const first = memory(), second = memory({ id: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", project_id: "fixture-project", kind: "project", topic: "发布方式", content: "先做内测" });
  const fixture = await installMemory(context, [first, second]);
  const dialog = await login(page);
  await dialog.getByRole("combobox", { name: "筛选记忆范围", exact: true }).selectOption("fixture-project");
  await expect(dialog.locator("article")).toHaveCount(1);
  await expect(dialog.locator("article")).toContainText("发布方式");
  await dialog.getByRole("combobox", { name: "筛选记忆分类", exact: true }).selectOption("work");
  await expect(dialog.locator("article")).toHaveCount(0);
  expect(fixture.writes).toHaveLength(0);
  await dialog.getByRole("combobox", { name: "筛选记忆分类", exact: true }).selectOption("all");
  await dialog.getByRole("button", { name: "编辑", exact: true }).click();
  await dialog.getByRole("textbox", { name: "记忆主题", exact: true }).fill("近期发布计划");
  await dialog.getByRole("textbox", { name: "编辑记忆内容", exact: true }).fill("本周完成内测后再发布");
  await dialog.getByRole("combobox", { name: "记忆分类", exact: true }).selectOption("work");
  await dialog.getByRole("combobox", { name: "适用范围", exact: true }).selectOption("");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog.locator("article")).toHaveCount(0);
  await dialog.getByRole("combobox", { name: "筛选记忆范围", exact: true }).selectOption("personal");
  await expect(dialog.locator("article")).toHaveCount(2);
  await expect(dialog.locator("article").filter({ hasText: "近期发布计划" })).toContainText("本周完成内测后再发布");
  expect(fixture.writes).toHaveLength(1);
  expect(fixture.writes[0].body.details).toEqual({ topic: "近期发布计划", kind: "work", project_id: null });
});

test("project facts require a project and forgetting can be undone with the returned receipt", async ({ page, context }) => {
  const fixture = await installMemory(context, [memory()]);
  const dialog = await login(page);
  await dialog.getByRole("button", { name: "新增记忆", exact: true }).click();
  await dialog.getByRole("textbox", { name: "记忆主题", exact: true }).fill("技术栈");
  await dialog.getByRole("textbox", { name: "记忆内容", exact: true }).fill("服务端使用 Go");
  await dialog.getByRole("combobox", { name: "记忆分类", exact: true }).selectOption("project");
  await expect(dialog.getByText("项目事实需要选择一个项目。", { exact: true })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "保存", exact: true })).toBeDisabled();
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await dialog.getByRole("button", { name: "忘记", exact: true }).click();
  await expect(dialog.locator("article")).toHaveCount(0);
  expect(fixture.writes).toHaveLength(1);
  await dialog.getByRole("button", { name: "撤销这次记忆修改", exact: true }).click();
  await expect(dialog.locator("article")).toContainText("喜欢极简设计");
  expect(fixture.writes).toHaveLength(2); expect(fixture.writes[1].path).toContain("/changes/");
});

test("a version conflict retains authored edits until loading the latest version explicitly", async ({ page, context }) => {
  const fixture = await installMemory(context, [memory()]); fixture.conflictNext = true;
  const dialog = await login(page);
  await dialog.getByRole("button", { name: "编辑", exact: true }).click();
  const content = dialog.getByRole("textbox", { name: "编辑记忆内容", exact: true });
  await content.fill("我尚未保存的修改");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("内容已更新");
  await expect(content).toHaveValue("我尚未保存的修改");
  await dialog.getByRole("button", { name: "检查状态", exact: true }).click();
  await expect(content).toHaveValue("我尚未保存的修改");
  await dialog.getByRole("button", { name: "载入最新版本", exact: true }).click();
  await expect(content).toHaveValue("另一设备更新的偏好");
  expect(fixture.writes).toHaveLength(1);
  await content.fill("基于最新版本的修改");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog.locator("article")).toContainText("基于最新版本的修改");
  expect(fixture.writes[1].body.revision).toBe(2);
});

test("lost save responses retain identity and only read current state without replay or invented undo", async ({ page, context }) => {
  const fixture = await installMemory(context); fixture.loseNext = true;
  const dialog = await login(page);
  await dialog.getByRole("button", { name: "新增记忆", exact: true }).click();
  await dialog.getByRole("textbox", { name: "记忆主题", exact: true }).fill("回答风格");
  const content = dialog.getByRole("textbox", { name: "记忆内容", exact: true });
  await content.fill("回答时先给结论");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("保存结果待确认");
  await expect(content).toHaveValue("回答时先给结论");
  await expect(dialog.getByRole("button", { name: "保存", exact: true })).toBeDisabled();
  const original = fixture.writes[0].path;
  await dialog.getByRole("button", { name: "检查状态", exact: true }).click();
  await expect(dialog.getByText("当前记忆已与本次内容一致。", { exact: true })).toBeVisible();
  expect(fixture.reads.filter(path => path === original)).toHaveLength(1);
  expect(fixture.writes).toHaveLength(1);
  await expect(dialog.getByRole("button", { name: "撤销这次记忆修改", exact: true })).toHaveCount(0);
  await dialog.getByRole("button", { name: "保留当前记忆", exact: true }).click();
  await expect(dialog.locator("article")).toContainText("回答时先给结论");
  await expect(dialog.getByRole("form", { name: "新增记忆", exact: true })).toHaveCount(0);
  expect(fixture.writes).toHaveLength(1);
});

test("a confirmed save remains saved when refreshing its list fails", async ({ page, context }) => {
  const fixture = await installMemory(context); fixture.failRefresh = true;
  const dialog = await login(page);
  await dialog.getByRole("button", { name: "新增记忆", exact: true }).click();
  await dialog.getByRole("textbox", { name: "记忆主题", exact: true }).fill("显示习惯");
  await dialog.getByRole("textbox", { name: "记忆内容", exact: true }).fill("优先显示成果");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("修改已保存，列表刷新失败");
  await expect(dialog.locator("article")).toContainText("优先显示成果");
  await expect(dialog.getByRole("form", { name: "新增记忆", exact: true })).toHaveCount(0);
  await dialog.getByRole("button", { name: "检查状态", exact: true }).click();
  expect(fixture.writes).toHaveLength(1);
  await expect(dialog.getByRole("alert")).toHaveCount(0);
});

test("undoing a newly created memory removes the confirmed local row", async ({ page, context }) => {
  const fixture = await installMemory(context);
  const dialog = await login(page);
  await dialog.getByRole("button", { name: "新增记忆", exact: true }).click();
  await dialog.getByRole("textbox", { name: "记忆主题", exact: true }).fill("临时想法");
  await dialog.getByRole("textbox", { name: "记忆内容", exact: true }).fill("这条记忆稍后撤销");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog.locator("article")).toContainText("这条记忆稍后撤销");
  await dialog.getByRole("button", { name: "撤销这次记忆修改", exact: true }).click();
  await expect(dialog.locator("article")).toHaveCount(0);
  expect(fixture.writes).toHaveLength(2);
});

test("an unknown request with no visible record can return to the same draft without automatically writing", async ({ page, context }) => {
  const fixture = await installMemory(context);
  await context.route("**/api/v1/personal/memories/*", route => route.request().method() === "PUT" ? route.abort("failed") : route.fulfill({ status: 404, json: { error: "记录不存在或无法访问" } }));
  const dialog = await login(page);
  await dialog.getByRole("button", { name: "新增记忆", exact: true }).click();
  await dialog.getByRole("textbox", { name: "记忆主题", exact: true }).fill("保留草稿");
  const content = dialog.getByRole("textbox", { name: "记忆内容", exact: true });
  await content.fill("未知结果不能把管理器锁住");
  let writes = 0; const ids: string[] = [];
  page.on("request", request => { if (request.method() === "PUT") { writes++; ids.push(new URL(request.url()).pathname); } });
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await dialog.getByRole("button", { name: "检查状态", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("尚未找到保存记录");
  await dialog.getByRole("button", { name: "返回编辑", exact: true }).click();
  await expect(content).toHaveValue("未知结果不能把管理器锁住");
  await expect(dialog.getByRole("button", { name: "保存", exact: true })).toBeEnabled();
  expect(writes).toBe(1); expect(fixture.writes).toHaveLength(0);
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("保存结果待确认");
  expect(writes).toBe(2); expect(ids[1]).toBe(ids[0]);
});

test("settings memory works independently of model and dialogue services, and paused memories remain editable", async ({ page, context }, info) => {
  const fixture = await installMemory(context, [memory()]);
  await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", domain: "127.0.0.1", path: "/" }]);
  await context.route("**/api/v1/model-deployments", route => route.fulfill({ status: 503, json: { error: "fixture model service unavailable" } }));
  await page.goto("/app/settings/memory?returnTo=%2Fapp%2Fp%2Ffixture-project");
  const manager = page.locator(".personal-memory-page");
  await expect(manager.getByRole("heading", { name: "Lester 的记忆", exact: true })).toBeVisible();
  await expect(manager.locator("article")).toContainText("个人范围");
  expect(fixture.reads).toEqual(["/api/v1/personal/memories"]);
  expect(fixture.writes).toHaveLength(0);
  await manager.getByRole("button", { name: "暂停使用", exact: true }).click();
  await expect(manager.getByRole("button", { name: "恢复使用", exact: true })).toBeVisible();
  expect(fixture.writes[0].body).toEqual({ revision: 1, paused: true });
  expect(fixture.reads.filter(path => path.endsWith("/history"))).toHaveLength(0);
  await manager.locator("summary").click();
  await expect(manager.getByText("由你暂停", { exact: false })).toBeVisible();
  expect(fixture.reads.filter(path => path.endsWith("/history"))).toHaveLength(1);
  await manager.getByRole("button", { name: "编辑", exact: true }).click();
  await manager.getByRole("textbox", { name: "编辑记忆内容", exact: true }).fill("暂停时也可以更正内容");
  await manager.getByRole("button", { name: "保存", exact: true }).click();
  await expect(manager.getByRole("button", { name: "恢复使用", exact: true })).toBeVisible();
  await expect(manager.locator(".personal-memory-history")).toContainText("暂停时也可以更正内容");
  await manager.getByRole("combobox", { name: "筛选记忆状态", exact: true }).selectOption("active");
  await expect(manager.locator("article")).toHaveCount(0);
  await manager.getByRole("combobox", { name: "筛选记忆状态", exact: true }).selectOption("paused");
  await expect(manager.locator("article")).toHaveCount(1);
  await manager.locator("summary").click();
  await expect(manager.locator(".personal-memory-version").first()).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)).toBe(false);
  await page.screenshot({ path: info.outputPath("memory-settings-paused-history.png") });
  await manager.getByRole("button", { name: "恢复使用", exact: true }).click();
  await expect(manager.locator("article")).toHaveCount(0);
  expect(fixture.writes).toHaveLength(3);
  await page.getByRole("button", { name: "返回工作区", exact: true }).click();
  await expect(page).toHaveURL(/\/app\/p\/fixture-project$/);
});

test("one memory editor keeps unknown save identity and authored draft across dialog and settings", async ({ page, context }) => {
  const fixture = await installMemory(context); fixture.loseNext = true;
  const dialog = await login(page);
  await dialog.getByRole("button", { name: "新增记忆", exact: true }).click();
  await dialog.getByRole("textbox", { name: "记忆主题", exact: true }).fill("切换入口保留");
  await dialog.getByRole("textbox", { name: "记忆内容", exact: true }).fill("同一份尚待确认的草稿");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("保存结果待确认");
  const original = fixture.writes[0].path;
  await dialog.getByRole("button", { name: "关闭记忆", exact: true }).click();
  await page.locator(".personal-dock").getByRole("button", { name: "收起个人助理", exact: true }).click();
  if (page.viewportSize()!.width < 700) await page.getByRole("button", { name: "打开会话栏", exact: true }).click();
  await page.locator(".user-menu-trigger").click();
  await page.getByRole("menuitem", { name: "记忆", exact: true }).click();
  const manager = page.locator(".personal-memory-page");
  await expect(manager.getByRole("textbox", { name: "记忆主题", exact: true })).toHaveValue("切换入口保留");
  await expect(manager.getByRole("textbox", { name: "记忆内容", exact: true })).toHaveValue("同一份尚待确认的草稿");
  await expect(manager.getByRole("button", { name: "保存", exact: true })).toBeDisabled();
  expect(fixture.writes).toHaveLength(1);
  await manager.getByRole("button", { name: "检查状态", exact: true }).click();
  expect(fixture.reads.filter(path => path === original)).toHaveLength(1);
  await expect(manager.getByRole("button", { name: "撤销这次记忆修改", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "返回工作区", exact: true }).click();
  await page.locator(".personal-dock-launcher").click();
  await page.getByRole("button", { name: "管理记忆", exact: true }).click();
  await expect(dialog.getByRole("textbox", { name: "记忆内容", exact: true })).toHaveValue("同一份尚待确认的草稿");
  await dialog.getByRole("button", { name: "保留当前记忆", exact: true }).click();
  await expect(dialog.getByRole("form", { name: "新增记忆", exact: true })).toHaveCount(0);
  expect(fixture.writes).toHaveLength(1);
});

test("unknown pause responses only read the original memory and never invent an undo receipt", async ({ page, context }) => {
  const fixture = await installMemory(context, [memory()]); fixture.loseNext = true;
  const dialog = await login(page);
  await dialog.getByRole("button", { name: "暂停使用", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("保存结果待确认");
  await dialog.getByRole("button", { name: "检查状态", exact: true }).click();
  await expect(dialog.getByText("当前记忆已与本次内容一致。", { exact: true })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "撤销这次记忆修改", exact: true })).toHaveCount(0);
  await dialog.getByRole("button", { name: "保留当前记忆", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "恢复使用", exact: true })).toBeEnabled();
  expect(fixture.writes).toHaveLength(1);
  expect(fixture.reads.filter(path => path.endsWith(memory().id))).toHaveLength(1);
});

test("unknown settings and manager undo responses reconcile through reads without replaying writes", async ({ page, context }) => {
  const fixture = await installMemory(context);
  const dialog = await login(page), automatic = dialog.getByRole("checkbox", { name: /自动记住重要信息/ });
  fixture.loseNext = true;
  await automatic.uncheck();
  await expect(dialog.getByRole("alert")).toContainText("操作结果未确认");
  await expect(automatic).toBeDisabled();
  await dialog.getByRole("button", { name: "检查状态", exact: true }).click();
  await expect(automatic).toBeEnabled(); await expect(automatic).not.toBeChecked();
  expect(fixture.writes).toHaveLength(1);
  await dialog.getByRole("button", { name: "新增记忆", exact: true }).click();
  await dialog.getByRole("textbox", { name: "记忆主题", exact: true }).fill("撤销回执");
  await dialog.getByRole("textbox", { name: "记忆内容", exact: true }).fill("未知撤销只检查当前状态");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  fixture.loseNext = true;
  await dialog.getByRole("button", { name: "撤销这次记忆修改", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("操作结果未确认");
  await expect(dialog.getByRole("button", { name: "撤销这次记忆修改", exact: true })).toHaveCount(0);
  await expect(dialog.getByRole("button", { name: "新增记忆", exact: true })).toBeDisabled();
  await dialog.getByRole("button", { name: "检查状态", exact: true }).click();
  await expect(dialog.locator("article")).toHaveCount(0);
  await expect(dialog.getByRole("button", { name: "新增记忆", exact: true })).toBeEnabled();
  expect(fixture.writes).toHaveLength(3);
});

test("an unknown chat undo only inspects the original turn receipt", async ({ page, context }) => {
  const fixture = await installMemory(context);
  fixture.state.turns.push({ id: "undo-turn", seq: 1, content: "记住界面偏好", reply: "已记下。", status: "completed", created_at: memory().updated_at, changes: [{ id: "original-change", memory_id: memory().id, topic: memory().topic, deleted: false, automatic: false, undone: false }] });
  const dialog = await login(page);
  await dialog.getByRole("button", { name: "关闭记忆", exact: true }).click();
  fixture.loseNext = true;
  const receipt = page.locator(".personal-memory-receipt");
  await receipt.getByRole("button", { name: "撤销记忆 界面风格", exact: true }).click();
  await expect(receipt.getByRole("alert")).toContainText("操作结果未确认");
  await expect(receipt.getByRole("button", { name: "撤销记忆 界面风格", exact: true })).toHaveCount(0);
  await receipt.getByRole("button", { name: "检查状态", exact: true }).click();
  await expect(receipt).toContainText("已撤销记忆更新");
  expect(fixture.writes).toHaveLength(1);
  expect(fixture.reads.filter(path => path === "/api/v1/personal/messages/undo-turn")).toHaveLength(1);
});

test("memory history loads only on disclosure and appends older revisions without writes", async ({ page, context }) => {
  const current = memory({ revision: 31 }), fixture = await installMemory(context, [current]);
  fixture.history.set(current.id, Array.from({ length: 31 }, (_, index) => {
    const revision = 31 - index, after = { ...current, revision, content: `保存内容 ${revision}` };
    return { id: `history-${revision}`, memory_id: current.id, revision, before: revision === 1 ? null : { ...after, revision: revision - 1, content: `保存内容 ${revision - 1}` }, after, undone: false, turn_id: null, created_at: current.updated_at };
  }));
  const dialog = await login(page);
  expect(fixture.reads.some(path => path.endsWith("/history"))).toBe(false);
  await dialog.locator("summary").click();
  await expect(dialog.locator(".personal-memory-version")).toHaveCount(30);
  await expect(dialog.locator(".personal-memory-version").first()).toContainText("版本 31");
  await dialog.getByRole("button", { name: "加载更多历史", exact: true }).click();
  await expect(dialog.locator(".personal-memory-version")).toHaveCount(31);
  await expect(dialog.locator(".personal-memory-version").last()).toContainText("版本 1");
  await expect(dialog.getByRole("button", { name: "加载更多历史", exact: true })).toHaveCount(0);
  expect(fixture.writes).toHaveLength(0);
});

test("arriving history preserves editing focus and the same draft survives navigation and a locale switch", async ({ page, context }) => {
  const current = memory(), fixture = await installMemory(context, [current]);
  let release!: () => void, requested!: () => void;
  const historyReady = new Promise<void>(resolve => { requested = resolve; }), historyGate = new Promise<void>(resolve => { release = resolve; });
  await context.route(`**/api/v1/personal/memories/${current.id}/history`, async route => { requested(); await historyGate; return route.fulfill({ json: { memory: current, changes: [{ id: "history-focus", memory_id: current.id, revision: 1, before: null, after: current, undone: false, turn_id: null, created_at: current.updated_at }], has_more: false } }); });
  try {
    const dialog = await login(page);
    await dialog.locator("summary").click(); await historyReady;
    await dialog.getByRole("button", { name: "编辑", exact: true }).click();
    const content = dialog.getByRole("textbox", { name: "编辑记忆内容", exact: true });
    await content.fill("历史到达时继续写作，切换语言也保留原文"); release();
    await expect(dialog.locator(".personal-memory-version")).toHaveCount(1);
    await expect(content).toBeFocused();
    await dialog.getByRole("button", { name: "关闭记忆", exact: true }).click();
    await page.locator(".personal-dock").getByRole("button", { name: "收起个人助理", exact: true }).click();
    if (page.viewportSize()!.width < 700) await page.getByRole("button", { name: "打开会话栏", exact: true }).click();
    await page.locator(".user-menu-trigger").click(); await page.getByRole("menuitem", { name: "记忆", exact: true }).click();
    const manager = page.locator(".personal-memory-page");
    await expect(manager.locator("textarea")).toHaveValue("历史到达时继续写作，切换语言也保留原文");
    await page.locator(".settings-sidebar .language-trigger").click();
    await page.getByRole("menuitemradio", { name: "English", exact: true }).click();
    await expect(manager.getByRole("heading", { name: "Lester’s memory", exact: true })).toBeVisible();
    await expect(manager.locator("textarea")).toHaveValue("历史到达时继续写作，切换语言也保留原文");
    expect(fixture.writes).toHaveLength(0);
  } finally { release(); }
});

test("an actor-mismatched memory collection is rejected and can be retried without exposing its facts", async ({ page, context }) => {
  await installMemory(context);
  await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", domain: "127.0.0.1", path: "/" }]);
  let reads = 0;
  await context.route("**/api/v1/personal/memories", route => { reads++; return route.fulfill({ json: { user_id: reads === 1 ? "another-account" : "22222222-2222-4222-8222-222222222222", workspace_id: "fixture-workspace", memories: [memory({ content: "仅正确账号才能看到" })], auto_memory: true } }); });
  await page.goto("/app/settings/memory");
  const manager = page.locator(".personal-memory-page");
  await expect(manager.getByRole("alert")).toContainText("记忆加载失败，请重试。");
  await expect(manager.getByText("仅正确账号才能看到", { exact: true })).toHaveCount(0);
  await manager.getByRole("button", { name: "重试", exact: true }).click();
  await expect(manager.locator("article")).toContainText("仅正确账号才能看到");
  await page.evaluate(() => window.dispatchEvent(new Event("lester:session-cleared")));
  await expect(manager.locator("article")).toHaveCount(0);
});

test("reply memory references expose only the supplied topic, scope and version on demand", async ({ page, context }) => {
  const fixture = await installMemory(context);
  fixture.state.turns.push({ id: "reference-turn", seq: 1, content: "需要简洁的回答", reply: "这是使用固定资料的模拟回复。", status: "completed", created_at: "2026-11-01T00:00:00Z", changes: [], memory_context: [{ id: memory().id, revision: 4, topic: "沟通习惯", project_id: null }, { id: "project-fact", revision: 2, topic: "上线计划", project_id: "fixture-project" }] });
  const dialog = await login(page);
  await dialog.getByRole("button", { name: "关闭记忆", exact: true }).click();
  const receipt = page.locator(".personal-memory-context");
  await expect(receipt.locator("summary")).toHaveText("本次参考 2 条记忆");
  await expect(receipt.getByText("沟通习惯", { exact: true })).not.toBeVisible();
  await receipt.locator("summary").click();
  await expect(receipt).toContainText("沟通习惯"); await expect(receipt).toContainText("版本 4");
  await expect(receipt).toContainText("个人范围"); await expect(receipt).toContainText("个人项目");
  expect(fixture.writes).toHaveLength(0);
});
