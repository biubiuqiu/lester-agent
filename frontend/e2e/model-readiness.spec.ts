import { test as base, expect } from "@playwright/test";
import { WorkspaceApi } from "./workspace-api";

const test = base.extend<{ api: WorkspaceApi }>({
  api: async ({ context, page }, runFixture) => {
    const api = new WorkspaceApi();
    const errors: string[] = [];
    page.on("pageerror", error => errors.push(error.message));
    await api.install(context);
    try { await runFixture(api); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); }
    finally { await api.close(); }
  },
});
test.beforeEach(async ({ page, api }) => {
  expect(api.mutations).toEqual([]);
  await page.goto("/app/new");
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeVisible();
});

test("verification is explicit and proves only the returned text-call scope", async ({ page, context, api }, info) => {
  let calls = 0;
  await context.route("**/api/v1/model-deployments/fixture-model/test", route => {
    calls++;
    return route.fulfill({ json: { ok: true, verified: true, status: "verified_text_call", capability_scope: "text_call_only", deployment_id: "fixture-model", latency_ms: 12 } });
  });
  await page.goto("/app/settings/models");
  await expect(page.getByRole("button", { name: "验证连接", exact: true })).toBeEnabled();
  expect(calls).toBe(0);
  await page.getByRole("button", { name: "验证连接", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "文本调用已验证；工具与附件尚未验证。" })).toBeVisible();
  expect(calls).toBe(1);
  expect(api.mutations.some(item => item.path.includes("/conversations"))).toBe(false);
  await page.screenshot({ path: info.outputPath("model-text-verification.png") });
});

test("a saved configuration cannot be rendered as verified provider access", async ({ page, context }) => {
  await context.route("**/api/v1/model-deployments/fixture-model/test", route => route.fulfill({ json: { ok: true, verified: false, status: "configuration_saved", capability_scope: "text_call_only" } }));
  await page.goto("/app/settings/models");
  await page.getByRole("button", { name: "验证连接", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "验证失败，请检查模型与服务商配置。" })).toBeVisible();
  await expect(page.getByText("文本调用已验证；工具与附件尚未验证。", { exact: true })).toHaveCount(0);
});

test("budget-only saves preserve model settings and disclose a failed list refresh", async ({ page, context }) => {
  let saved = false;
  await context.route("**/api/v1/model-deployments", route => saved ? route.fulfill({ status: 503, json: { error: "fixture refresh unavailable" } }) : route.fallback());
  await context.route("**/api/v1/model-deployments/fixture-model", route => {
    expect(route.request().method()).toBe("PATCH");
    expect(route.request().postDataJSON()).toEqual({ context_window: 200000, output_reserve: 12000, policy_window: 64000 });
    saved = true;
    return route.fulfill({ status: 204 });
  });
  await page.goto("/app/settings/models");
  await page.locator(".model-budget-settings>summary").click();
  const form = page.getByRole("form", { name: "上下文预算", exact: true });
  await form.getByLabel("模型上下文窗口（tokens）", { exact: true }).fill("200000");
  await form.getByLabel("输出预留（tokens）", { exact: true }).fill("12000");
  await form.getByLabel("本地上下文策略（tokens）", { exact: true }).fill("64000");
  await form.getByRole("button", { name: "保存预算", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "预算已保存，但列表刷新失败。请刷新配置。" })).toBeVisible();
  expect(saved).toBe(true);
});

test("members can verify a shared model while its budget controls stay private", async ({ page, context }) => {
  await context.route("**/api/v1/model-deployments", route => route.fulfill({ json: { deployments: [{ id: "shared-model", model_id: "fixture", name: "共享测试模型", enabled: true, shared: true }] } }));
  await context.route("**/api/v1/model-deployments/shared-model/test", route => route.fulfill({ json: { ok: false, verified: false, status: "failed", capability_scope: "text_call_only", error_kind: "authentication" } }));
  await page.goto("/app/settings/models");
  await expect(page.locator(".model-budget-settings")).toHaveCount(0);
  await page.getByRole("button", { name: "验证连接", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "验证失败：请检查服务商凭证。" })).toBeVisible();
});

test("desktop models show their device status and are managed on the desktop", async ({ page, context, api }, info) => {
  await context.route("**/api/v1/model-deployments", route => route.fulfill({ json: { deployments: [
    { id: "device-online", connection_id: "desktop-connection", name: "桌面在线模型", model_id: "fixture-online", enabled: true, shared: false, source: "device", device_name: "工作电脑", online: true, read_only: true },
    { id: "device-offline", connection_id: "desktop-connection", name: "桌面离线模型", model_id: "fixture-offline", enabled: true, shared: false, source: "device", device_name: "家用电脑", online: false, read_only: true },
  ] } }));
  let verificationCalls = 0;
  await context.route("**/api/v1/model-deployments/device-*/test", route => {
    verificationCalls++;
    expect(new URL(route.request().url()).pathname).toBe("/api/v1/model-deployments/device-online/test");
    return route.fulfill({ json: { ok: true, verified: true, status: "verified_text_call", capability_scope: "text_call_only" } });
  });
  await page.goto("/app/settings/models");
  const online = page.locator(".deployment-list article").filter({ hasText: "桌面在线模型" });
  const offline = page.locator(".deployment-list article").filter({ hasText: "桌面离线模型" });
  await expect(online.getByText("工作电脑 · 在线", { exact: true })).toBeVisible();
  await expect(offline.getByText("家用电脑 · 离线", { exact: true })).toBeVisible();
  await expect(offline.getByRole("button", { name: "验证连接", exact: true })).toBeDisabled();
  await expect(page.getByText("请在桌面客户端管理此模型。", { exact: true })).toHaveCount(2);
  await expect(page.locator(".model-budget-settings")).toHaveCount(0);
  expect(verificationCalls).toBe(0);
  await online.getByRole("button", { name: "验证连接", exact: true }).click();
  await expect(online.getByRole("status")).toHaveText("文本调用已验证；工具与附件尚未验证。");
  expect(verificationCalls).toBe(1);
  expect(api.mutations.some(item => item.path.includes("/model-deployments/"))).toBe(false);
  await page.screenshot({ path: info.outputPath("desktop-model-status.png") });
});

test("read-only deployments do not expose editable budget controls", async ({ page, context }) => {
  await context.route("**/api/v1/model-deployments", route => route.fulfill({ json: { deployments: [
    { id: "read-only-model", connection_id: "fixture-connection", name: "只读模型", model_id: "fixture", enabled: true, shared: false, source: "cloud", read_only: true },
  ] } }));
  await page.goto("/app/settings/models");
  await expect(page.locator(".deployment-list article").filter({ hasText: "只读模型" })).toBeVisible();
  await expect(page.getByRole("button", { name: "验证连接", exact: true })).toBeEnabled();
  await expect(page.locator(".model-budget-settings")).toHaveCount(0);
});
