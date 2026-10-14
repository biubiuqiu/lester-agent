import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";

const prefix = `/api/v1/conversations/${conversationId}`;
const test = base.extend<{ api: WorkspaceApi }>({
  api: async ({ context, page }, runFixture) => {
    const api = new WorkspaceApi(), errors: string[] = [];
    page.on("pageerror", error => errors.push(error.message));
    page.on("console", message => {
      if (message.type() === "error" && !/status of (401|404|503)|net::ERR_FAILED/.test(message.text())) errors.push(message.text());
    });
    await api.install(context);
    try { await runFixture(api); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); }
    finally { await api.close(); }
  },
});
async function start(page: Page, api: WorkspaceApi) {
  await page.goto("/app");
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill(prompt); await input.press("Enter");
  await expect(page).toHaveURL(`/app/c/${conversationId}`);
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeVisible();
  await expect.poll(() => api.streamCount).toBeGreaterThan(0);
}
async function finish(page: Page, api: WorkspaceApi) {
  api.finish();
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toHaveCount(0);
  await expect(page.locator(".chat-message.assistant")).toBeVisible();
  // File inventory is a separate asynchronous receipt from run completion.
  // Wait for the actual auto-open before closing it, so a late mobile modal
  // cannot cover the next model-menu interaction.
  await expect(page.locator(".open-file-tabs [role=tab]")).toHaveCount(2);
  const close = page.locator(".computer-panel .close-files-panel");
  await expect(close).toBeVisible();
  await close.click();
}

test("accepted send survives a failed refresh without restoring its draft or losing Stop", async ({ page, context, api }, info) => {
  await start(page, api); await finish(page, api);
  let failRefresh = false;
  await context.route(`**${prefix}**`, async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === `${prefix}/permissions/messages` && route.request().method() === "POST") failRefresh = true;
    if (path === prefix && route.request().method() === "GET" && failRefresh) {
      failRefresh = false;
      return route.fulfill({ status: 503, json: { error: "fixture sync unavailable" } });
    }
    return route.fallback();
  });
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill("修改标题，保留现有文件。"); await input.press("Enter");
  await expect(page.locator(".workspace-error")).toContainText("消息已受理，但会话同步失败");
  await expect(input).toHaveValue("");
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeEnabled();
  await expect(page.locator(".chat-message.user").filter({ hasText: "修改标题，保留现有文件。" })).toHaveCount(1);
  expect(api.conversation?.run_status).toBe("running");
  expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(2);
  await page.screenshot({ path: info.outputPath("accepted-send-sync-error.png") });
  await page.getByRole("button", { name: "重新同步状态", exact: true }).click();
  await expect(page.locator(".workspace-error")).toHaveCount(0);
  expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(2);
});

test("a confirmed stop keeps stopping when the subsequent read fails", async ({ page, context, api }, info) => {
  await start(page, api);
  let failRefresh = false, stops = 0;
  await context.route(`**${prefix}**`, async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === `${prefix}/runs/fixture-run/cancel`) {
      stops++; api.conversation!.run_status = "cancelling"; failRefresh = true;
      return route.fulfill({ status: 200, json: {} });
    }
    if (path === prefix && route.request().method() === "GET" && failRefresh) {
      failRefresh = false;
      return route.fulfill({ status: 503, json: { error: "fixture sync unavailable" } });
    }
    return route.fallback();
  });
  await page.getByRole("button", { name: "停止生成", exact: true }).click();
  await expect(page.locator(".workspace-error")).toContainText("停止请求已受理");
  await expect(page.getByRole("button", { name: "正在停止任务", exact: true })).toBeDisabled();
  await expect(page.locator(".conversation-run-label")).toHaveText("正在停止");
  expect(stops).toBe(1);
  await page.screenshot({ path: info.outputPath("accepted-stop-sync-error.png") });
  await page.getByRole("button", { name: "重新同步状态", exact: true }).click();
  await expect(page.locator(".workspace-error")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "正在停止任务", exact: true })).toBeDisabled();
  expect(stops).toBe(1);
});

test("model changes serialize, stay disabled during a run and preserve drafts on failure", async ({ page, context, api }) => {
  await context.route("**/api/v1/model-deployments", route => route.fulfill({ json: { deployments: [
    { id: "fixture-model", name: "模拟模型", is_default: true, enabled: true },
    { id: "other-model", name: "另一个模型", is_default: false, enabled: true },
  ] } }));
  await start(page, api);
  await page.getByRole("button", { name: "更多会话选项", exact: true }).click();
  let select = page.getByRole("combobox", { name: "切换模型", exact: true });
  await expect(select).toBeDisabled();
  await expect(page.locator(".conversation-options-popover")).toContainText("当前任务结束后可切换模型");
  await page.getByRole("button", { name: "更多会话选项", exact: true }).press("Escape");
  await finish(page, api);
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill("保留模型切换期间的草稿");
  let release!: () => void, patches = 0;
  const pending = new Promise<void>(resolve => { release = resolve; });
  await context.route(`**${prefix}`, async route => {
    if (route.request().method() !== "PATCH") return route.fallback();
    patches++;
    if (patches === 1) { await pending; return route.fallback(); }
    return route.fulfill({ status: 503, json: { error: "fixture model save failed" } });
  });
  await page.getByRole("button", { name: "更多会话选项", exact: true }).click();
  select = page.getByRole("combobox", { name: "切换模型", exact: true });
  await select.selectOption("other-model");
  await expect.poll(() => patches).toBe(1);
  await page.getByRole("button", { name: "更多会话选项", exact: true }).click();
  select = page.getByRole("combobox", { name: "切换模型", exact: true });
  await expect(select).toBeDisabled();
  await expect(page.getByRole("button", { name: "发送消息", exact: true })).toBeDisabled();
  // A stale change callback cannot bypass the pending guard or create an out-of-order save.
  await select.evaluate((node: HTMLSelectElement) => { node.value = "fixture-model"; node.dispatchEvent(new Event("change", { bubbles: true })); });
  expect(patches).toBe(1);
  release();
  await page.getByRole("button", { name: "更多会话选项", exact: true }).click();
  select = page.getByRole("combobox", { name: "切换模型", exact: true });
  await expect(select).toBeEnabled();
  await expect(select).toHaveValue("other-model");
  expect(api.conversation?.model_deployment_id).toBe("other-model");
  await select.selectOption("fixture-model");
  await expect(page.locator(".workspace-error")).toHaveText("fixture model save failed");
  await expect(input).toHaveValue("保留模型切换期间的草稿");
  await page.getByRole("button", { name: "更多会话选项", exact: true }).click();
  await expect(page.getByRole("combobox", { name: "切换模型", exact: true })).toHaveValue("other-model");
  expect(patches).toBe(2);
});

test("partial attachment failure retries only the missing upload before explicit send", async ({ page, context, api }) => {
  await start(page, api); await finish(page, api);
  const counts = new Map<string, number>();
  await context.route(`**${prefix}/attachments`, async route => {
    const name = /filename="([^"]+)"/.exec(route.request().postDataBuffer()!.toString())![1];
    const count = (counts.get(name) || 0) + 1; counts.set(name, count);
    if (name === "retry.txt" && count === 1) return route.fulfill({ status: 503, json: { error: "fixture upload failed" } });
    return route.fulfill({ json: { id: `upload-${name}`, conversation_id: conversationId, original_name: name, size_bytes: 1 } });
  });
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill("分析这两个附件");
  await page.locator('.composer input[type="file"]').setInputFiles([
    { name: "saved.txt", mimeType: "text/plain", buffer: Buffer.from("a") },
    { name: "retry.txt", mimeType: "text/plain", buffer: Buffer.from("b") },
  ]);
  await input.press("Enter");
  await expect(page.locator(".compose-error")).toHaveText("fixture upload failed");
  await expect(input).toHaveValue("分析这两个附件");
  await expect(page.locator(".pending-attachment")).toHaveCount(2);
  expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(1);
  await input.press("Enter");
  await expect(input).toHaveValue("");
  expect(Object.fromEntries(counts)).toEqual({ "saved.txt": 1, "retry.txt": 2 });
  const sent = api.mutations.filter(item => item.path.endsWith("/messages"));
  expect(sent).toHaveLength(2);
  expect(sent[1].body.attachment_ids).toEqual(["upload-saved.txt", "upload-retry.txt"]);
});

test("recovery instructions append to the current draft without sending or replacing it", async ({ page, api }) => {
  await start(page, api);
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill("保留我刚写的新要求");
  api.failRun();
  await page.getByRole("button", { name: "从当前结果继续", exact: true }).click();
  await expect(input).toHaveValue("保留我刚写的新要求\n\n请检查当前会话目录里已经产生的结果，从上次中断处继续；不要重复已经成功完成的步骤。");
  await expect(input).toBeFocused();
  await expect(page.getByText("恢复指令已添加到草稿，请确认后发送。", { exact: true })).toBeVisible();
  expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(1);
});

test("a lost send response is looked up by submission ID instead of replaying the POST", async ({ page, context, api }) => {
  await start(page, api); await finish(page, api);
  let submission = "", posts = 0, reads = 0;
  await context.route(`**${prefix}**`, async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === `${prefix}/permissions/messages` && route.request().method() === "POST") {
      posts++; const body = route.request().postDataJSON(); submission = body.submission_id;
      api.messages.push({ id: "lost-response-user", role: "user", content: body.content, created_at: new Date().toISOString() });
      api.conversation!.run_status = "running";
      return route.abort("failed");
    }
    if (path === `${prefix}/submissions/${submission}`) {
      reads++; return route.fulfill({ json: { run_id: "fixture-run", status: "running", message_id: "lost-response-user" } });
    }
    return route.fallback();
  });
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill("已受理但响应丢失的追问"); await input.press("Enter");
  await expect(input).toHaveValue("");
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeEnabled();
  await expect(page.locator(".chat-message.user").last()).toContainText("已受理但响应丢失的追问");
  expect(posts).toBe(1); expect(reads).toBe(1);
  expect(submission).toMatch(/^[0-9a-f-]{36}$/);
});

test("file completion events replay after a disconnected conversation stream", async ({ page, context, api }, info) => {
  await start(page, api);
  let release!: () => void;
  const reconnect = new Promise<void>(resolve => { release = resolve; });
  await context.route("**/api/v1/events*", async route => { await reconnect; await route.fallback(); });
  try {
    await page.reload();
    await page.getByRole("textbox", { name: "消息输入框", exact: true }).fill("事件回放期间保留的草稿");
    await expect(page.getByRole("button", { name: "运行中输入", exact: true })).toBeEnabled();
    await expect.poll(() => api.streamCount).toBe(0);
    api.finish();
    api.writeFile("replay.html", "<!doctype html><h1>事件回放产物</h1>");
    release();
    await expect.poll(() => api.streamCount).toBeGreaterThan(0);
    const tabs = page.getByRole("tablist", { name: "已打开文件", exact: true });
    await expect(tabs.getByRole("tab", { name: "replay.html", exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "停止生成", exact: true })).toHaveCount(0);
    await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toHaveValue("事件回放期间保留的草稿");
    await page.screenshot({ path: info.outputPath("replayed-files.png") });
  } finally { release(); }
});
