import { test as base, expect } from "@playwright/test";
import { readFileSync } from "node:fs";
import { WorkspaceApi, conversationId } from "./workspace-api";
import { appProviders, type AppConnection } from "../src/lib/app-connections";

const origin = "http://127.0.0.1:13020";
const test = base.extend<{ api: WorkspaceApi }>({ api: async ({ context }, run) => {
  const api = new WorkspaceApi(); await api.install(context);
  await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", url: origin }]);
  try { await run(api); expect(api.unexpected).toEqual([]); } finally { await api.close(); }
} });
function rows(): AppConnection[] { return appProviders.map(provider => ({ provider, configured: true, status: "disconnected", account_name: "", scopes: [], revision: 0 })); }
function connected(): AppConnection[] { const values = rows(); values[0] = { ...values[0], status: "connected", account_name: "demo-github", scopes: ["contents:read", "issues:read"], revision: 2 }; return values; }

test("cards have logos, honest availability and keyboard-friendly scope disclosure", async ({ page, context }, info) => {
  const values = connected(); values[1].configured = false; values[2].status = "reauthorization_required";
  await context.route("**/api/v1/connectors", route => route.fulfill({ json: { connections: values } }));
  await page.goto("/app/settings/connectors");
  const cards = page.locator(".app-connection-card"); await expect(cards).toHaveCount(5);
  await expect(page.locator(".app-connector-logo svg")).toHaveCount(5);
  await expect(cards.nth(1).getByRole("button", { name: "连接", exact: true })).toBeDisabled();
  await expect(cards.nth(2)).toContainText("需要重新授权");
  await expect(cards.first().locator("details")).not.toHaveAttribute("open", "");
  await cards.first().locator("summary").focus(); await page.keyboard.press("Enter");
  await expect(cards.first().getByText("contents:read · issues:read")).toBeVisible();
  await expect(page.getByRole("link", { name: "连接帮助" })).toHaveAttribute("href", "/docs/usage#app-connections");
  await page.screenshot({ path: info.outputPath("connections.png"), fullPage: true });
});

test("Connect opens a provider once and returns without confusing app grants with sign-in", async ({ page, context, api }) => {
  const values = rows(); let starts = 0;
  const source = `/app/c/${conversationId}`, now = new Date().toISOString();
  api.conversation = { id: conversationId, project_id: "fixture-project", workspace_id: "fixture-workspace", created_by: "22222222-2222-4222-8222-222222222222", agent_slug: "lester", agent_name: "Lester", model_deployment_id: "fixture-model", title: "连接资料的原任务", run_status: "completed", pinned: false, created_at: now, updated_at: now };
  await context.route("**/api/v1/connectors", route => route.fulfill({ json: { connections: values } }));
  await context.route("**/api/v1/connectors/github/connect", async route => {
    starts++; expect(route.request().method()).toBe("POST"); expect(route.request().postDataJSON()).toEqual({ revision: 0, return_to: source });
    const auth = new URL("https://github.com/login/oauth/authorize"); auth.search = new URLSearchParams({ client_id: "fixture-client", state: "s".repeat(43), code_challenge: "p".repeat(43), code_challenge_method: "S256", redirect_uri: `${origin}/api/v1/connectors/github/callback` }).toString();
    await route.fulfill({ json: { url: auth.href } });
  });
  await context.route("https://github.com/login/oauth/authorize?**", route => route.fulfill({ contentType: "text/html", body: `<html><body><p>Provider authorization fixture</p><a href="${origin}/api/v1/connectors/github/callback?code=fixture-code&amp;state=${"s".repeat(43)}">Authorize read access</a></body></html>` }));
  await context.route("**/api/v1/connectors/github/callback?**", route => {
    values[0] = connected()[0]; return route.fulfill({ status: 303, headers: { location: `/app/settings/connectors?connected=github&returnTo=${encodeURIComponent(source)}` } });
  });
  await page.goto(source); await page.getByRole("textbox", { name: "消息输入框", exact: true }).fill("保留未发送的任务草稿");
  await page.goto(`/app/settings/connectors?returnTo=${encodeURIComponent(source)}`);
  await page.locator(".app-connection-card").first().getByRole("button", { name: "连接", exact: true }).click();
  await expect(page).toHaveURL(/^https:\/\/github.com\/login\/oauth\/authorize/);
  await page.getByRole("link", { name: "Authorize read access" }).click();
  await expect(page.getByRole("status")).toContainText("GitHub 已连接"); expect(starts).toBe(1);
  await expect(page).toHaveURL(`/app/settings/connectors?returnTo=${encodeURIComponent(source)}`);
  await page.getByRole("button", { name: "返回工作区", exact: true }).click();
  await expect(page).toHaveURL(source); await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toHaveValue("保留未发送的任务草稿");
  expect(api.mutations).toEqual([]);
});

test("canceled authorization is explicit and failed metadata can be retried without mutations", async ({ page, context }) => {
  let failed = true, mutations = 0;
  await context.route("**/api/v1/connectors", route => { if (route.request().method() !== "GET") mutations++; return route.fulfill(failed ? { status: 503, json: { error: "应用连接状态读取失败，请重试。" } } : { json: { connections: rows() } }); });
  await page.goto("/app/settings/connectors?connection_error=access_denied");
  await expect(page.locator(".app-connections-page").getByRole("alert")).toContainText("读取失败"); await expect(page.locator(".app-connection-card")).toHaveCount(0);
  failed = false; await page.getByRole("button", { name: "刷新状态", exact: true }).click();
  await expect(page.locator(".app-connection-card")).toHaveCount(5);
  await page.goto("/app/settings/connectors?connection_error=access_denied");
  await expect(page.locator(".app-connections-page").getByRole("alert")).toContainText("你取消了应用授权"); expect(mutations).toBe(0);
});

test("disconnect cancel restores focus; a confirmed receipt needs no subsequent successful read", async ({ page, context }, info) => {
  const values = connected(); let deletes = 0, failRead = false;
  await context.route("**/api/v1/connectors", route => route.fulfill(failRead ? { status: 503, json: { error: "应用连接状态读取失败，请重试。" } } : { json: { connections: values } }));
  await context.route("**/api/v1/connectors/github", route => { expect(route.request().method()).toBe("DELETE"); expect(route.request().postDataJSON()).toEqual({ revision: 2 }); deletes++; failRead = true; return route.fulfill({ status: 204 }); });
  await page.goto("/app/settings/connectors"); const button = page.locator(".app-connection-card").first().getByRole("button", { name: "断开", exact: true });
  await button.click(); const dialog = page.getByRole("dialog", { name: "断开 GitHub？" }); await expect(dialog).toBeVisible();
  await page.keyboard.press("Escape"); await expect(dialog).toHaveCount(0); await expect(button).toBeFocused(); expect(deletes).toBe(0);
  await button.click(); await page.screenshot({ path: info.outputPath("disconnect.png"), fullPage: true });
  await dialog.getByRole("button", { name: "确认断开", exact: true }).click();
  await expect(dialog).toHaveCount(0); await expect(page.getByRole("status")).toContainText("已断开 GitHub");
  await expect(page.locator(".app-connection-card").first()).not.toContainText("demo-github"); expect(deletes).toBe(1);
});

test("lost disconnect responses reconcile with one read instead of repeating DELETE", async ({ page, context }) => {
  const values = connected(); let deletes = 0;
  await context.route("**/api/v1/connectors", route => route.fulfill({ json: { connections: values } }));
  await context.route("**/api/v1/connectors/github", route => { deletes++; values[0] = { ...rows()[0], revision: 3 }; return route.abort("connectionfailed"); });
  await page.goto("/app/settings/connectors"); await page.locator(".app-connection-card").first().getByRole("button", { name: "断开", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "确认断开", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0); await expect(page.getByRole("status")).toContainText("已断开 GitHub"); expect(deletes).toBe(1);
});

test("stale disconnect does not claim success or remove a newer account", async ({ page, context }) => {
  const values = connected(); let deletes = 0;
  await context.route("**/api/v1/connectors", route => route.fulfill({ json: { connections: values } }));
  await context.route("**/api/v1/connectors/github", route => { deletes++; values[0] = { ...values[0], account_name: "new-account", revision: 4 }; return route.fulfill({ status: 409, json: { error: "应用连接状态已变化，请刷新后重试" } }); });
  await page.goto("/app/settings/connectors"); await page.locator(".app-connection-card").first().getByRole("button", { name: "断开", exact: true }).click();
  const dialog = page.getByRole("dialog"); await dialog.getByRole("button", { name: "确认断开", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("状态已变化"); await expect(page.locator(".app-connection-card").first()).toContainText("new-account"); expect(deletes).toBe(1);
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
});

test("native shells explain browser authorization while showing server-side connections", async ({ page, context }) => {
  await context.addInitScript(() => {
    Object.defineProperty(window, "__LESTER_NATIVE__", { value: { version: 1, platform: "android" } });
    Object.defineProperty(window, "LesterAndroid", { value: { postMessage: () => {} } });
  });
  await context.route("**/api/v1/connectors", route => route.fulfill({ json: { connections: connected() } }));
  await page.goto("/app/settings/connectors");
  await expect(page.getByText("请在浏览器版中授权应用，完成后 App 会同步连接。", { exact: true })).toBeVisible();
  await expect(page.locator(".app-connection-card").first()).toContainText("demo-github");
  await expect(page.locator(".app-connection-card").first().getByRole("button", { name: "重新连接", exact: true })).toBeDisabled();
});

for (const locale of ["zh-CN", "en", "ja", "ko", "fr", "es"]) {
  test(`${locale} app settings and authored help fit a narrow viewport`, async ({ page, context }, info) => {
    const dictionary = JSON.parse(readFileSync(`src/lib/i18n/messages/${locale}.json`, "utf8")) as Record<string, string>;
    await context.addCookies([{ name: "lester_locale", value: locale, url: origin }]);
    await context.route("**/api/v1/connectors", route => route.fulfill({ json: { connections: connected() } }));
    await page.setViewportSize({ width: 320, height: 740 }); await page.goto("/app/settings/connectors");
    await expect(page.getByRole("heading", { name: dictionary["应用连接"], exact: true })).toBeVisible();
    await expect(page.locator(".app-connection-card")).toHaveCount(5);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await page.screenshot({ path: info.outputPath(`${locale}-connections-narrow.png`), fullPage: true });
    await page.goto("/docs/usage#app-connections");
    await expect(page.locator("#app-connections")).toContainText(dictionary["应用连接"]);
    if (locale !== "zh-CN" && locale !== "ja") await expect(page.locator("#app-connections")).not.toContainText("管理员需");
  });
}
