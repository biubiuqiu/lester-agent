import { test as base, expect } from "@playwright/test";
import { WorkspaceApi, conversationId } from "./workspace-api";
import type { PersonalState } from "../src/lib/personal";

const test = base.extend<{ api: WorkspaceApi }>({ api: async ({ context }, run) => {
  const api = new WorkspaceApi(); await api.install(context);
  await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", url: "http://127.0.0.1:13020" }]);
  try { await run(api); expect(api.unexpected).toEqual([]); } finally { await api.close(); }
} });

test("keyboard account navigation returns from settings to the original task and draft", async ({ page, api }, info) => {
  const now = new Date().toISOString(), source = `/app/c/${conversationId}`;
  api.conversation = { id: conversationId, project_id: "fixture-project", workspace_id: "fixture-workspace", created_by: "22222222-2222-4222-8222-222222222222", agent_slug: "lester", agent_name: "Lester", model_deployment_id: "fixture-model", title: "继续编辑介绍页", run_status: "completed", pinned: false, created_at: now, updated_at: now };
  await page.goto(source);
  const draft = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await draft.fill("调整之前的网页，暂未发送");
  if (info.project.name === "mobile") await page.getByRole("button", { name: "打开会话栏", exact: true }).click();
  const trigger = page.locator(".user-menu-trigger");
  await trigger.focus(); await page.keyboard.press("ArrowDown");
  const items = page.locator('.user-menu-items [role="menuitem"]');
  await expect(items.first()).toBeFocused();
  await page.keyboard.press("ArrowDown"); await expect(items.nth(1)).toBeFocused();
  await page.keyboard.press("End"); await expect(page.locator(".user-menu-logout")).toBeFocused();
  await page.keyboard.press("Escape"); await expect(trigger).toBeFocused();
  await page.keyboard.press("ArrowUp"); await expect(page.locator(".user-menu-logout")).toBeFocused();
  await page.getByRole("menuitem", { name: "模型", exact: true }).click();
  await expect(page).toHaveURL(`/app/settings/models?returnTo=${encodeURIComponent(source)}`);
  await page.locator(".settings-sidebar nav").getByRole("button", { name: "Skill 广场", exact: true }).click();
  await expect(page).toHaveURL(`/app/settings/skills?returnTo=${encodeURIComponent(source)}`);
  await page.getByRole("button", { name: "返回工作区", exact: true }).click();
  await expect(page).toHaveURL(source); await expect(draft).toHaveValue("调整之前的网页，暂未发送");
  await page.screenshot({ path: info.outputPath("01-settings-return-to-task.png") });
  expect(api.mutations).toEqual([]);
});

test("reopening the assistant refreshes other-window changes and recovers a failed initial read", async ({ page, context, api }, info) => {
  const state: PersonalState = { turns: [], has_more: false, memories: [], tasks: [], auto_memory: true };
  let fail = true, reads = 0;
  await context.route("**/api/v1/personal", route => { reads++; return route.fulfill(fail ? { status: 503, json: { error: "暂时无法读取助理对话" } } : { json: state }); });
  await page.goto("/app"); await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeVisible();
  await page.locator(".personal-dock-launcher").click();
  const panel = page.getByRole("dialog", { name: "个人助理", exact: true }), input = panel.getByRole("textbox", { name: "个人助理消息", exact: true });
  await expect(panel.getByRole("alert")).toContainText("暂时无法读取助理对话");
  await input.fill("留着下次发送的助理草稿");
  await panel.getByRole("button", { name: "收起个人助理", exact: true }).click();
  fail = false;
  state.turns.push({ id: "other-window-turn", seq: 1, content: "在另一个窗口安排工作", reply: "已经记下最新安排", status: "completed", changes: [], created_at: new Date().toISOString() });
  await page.locator(".personal-dock-launcher").click();
  await expect(panel.getByText("已经记下最新安排", { exact: true })).toBeVisible();
  await expect(panel.getByRole("alert")).toHaveCount(0);
  await expect(input).toHaveValue("留着下次发送的助理草稿");
  await expect(panel.getByRole("button", { name: "发送给个人助理", exact: true })).toBeEnabled();
  expect(reads).toBe(2); expect(api.mutations).toEqual([]);
  await page.screenshot({ path: info.outputPath("02-assistant-refreshed.png") });
});

test("memory search has an empty result and short-screen editing keeps the whole field usable", async ({ page, context, api }, info) => {
  const state: PersonalState = { turns: [], has_more: false, tasks: [], auto_memory: true, memories: [{ id: "memory-1", project_id: null, kind: "preference", topic: "写作习惯", content: "使用中文回答", evidence: "我希望中文回答", source_turn_id: "turn-1", automatic: false, deleted: false, revision: 1, updated_at: new Date().toISOString() }] };
  await context.route("**/api/v1/personal", route => route.fulfill({ json: state }));
  await page.setViewportSize({ width: 393, height: 360 });
  await page.goto("/app"); await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeVisible();
  await page.locator(".personal-dock-launcher").click(); await page.getByRole("button", { name: "管理记忆", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Lester 的记忆", exact: true }), search = dialog.getByRole("searchbox", { name: "搜索记忆", exact: true });
  await search.fill("不存在的关键词");
  await expect(dialog.getByRole("status")).toHaveText("没有匹配的记忆，试试其他关键词。");
  await search.fill("  中文  "); await expect(dialog.getByRole("button", { name: "编辑", exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "编辑", exact: true }).click();
  const editor = dialog.getByRole("textbox", { name: "编辑记忆内容", exact: true });
  await editor.fill("用中文回答，保留具体事实。".repeat(30));
  await expect.poll(() => editor.evaluate(element => {
    const field = element.getBoundingClientRect(), container = element.closest("dialog")!, header = container.querySelector("header")!.getBoundingClientRect(), bounds = container.getBoundingClientRect();
    return field.top >= header.bottom - 1 && field.bottom <= bounds.bottom + 1;
  })).toBe(true);
  await page.screenshot({ path: info.outputPath("03-memory-edit-short-screen.png") });
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await dialog.getByRole("button", { name: "关闭记忆", exact: true }).click();
  await expect(dialog).toHaveCount(0); expect(api.mutations).toEqual([]);
  await page.screenshot({ path: info.outputPath("04-memory-closed-short-screen.png") });
});
