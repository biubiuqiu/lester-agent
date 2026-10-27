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
  const scopes = cards.first().locator(".app-connection-scopes");
  await expect(scopes).not.toHaveAttribute("open", "");
  await scopes.locator("summary").focus(); await page.keyboard.press("Enter");
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
  await expect(page.locator(".app-connections-page").getByRole("alert")).toContainText("你取消了应用授权"); expect(mutations).toBe(0);
});

test("authorization success survives a failed initial metadata read and is confirmed on retry", async ({ page, context }) => {
  let failed = true, mutations = 0;
  await context.route("**/api/v1/connectors", route => { if (route.request().method() !== "GET") mutations++; return route.fulfill(failed ? { status: 503, json: { error: "应用连接状态读取失败，请重试。" } } : { json: { connections: connected() } }); });
  await page.goto("/app/settings/connectors?connected=github");
  await expect(page.locator(".app-connections-page").getByRole("alert")).toContainText("读取失败");
  await expect(page.locator(".app-connection-notice")).toHaveCount(0);
  failed = false; await page.getByRole("button", { name: "刷新状态", exact: true }).click();
  await expect(page.locator(".app-connection-notice")).toContainText("GitHub 已连接");
  await expect(page.locator(".app-connection-card").first()).toContainText("demo-github"); expect(mutations).toBe(0);
});

test("a superseded initial read cannot confirm an already disconnected app", async ({ page, context }) => {
  let reads = 0, release!: () => void;
  const held = new Promise<void>(resolve => { release = resolve; });
  await context.route("**/api/v1/connectors", async route => {
    reads++;
    if (reads === 1) { await held; return route.fulfill({ json: { connections: connected() } }); }
    return route.fulfill({ json: { connections: rows() } });
  });
  try {
    await page.goto("/app/settings/connectors?connected=github");
    await expect(page.locator(".app-connections-page").getByRole("heading", { name: "应用连接", exact: true })).toBeVisible();
    await expect.poll(() => reads).toBe(1);
    await page.evaluate(() => window.dispatchEvent(new Event("focus")));
    await expect(page.locator(".app-connection-card")).toHaveCount(5);
    const response = page.waitForResponse(value => new URL(value.url()).pathname === "/api/v1/connectors");
    release(); await (await response).finished();
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
    await expect(page.locator(".app-connection-card").first()).toContainText("未连接");
    await expect(page.locator(".app-connection-notice")).toHaveCount(0);
  } finally { release(); }
});

test("an older failed refresh cannot replace a newer successful connection read", async ({ page, context }) => {
  let reads = 0, release!: () => void;
  const held = new Promise<void>(resolve => { release = resolve; });
  await context.route("**/api/v1/connectors", async route => {
    reads++;
    if (reads === 2) { await held; return route.fulfill({ status: 503, json: { error: "应用连接状态读取失败，请重试。" } }); }
    const values = connected(); if (reads > 2) values[0] = { ...values[0], account_name: "new-account", revision: 4 };
    return route.fulfill({ json: { connections: values } });
  });
  try {
    await page.goto("/app/settings/connectors"); await expect(page.locator(".app-connection-card").first()).toContainText("demo-github");
    await page.evaluate(() => window.dispatchEvent(new Event("focus")));
    await expect.poll(() => reads).toBe(2);
    await page.getByRole("button", { name: "刷新连接状态", exact: true }).click();
    await expect(page.locator(".app-connection-card").first()).toContainText("new-account");
    const response = page.waitForResponse(value => new URL(value.url()).pathname === "/api/v1/connectors");
    release(); await (await response).finished();
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
    await expect(page.locator(".app-connections-page").getByRole("alert")).toHaveCount(0);
    await expect(page.locator(".app-connection-card").first()).toContainText("new-account");
  } finally { release(); }
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
  await expect(page.locator(".app-connection-card").first()).not.toContainText("demo-github");
  await expect(page.locator(".app-connection-card").first().getByRole("button", { name: "连接", exact: true })).toBeFocused(); expect(deletes).toBe(1);
});

test("lost disconnect responses reconcile with one read instead of repeating DELETE", async ({ page, context }) => {
  const values = connected(); let deletes = 0;
  await context.route("**/api/v1/connectors", route => route.fulfill({ json: { connections: values } }));
  await context.route("**/api/v1/connectors/github", route => { deletes++; values[0] = { ...rows()[0], revision: 3 }; return route.abort("connectionfailed"); });
  await page.goto("/app/settings/connectors"); await page.locator(".app-connection-card").first().getByRole("button", { name: "断开", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "确认断开", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0); await expect(page.getByRole("status")).toContainText("已断开 GitHub"); expect(deletes).toBe(1);
});

test("stale disconnect requires reviewing the current account before a fresh confirmation", async ({ page, context }) => {
  const values = connected(); const revisions: number[] = [];
  await context.route("**/api/v1/connectors", route => route.fulfill({ json: { connections: values } }));
  await context.route("**/api/v1/connectors/github", route => {
    revisions.push(route.request().postDataJSON().revision);
    if (revisions.length === 1) { values[0] = { ...values[0], account_name: "new-account", revision: 4 }; return route.fulfill({ status: 409, json: { error: "应用连接状态已变化，请刷新后重试" } }); }
    expect(route.request().postDataJSON()).toEqual({ revision: 4 }); values[0] = { ...rows()[0], revision: 5 }; return route.fulfill({ status: 204 });
  });
  await page.goto("/app/settings/connectors"); const card = page.locator(".app-connection-card").first();
  await card.getByRole("button", { name: "断开", exact: true }).click();
  const dialog = page.getByRole("dialog"); await expect(dialog).toContainText("demo-github");
  await dialog.getByRole("button", { name: "确认断开", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("请关闭此窗口并重新查看");
  await expect(dialog.getByRole("button", { name: "确认断开", exact: true })).toBeDisabled();
  await expect(card).toContainText("new-account"); expect(revisions).toEqual([2]);
  await dialog.getByRole("button", { name: "关闭并查看", exact: true }).click();
  await expect(card.getByRole("button", { name: "断开", exact: true })).toBeFocused();
  await card.getByRole("button", { name: "断开", exact: true }).click();
  await expect(dialog).toContainText("new-account"); expect(revisions).toEqual([2]);
  await dialog.getByRole("button", { name: "确认断开", exact: true }).click();
  await expect(dialog).toHaveCount(0); expect(revisions).toEqual([2, 4]);
});

test("a conflict followed by a failed read cannot reuse the unverified revision", async ({ page, context }) => {
  const values = connected(); let failReads = false, deletes = 0;
  await context.route("**/api/v1/connectors", route => route.fulfill(failReads ? { status: 503, json: { error: "应用连接状态读取失败，请重试。" } } : { json: { connections: values } }));
  await context.route("**/api/v1/connectors/github", route => { deletes++; values[0] = { ...values[0], account_name: "new-account", revision: 4 }; failReads = true; return route.fulfill({ status: 409, json: { error: "应用连接状态已变化，请刷新后重试" } }); });
  await page.goto("/app/settings/connectors"); const card = page.locator(".app-connection-card").first();
  await card.getByRole("button", { name: "断开", exact: true }).click();
  const dialog = page.getByRole("dialog"); await dialog.getByRole("button", { name: "确认断开", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "确认断开", exact: true })).toBeDisabled();
  await dialog.getByRole("button", { name: "关闭并查看", exact: true }).click();
  await expect(card.getByRole("button", { name: "断开", exact: true })).toBeDisabled();
  await expect(page.locator(".app-connections-page").getByRole("alert")).toContainText("读取失败");
  failReads = false; await page.getByRole("button", { name: "刷新状态", exact: true }).click();
  await expect(card).toContainText("new-account"); await card.getByRole("button", { name: "断开", exact: true }).click();
  await expect(dialog).toContainText("new-account"); await expect(dialog.getByRole("button", { name: "确认断开", exact: true })).toBeEnabled();
  expect(deletes).toBe(1); await dialog.getByRole("button", { name: "取消", exact: true }).click();
});

test("an account changed in another window invalidates an open confirmation", async ({ page, context }) => {
  const values = connected(); let deletes = 0;
  await context.route("**/api/v1/connectors", route => route.fulfill({ json: { connections: values } }));
  await context.route("**/api/v1/connectors/github", route => { deletes++; return route.fulfill({ status: 204 }); });
  await page.goto("/app/settings/connectors"); const card = page.locator(".app-connection-card").first();
  await card.getByRole("button", { name: "断开", exact: true }).click();
  const dialog = page.getByRole("dialog"); await expect(dialog).toContainText("demo-github");
  values[0] = { ...values[0], account_name: "new-account", revision: 4 };
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect(dialog.getByRole("alert")).toContainText("请关闭此窗口并重新查看");
  await expect(dialog.getByRole("button", { name: "确认断开", exact: true })).toBeDisabled(); expect(deletes).toBe(0);
  await dialog.getByRole("button", { name: "关闭并查看", exact: true }).click();
  await card.getByRole("button", { name: "断开", exact: true }).click(); await expect(dialog).toContainText("new-account");
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
});

test("native shells explain browser authorization while showing server-side connections", async ({ page, context }) => {
  await context.addInitScript(() => {
    Object.defineProperty(window, "__LESTER_NATIVE__", { value: { version: 1, platform: "android" } });
    Object.defineProperty(window, "LesterAndroid", { value: { postMessage: () => {} } });
  });
  await context.route("**/api/v1/connectors", route => route.fulfill({ json: { connections: connected() } }));
  await context.route("**/api/v1/connectors/github", route => route.fulfill({ status: 204 }));
  await page.goto("/app/settings/connectors");
  await expect(page.getByText("请在浏览器版中授权应用，完成后 App 会同步连接。", { exact: true })).toBeVisible();
  await expect(page.locator(".app-connection-card").first()).toContainText("demo-github");
  await expect(page.locator(".app-connection-card").first().getByRole("button", { name: "重新连接", exact: true })).toBeDisabled();
  await page.locator(".app-connection-card").first().getByRole("button", { name: "断开", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "确认断开", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.locator(".app-connection-card").first()).toBeFocused();
  await expect(page.locator(".app-connection-card").first().getByRole("button", { name: "连接", exact: true })).toBeDisabled();
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
    await context.clearCookies({ name: "lester_access_token" });
    await page.goto("/docs/troubleshooting#capabilities");
    await expect(page.locator('section[aria-labelledby="capabilities"]')).toContainText(dictionary['当前支持个人助理记忆、定时任务、通用入站 Webhook 和五种应用连接。应用默认用于搜索和读取；GitHub 可另外授权代码任务，在沙箱中修改、测试并推送分支或创建 PR。个人助理负责记忆与安排，具体工作由独立任务 Agent 执行。任务 Agent 也可使用 Computer 中已安装的 Playwright，没有专用浏览器界面。执行器仍在 API 内，尚无独立 Worker；中断后不会自动重放工具。']);
  });
}
