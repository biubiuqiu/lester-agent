import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId } from "./workspace-api";
import type { PersonalState, PersonalTurn } from "../src/lib/personal";

const test = base.extend<{ api: WorkspaceApi }>({ api: async ({ context }, run) => {
  const api = new WorkspaceApi(); await api.install(context);
  try { await run(api); expect(api.unexpected).toEqual([]); } finally { await api.close(); }
} });
async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "个人助理消息", exact: true })).toBeVisible();
}
function empty(): PersonalState { return { turns: [], has_more: false, memories: [], tasks: [], auto_memory: true }; }

test("persistent assistant remembers, corrects, forgets and undoes without creating a task", async ({ page, context, api }, info) => {
  const state = empty(), mutations: string[] = [], errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await context.route("**/api/v1/personal**", async route => {
    const request = route.request(), path = new URL(request.url()).pathname;
    if (request.method() === "GET") {
      if (path === "/api/v1/personal/tasks") return route.fulfill({ json: { tasks: state.tasks } });
      if (path.includes("/messages/")) return route.fulfill({ json: state.turns.find(turn => turn.id === path.split("/").at(-1)) });
      return route.fulfill({ json: state });
    }
    mutations.push(path);
    const body = request.postDataJSON();
    if (path === "/api/v1/personal/messages") {
      const now = new Date().toISOString();
      const turn: PersonalTurn = { id: body.id, seq: state.turns.length + 1, content: body.content, reply: "我会记住你的设计偏好。", status: "completed", created_at: now, changes: [{ id: "change-1", memory_id: "memory-1", topic: "设计风格", automatic: false, deleted: false, undone: false }] };
      state.turns.push(turn);
      state.memories = [{ id: "memory-1", project_id: null, topic: "设计风格", kind: "preference", content: "喜欢极简设计", evidence: body.content, source_turn_id: turn.id, automatic: false, deleted: false, revision: 1, updated_at: now }];
      return route.fulfill({ json: turn });
    }
    if (path === "/api/v1/personal/memories/memory-1") {
      state.memories = body.forget ? [] : [{ ...state.memories[0], content: body.content, revision: 2 }];
      return route.fulfill({ json: { memory: state.memories[0] ?? null, change_id: "manual-change" } });
    }
    if (path.includes("/changes/")) { state.memories = []; state.turns[0].changes[0].undone = true; return route.fulfill({ status: 204 }); }
    if (path.endsWith("/settings")) { state.auto_memory = body.auto_memory; return route.fulfill({ status: 204 }); }
    throw new Error(`Unexpected personal mutation: ${path}`);
  });
  await login(page);
  await expect(page.getByRole("heading", { name: "随时接着聊。" })).toBeVisible();
  await expect(page.locator(".personal-assistant .permission-mode, .personal-assistant .context-input, .computer-panel")).toHaveCount(0);
  const input = page.getByRole("textbox", { name: "个人助理消息", exact: true });
  await input.fill("记住我喜欢极简设计"); await input.press("Enter");
  await expect(page.getByText("已记住：设计风格", { exact: true })).toBeVisible();
  await expect(page).toHaveURL(/\/app$/);
  await expect(input).toHaveValue("");
  await page.reload();
  await expect(page.getByText("我会记住你的设计偏好。", { exact: true })).toBeVisible();
  expect(mutations.filter(path => path.endsWith("/messages"))).toHaveLength(1);
  await page.getByRole("button", { name: "管理记忆", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Lester 的记忆", exact: true });
  await expect(dialog).toBeVisible();
  await dialog.getByText("记忆来源", { exact: false }).click();
  await expect(dialog.getByText("记住我喜欢极简设计", { exact: true })).toBeVisible();
  await page.screenshot({ path: info.outputPath("personal-memory-source.png") });
  await dialog.getByRole("button", { name: "编辑", exact: true }).click();
  await dialog.getByRole("textbox", { name: "编辑记忆内容", exact: true }).fill("喜欢温暖的极简设计");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog.getByText("喜欢温暖的极简设计", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "忘记", exact: true }).click();
  await expect(dialog.getByText("还没有记忆。聊天时告诉我“记住这个”就好。", { exact: true })).toBeVisible();
  await dialog.getByRole("checkbox", { name: /自动记住重要信息/ }).uncheck();
  await expect(dialog.getByRole("checkbox")).not.toBeChecked();
  await page.screenshot({ path: info.outputPath("personal-memory.png") });
  await dialog.getByRole("button", { name: "关闭记忆", exact: true }).click();
  await page.getByRole("button", { name: "撤销记忆 设计风格", exact: true }).click();
  await expect(page.getByText("已撤销记忆更新", { exact: true })).toBeVisible();
  expect(api.mutations.filter(item => item.path.startsWith("/api/v1/conversations"))).toEqual([]);
  expect(errors).toEqual([]);
  await page.screenshot({ path: info.outputPath("personal-dialogue.png") });
});

test("a lost reply is recovered by its original receipt and task cards show project and queue", async ({ page, context, api }, info) => {
  const state = empty(); let posts = 0;
  await context.route("**/api/v1/personal**", async route => {
    const request = route.request(), path = new URL(request.url()).pathname;
    if (request.method() === "GET") {
      if (path.endsWith("/tasks")) return route.fulfill({ json: { tasks: state.tasks } });
      if (path.includes("/messages/")) return route.fulfill({ json: state.turns[0] });
      return route.fulfill({ json: state });
    }
    if (path === "/api/v1/personal/messages") {
      posts++; const body = request.postDataJSON(), now = new Date().toISOString();
      state.turns.push({ id: body.id, seq: 1, content: body.content, reply: "已经安排继续处理。", status: "completed", created_at: now, changes: [], task: { conversation_id: conversationId, project_id: "fixture-project", title: "Lester 介绍页", status: "queued", run_id: null, input_id: "fixture-input" } });
      state.tasks = [{ id: conversationId, project_id: "fixture-project", project_name: "个人项目", title: "Lester 介绍页", status: "running", run_id: "fixture-run", reply: "", updated_at: now }];
      return route.abort("failed");
    }
    throw new Error(`Unexpected personal mutation ${path}`);
  });
  await login(page);
  const input = page.getByRole("textbox", { name: "个人助理消息", exact: true });
  await input.fill("让之前的介绍页继续优化排版"); await input.press("Enter");
  const card = page.locator(".personal-task-card");
  await expect(card).toContainText("个人项目 · 已加入会话队列");
  await expect(card.getByRole("link", { name: "打开任务 Lester 介绍页", exact: true })).toHaveAttribute("href", `/app/c/${conversationId}`);
  await page.reload();
  await expect(card).toBeVisible(); expect(posts).toBe(1);
  await input.fill("还没发送的补充");
  const newPage = await context.newPage(); await newPage.goto("/app");
  await expect(newPage.getByRole("textbox", { name: "个人助理消息", exact: true })).toHaveValue("还没发送的补充");
  expect(posts).toBe(1);
  await page.screenshot({ path: info.outputPath("personal-task-queue.png") });
  await newPage.close();
  expect(api.mutations.some(item => item.path.includes("/permissions") || item.path.includes("/inputs"))).toBe(false);
});

test("assistant loading failure retries reads and the memory dialog fits a short screen", async ({ page, context, api }, info) => {
  let reads = 0;
  await context.route("**/api/v1/personal", route => { reads++; return reads === 1 ? route.fulfill({ status: 503, json: { error: "个人助理加载失败，请稍后检查状态" } }) : route.fulfill({ json: empty() }); });
  await page.setViewportSize(info.project.name === "mobile" ? { width: 393, height: 560 } : { width: 1280, height: 650 });
  await login(page);
  await expect(page.locator(".personal-error[role=alert]")).toContainText("个人助理加载失败");
  await page.getByRole("button", { name: "检查状态", exact: true }).click();
  await expect(page.getByRole("heading", { name: "随时接着聊。" })).toBeVisible();
  await page.getByRole("button", { name: "管理记忆", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Lester 的记忆", exact: true });
  await expect(dialog).toBeVisible();
  const box = await dialog.boundingBox(); expect(box!.y).toBeGreaterThanOrEqual(0); expect(box!.y + box!.height).toBeLessThanOrEqual(page.viewportSize()!.height + 1);
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole("button", { name: "管理记忆", exact: true })).toBeFocused();
  const composer = await page.locator(".personal-composer").boundingBox(); expect(composer!.y + composer!.height).toBeLessThanOrEqual(page.viewportSize()!.height + 1);
  await page.screenshot({ path: info.outputPath("personal-short-screen.png") });
  expect(api.unexpected).toEqual([]);
});

test("assistant and full memory help render in six languages without translating authored text", async ({ page, context, api }) => {
  test.setTimeout(90_000);
  const state = empty();
  state.turns = [{ id: "language-turn", seq: 1, content: "保留用户原文：我喜欢极简设计", reply: "这段回复也保持原样。", status: "completed", changes: [], created_at: new Date().toISOString() }];
  await context.route("**/api/v1/personal", route => route.fulfill({ json: state }));
  await login(page);
  const languages = [["en", "Lester’s memory"], ["ja", "Lester のメモリ"], ["ko", "Lester의 기억"], ["fr", "La mémoire de Lester"], ["es", "La memoria de Lester"], ["zh-CN", "Lester 的记忆"]];
  for (const [locale, heading] of languages) {
    await context.addCookies([{ name: "lester_locale", value: locale, url: "http://127.0.0.1:13020" }]);
    await page.goto("/app");
    await expect(page.getByText("保留用户原文：我喜欢极简设计", { exact: true })).toBeVisible();
    await page.locator(".personal-toolbar button").click();
    await expect(page.getByRole("heading", { name: heading, exact: true })).toBeVisible();
    await page.keyboard.press("Escape");
    await page.goto("/docs/usage#personal-assistant");
    await expect(page.locator("#personal-assistant")).toBeVisible();
    expect((await page.locator("#personal-assistant").innerText()).length).toBeGreaterThan(5);
  }
  expect(api.unexpected).toEqual([]);
});

test("full memory stays explicit and uncertain delegation only inspects its original receipt", async ({ page, context, api }, info) => {
  const state = empty(); let receiptReads = 0;
  state.turns = [{ id: "original-personal-turn", seq: 1, content: "继续处理介绍页，并记住设计偏好", reply: "这项工作会交给对应任务会话。", memory_notice: "记忆空间已满，部分信息未保存。可忘记不再需要的记忆后继续。", status: "completed", changes: [], created_at: new Date().toISOString(), task: { conversation_id: conversationId, project_id: "fixture-project", title: "介绍页", status: "unknown", run_id: null, input_id: null } }];
  await context.route("**/api/v1/personal**", route => {
    expect(route.request().method()).toBe("GET");
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/tasks")) return route.fulfill({ json: { tasks: state.tasks } });
    if (path.includes("/messages/")) { receiptReads++; state.turns[0].task!.status = "queued"; state.turns[0].task!.input_id = "original-input"; return route.fulfill({ json: state.turns[0] }); }
    return route.fulfill({ json: state });
  });
  await login(page);
  await expect(page.getByText("记忆空间已满，部分信息未保存。可忘记不再需要的记忆后继续。", { exact: true })).toBeVisible();
  const card = page.locator(".personal-task-card");
  await expect(card).toContainText("委派结果待确认");
  await expect(card.getByRole("link")).toHaveCount(0);
  await card.getByRole("button", { name: "检查状态", exact: true }).click();
  await expect(card).toContainText("已加入会话队列");
  await expect(card.getByRole("link", { name: "打开任务 介绍页", exact: true })).toBeVisible();
  expect(receiptReads).toBe(1);
  state.tasks = [{ id: conversationId, project_id: "fixture-project", project_name: "个人项目", title: "介绍页", status: "awaiting_input", run_id: "run-waiting", reply: "", updated_at: new Date().toISOString() }];
  await page.reload();
  await expect(card).toContainText("等待你确认");
  expect(api.mutations.filter(item => item.path.startsWith("/api/v1/conversations"))).toEqual([]);
  await page.screenshot({ path: info.outputPath("personal-original-receipt.png") });
});

test("removed saved models and projects fall back to visible available choices", async ({ page, context, api }) => {
  await context.addInitScript(() => localStorage.setItem("lester.draft.v1.22222222-2222-4222-8222-222222222222.fixture-workspace.personal", JSON.stringify({ version: 1, text: "这是保留的草稿", personalModel: "removed-model", personalProject: "removed-project" })));
  let sends = 0;
  await context.route("**/api/v1/personal/messages", route => {
    sends++; const body = route.request().postDataJSON();
    expect(body).toMatchObject({ model_id: "fixture-model", project_id: null, content: "这是保留的草稿" });
    return route.fulfill({ json: { id: body.id, seq: 1, content: body.content, reply: "可以继续聊。", status: "completed", changes: [], created_at: new Date().toISOString() } });
  });
  await login(page);
  await expect(page.getByRole("combobox", { name: "个人助理模型", exact: true })).toHaveValue("fixture-model");
  await expect(page.getByRole("combobox", { name: "对话项目范围", exact: true })).toHaveValue("");
  await page.getByRole("button", { name: "发送给个人助理", exact: true }).click();
  await expect(page.getByText("可以继续聊。", { exact: true })).toBeVisible();
  expect(sends).toBe(1);
  expect(api.mutations.filter(item => item.path.startsWith("/api/v1/conversations"))).toEqual([]);
});

test("new-user teaching explains personal memory and delegation without starting work", async ({ page, context, api }) => {
  await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", url: "http://127.0.0.1:13020" }]);
  await context.route("**/api/v1/me/guides", route => route.fulfill({ json: { guides: [] } }));
  await page.goto("/app");
  const tour = page.getByRole("dialog");
  await expect(tour.getByRole("heading", { name: "欢迎，把一个想法交给 Lester", exact: true })).toBeVisible();
  await expect(tour).toContainText("持续陪伴你的个人助理");
  await tour.getByRole("button", { name: "开始了解", exact: true }).click();
  await tour.getByRole("button", { name: "下一步", exact: true }).click();
  await expect(tour).toContainText("Lester 会找到对应任务或创建独立会话");
  await tour.getByRole("button", { name: "下一步", exact: true }).click();
  await expect(tour.getByRole("heading", { name: "把重要的事交给它记住", exact: true })).toBeVisible();
  await expect(tour).toContainText("编辑、忘记和撤销");
  await tour.getByRole("button", { name: "稍后再学", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "个人助理消息", exact: true })).toBeVisible();
  expect(api.mutations.every(item => item.path.startsWith("/api/v1/me/guides/"))).toBe(true);
});
