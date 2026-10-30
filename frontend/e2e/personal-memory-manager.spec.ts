import { test as base, expect, type Page, type BrowserContext } from "@playwright/test";
import { WorkspaceApi } from "./workspace-api";
import type { PersonalMemory, PersonalState } from "../src/lib/personal";

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
  const fixture = { state, all, writes: [] as { method: string; path: string; body: Record<string, unknown> }[], reads: [] as string[], loseNext: false, failRefresh: false, conflictNext: false };
  await context.route("**/api/v1/personal**", async route => {
    const request = route.request(), path = new URL(request.url()).pathname, id = path.split("/").at(-1)!;
    if (request.method() === "GET") {
      fixture.reads.push(path);
      if (path.includes("/memories/")) return all.has(id) ? route.fulfill({ json: all.get(id) }) : route.fulfill({ status: 404, json: { error: "记录不存在或无法访问" } });
      if (path.endsWith("/tasks")) return route.fulfill({ json: { tasks: [], inbox: state.inbox } });
      if (fixture.failRefresh && fixture.writes.length) { fixture.failRefresh = false; return route.fulfill({ status: 503, json: { error: "fixture refresh failure" } }); }
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
      return route.fulfill({ status: 204 });
    }
    if (path.endsWith("/settings")) { state.auto_memory = body.auto_memory; return route.fulfill({ status: 204 }); }
    expect(path).toMatch(/^\/api\/v1\/personal\/memories\//);
    const prior = all.get(id);
    if (fixture.conflictNext && prior) {
      fixture.conflictNext = false;
      all.set(id, { ...prior, content: "另一设备更新的偏好", revision: prior.revision + 1 });
      state.memories = [...all.values()].filter(item => !item.deleted);
      return route.fulfill({ status: 409, json: { error: "内容已更新，请检查最新状态后重试" } });
    }
    if (request.method() === "PATCH") expect(body.revision).toBe(prior?.revision);
    const next: PersonalMemory = request.method() === "PUT" ? memory({ ...body, id }) : { ...prior!, ...body.details, content: body.forget ? "" : body.content, deleted: !!body.forget, evidence: "", source_turn_id: null, automatic: false, revision: prior!.revision + 1 };
    all.set(id, next); state.memories = [...all.values()].filter(item => !item.deleted);
    const change = `change-${id}-${next.revision}`; changes.set(change, prior ?? null);
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
