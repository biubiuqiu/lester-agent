import { test as base, expect } from "@playwright/test";
import { WorkspaceApi } from "./workspace-api";
import type { PersonalState, PersonalTask, PersonalTurn } from "../src/lib/personal";

const test = base.extend<{ api: WorkspaceApi }>({ api: async ({ context }, run) => {
  const api = new WorkspaceApi(); await api.install(context);
  await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", url: "http://127.0.0.1:13020" }, { name: "lester_locale", value: "zh-CN", url: "http://127.0.0.1:13020" }]);
  try { await run(api); expect(api.unexpected).toEqual([]); } finally { await api.close(); }
} });

for (const viewport of [{ width: 390, height: 320 }, { width: 1280, height: 360 }, { width: 320, height: 480 }]) {
  test(`expanded inbox preserves controls and bounds long project labels at ${viewport.width}x${viewport.height}`, async ({ page, context, api }, info) => {
    await page.setViewportSize(viewport);
    const now = new Date().toISOString(), projectName = "LongUnbrokenProjectName".repeat(10), errors: string[] = [];
    page.on("pageerror", error => errors.push(error.message));
    const tasks: PersonalTask[] = Array.from({ length: 10 }, (_, index) => ({ id: `11111111-1111-4111-8111-${String(index + 1).padStart(12, "0")}`, title: `待查看的回复 ${index + 1}`, project_id: "fixture-project", project_name: projectName, status: "completed", run_id: null, reply: "一段待查看的任务回复。", updated_at: now, latest_reply_id: "33333333-3333-4333-8333-333333333333", latest_reply_seq: 2, reply_at: now, unread: true }));
    const state: PersonalState = { turns: [], has_more: false, memories: [], tasks: [], inbox: { tasks, total: tasks.length, next_cursor: "" }, auto_memory: true };
    let sends = 0;
    await context.route("**/api/v1/personal**", route => {
      const path = new URL(route.request().url()).pathname;
      if (route.request().method() === "GET") return route.fulfill({ json: path.endsWith("/tasks") ? { tasks: state.tasks, inbox: state.inbox } : state });
      expect(path).toBe("/api/v1/personal/messages"); sends++;
      const body = route.request().postDataJSON();
      const turn: PersonalTurn = { id: body.id, seq: 1, content: body.content, reply: "保留上下文，可以继续聊。", status: "completed", changes: [], created_at: now };
      state.turns.push(turn); return route.fulfill({ json: turn });
    });
    await page.goto("/app");
    await page.locator(".personal-dock-launcher").click();
    const panel = page.getByRole("dialog", { name: "个人助理", exact: true });
    await panel.locator(".personal-inbox-toggle").click();
    const list = panel.locator(".personal-inbox-content");
    await expect(list.locator("li")).toHaveCount(10);
    await expect(list.locator(".personal-inbox-project").first()).toHaveAttribute("title", projectName);
    const input = panel.getByRole("textbox", { name: "个人助理消息", exact: true });
    await input.fill("这条草稿等待我明确发送。");
    // Wait on rendered geometry through the normal modal animation; no fixed
    // sleep or forced clicks can hide a clipped composer.
    await expect.poll(async () => {
      const modal = await panel.boundingBox(), composer = await panel.locator(".personal-composer-wrap").boundingBox();
      return !!modal && !!composer && composer.y >= modal.y && composer.y + composer.height <= modal.y + modal.height + 1;
    }).toBe(true);
    const geometry = await list.evaluate(node => ({ client: node.clientWidth, scroll: node.scrollWidth, height: node.clientHeight, content: node.scrollHeight }));
    expect(geometry.scroll).toBeLessThanOrEqual(geometry.client + 1);
    expect(geometry.height).toBeGreaterThanOrEqual(50);
    expect(geometry.content).toBeGreaterThan(geometry.height);
    const firstTitle = list.getByText("待查看的回复 1", { exact: true });
    await expect(firstTitle).toBeVisible();
    const titleBounds = await firstTitle.boundingBox(), listBounds = await list.boundingBox();
    expect(titleBounds!.y).toBeGreaterThanOrEqual(listBounds!.y);
    expect(titleBounds!.y + titleBounds!.height).toBeLessThanOrEqual(listBounds!.y + listBounds!.height + 1);
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
    expect(sends).toBe(0); expect(api.mutations).toEqual([]);
    await page.screenshot({ path: info.outputPath(`personal-inbox-${viewport.width}x${viewport.height}.png`) });
    await panel.getByRole("button", { name: "发送给个人助理", exact: true }).click();
    await expect(input).toHaveValue(""); expect(sends).toBe(1);
    expect(api.mutations).toEqual([]); expect(errors).toEqual([]);
  });
}
