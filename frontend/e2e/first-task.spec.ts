import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";

const test = base.extend<{ api: WorkspaceApi }>({
  api: async ({ context }, runFixture) => {
    const api = new WorkspaceApi();
    await api.install(context);
    try { await runFixture(api); expect(api.unexpected).toEqual([]); }
    finally { await api.close(); }
  },
});

async function login(page: Page) {
  await page.goto("/app");
  await expect(page).toHaveURL(/\/login\?returnTo=%2Fapp$/);
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeVisible();
}

test("login waits for hydration before accepting credentials", async ({ page, api }, info) => {
  let release!: () => void;
  const scripts = new Promise<void>(resolve => { release = resolve; });
  await page.route("**/_next/static/**/*.js", async route => { await scripts; await route.continue(); });
  try {
    await page.goto("/login", { waitUntil: "commit" });
    const email = page.getByLabel("邮箱", { exact: true });
    const password = page.getByLabel("密码", { exact: true });
    const submit = page.getByRole("button", { name: "登录", exact: true });
    await expect(email).toBeDisabled();
    await expect(password).toBeDisabled();
    await expect(submit).toBeDisabled();
    expect(api.mutations).toEqual([]);
    await page.screenshot({ path: info.outputPath("login-before-hydration.png") });
    release();
    await email.fill("browser@example.test");
    await password.fill("fixture-password-123");
    await expect(email).toHaveValue("browser@example.test");
    await expect(submit).toBeEnabled();
    await page.screenshot({ path: info.outputPath("login-ready.png") });
    await submit.click();
    await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeVisible();
    expect(api.mutations.filter(item => item.path === "/api/v1/auth/login")).toHaveLength(1);
    expect(api.mutations.some(item => item.path.startsWith("/api/v1/conversations"))).toBe(false);
  } finally { release(); }
});

test("login, explicit first task, live file tabs and independent HTML preview", async ({ page, context, api }, testInfo) => {
  const errors: string[] = [];
  context.on("page", opened => opened.on("pageerror", error => errors.push(error.message)));
  page.on("pageerror", error => errors.push(error.message));
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1 })).toContainText("想清楚。");
  await page.getByRole("link", { name: "开始使用", exact: true }).click();
  await expect(page).toHaveURL(/\/login\?returnTo=%2Fapp$/);
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("wrong-password");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.locator(".login-card").getByRole("alert")).toHaveText("邮箱或密码不正确");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const composer = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await expect(composer).toBeVisible();
  expect((await context.cookies()).find(cookie => cookie.name === "lester_access_token")?.httpOnly).toBe(true);
  await expect(page.getByRole("group", { name: "任务示例", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "制作一个网页", exact: true }).click();
  await expect(composer).not.toHaveValue("");
  expect(api.mutations.filter(item => item.path.startsWith("/api/v1/conversations"))).toEqual([]);
  await composer.fill(prompt);
  await composer.press("Enter");
  await expect(page).toHaveURL(`/app/c/${conversationId}`);
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeVisible();
  const taskMutations = api.mutations.filter(item => item.path.startsWith("/api/v1/conversations"));
  expect(taskMutations.map(item => item.path)).toEqual(["/api/v1/conversations", `/api/v1/conversations/${conversationId}/messages`]);
  expect(taskMutations[0].body).toMatchObject({ project_id: "fixture-project", agent_slug: "lester", model_deployment_id: "fixture-model" });
  expect(taskMutations[1].body).toEqual({ content: prompt, context_ids: [], attachment_ids: [], submission_id: expect.stringMatching(/^[0-9a-f-]{36}$/) });
  await expect.poll(() => api.streamCount).toBeGreaterThan(0);
  api.finish();
  const tabs = page.getByRole("tablist", { name: "已打开文件" });
  await expect(tabs.getByRole("tab", { name: "index.html", exact: true })).toBeVisible();
  await expect(tabs.getByRole("tab", { name: "notes.txt", exact: true })).toBeVisible();
  await expect(page.frameLocator('iframe[title="index.html preview"]').getByRole("heading", { name: "浏览器回归产物" })).toBeVisible();
  await expect(page.locator('iframe[title="index.html preview"]')).toHaveAttribute("sandbox", "allow-scripts");
  await page.getByRole("button", { name: "源码", exact: true }).click();
  await expect(page.getByLabel("index.html 源代码", { exact: true })).toContainText("浏览器回归产物");
  await tabs.getByRole("tab", { name: "notes.txt", exact: true }).click();
  await expect(page.getByLabel("notes.txt 源代码", { exact: true })).toContainText("请人工验收");
  await tabs.getByRole("tab", { name: "index.html", exact: true }).click();
  await page.getByRole("button", { name: "预览", exact: true }).click();
  await page.screenshot({ path: testInfo.outputPath("workspace-preview.png") });
  const newPage = context.waitForEvent("page");
  await page.getByRole("link", { name: "在新页面预览 index.html", exact: true }).click();
  const preview = await newPage;
  await expect(preview).toHaveURL(`/preview/${conversationId}?path=index.html`);
  await expect(preview).toHaveTitle("index.html · Lester 预览");
  await expect(preview.frameLocator("iframe").getByRole("heading", { name: "浏览器回归产物" })).toBeVisible();
  await expect(preview.locator(".workspace-shell, .computer-panel, .conversation-sidebar, .guide-dialog")).toHaveCount(0);
  const child = preview.frames().find(frame => frame.parentFrame());
  expect(child).toBeTruthy();
  expect(await child!.evaluate(() => { try { void parent.document.body; return "accessible"; } catch { return "isolated"; } })).toBe("isolated");
  await preview.screenshot({ path: testInfo.outputPath("standalone-preview.png") });
  await preview.close();
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toHaveCount(0);
  expect(errors).toEqual([]);
});

test("model setup navigation keeps a first-task draft without creating a conversation", async ({ page, api }) => {
  api.hasModel = false;
  await login(page);
  const composer = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await composer.fill(prompt);
  await expect(page.getByRole("button", { name: "发送消息", exact: true })).toBeDisabled();
  await page.getByRole("link", { name: "配置第一个模型" }).click();
  await expect(page).toHaveURL(/\/app\/settings\/models\?returnTo=/);
  await page.goBack();
  await expect(composer).toHaveValue(prompt);
  expect(api.mutations.filter(item => item.path.startsWith("/api/v1/conversations"))).toEqual([]);
});

test("creation and first-send failures retain drafts and never auto-retry mutations", async ({ page, api }) => {
  await login(page);
  api.failCreate = true;
  const composer = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await composer.fill(prompt);
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  await expect(page.locator(".new-chat-composer").getByRole("alert")).toHaveText("模拟会话创建失败");
  await expect(composer).toHaveValue(prompt);
  expect(api.mutations.filter(item => item.path === "/api/v1/conversations")).toHaveLength(1);
  await page.reload();
  await expect(composer).toHaveValue(prompt);
  expect(api.mutations.filter(item => item.path === "/api/v1/conversations")).toHaveLength(1);
  api.failCreate = false; api.failSend = true;
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  await expect(page).toHaveURL(`/app/c/${conversationId}`);
  await expect(composer).toHaveValue(prompt);
  await expect(page.getByText(/会话已创建，但首条消息未确认发送成功/)).toBeVisible();
  const sent = api.mutations.filter(item => item.path.endsWith("/messages"));
  expect(sent).toHaveLength(1);
  await page.reload();
  await expect(composer).toHaveValue(prompt);
  expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(1);
});
