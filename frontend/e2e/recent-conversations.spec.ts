import { test as base, expect, type Page } from "@playwright/test";
import type { Conversation, Project } from "../src/lib/api";
import { WorkspaceApi, conversationId } from "./workspace-api";

type HistoryFixture = { api: WorkspaceApi; items: Conversation[]; projects: Project[] };
const test = base.extend<{ history: HistoryFixture }>({ history: async ({ context, page }, runFixture) => {
  const api = new WorkspaceApi(), errors: string[] = [];
  const main: Conversation = { id: conversationId, workspace_id: "fixture-workspace", created_by: "22222222-2222-4222-8222-222222222222", project_id: "product-project", agent_slug: "lester", agent_name: "Lester", model_deployment_id: "fixture-model", title: "整理季度报告", pinned: false, run_status: "idle", created_at: "2026-10-01T09:00:00Z", updated_at: "2026-10-17T09:00:00Z" };
  const items = [main, ...Array.from({ length: 24 }, (_, index) => ({ ...main, id: `33333333-3333-4333-8333-${String(index).padStart(12, "0")}`, project_id: index === 0 ? "product-project" : index === 23 ? "missing-project" : "fixture-project", title: index === 0 ? "检查发布前的页面细节" : index === 23 ? "未归属资料" : `历史记录 ${index}`, pinned: index === 22, updated_at: index === 0 ? "2026-10-18T08:00:00Z" : new Date(Date.UTC(2026, 9, 16, 10) - index * 3600000).toISOString() }))];
  const projects: Project[] = [{ id: "fixture-project", name: "个人项目", is_default: true, pinned: false, conversation_count: 22, created_at: main.created_at }, { id: "product-project", name: "Lester 官网", is_default: false, pinned: false, conversation_count: 2, created_at: main.created_at }];
  api.conversation = main;
  await api.install(context);
  await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", url: "http://127.0.0.1:13020" }]);
  await context.route("**/api/v1/conversations", route => route.request().method() === "GET" ? route.fulfill({ json: { conversations: items } }) : route.fallback());
  await context.route("**/api/v1/projects", route => route.fulfill({ json: { projects } }));
  await context.route("**/api/v1/projects/product-project", route => {
    expect(route.request().method()).toBe("PATCH"); const body = route.request().postDataJSON();
    api.mutations.push({ path: "/api/v1/projects/product-project", body }); projects[1].name = body.name;
    return route.fulfill({ json: projects[1] });
  });
  await context.route(`**/api/v1/conversations/${conversationId}/organization`, route => {
    expect(route.request().method()).toBe("PATCH"); const body = route.request().postDataJSON();
    api.mutations.push({ path: `/api/v1/conversations/${conversationId}/organization`, body }); main.project_id = body.project_id;
    return route.fulfill({ json: main });
  });
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => { if (message.type() === "error") errors.push(message.text()); });
  try { await runFixture({ api, items, projects }); expect(errors).toEqual([]); expect(api.unexpected).toEqual([]); }
  finally { await api.close(); }
} });

async function openRecent(page: Page, mobile: boolean) {
  if (mobile) await page.getByRole("button", { name: "打开会话栏", exact: true }).click();
  const trigger = page.getByRole("button", { name: "最近会话", exact: true });
  await trigger.click();
  const dialog = page.getByRole("dialog", { name: "最近会话", exact: true });
  await expect(dialog.getByRole("searchbox", { name: "搜索会话或项目", exact: true })).toBeFocused();
  return { trigger, dialog };
}

test("recent conversations sort across projects, search both names and bound the initial list", async ({ page, history }, info) => {
  await page.goto("/app/new"); await expect(page).toHaveTitle(/Lester/);
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true }); await input.fill("还没发出的目标");
  const { trigger, dialog } = await openRecent(page, info.project.name === "mobile");
  const rows = dialog.locator(".recent-conversation-row"), search = dialog.getByRole("searchbox");
  await expect(rows).toHaveCount(20); await expect(rows.first().locator("strong")).toHaveText("检查发布前的页面细节");
  await expect(rows.first().locator("small")).toHaveText("Lester 官网");
  await expect(rows.nth(1).locator("strong")).toHaveText("整理季度报告");
  await expect(dialog.getByRole("button", { name: "云端任务", exact: true })).toHaveCount(0);
  await page.screenshot({ path: info.outputPath("recent-conversations.png") });
  await dialog.getByRole("button", { name: "显示更多会话", exact: true }).click(); await expect(rows).toHaveCount(25);
  await expect(rows.nth(23)).toContainText("历史记录 22"); // A pinned old conversation stays in activity order.
  await search.fill("  lEsTeR  "); await expect(rows).toHaveCount(2);
  await expect(rows.locator("small")).toHaveText(["Lester 官网", "Lester 官网"]);
  await search.fill("季度"); await expect(rows).toHaveCount(1); await expect(rows).toContainText("整理季度报告");
  await search.fill("未归属"); await expect(rows.locator("small")).toHaveText("项目不可用");
  await search.fill("没有这样的会话"); await expect(dialog.getByRole("status")).toHaveText("没有找到匹配的会话");
  await search.fill(""); await expect(rows).toHaveCount(20);
  expect(await dialog.evaluate(node => node.scrollWidth <= node.clientWidth + 1)).toBe(true);
  await page.keyboard.press("Escape"); await expect(dialog).toHaveCount(0); await expect(trigger).toBeFocused();
  await expect(input).toHaveValue("还没发出的目标"); expect(history.api.mutations).toEqual([]);
});

test("opening an existing conversation keeps project and conversation drafts without starting a task", async ({ page, history }, info) => {
  await page.goto("/app/new");
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true }); await input.fill("新任务的未发送草稿");
  let panel = await openRecent(page, info.project.name === "mobile");
  await panel.dialog.getByRole("button", { name: /整理季度报告/ }).click();
  await expect(page).toHaveURL(`/app/c/${conversationId}`); await expect(input).toHaveValue("");
  await input.fill("这个会话的未发送反馈");
  panel = await openRecent(page, info.project.name === "mobile");
  const current = panel.dialog.getByRole("button", { name: /整理季度报告/ });
  await expect(current).toHaveAttribute("aria-current", "page"); await expect(current).toContainText("Lester 官网");
  await current.click(); await expect(input).toHaveValue("这个会话的未发送反馈");
  await page.goBack(); await expect(page).toHaveURL(/\/app\/new$/); await expect(input).toHaveValue("新任务的未发送草稿");
  expect(history.api.mutations).toEqual([]);
});

test("project renames and conversation moves immediately update recent project labels", async ({ page, history }, info) => {
  await page.goto("/app/new");
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true }); await input.fill("项目操作时保留草稿");
  if (info.project.name === "mobile") await page.getByRole("button", { name: "打开会话栏", exact: true }).click();
  await page.getByRole("button", { name: "项目 Lester 官网 的操作", exact: true }).click();
  await page.getByRole("button", { name: "重命名项目", exact: true }).click();
  await page.getByRole("textbox", { name: "项目名称", exact: true }).fill("Lester 产品设计");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  let panel = await openRecent(page, false);
  await expect(panel.dialog.getByRole("button", { name: /整理季度报告/ }).locator("small")).toHaveText("Lester 产品设计");
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "会话 整理季度报告 的操作", exact: true }).click();
  await page.getByRole("combobox", { name: "移动会话 整理季度报告", exact: true }).selectOption("fixture-project");
  panel = await openRecent(page, false);
  await expect(panel.dialog.getByRole("button", { name: /整理季度报告/ }).locator("small")).toHaveText("个人项目");
  await page.keyboard.press("Escape"); await expect(input).toHaveValue("项目操作时保留草稿");
  expect(history.api.mutations.map(item => item.path)).toEqual(["/api/v1/projects/product-project", `/api/v1/conversations/${conversationId}/organization`]);
});

test("existing workspace events update recent order and unread state without stealing search focus", async ({ page, history }, info) => {
  await page.goto("/app/new");
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true }); await input.fill("运行更新时保留草稿");
  await expect.poll(() => history.api.streamCount).toBe(1);
  const { dialog } = await openRecent(page, info.project.name === "mobile");
  await expect(dialog.locator(".recent-conversation-row").first()).toContainText("检查发布前的页面细节");
  history.api.finish();
  const first = dialog.locator(".recent-conversation-row").first();
  await expect(first).toContainText("整理季度报告"); await expect(first.getByRole("status")).toHaveAttribute("aria-label", "有一个后台任务已完成");
  await expect(dialog.getByRole("searchbox")).toBeFocused(); await expect(input).toHaveValue("运行更新时保留草稿");
  expect(history.api.streamCount).toBe(1); expect(history.api.mutations).toEqual([]);
});

test("empty recent history and an empty search remain distinct without creating conversations", async ({ page, history }, info) => {
  history.items.length = 0; history.api.conversation = null;
  await page.goto("/app/new");
  const { dialog } = await openRecent(page, info.project.name === "mobile");
  await expect(dialog.getByRole("status")).toHaveText("还没有会话，发送一个目标开始吧。");
  await dialog.getByRole("searchbox").fill("我的项目");
  await expect(dialog.getByRole("status")).toHaveText("没有找到匹配的会话");
  expect(history.api.mutations).toEqual([]);
});
