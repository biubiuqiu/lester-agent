import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";

const prefix = `/api/v1/conversations/${conversationId}/interactions`;
const item = { id: "44444444-4444-4444-8444-444444444444", run_id: "fixture-run", kind: "plan", request_digest: "a".repeat(64), status: "pending", request: { kind: "plan", title: "先审阅再修改", steps: ["读取当前文件", "修改首页布局", "运行验证"] } };
const test = base.extend<{ api: WorkspaceApi }>({ api: [async ({ page, context }, runFixture) => {
  const api = new WorkspaceApi(), errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await api.install(context);
  try { await runFixture(api); expect(errors).toEqual([]); expect(api.unexpected).toEqual([]); }
  finally { await api.close(); }
}, { auto: true }] });
async function start(page: Page, api: WorkspaceApi) {
  await page.goto("/app/new"); await expect(page).toHaveTitle(/Lester/);
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill(prompt); await input.press("Enter");
  await expect(page).toHaveURL(`/app/c/${conversationId}`);
  await expect.poll(() => api.streamCount).toBeGreaterThan(0);
  await expect(page.locator(".composer-motion-ghost")).toHaveCount(0);
  return input;
}

test("plan review starts with two actions and feedback submits changes instead of approval", async ({ page, context, api }, info) => {
  let responses = 0;
  await context.route(`**${prefix}`, route => route.fulfill({ json: { interactions: [item], execution_mode: "plan" } }));
  await context.route(`**${prefix}/${item.id}/respond`, route => {
    responses++;
    const response = route.request().postDataJSON();
    expect(response).toEqual({ request_digest: item.request_digest, decision: "request_changes", feedback: "先不要修改布局，先给我两种方案。" });
    return route.fulfill({ json: { ...item, status: "changes_requested", response } });
  });
  const input = await start(page, api); await input.fill("尚未发送的聊天草稿");
  const card = page.locator(".plan-card");
  await expect(card.getByRole("button")).toHaveCount(2);
  await expect(card.getByRole("textbox", { name: "调整意见", exact: true })).not.toBeVisible();
  await expect(input).toBeFocused();
  await card.screenshot({ path: info.outputPath("plan-default.png") });
  await card.locator(".plan-feedback > summary").click();
  await card.getByRole("textbox", { name: "调整意见", exact: true }).fill("先不要修改布局，先给我两种方案。");
  await expect(card.getByRole("button", { name: "批准并执行", exact: true })).toHaveCount(0);
  await expect(card.getByRole("button", { name: "提交调整", exact: true })).toHaveClass(/primary/);
  await card.screenshot({ path: info.outputPath("plan-feedback.png") });
  await card.getByRole("button", { name: "提交调整", exact: true }).focus();
  await page.keyboard.press("Enter");
  await expect(card).toHaveCount(0);
  await expect(input).toBeFocused(); await expect(input).toHaveValue("尚未发送的聊天草稿");
  await expect(page.locator(".interaction-mode")).toContainText("Plan");
  expect(responses).toBe(1);
});

test("plan feedback survives reload and clearing it restores an explicit approve action", async ({ page, context, api }) => {
  let responses = 0;
  await context.route(`**${prefix}`, route => route.fulfill({ json: { interactions: [item], execution_mode: "plan" } }));
  await context.route(`**${prefix}/${item.id}/respond`, route => {
    responses++; const response = route.request().postDataJSON();
    expect(response).toEqual({ request_digest: item.request_digest, decision: "approve" });
    return route.fulfill({ json: { ...item, status: "approved", response } });
  });
  await start(page, api);
  await page.locator(".plan-feedback > summary").click();
  await page.getByRole("textbox", { name: "调整意见", exact: true }).fill("只提供建议");
  await page.reload();
  const feedback = page.getByRole("textbox", { name: "调整意见", exact: true });
  await expect(feedback).toBeVisible(); await expect(feedback).toHaveValue("只提供建议");
  await expect(page.getByRole("button", { name: "提交调整", exact: true })).toBeEnabled();
  expect(responses).toBe(0);
  await feedback.fill("   ");
  await expect(feedback).toBeVisible();
  await page.getByRole("button", { name: "批准并执行", exact: true }).click();
  await expect(page.locator(".plan-card")).toHaveCount(0); expect(responses).toBe(1);
});

test("declining a plan preserves its explicit feedback without approving execution", async ({ page, context, api }) => {
  let responses = 0;
  await context.route(`**${prefix}`, route => route.fulfill({ json: { interactions: [item], execution_mode: "plan" } }));
  await context.route(`**${prefix}/${item.id}/respond`, route => {
    responses++; const response = route.request().postDataJSON();
    expect(response).toEqual({ request_digest: item.request_digest, decision: "reject", feedback: "暂不修改，等业务范围确定" });
    return route.fulfill({ json: { ...item, status: "rejected", response } });
  });
  const input = await start(page, api); await input.fill("之后再发送的草稿");
  await page.locator(".plan-feedback > summary").click();
  await page.getByRole("textbox", { name: "调整意见", exact: true }).fill("暂不修改，等业务范围确定");
  await page.getByRole("button", { name: "暂不执行", exact: true }).click();
  await expect(page.locator(".plan-card")).toHaveCount(0);
  await expect(page.locator(".interaction-mode")).toContainText("Plan");
  await expect(input).toHaveValue("之后再发送的草稿"); expect(responses).toBe(1);
});

test("a lost adjustment response is inspected without replay", async ({ page, context, api }) => {
  let received: Record<string, unknown> | undefined, posts = 0, reads = 0;
  await context.route(`**${prefix}`, route => route.fulfill({ json: { interactions: [item], execution_mode: "plan" } }));
  await context.route(`**${prefix}/${item.id}/respond`, route => {
    posts++; received = route.request().postDataJSON();
    expect(received?.decision).toBe("request_changes");
    return route.fulfill({ status: 503, json: { error: "fixture lost response" } });
  });
  await context.route(`**${prefix}/${item.id}`, route => { reads++; return route.fulfill({ json: { ...item, status: "changes_requested", response: received } }); });
  const input = await start(page, api); await input.fill("保留未发送要求");
  await page.locator(".plan-feedback > summary").click();
  await page.getByRole("textbox", { name: "调整意见", exact: true }).fill("先保留原文件");
  await page.getByRole("button", { name: "提交调整", exact: true }).click();
  await expect(page.locator(".plan-card")).toHaveCount(0);
  await expect(input).toHaveValue("保留未发送要求");
  expect(posts).toBe(1); expect(reads).toBe(1);
});
