import { test, expect, type BrowserContext, type Page } from "@playwright/test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { translator, type Locale } from "../src/lib/i18n";
import type { ProjectDeliverable } from "../src/lib/project-workbench";
import { WorkspaceApi, conversationId } from "./workspace-api";

function sample(index: number): ProjectDeliverable {
  return { id: `result-${index}`, conversation_id: conversationId, run_id: "fixture-run", title: index === 0 ? "新版官网" : `项目说明 ${index}`, summary: "用于浏览器回归的固定样例，需要人工验收。", entry_path: index === 0 ? "index.html" : `report-${index}.md`, kind: index === 0 ? "html" : "markdown", content_sha256: "a".repeat(64), created_at: "2026-10-17T09:00:00Z", updated_at: "2026-10-17T09:00:00Z", conversation_title: "官网迭代任务", run_status: "completed" };
}

async function fixture(context: BrowserContext, locale = "zh-CN") {
  const api = new WorkspaceApi();
  api.conversation = { id: conversationId, workspace_id: "fixture-workspace", created_by: "22222222-2222-4222-8222-222222222222", project_id: "fixture-project", agent_slug: "lester", agent_name: "Lester", model_deployment_id: "fixture-model", title: "官网迭代任务", pinned: false, run_status: "completed", created_at: "2026-10-17T09:00:00Z", updated_at: "2026-10-17T09:00:00Z" };
  api.finish(); api.writeFile("report-1.md", "# 固定样例说明\n这不是模型执行。");
  await api.install(context);
  await context.route("**/api/v1/projects/fixture-project/rules", route => route.fulfill({ json: { project_id: "fixture-project", project_name: "个人项目", source: "project_settings", version: 1, sha256: "a".repeat(64), content: "保留项目规范" } }));
  await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", url: "http://127.0.0.1:13020" }, { name: "lester_locale", value: locale, url: "http://127.0.0.1:13020" }]);
  return api;
}

async function openProject(page: Page, compact: boolean) {
  if (compact) await page.getByRole("button", { name: "打开会话栏", exact: true }).click();
  await page.getByRole("button", { name: "打开项目 个人项目", exact: true }).click();
  await expect(page).toHaveURL(/\/app\/p\/fixture-project\/overview$/);
  await expect(page.getByRole("heading", { name: "个人项目", exact: true })).toBeVisible();
}

test("project results preserve project and conversation drafts, preview real source files and continue without sending", async ({ context, page }, info) => {
  const api = await fixture(context), errors: string[] = [], fileReads: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("request", request => { if (new URL(request.url()).pathname.endsWith("/files")) fileReads.push(request.url()); });
  await context.route("**/api/v1/projects/fixture-project/deliverables?*", route => { const params = new URL(route.request().url()).searchParams; return route.fulfill({ json: { deliverables: [sample(0), sample(1)].filter(item => (!params.get("kind") || item.kind === params.get("kind")) && (!params.get("q") || item.title.includes(params.get("q")!))), next_cursor: "" } }); });
  try {
    await page.goto("/app");
    let input = page.getByRole("textbox", { name: "消息输入框", exact: true }); await input.fill("保留项目中新任务的草稿");
    await openProject(page, info.project.name === "mobile");
    await expect(page.locator(".project-result-card")).toHaveCount(2); expect(fileReads).toHaveLength(0); expect(api.mutations).toEqual([]);
    await expect(page).toHaveTitle("Lester"); await expect(page.locator("nextjs-portal")).toHaveCount(0);
    await page.screenshot({ path: info.outputPath("project-results.png") });
    const rulesButton = page.getByRole("button", { name: "项目规则", exact: true }); await rulesButton.click(); const rulesDialog = page.getByRole("dialog", { name: "项目规则", exact: true }); await expect(rulesDialog.getByRole("textbox")).toHaveValue("保留项目规范"); await page.keyboard.press("Escape"); await expect(rulesDialog).toHaveCount(0); await expect(rulesButton).toBeFocused();
    await page.getByRole("combobox", { name: "成果类型", exact: true }).selectOption("markdown"); await expect(page.locator(".project-result-card")).toHaveCount(1); await expect(page.locator(".project-result-card h2")).toHaveText("项目说明 1");
    await page.getByRole("combobox", { name: "成果类型", exact: true }).selectOption(""); await expect(page.locator(".project-result-card")).toHaveCount(2);
    const projectSearch = page.getByRole("textbox", { name: "搜索项目成果", exact: true }); await projectSearch.fill("不存在的成果"); await expect(page.getByRole("heading", { name: "没有匹配的成果", exact: true })).toBeVisible(); await projectSearch.fill(""); await expect(page.locator(".project-result-card")).toHaveCount(2);
    await page.getByRole("link", { name: "新任务", exact: true }).click(); await expect(input).toHaveValue("保留项目中新任务的草稿");
    await page.getByRole("link", { name: "打开项目 个人项目", exact: true }).click();
    await page.getByRole("button", { name: "预览项目成果 新版官网", exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`/app/c/${conversationId}\\?preview=index.html$`));
    await expect(page.locator('iframe[title="index.html preview"]')).toBeVisible();
    await page.keyboard.press("Escape"); await expect(page.locator(".computer-panel")).toBeHidden();
    input = page.getByRole("textbox", { name: "消息输入框", exact: true }); await input.fill("保留来源会话已有的草稿");
    await openProject(page, info.project.name === "mobile");
    await page.getByRole("button", { name: "继续修改项目成果 新版官网", exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`/app/c/${conversationId}$`)); await expect(input).toHaveValue("保留来源会话已有的草稿"); await expect(page.locator(".file-reference")).toContainText("index.html");
    await expect(page.locator(".computer-panel")).toBeHidden(); expect(api.mutations).toEqual([]);
    await openProject(page, info.project.name === "mobile"); await page.getByRole("tab", { name: /^会话/ }).click();
    await expect(page.locator(".project-conversation-list")).toContainText("官网迭代任务"); await expect(page.locator(".project-result-card")).toHaveCount(0);
    expect(errors).toEqual([]); expect(api.unexpected).toEqual([]);
  } finally { await api.close(); }
});

test("project pagination and changed searches reject stale replies without directory scans", async ({ context, page }) => {
  const api = await fixture(context), items = Array.from({ length: 27 }, (_, index) => sample(index));
  let release: (() => void) | undefined, staleDone: (() => void) | undefined, oldStarted = false;
  const gate = new Promise<void>(resolve => { release = resolve; }), delivered = new Promise<void>(resolve => { staleDone = resolve; });
  await context.route("**/api/v1/projects/fixture-project/deliverables?*", async route => {
    const params = new URL(route.request().url()).searchParams;
    if (params.get("q") === "旧") { oldStarted = true; await gate; await route.fulfill({ json: { deliverables: [{ ...sample(0), title: "过期的旧数据" }], next_cursor: "" } }); staleDone!(); return; }
    if (params.get("q") === "新版") return route.fulfill({ json: { deliverables: [sample(0)], next_cursor: "" } });
    return route.fulfill({ json: { deliverables: params.get("cursor") ? items.slice(24) : items.slice(0, 24), next_cursor: params.get("cursor") ? "" : "next-fixture-page" } });
  });
  try {
    await page.goto("/app/p/fixture-project/overview"); await expect(page.locator(".project-result-card")).toHaveCount(24);
    await page.getByRole("button", { name: "加载更多成果", exact: true }).click(); await expect(page.locator(".project-result-card")).toHaveCount(27);
    await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange"))); await expect(page.locator(".project-result-card")).toHaveCount(27);
    const search = page.getByRole("textbox", { name: "搜索项目成果", exact: true }); await search.fill("旧"); await expect.poll(() => oldStarted).toBe(true);
    await search.fill("新版"); await expect(page.locator(".project-result-card")).toHaveCount(1); await expect(page.locator(".project-result-card h2")).toHaveText("新版官网");
    release!(); await delivered; await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
    await expect(page.locator(".project-result-card h2")).toHaveText("新版官网"); expect(api.mutations).toEqual([]); expect(api.unexpected).toEqual([]);
  } finally { release?.(); await api.close(); }
});

test("catalogue failures and missing source files stay explicit and never navigate or start a task", async ({ context, page }, info) => {
  const api = await fixture(context); let unavailable = true;
  await context.route("**/api/v1/projects/fixture-project/deliverables?*", route => route.fulfill(unavailable ? { status: 503, json: { error: "fixture unavailable" } } : { json: { deliverables: [sample(0)], next_cursor: "" } }));
  await context.route(`**/api/v1/conversations/${conversationId}/files?*`, route => route.fulfill({ json: { files: [] } }));
  try {
    await page.goto("/app/p/fixture-project/overview"); await expect(page.locator(".project-workbench-error[role=alert]")).toContainText("项目成果加载失败"); await expect(page.getByText("项目的成果，从这里积累", { exact: true })).toHaveCount(0);
    unavailable = false; await page.getByRole("button", { name: "重试", exact: true }).click(); await expect(page.locator(".project-result-card")).toHaveCount(1);
    await page.getByRole("button", { name: "继续修改项目成果 新版官网", exact: true }).click(); await expect(page.locator(".project-workbench-error[role=alert]")).toContainText("无法打开成果"); await expect(page).toHaveURL(/\/overview$/); expect(api.mutations).toEqual([]);
    await page.getByRole("button", { name: "预览项目成果 新版官网", exact: true }).click(); await expect(page.locator(".project-workbench-error[role=alert]")).toContainText("无法打开成果"); await expect(page).toHaveURL(/\/overview$/);
    await page.screenshot({ path: info.outputPath("project-missing-file.png") }); expect(api.unexpected).toEqual([]);
  } finally { await api.close(); }
});

for (const locale of ["zh-CN", "en", "ja", "ko", "fr", "es"] as Locale[]) {
  test(`project navigation and published-site return work in ${locale}`, async ({ context, page }, info) => {
    const api = await fixture(context, locale), t = translator(locale === "zh-CN" ? {} : JSON.parse(readFileSync(join(process.cwd(), `src/lib/i18n/messages/${locale}.json`), "utf8")));
    const errors: string[] = []; page.on("pageerror", error => errors.push(error.message));
    await context.route("**/api/v1/projects/fixture-project/deliverables?*", route => route.fulfill({ json: { deliverables: [sample(0)], next_cursor: "" } }));
    await context.route("**/api/v1/artifacts", route => route.fulfill({ json: { artifacts: [{ id: "other-site", conversation_id: "33333333-3333-4333-8333-333333333333", project_id: "other-project", name: "其他项目的站点", source_path: "index.html", entry_path: "index.html", version: "fixture-version", status: "published", url: "https://fixture.invalid/site", file_count: 1, size_bytes: 100, created_at: "2026-10-17T09:00:00Z", updated_at: "2026-10-17T09:00:00Z" }] } }));
    try {
      if (info.project.name === "mobile") await page.setViewportSize({ width: 320, height: 740 });
      await page.goto("/app/p/fixture-project/overview"); await expect(page.getByRole("heading", { name: "个人项目", exact: true })).toBeVisible(); await expect(page.locator(".project-result-card")).toHaveCount(1);
      await expect(page.getByRole("link", { name: t("新任务"), exact: true })).toBeVisible();
      const resultTab = page.getByRole("tab", { name: t("成果"), exact: true }); await resultTab.focus(); await resultTab.press("ArrowRight"); await expect(page.getByRole("tab", { name: new RegExp(t("会话")) })).toHaveAttribute("aria-selected", "true");
      await page.keyboard.press("Home"); await expect(resultTab).toHaveAttribute("aria-selected", "true");
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await page.getByRole("link", { name: t("已发布站点"), exact: true }).click(); await expect(page.getByRole("heading", { name: t("已发布站点"), exact: true })).toBeVisible(); await expect(page.getByRole("combobox", { name: t("按项目筛选站点"), exact: true })).toHaveValue("fixture-project"); await expect(page.locator(".artifact-summary>span").first()).toHaveText(`0 ${t("全部站点")}`); await expect(page.getByRole("heading", { name: "其他项目的站点", exact: true })).toHaveCount(0);
      await page.getByRole("link", { name: t("返回工作区"), exact: true }).click(); await expect(page).toHaveURL(/\/app\/p\/fixture-project\/overview$/); await expect(page.getByRole("heading", { name: "个人项目", exact: true })).toBeVisible();
      expect(api.mutations).toEqual([]); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]);
      if (locale === "fr") await page.screenshot({ path: info.outputPath("project-french.png") });
    } finally { await api.close(); }
  });
}
