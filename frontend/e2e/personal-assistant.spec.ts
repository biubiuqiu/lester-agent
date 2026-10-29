import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId } from "./workspace-api";
import type { PersonalState, PersonalTask, PersonalTurn } from "../src/lib/personal";

const test = base.extend<{ api: WorkspaceApi }>({ api: async ({ context }, run) => {
  const api = new WorkspaceApi(); await api.install(context);
  try { await run(api); expect(api.unexpected).toEqual([]); } finally { await api.close(); }
} });
async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "个人助理消息", exact: true })).toHaveCount(0);
  await openAssistant(page);
}
async function openAssistant(page: Page) {
  await page.locator(".personal-dock-launcher").click();
  await expect(page.getByRole("textbox", { name: "个人助理消息", exact: true })).toBeVisible();
}
function empty(): PersonalState { return { turns: [], has_more: false, memories: [], tasks: [], inbox: { tasks: [], total: 0, next_cursor: "" }, auto_memory: true }; }
function unreadTask(id = conversationId, title = "介绍页有了新回复"): PersonalTask {
  const now = new Date().toISOString();
  return { id, title, project_id: "fixture-project", project_name: "产品项目", status: "completed", run_id: "fixture-run", reply: "readme 已更新，可以检查这份成果。", updated_at: now, latest_reply_id: "33333333-3333-4333-8333-333333333333", latest_reply_seq: 2, reply_at: now, unread: true };
}

test("assistant examples preserve the current draft and only an explicit send delegates a new task", async ({ page, context, api }) => {
  const state = empty(); let sends = 0;
  await context.route("**/api/v1/personal**", route => {
    const path = new URL(route.request().url()).pathname;
    if (route.request().method() === "GET") return route.fulfill({ json: path.endsWith("/tasks") ? { tasks: state.tasks, inbox: state.inbox } : state });
    expect(path).toBe("/api/v1/personal/messages"); sends++;
    const body = route.request().postDataJSON();
    expect(body.content).toContain("先保留这个想法");
    expect(body.content).toContain("帮我新建一个任务会话，完成：");
    const turn: PersonalTurn = { id: body.id, seq: 1, content: body.content, reply: "已交给新的任务会话。", status: "completed", created_at: new Date().toISOString(), changes: [], task: { conversation_id: conversationId, project_id: "fixture-project", title: "新建的介绍页任务", status: "started", run_id: "fixture-run", input_id: null } };
    state.turns.push(turn); return route.fulfill({ json: turn });
  });
  await login(page);
  const initialMutations = [...api.mutations];
  const input = page.getByRole("textbox", { name: "个人助理消息", exact: true });
  await input.fill("先保留这个想法");
  await page.getByRole("button", { name: "安排新任务", exact: true }).click();
  await expect(input).toHaveValue("先保留这个想法\n\n帮我新建一个任务会话，完成：");
  await page.getByRole("button", { name: "查看未读回复", exact: true }).click();
  await expect(input).toHaveValue("先保留这个想法\n\n帮我新建一个任务会话，完成：\n\n帮我看看哪些会话有新回复，但我还没看。");
  expect(sends).toBe(0); expect(api.mutations).toEqual(initialMutations);
  await input.press("Enter");
  await expect(page.getByRole("link", { name: "打开任务 新建的介绍页任务", exact: true })).toHaveAttribute("href", `/app/c/${conversationId}`);
  await expect(input).toHaveValue(""); expect(sends).toBe(1);
});

test("unread reply lists keep project labels and previews read-only, with recoverable pagination", async ({ page, context, api }, info) => {
  const state = empty(), first = unreadTask(), second = unreadTask("44444444-4444-4444-8444-444444444444", "另一项目的测试结果");
  second.project_name = "基础服务";
  state.inbox = { tasks: [first], total: 2, next_cursor: "page-two/+" };
  let pageReads = 0;
  await context.route("**/api/v1/personal**", route => {
    expect(route.request().method()).toBe("GET");
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/unread")) {
      pageReads++; expect(url.searchParams.get("cursor")).toBe("page-two/+");
      return pageReads === 1 ? route.fulfill({ status: 503, json: { error: "fixture read failure" } }) : route.fulfill({ json: { tasks: [second], total: 2, next_cursor: "" } });
    }
    return route.fulfill({ json: url.pathname.endsWith("/tasks") ? { tasks: state.tasks, inbox: state.inbox } : state });
  });
  await login(page);
  const initialMutations = [...api.mutations];
  const inbox = page.locator(".personal-inbox");
  await expect(inbox.getByRole("button", { name: "未读回复 2", exact: true })).toHaveAttribute("aria-expanded", "false");
  await inbox.getByRole("button", { name: "未读回复 2", exact: true }).click();
  await expect(inbox.getByRole("link", { name: "打开会话 介绍页有了新回复", exact: true })).toHaveAttribute("href", `/app/c/${conversationId}`);
  await expect(inbox).toContainText("产品项目");
  await inbox.getByText("预览回复", { exact: true }).click();
  await expect(inbox.getByText(first.reply, { exact: true })).toBeVisible();
  expect(api.readAcks).toEqual([]); expect(api.mutations).toEqual(initialMutations);
  await inbox.getByRole("button", { name: "加载更多回复", exact: true }).click();
  await expect(inbox.getByRole("alert")).toContainText("未读回复加载失败，请重试。");
  await expect(inbox).not.toContainText("暂无未读回复");
  await expect(inbox.getByRole("link", { name: "打开会话 介绍页有了新回复", exact: true })).toBeVisible();
  await inbox.getByRole("button", { name: "重试", exact: true }).click();
  await expect(inbox.getByRole("link", { name: "打开会话 另一项目的测试结果", exact: true })).toBeVisible();
  await expect(inbox).toContainText("基础服务"); await expect(inbox.getByRole("alert")).toHaveCount(0);
  expect(pageReads).toBe(2); expect(api.readAcks).toEqual([]); expect(api.mutations).toEqual(initialMutations);
  await page.screenshot({ path: info.outputPath("personal-unread-replies.png") });
});

test("confirmed matching read receipts refresh the inbox without erasing drafts or opening the list", async ({ page, context, api }) => {
  const state = empty(); state.inbox = { tasks: [unreadTask()], total: 1, next_cursor: "" }; let reads = 0;
  await context.route("**/api/v1/personal**", route => {
    expect(route.request().method()).toBe("GET");
    if (new URL(route.request().url()).pathname.endsWith("/tasks")) { reads++; return route.fulfill({ json: { tasks: [], inbox: state.inbox } }); }
    return route.fulfill({ json: state });
  });
  await login(page);
  const initialMutations = [...api.mutations];
  const input = page.getByRole("textbox", { name: "个人助理消息", exact: true }), toggle = page.locator(".personal-inbox-toggle");
  await input.fill("正在想的下一件事");
  await page.evaluate(() => window.dispatchEvent(new CustomEvent("lester:conversation-read", { detail: { userId: "other-user", workspaceId: "fixture-workspace", conversationId: "any", lastReadSeq: 2 } })));
  await expect(toggle).toHaveText("未读回复1");
  state.inbox = { tasks: [], total: 0, next_cursor: "" };
  await page.evaluate(() => window.dispatchEvent(new CustomEvent("lester:conversation-read", { detail: { userId: "22222222-2222-4222-8222-222222222222", workspaceId: "fixture-workspace", conversationId: "11111111-1111-4111-8111-111111111111", lastReadSeq: 2 } })));
  await expect(toggle).toHaveText("未读回复0");
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
  await expect(input).toHaveValue("正在想的下一件事"); expect(reads).toBe(1);
  await toggle.click(); await expect(page.locator(".personal-inbox")).toContainText("暂无未读回复");
  expect(api.mutations).toEqual(initialMutations);
});

test("a newer inbox read wins over a delayed initial assistant state", async ({ page, context, api }) => {
  const old = empty(); old.inbox = { tasks: [unreadTask()], total: 1, next_cursor: "" };
  let release!: () => void, started!: () => void, taskReads = 0;
  const held = new Promise<void>(resolve => { release = resolve; }), initialStarted = new Promise<void>(resolve => { started = resolve; });
  await context.route("**/api/v1/personal**", async route => {
    if (new URL(route.request().url()).pathname.endsWith("/tasks")) { taskReads++; return route.fulfill({ json: { tasks: [], inbox: { tasks: [], total: 0, next_cursor: "" } } }); }
    started(); await held; return route.fulfill({ json: old });
  });
  try {
    await login(page); await initialStarted;
    const initialMutations = [...api.mutations];
    const taskResponse = page.waitForResponse(response => new URL(response.url()).pathname === "/api/v1/personal/tasks");
    await page.evaluate(() => window.dispatchEvent(new CustomEvent("lester:conversation-read", { detail: { userId: "22222222-2222-4222-8222-222222222222", workspaceId: "fixture-workspace", conversationId: "11111111-1111-4111-8111-111111111111", lastReadSeq: 2 } })));
    await expect.poll(() => taskReads).toBe(1);
    await (await taskResponse).finished();
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())));
    release();
    await expect(page.locator(".personal-inbox-toggle")).toHaveText("未读回复0");
    await page.locator(".personal-inbox-toggle").click();
    await expect(page.locator(".personal-inbox")).toContainText("暂无未读回复");
    expect(api.mutations).toEqual(initialMutations); expect(api.unexpected).toEqual([]);
  } finally { release(); }
});

test("reading a task in another tab refreshes the assistant inbox through the actual broadcast channel", async ({ page, context, api }) => {
  const state = empty(), reply = unreadTask(), now = new Date().toISOString();
  state.inbox = { tasks: [reply], total: 1, next_cursor: "" };
  api.conversation = { id: conversationId, project_id: "fixture-project", workspace_id: "fixture-workspace", created_by: "22222222-2222-4222-8222-222222222222", agent_slug: "lester", agent_name: "Lester", model_deployment_id: "fixture-model", title: reply.title, run_status: "completed", pinned: false, created_at: now, updated_at: now };
  api.messages = [{ id: "fixture-user", role: "user", seq: 1, content: "检查这份介绍页", created_at: now }, { id: reply.latest_reply_id!, role: "assistant", seq: 2, content: reply.reply, created_at: now }];
  let assistantTaskReads = 0, readWrites = 0;
  await context.route("**/api/v1/personal**", route => {
    expect(route.request().method()).toBe("GET");
    if (new URL(route.request().url()).pathname.endsWith("/tasks")) {
      if (route.request().frame().page() === page) assistantTaskReads++;
      return route.fulfill({ json: { tasks: state.tasks, inbox: state.inbox } });
    }
    return route.fulfill({ json: state });
  });
  await context.route(`**/api/v1/conversations/${conversationId}/read`, route => {
    expect(route.request().method()).toBe("POST");
    expect(route.request().postDataJSON()).toEqual({ message_id: reply.latest_reply_id });
    readWrites++; state.inbox = { tasks: [], total: 0, next_cursor: "" };
    return route.fulfill({ json: { last_read_seq: 2 } });
  });
  await login(page);
  const initialMutations = [...api.mutations], beforeRead = assistantTaskReads;
  const input = page.getByRole("textbox", { name: "个人助理消息", exact: true });
  await input.fill("等看完以后再安排下一步");
  await expect(page.locator(".personal-inbox-toggle")).toHaveText("未读回复1");
  const other = await context.newPage();
  try {
    await other.goto(`/app/c/${conversationId}`); await other.bringToFront();
    await expect(other.locator(`[data-reply-read-id="${reply.latest_reply_id}"]`)).toBeVisible();
    await expect.poll(() => readWrites).toBe(1);
    // Keep the assistant tab in the background: a focus refresh cannot mask a
    // missing BroadcastChannel subscription or account-scope mismatch.
    await expect(page.locator(".personal-inbox-toggle")).toHaveText("未读回复0");
    await expect.poll(() => assistantTaskReads).toBeGreaterThan(beforeRead);
    await expect(input).toHaveValue("等看完以后再安排下一步");
    await expect(page.locator(".personal-inbox-toggle")).toHaveAttribute("aria-expanded", "false");
    expect(api.mutations).toEqual(initialMutations); expect(api.unexpected).toEqual([]);
  } finally { await other.close(); }
});

test("returning to the active assistant reconciles remotely changed read state through reads only", async ({ page, context, api }) => {
  const state = empty(); state.inbox = { tasks: [unreadTask()], total: 1, next_cursor: "" }; let taskReads = 0;
  await context.route("**/api/v1/personal**", route => {
    expect(route.request().method()).toBe("GET");
    if (new URL(route.request().url()).pathname.endsWith("/tasks")) { taskReads++; return route.fulfill({ json: { tasks: state.tasks, inbox: state.inbox } }); }
    return route.fulfill({ json: state });
  });
  await login(page);
  const initialMutations = [...api.mutations], beforeFocus = taskReads;
  const input = page.getByRole("textbox", { name: "个人助理消息", exact: true });
  await input.fill("这条草稿继续保留");
  await expect(page.locator(".personal-inbox-toggle")).toHaveText("未读回复1");
  state.inbox = { tasks: [], total: 0, next_cursor: "" };
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect.poll(() => taskReads).toBeGreaterThan(beforeFocus);
  await expect(page.locator(".personal-inbox-toggle")).toHaveText("未读回复0");
  await expect(input).toHaveValue("这条草稿继续保留");
  await expect(page.locator(".personal-inbox-toggle")).toHaveAttribute("aria-expanded", "false");
  expect(api.mutations).toEqual(initialMutations); expect(api.readAcks).toEqual([]);
});

test("the independent dock preserves two drafts across tasks, settings and minimizing", async ({ page, context, api }, info) => {
  const state = empty(), now = new Date().toISOString(), errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => { if (message.type() === "error") errors.push(message.text()); });
  api.conversation = { id: conversationId, project_id: "fixture-project", workspace_id: "fixture-workspace", created_by: "22222222-2222-4222-8222-222222222222", agent_slug: "lester", agent_name: "Lester", model_deployment_id: "fixture-model", title: "现有介绍页", run_status: "completed", pinned: false, created_at: now, updated_at: now };
  state.turns = [{ id: "existing-turn", seq: 1, content: "找到之前的介绍页", reply: "可以继续处理这项工作。", status: "completed", changes: [], created_at: now, task: { conversation_id: conversationId, project_id: "fixture-project", title: "现有介绍页", status: "started", run_id: null, input_id: null } }];
  let historyReads = 0;
  await context.route("**/api/v1/personal**", route => {
    expect(route.request().method()).toBe("GET");
    if (new URL(route.request().url()).pathname.endsWith("/tasks")) return route.fulfill({ json: { tasks: [] } });
    historyReads++; return route.fulfill({ json: state });
  });
  await login(page);
  const panel = page.getByRole("dialog", { name: "个人助理", exact: true });
  await panel.getByRole("button", { name: "收起个人助理", exact: true }).click();
  const taskInput = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await taskInput.fill("未发送的任务草稿");
  await expect(page.locator(".compose-box .personal-project, .compose-box .personal-options")).toHaveCount(0);
  await openAssistant(page);
  const personalInput = panel.getByRole("textbox", { name: "个人助理消息", exact: true });
  await personalInput.fill("未发送的助理草稿");
  if (info.project.name === "desktop") {
    const send = await page.getByRole("button", { name: "发送消息", exact: true }).boundingBox(), companion = await panel.boundingBox();
    expect(send!.x + send!.width).toBeLessThanOrEqual(companion!.x);
  }
  await page.screenshot({ path: info.outputPath("01-independent-dock.png") });
  await panel.getByRole("link", { name: "打开任务 现有介绍页", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/app/c/${conversationId}$`));
  if (info.project.name === "mobile") { await expect(panel).not.toBeVisible(); await openAssistant(page); }
  else await expect(panel).toBeVisible();
  await expect(personalInput).toHaveValue("未发送的助理草稿");
  await expect.poll(() => historyReads).toBe(info.project.name === "mobile" ? 3 : 2);
  await page.keyboard.press("Escape");
  await expect(panel).not.toBeVisible();
  await expect(page.locator(".personal-dock-launcher")).toBeFocused();
  const launcher = await page.locator(".personal-dock-launcher").boundingBox(), composer = await page.locator(".compose-box").boundingBox();
  expect(launcher!.y + launcher!.height).toBeLessThanOrEqual(composer!.y);
  expect(launcher && composer && launcher.x < composer.x + composer.width && launcher.x + launcher.width > composer.x && launcher.y < composer.y + composer.height && launcher.y + launcher.height > composer.y).toBe(false);
  if (info.project.name === "mobile") await page.getByRole("button", { name: "打开会话栏", exact: true }).click();
  await page.locator(".user-menu-trigger").click();
  await page.getByRole("menuitem", { name: "模型", exact: true }).click();
  await expect(page).toHaveURL(/\/app\/settings\/models/);
  const settingsEntry = await page.locator(".personal-dock-launcher").boundingBox();
  if (info.project.name === "mobile") await expect(page.locator(".personal-launcher-slot .personal-dock-launcher")).toBeVisible();
  else expect(page.viewportSize()!.height - settingsEntry!.y - settingsEntry!.height).toBeLessThanOrEqual(32);
  await openAssistant(page);
  await expect(personalInput).toHaveValue("未发送的助理草稿");
  await page.screenshot({ path: info.outputPath("02-dock-in-settings.png") });
  await panel.getByRole("button", { name: "收起个人助理", exact: true }).click();
  await page.getByRole("link", { name: "返回工作区", exact: true }).first().click();
  await expect(page).toHaveURL(new RegExp(`/app/c/${conversationId}$`));
  if (info.project.name === "mobile") await page.getByRole("button", { name: "打开会话栏", exact: true }).click();
  await page.getByRole("link", { name: "新任务", exact: true }).click();
  await expect(page).toHaveURL("/app/p/fixture-project");
  await expect(taskInput).toHaveValue("未发送的任务草稿");
  await openAssistant(page);
  await expect(personalInput).toHaveValue("未发送的助理草稿");
  if (info.project.name === "desktop") {
    await page.setViewportSize({ width: 834, height: 1112 });
    await expect.poll(() => panel.evaluate(element => element.matches(":modal"))).toBe(true);
    await expect(personalInput).toHaveValue("未发送的助理草稿");
    await page.screenshot({ path: info.outputPath("04-assistant-tablet.png") });
  }
  await expect(page).toHaveTitle(/Lester/);
  await expect(page.locator("nextjs-portal")).toHaveCount(0);
  expect(api.mutations.filter(item => item.path.startsWith("/api/v1/conversations"))).toEqual([]);
  expect(errors).toEqual([]);
});

test("assistant replies preserve task focus when minimized or open beside it", async ({ page, context, api }, info) => {
  const state = empty(); let posts = 0, release!: () => void;
  let held = new Promise<void>(resolve => { release = resolve; });
  await context.route("**/api/v1/personal**", async route => {
    if (route.request().method() === "GET") return route.fulfill({ json: new URL(route.request().url()).pathname.endsWith("/tasks") ? { tasks: [] } : state });
    posts++; const body = route.request().postDataJSON(); await held;
    const turn: PersonalTurn = { id: body.id, seq: state.turns.length + 1, content: body.content, reply: "我在这里，随时接着聊。", status: "completed", changes: [], created_at: new Date().toISOString() };
    state.turns.push(turn); return route.fulfill({ json: turn });
  });
  try {
    await login(page);
    const panel = page.getByRole("dialog", { name: "个人助理", exact: true });
    await panel.getByRole("textbox", { name: "个人助理消息", exact: true }).fill("hello");
    await panel.getByRole("button", { name: "发送给个人助理", exact: true }).click();
    await expect(panel.locator(".personal-working")).toBeVisible();
    await panel.getByRole("button", { name: "收起个人助理", exact: true }).click();
    const task = page.getByRole("textbox", { name: "消息输入框", exact: true });
    await task.fill("我仍在写任务"); release();
    await expect(page.getByLabel("有更新", { exact: true })).toBeVisible();
    await expect(task).toBeFocused(); await expect(task).toHaveValue("我仍在写任务");
    await expect(panel).not.toBeVisible();
    await openAssistant(page);
    await expect(panel.getByText("我在这里，随时接着聊。", { exact: true })).toBeVisible();
    expect(posts).toBe(1);
    if (info.project.name === "desktop") {
      held = new Promise<void>(resolve => { release = resolve; });
      await panel.getByRole("textbox", { name: "个人助理消息", exact: true }).fill("再聊一句");
      await panel.getByRole("button", { name: "发送给个人助理", exact: true }).click();
      await expect(panel.locator(".personal-working")).toBeVisible();
      await task.click({ position: { x: 20, y: 20 } });
      await task.fill("助理打开时我也能继续写任务"); release();
      await expect(panel.getByText("我在这里，随时接着聊。", { exact: true })).toHaveCount(2);
      await expect(panel.locator(".personal-working")).toHaveCount(0);
      await expect(task).toBeFocused(); expect(posts).toBe(2);
    }
    expect(api.mutations.filter(item => item.path.startsWith("/api/v1/conversations"))).toEqual([]);
    await page.screenshot({ path: info.outputPath("03-minimized-reply.png") });
  } finally { release(); }
});

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
  await openAssistant(page);
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
  await openAssistant(page);
  await expect(card).toBeVisible(); expect(posts).toBe(1);
  await input.fill("还没发送的补充");
  const newPage = await context.newPage(); await newPage.goto("/app"); await openAssistant(newPage);
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
    await page.locator(".personal-dock-launcher").click();
    await expect(page.getByText("保留用户原文：我喜欢极简设计", { exact: true })).toBeVisible();
    await page.locator(".personal-toolbar button").first().click();
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
  await openAssistant(page);
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
  await page.getByLabel("助理设置", { exact: true }).click();
  await expect(page.getByRole("combobox", { name: "个人助理模型", exact: true })).toHaveValue("fixture-model");
  await expect(page.getByRole("combobox", { name: "对话项目范围", exact: true })).toHaveValue("");
  await page.getByRole("textbox", { name: "个人助理消息", exact: true }).click();
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
  await expect(tour).toContainText("固定的 Lester 入口");
  await tour.getByRole("button", { name: "下一步", exact: true }).click();
  await expect(tour.getByRole("heading", { name: "把重要的事交给它记住", exact: true })).toBeVisible();
  await expect(tour).toContainText("编辑、忘记和撤销");
  await tour.getByRole("button", { name: "稍后再学", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeVisible();
  await openAssistant(page);
  expect(api.mutations.every(item => item.path.startsWith("/api/v1/me/guides/"))).toBe(true);
});
