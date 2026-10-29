import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId } from "./workspace-api";

const userId = "22222222-2222-4222-8222-222222222222";
const firstReply = "33333333-3333-4333-8333-333333333333";
const secondReply = "44444444-4444-4444-8444-444444444444";
const prefix = `/api/v1/conversations/${conversationId}`;
const test = base.extend<{ api: WorkspaceApi }>({ api: async ({ context, page }, run) => {
  const api = new WorkspaceApi(), errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => { if (message.type() === "error") errors.push(message.text()); });
  await api.install(context);
  await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", url: "http://127.0.0.1:13020" }, { name: "lester_locale", value: "zh-CN", url: "http://127.0.0.1:13020" }]);
  api.conversation = { id: conversationId, workspace_id: "fixture-workspace", created_by: userId, project_id: "fixture-project", agent_slug: "lester", agent_name: "Lester", model_deployment_id: "fixture-model", title: "未读回复测试", pinned: false, run_status: "completed", created_at: new Date().toISOString(), updated_at: new Date().toISOString() };
  api.messages = [{ id: "fixture-user", role: "user", seq: 1, content: "检查这个任务", created_at: new Date().toISOString() }, { id: firstReply, role: "assistant", seq: 2, content: "这是一条已持久保存的公开回复。", created_at: new Date().toISOString() }];
  try { await run(api); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); }
  finally { await context.close(); await api.close(); }
} });

async function frames(page: Page) {
  await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
}

test("a visible reply end is acknowledged without sending the draft or changing a task", async ({ page, api }, info) => {
  await page.goto(`/app/c/${conversationId}`);
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill("保留我的未发送草稿");
  await expect.poll(() => api.readAcks).toEqual([{ conversationId, messageId: firstReply, seq: 2 }]);
  await expect(page).toHaveTitle(/Lester/);
  await expect(page.locator("nextjs-portal")).toHaveCount(0);
  await expect(input).toHaveValue("保留我的未发送草稿");
  expect(api.mutations).toEqual([]);
  await page.screenshot({ path: info.outputPath("visible-reply-read-draft-preserved.png") });
});

test("a hidden or unfocused page waits until it is foregrounded before acknowledging", async ({ page, context, api }) => {
  await context.addInitScript(() => {
    Object.defineProperty(document, "hidden", { configurable: true, value: true });
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "hidden" });
    Object.defineProperty(document, "hasFocus", { configurable: true, value: () => false });
  });
  await page.goto(`/app/c/${conversationId}`);
  await expect(page.locator(`[data-reply-read-id="${firstReply}"]`)).toBeVisible();
  await frames(page);
  expect(api.readAcks).toEqual([]);
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", { configurable: true, value: false });
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await frames(page);
  expect(api.readAcks).toEqual([]);
  await page.evaluate(() => {
    Object.defineProperty(document, "hasFocus", { configurable: true, value: () => true });
    window.dispatchEvent(new Event("focus"));
  });
  await expect.poll(() => api.readAcks.length).toBe(1);
  expect(api.mutations).toEqual([]);
});

test("opening a long reply at a saved reading position does not mark its unseen end read", async ({ page, context, api }) => {
  api.messages[1].content = Array.from({ length: 65 }, (_, index) => `第 ${index + 1} 段：这是一份需要向下阅读的长回复。`).join("\n\n");
  await context.addInitScript(key => sessionStorage.setItem(key, JSON.stringify({ version: 1, thread: { top: 0, following: false } })), `lester.view.v1.${userId}.fixture-workspace.${conversationId}`);
  await page.goto(`/app/c/${conversationId}`);
  await expect(page.locator(".conversation-stream")).toContainText("第 65 段");
  await expect.poll(() => page.locator(".thread").evaluate(node => node.scrollTop)).toBeLessThan(10);
  await frames(page);
  expect(api.readAcks).toEqual([]);
  await page.locator(`[data-reply-read-id="${firstReply}"]`).scrollIntoViewIfNeeded();
  await expect.poll(() => api.readAcks.length).toBe(1);
  expect(api.readAcks[0].messageId).toBe(firstReply);
  expect(api.mutations).toEqual([]);
});

test("closing an obscuring modal acknowledges the newly exposed reply without another scroll", async ({ page, context, api }) => {
  await context.addInitScript(() => Object.defineProperty(document, "hidden", { configurable: true, value: true }));
  await page.goto(`/app/c/${conversationId}`);
  await page.getByRole("button", { name: "打开个人助理", exact: true }).click();
  await page.getByRole("button", { name: "管理记忆", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "Lester 的记忆", exact: true })).toBeVisible();
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", { configurable: true, value: false });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await frames(page);
  expect(api.readAcks).toEqual([]);
  await page.getByRole("button", { name: "关闭记忆", exact: true }).click();
  // Phones keep the separate assistant modal above the task until minimized.
  await page.locator("#personal-assistant-panel").getByRole("button", { name: "收起个人助理", exact: true }).click();
  await expect.poll(() => api.readAcks.length).toBe(1);
  expect(api.mutations).toEqual([]);
});

test("a receipt targets the displayed message rather than a newer reply that arrived during acknowledgement", async ({ page, context, api }) => {
  let requests = 0;
  await context.route(`**${prefix}/read`, async route => {
    requests++;
    if (requests > 1) return route.fallback();
    const body = route.request().postDataJSON();
    expect(body).toEqual({ message_id: firstReply });
    api.readAcks.push({ conversationId, messageId: body.message_id, seq: 2 });
    api.messages.push({ id: secondReply, role: "assistant", seq: 4, content: "这是稍后到达、尚未显示的新回复。", created_at: new Date().toISOString() });
    return route.fulfill({ json: { last_read_seq: 2 } });
  });
  await page.goto(`/app/c/${conversationId}`);
  await expect.poll(() => api.readAcks.length).toBe(1);
  expect(api.readAcks[0].seq).toBe(2);
  await expect(page.locator(`[data-reply-read-id="${secondReply}"]`)).toHaveCount(0);
  await page.reload();
  await expect.poll(() => api.readAcks.at(-1)?.seq).toBe(4);
  expect(api.mutations).toEqual([]);
});

test("opening an accessible conversation owned by another user does not write a personal receipt", async ({ page, api }) => {
  api.conversation!.created_by = "55555555-5555-4555-8555-555555555555";
  await page.goto(`/app/c/${conversationId}`);
  await expect(page.locator(`[data-reply-read-id="${firstReply}"]`)).toBeVisible();
  await frames(page);
  expect(api.readAcks).toEqual([]);
  expect(api.mutations).toEqual([]);
});
