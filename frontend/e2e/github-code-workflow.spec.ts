import { test as base, expect } from "@playwright/test";
import { readFileSync } from "node:fs";
import { WorkspaceApi, conversationId } from "./workspace-api";
import { appProviders, type AppConnection } from "../src/lib/app-connections";

const origin = "http://127.0.0.1:13020";
const test = base.extend<{ api: WorkspaceApi }>({ api: async ({ context, page }, run) => {
  const api = new WorkspaceApi(), errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await api.install(context);
  await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", url: origin }]);
  try { await run(api); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); } finally { await api.close(); }
} });
function connections(code = false): AppConnection[] { return appProviders.map(provider => ({ provider, configured: true, status: provider === "github" ? "connected" : "disconnected", account_name: provider === "github" ? "demo-github" : "", scopes: [], revision: provider === "github" ? 2 : 0, ...(provider === "github" ? { github_code_enabled: code } : {}) })); }

function running(api: WorkspaceApi) {
  const now = new Date().toISOString(); api.currentPermissionMode = "full";
  api.conversation = { id: conversationId, project_id: "fixture-project", workspace_id: "fixture-workspace", created_by: "22222222-2222-4222-8222-222222222222", agent_slug: "lester", agent_name: "Lester", model_deployment_id: "fixture-model", title: "修复 GitHub 登录流程", run_id: "fixture-run", run_status: "running", pinned: false, created_at: now, updated_at: now };
  api.messages = [{ id: "github-code-input", role: "user", run_id: "fixture-run", content: "修复 acme/site，运行测试后推送工作分支并创建 PR", created_at: now }];
}
const permission = { id: "88888888-8888-4888-8888-888888888888", run_id: "fixture-run", tool: "github_publish", request_digest: "d".repeat(64), status: "pending", allow_run: false, summary: { repository: "acme/site", branch: "lester/task-fixture", summary: "Update login form · 2 files", proposal_id: "99999999-9999-4999-8999-999999999999", digest: "e".repeat(64) } };
const prefix = `/api/v1/conversations/${conversationId}/permissions`;

test("GitHub code access is explicit and ordinary reconnect retains it", async ({ page, context }, info) => {
  const rows = connections(), posts: Record<string, unknown>[] = [];
  await context.route("**/api/v1/connectors", route => route.fulfill({ json: { connections: rows } }));
  await context.route("**/api/v1/connectors/github/connect", route => {
    const body = route.request().postDataJSON(); posts.push(body);
    expect(body).toEqual({ revision: rows[0].revision, return_to: "/app", github_code: true });
    const auth = new URL("https://github.com/login/oauth/authorize"); auth.search = new URLSearchParams({ client_id: "fixture-client", state: "s".repeat(43), code_challenge: "p".repeat(43), code_challenge_method: "S256", redirect_uri: `${origin}/api/v1/connectors/github/callback` }).toString();
    return route.fulfill({ json: { url: auth.href } });
  });
  await context.route("https://github.com/login/oauth/authorize?**", route => route.fulfill({ contentType: "text/html", body: `<a href="${origin}/api/v1/connectors/github/callback?code=fixture-code&amp;state=${"s".repeat(43)}">Authorize code tasks fixture</a>` }));
  await context.route("**/api/v1/connectors/github/callback?**", route => { rows[0] = { ...rows[0], github_code_enabled: true, revision: 4 }; return route.fulfill({ status: 303, headers: { location: "/app/settings/connectors?connected=github" } }); });
  await page.goto("/app/settings/connectors");
  await expect(page.locator(".app-github-code")).toHaveCount(1);
  await expect(page.locator(".app-github-code")).not.toHaveAttribute("open");
  await page.locator(".app-github-code > summary").click();
  await expect(page.locator(".app-github-code")).toContainText("实际可用范围以安装授权和仓库权限为准");
  await page.getByRole("button", { name: "授权代码任务", exact: true }).click();
  await expect(page).toHaveURL(/^https:\/\/github.com\/login\/oauth\/authorize/);
  await page.getByRole("link", { name: "Authorize code tasks fixture" }).click();
  await expect(page.locator(".app-github-code > summary")).toContainText("已开启");
  await page.locator(".app-github-code > summary").click();
  await expect(page.getByRole("button", { name: "重新授权代码任务", exact: true })).toBeVisible();
  await page.screenshot({ path: info.outputPath("github-code-authorized.png"), fullPage: true });
  await page.locator(".app-connection-card").first().getByRole("button", { name: "重新连接", exact: true }).click();
  await expect(page).toHaveURL(/^https:\/\/github.com\/login\/oauth\/authorize/);
  expect(posts).toHaveLength(2); expect(posts.every(body => body.github_code === true)).toBe(true);
});

test("unconfigured and native environments cannot start code authorization", async ({ page, context }) => {
  const rows = connections(); rows[0].configured = false;
  await context.route("**/api/v1/connectors", route => route.fulfill({ json: { connections: rows } }));
  await page.goto("/app/settings/connectors"); await page.locator(".app-github-code > summary").click();
  await expect(page.getByRole("button", { name: "授权代码任务", exact: true })).toBeDisabled();
  rows[0].configured = true;
  await context.addInitScript(() => { Object.defineProperty(window, "__LESTER_NATIVE__", { value: { version: 1, platform: "android" } }); Object.defineProperty(window, "LesterAndroid", { value: { postMessage: () => {} } }); });
  await page.reload(); await page.locator(".app-github-code > summary").click();
  await expect(page.getByRole("button", { name: "授权代码任务", exact: true })).toBeDisabled();
  await expect(page.getByText("请在浏览器版中授权应用，完成后 App 会同步连接。", { exact: true })).toBeVisible();
});

test("full access still shows the exact GitHub target and lost publish approval is never repeated", async ({ page, context, api }, info) => {
  running(api); let posts = 0, approved = false;
  await context.route(`**${prefix}`, route => route.fulfill({ json: { permissions: [{ ...permission, ...(approved ? { status: "approved", decision: "once" } : {}) }] } }));
  await context.route(`**${prefix}/${permission.id}/respond`, route => { posts++; expect(route.request().postDataJSON()).toEqual({ request_digest: permission.request_digest, decision: "once" }); approved = true; return route.fulfill({ status: 503, json: { error: "fixture receipt lost" } }); });
  await context.route(`**${prefix}/${permission.id}`, route => route.fulfill({ json: { ...permission, status: "approved", decision: "once" } }));
  await page.goto(`/app/c/${conversationId}`);
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true }); await input.fill("保留我的修改意见");
  await expect(page.getByRole("button", { name: "权限设置", exact: true })).toContainText("完全访问权限");
  const card = page.locator(".permission-card"); await expect(card.getByText("推送 GitHub", { exact: true })).toBeVisible();
  await expect(card).toContainText("acme/site"); await expect(card).toContainText("lester/task-fixture"); await expect(card).toContainText("Update login form · 2 files");
  await expect(card).not.toContainText("不会扩大 Computer 范围");
  await expect(card.getByRole("button", { name: "本次任务完全访问", exact: true })).toHaveCount(0);
  await expect(card.getByRole("button", { name: "本次任务允许此类操作", exact: true })).toHaveCount(0);
  await expect(card.locator(".github-publish-details")).not.toHaveAttribute("open");
  await card.locator(".github-publish-details > summary").click(); await expect(card.locator(".github-publish-details")).toContainText(permission.summary.digest);
  await page.screenshot({ path: info.outputPath("github-publish-approval.png"), fullPage: true });
  await card.getByRole("button", { name: "允许一次", exact: true }).click();
  await expect(card).toHaveCount(0); await expect(input).toHaveValue("保留我的修改意见"); expect(posts).toBe(1);
});

for (const locale of ["zh-CN", "en", "ja", "ko", "fr", "es"]) {
  test(`${locale} code authorization and publish confirmation remain readable`, async ({ page, context, api }, info) => {
    const dictionary = JSON.parse(readFileSync(`src/lib/i18n/messages/${locale}.json`, "utf8")) as Record<string, string>;
    await context.addCookies([{ name: "lester_locale", value: locale, url: origin }]);
    await context.route("**/api/v1/connectors", route => route.fulfill({ json: { connections: connections(true) } }));
    await page.setViewportSize({ width: 320, height: 760 }); await page.goto("/app/settings/connectors");
    await page.locator(".app-github-code > summary").click();
    await expect(page.getByRole("button", { name: dictionary["重新授权代码任务"], exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.goto("/docs/usage#github-code");
    await expect(page.locator('section[aria-labelledby="github-code"]')).toContainText(dictionary["GitHub 代码任务"]);
    running(api); await context.route(`**${prefix}`, route => route.fulfill({ json: { permissions: [permission] } }));
    await page.goto(`/app/c/${conversationId}`);
    await expect(page.locator(".permission-card").getByText(dictionary["推送 GitHub"], { exact: true })).toBeVisible();
    await expect(page.locator(".github-publish-target")).toContainText("acme/site");
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: info.outputPath(`${locale}-github-code-narrow.png`), fullPage: true });
  });
}

const workflowErrors = [
  "GitHub 代码任务参数无效，请检查仓库、分支和文件路径",
  "GitHub 工作目录不可用，请重新选择仓库并创建工作目录",
  "GitHub 仓库或授权已变化，请重新拉取并准备发布",
  "仓库或改动超过代码任务范围，请缩小文件或仓库范围",
  "此代码任务暂不支持符号链接、子模块或修改可执行文件模式",
  "发布预览已失效或不属于本次任务，请重新准备发布",
  "GitHub 操作结果尚未确认，请检查原发布记录，不要重复推送",
  "代码工作目录处理失败，请检查沙箱中的 Git 与 Lester 工具箱",
  "GitHub 代码任务服务暂不可用",
  "GitHub 代码工具需要正在运行的任务",
  "GitHub 代码任务环境不完整",
  "GitHub 代码任务需要 Computer 原生文件传输",
  "GitHub 发布校验暂不可用",
];

for (const locale of ["zh-CN", "en", "ja", "ko", "fr", "es"]) {
  test(`${locale} known GitHub failures are localized while authored text is preserved`, async ({ page, context, api }) => {
    const dictionary = JSON.parse(readFileSync(`src/lib/i18n/messages/${locale}.json`, "utf8")) as Record<string, string>;
    await context.addCookies([{ name: "lester_locale", value: locale, url: origin }]);
    const now = new Date().toISOString();
    running(api);
    api.conversation = { ...api.conversation!, run_status: "completed" };
    api.messages = [{ id: "github-error-authored", role: "user", run_id: "fixture-run", seq: 1, content: `保留原文：${workflowErrors[0]}`, created_at: now }];
    const event = (id: number, type: string, payload: Record<string, unknown> = {}) => ({ id, type, payload, run_id: "fixture-run", conversation_id: conversationId, created_at: now });
    const events = [event(1, "RUN_STARTED"), ...workflowErrors.flatMap((error, index) => [
      event(index * 2 + 2, "TOOL_STARTED", { tool: "github_prepare_publish", tool_call_id: `failure-${index}`, arguments: "{}" }),
      event(index * 2 + 3, "TOOL_FAILED", { tool: "github_prepare_publish", tool_call_id: `failure-${index}`, error }),
    ]), event(workflowErrors.length * 2 + 2, "RUN_COMPLETED")];
    await context.route(`**/api/v1/conversations/${conversationId}/events/history`, route => route.fulfill({ json: { events } }));
    await page.setViewportSize({ width: 320, height: 760 });
    await page.goto(`/app/c/${conversationId}`);
    const failures = page.locator(".conversation-stream .tool-activity.failed");
    await expect(failures).toHaveCount(workflowErrors.length);
    for (let index = 0; index < workflowErrors.length; index++) {
      const failure = failures.nth(index);
      await failure.locator(":scope > summary").click();
      await expect(failure.locator(".tool-activity-details")).toContainText(dictionary[workflowErrors[index]]);
    }
    await expect(page.locator(".chat-message.user")).toContainText(`保留原文：${workflowErrors[0]}`);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    expect(api.mutations).toEqual([]);
  });
}
