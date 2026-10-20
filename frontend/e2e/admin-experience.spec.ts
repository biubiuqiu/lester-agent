import { test as base, expect } from "@playwright/test";
import { WorkspaceApi } from "./workspace-api";
import { translator } from "../src/lib/i18n";
import en from "../src/lib/i18n/messages/en.json";
import fr from "../src/lib/i18n/messages/fr.json";
import type { Automation } from "../src/lib/automation";

const t = translator(en), french = translator(fr);
const test = base.extend<{ api: WorkspaceApi }>({ api: async ({ context }, run) => {
  const api = new WorkspaceApi(); await api.install(context);
  await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", url: "http://127.0.0.1:13020" }, { name: "lester_locale", value: "en", url: "http://127.0.0.1:13020" }]);
  try { await run(api); expect(api.unexpected).toEqual([]); } finally { await api.close(); }
} });

test("admin reads distinguish errors from empty data and retry cannot repeat a confirmed account creation", async ({ page, context, api }, info) => {
  const self = { id: "22222222-2222-4222-8222-222222222222", display_name: "Administrator", email: "admin@example.test", role: "admin", disabled: false, created_at: new Date().toISOString() };
  const users = [self]; let fail = true, posts = 0, hold: Promise<void> | undefined, release: (() => void) | undefined;
  await context.route("**/api/v1/me", route => route.fulfill({ json: { user_id: self.id, workspace_id: "fixture-workspace", ...self } }));
  await context.route("**/api/v1/admin/users", async route => {
    if (route.request().method() === "POST") { posts++; const saved = { ...route.request().postDataJSON(), id: "created-user", created_at: self.created_at }; users.push(saved); fail = true; return route.fulfill({ json: saved }); }
    if (hold) await hold;
    return route.fulfill(fail ? { status: 503, json: { error: "Fixture: user list unavailable" } } : { json: { users } });
  });
  try {
    await page.goto("/admin/users");
    await expect(page.locator(".settings-main > .settings-error[role=alert]")).toContainText("Fixture: user list unavailable");
    await expect(page.getByText(t("暂无人员。"), { exact: true })).toHaveCount(0);
    await expect(page.locator(".admin-summary")).toHaveCount(0);
    fail = false; hold = new Promise<void>(resolve => { release = resolve; });
    await page.getByRole("button", { name: t("重试"), exact: true }).click();
    await expect(page.getByRole("status")).toContainText(t("正在加载人员…"));
    await expect(page.getByRole("button", { name: t("新增人员"), exact: true })).toBeDisabled();
    release!(); hold = undefined;
    await expect(page.getByRole("button", { name: t("编辑 {0}", [self.display_name]), exact: true })).toBeVisible();
    await page.getByRole("button", { name: t("新增人员"), exact: true }).click();
    const dialog = page.locator(".admin-dialog[open]");
    await dialog.getByLabel(t("姓名"), { exact: true }).fill("Alex");
    await dialog.getByLabel(t("邮箱"), { exact: true }).fill("alex@example.test");
    await dialog.getByLabel(t("初始密码"), { exact: true }).fill("fixture-password-123");
    await dialog.getByRole("button", { name: t("保存"), exact: true }).click();
    await expect(dialog).toHaveCount(0);
    await expect(page.getByRole("status")).toHaveText(t("账号已创建，可使用设置的密码登录。"));
    await expect(page.locator(".settings-main > .settings-error[role=alert]")).toContainText("Fixture: user list unavailable");
    fail = false; await page.getByRole("button", { name: t("重试"), exact: true }).click();
    await expect(page.getByRole("button", { name: t("编辑 {0}", ["Alex"]), exact: true })).toBeVisible();
    expect(posts).toBe(1);
    await page.getByRole("button", { name: t("编辑 {0}", [self.display_name]), exact: true }).click();
    await expect(dialog.getByRole("combobox", { name: t("角色"), exact: true })).toBeDisabled();
    await expect(dialog.getByRole("checkbox", { name: t("停用此账号"), exact: true })).toBeDisabled();
    await page.keyboard.press("Escape");
    await page.screenshot({ path: info.outputPath("admin-read-recovery-and-translated-save.png") });
    expect(api.mutations).toEqual([]);
  } finally { release?.(); }
});

test("model save notices follow language changes without resetting the next model draft or resubmitting", async ({ page, context, api }, info) => {
  api.hasModel = false; let posts = 0;
  await context.route("**/api/v1/model-connections", route => {
    if (route.request().method() === "GET") return route.fulfill({ json: { connections: [] } });
    posts++; return route.fulfill({ json: { id: "saved-connection", provider: "openai", protocol: "openai", endpoint: "", config: {}, name: "Keep My Connection Name" } });
  });
  await page.goto("/app/settings/models");
  await page.locator('input[name="credential"]').fill("fixture-api-key");
  await page.getByRole("button", { name: t("保存连接，继续"), exact: true }).click();
  await expect(page.getByRole("status")).toHaveText(t("连接已保存。接下来添加你要使用的模型。"));
  const model = page.locator('input[name="model"]'); await model.fill("keep-my-model-id");
  await page.locator(".language-trigger").click(); await page.getByRole("menuitemradio", { name: /Français/ }).click();
  await expect(page.getByRole("status")).toHaveText(french("连接已保存。接下来添加你要使用的模型。"));
  await expect(model).toHaveValue("keep-my-model-id");
  await expect(page.locator('select[name="connection"]')).toContainText("Keep My Connection Name");
  expect(posts).toBe(1); expect(api.mutations).toEqual([]);
  await page.screenshot({ path: info.outputPath("model-save-language-switch.png") });
});

test("automation feedback changes language without triggering the accepted work again", async ({ page, context, api }) => {
  const item: Automation = { id: "77777777-7777-4777-8777-777777777777", created_by: "fixture-user", name: "Keep my automation name", kind: "webhook", project_id: "fixture-project", agent_slug: "lester", model_deployment_id: "fixture-model", prompt: "Keep this task content", permission_mode: "standard", enabled: true, cron: "", timezone: "UTC", run_at: null, next_run_at: null, revision: 1, created_at: new Date().toISOString() };
  let triggers = 0;
  await context.route("**/api/v1/automations", route => route.fulfill({ json: { automations: [item] } }));
  await context.route(`**/api/v1/automations/${item.id}/trigger`, route => { triggers++; return route.fulfill({ json: { id: route.request().postDataJSON().id, state: "pending" } }); });
  await context.route(`**/api/v1/automations/${item.id}/deliveries`, route => route.fulfill({ json: { deliveries: [] } }));
  await page.goto("/app/settings/automations");
  await page.getByRole("button", { name: t("立即运行"), exact: true }).click();
  await expect(page.locator(".automation-notice")).toHaveText(t("已加入触发队列"));
  await page.locator(".language-trigger").click(); await page.getByRole("menuitemradio", { name: /Français/ }).click();
  await expect(page.locator(".automation-notice")).toHaveText(french("已加入触发队列"));
  await expect(page.getByRole("heading", { name: item.name, exact: true })).toBeVisible();
  await expect(page.locator(".automation-prompt")).toHaveText(item.prompt);
  expect(triggers).toBe(1); expect(api.mutations).toEqual([]);
});
