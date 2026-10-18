import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";

const taskId = "33333333-3333-4333-8333-333333333333";
const prefix = `/api/v1/conversations/${conversationId}/tasks`;
const test = base.extend<{ api: WorkspaceApi }>({
  api: async ({ context, page }, runFixture) => {
    const api = new WorkspaceApi(), errors: string[] = [];
    page.on("pageerror", error => errors.push(error.message));
    await api.install(context);
    try { await runFixture(api); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); }
    finally { await api.close(); }
  },
});
async function start(page: Page, api: WorkspaceApi) {
  await page.goto("/app");
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill(prompt); await input.press("Enter");
  await expect(page).toHaveURL(`/app/c/${conversationId}`);
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeVisible();
  await expect.poll(() => api.streamCount).toBeGreaterThan(0);
  api.finish();
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toHaveCount(0);
  // Completion also delivers files. Let the actual preview settle, then return
  // from the mobile modal before interacting with commands in the transcript.
  await expect(page.locator('iframe[title="index.html preview"]')).toBeVisible();
  const returnToChat = page.getByRole("button", { name: "返回对话", exact: true });
  if (await returnToChat.isVisible()) await returnToChat.click();
}

test("background logs survive Agent completion, paginate and stop without stale state regression", async ({ page, context, api }, info) => {
  const initial = { task_id: taskId, run_id: "fixture-run", status: "running", description: "后台检查", shell: "bash", revision: 1 };
  let lists = 0, stops = 0;
  const offsets: number[] = [];
  await context.route(`**${prefix}**`, async route => {
    const url = new URL(route.request().url());
    if (url.pathname === prefix) { lists++; return route.fulfill({ json: { tasks: [initial] } }); }
    if (url.pathname === `${prefix}/${taskId}/stop`) { stops++; return route.fulfill({ json: { ...initial, status: "cancelled", exit_code: 130, cleanup_confirmed: true, revision: 2 } }); }
    if (url.pathname === `${prefix}/${taskId}`) {
      const offset = Number(url.searchParams.get("offset") || 0); offsets.push(offset);
      const text = offset === 0 ? "第一页：检查中🙂" : "第二页：更多输出";
      return route.fulfill({ json: { ...initial, page: { data: Buffer.from(text).toString("base64"), offset, next_offset: offset === 0 ? 30 : 60, has_more: offset === 0 } } });
    }
    return route.fallback();
  });
  await start(page, api);
  await page.getByRole("button", { name: "查看后台命令", exact: true }).click();
  const task = page.locator(".command-task-row").filter({ hasText: "后台检查" });
  await task.getByRole("button", { name: "查看日志", exact: true }).click();
  await expect(task.getByLabel("命令输出日志", { exact: true })).toHaveText("第一页：检查中🙂");
  await task.getByRole("button", { name: "下一页日志", exact: true }).click();
  await expect(task.getByLabel("命令输出日志", { exact: true })).toHaveText("第二页：更多输出");
  expect(offsets).toEqual([0, 30]);
  await task.getByRole("button", { name: "停止命令", exact: true }).click();
  await expect(task).toContainText("已停止");
  await expect(task.getByRole("button", { name: "停止命令", exact: true })).toHaveCount(0);
  await expect(task.getByLabel("命令输出日志", { exact: true })).toHaveText("第二页：更多输出");
  const afterStop = lists;
  await expect.poll(() => lists).toBeGreaterThan(afterStop);
  await expect(task).toContainText("已停止");
  expect(stops).toBe(1);
  const returnToChat = page.getByRole("button", { name: "返回对话", exact: true });
  if (await returnToChat.isVisible()) await returnToChat.click();
  await expect(task.getByLabel("命令输出日志", { exact: true })).toBeVisible();
  await page.screenshot({ path: info.outputPath("command-task-stopped-log.png") });
});

test("a lost stop receipt remains unknown until a scoped inspect confirms the result", async ({ page, context, api }) => {
  const initial = { task_id: taskId, run_id: "fixture-run", status: "running", description: "结果待确认的命令", revision: 1 };
  let stopped = false, stops = 0;
  await context.route(`**${prefix}**`, route => {
    const url = new URL(route.request().url());
    if (url.pathname === prefix) return route.fulfill({ json: { tasks: [initial] } });
    if (url.pathname.endsWith("/stop")) { stopped = true; stops++; return route.fulfill({ status: 503, json: { error: "fixture response lost" } }); }
    if (url.pathname === `${prefix}/${taskId}`) return route.fulfill({ json: { ...initial, status: stopped ? "cancelled" : "running", cleanup_confirmed: stopped, revision: stopped ? 2 : 1, page: { data: Buffer.from("已保留局部输出").toString("base64"), offset: 0, next_offset: 24, has_more: false } } });
    return route.fallback();
  });
  await start(page, api);
  await page.getByRole("button", { name: "查看后台命令", exact: true }).click();
  const task = page.locator(".command-task-row");
  await task.getByRole("button", { name: "停止命令", exact: true }).click();
  await expect(task.getByRole("alert")).toHaveText("停止结果未确认，请检查任务状态。");
  await expect(task).toContainText("命令状态尚未确认");
  expect(stops).toBe(1);
  await task.getByRole("button", { name: "查看日志", exact: true }).click();
  await expect(task).toContainText("已停止");
  await expect(task.getByLabel("命令输出日志", { exact: true })).toHaveText("已保留局部输出");
  expect(stops).toBe(1);
});
