import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";

const progress = "明白了，我会先检查当前资料，再生成可以直接评审的文档。";
const finalReply = "文档已生成：`index.html`。\n\n## 交付内容\n\n- 目标与功能范围\n- 可核对的验收标准\n\n## 验证与限制\n\n这是固定浏览器测试资料，内容仍需人工确认。";
const test = base.extend<{ api: WorkspaceApi }>({ api: async ({ context, page }, runFixture) => {
  const api = new WorkspaceApi(), errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => { if (message.type() === "error" && !/Failed to load resource:.*status of 401\b/.test(message.text())) errors.push(message.text()); });
  await api.install(context);
  try { await runFixture(api); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); } finally { await api.close(); }
} });
async function start(page: Page, api: WorkspaceApi) {
  await page.goto("/app/new"); await expect(page).toHaveTitle(/Lester/);
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill(prompt); await input.press("Enter");
  await expect(page).toHaveURL(`/app/c/${conversationId}`);
  await expect.poll(() => api.streamCount).toBeGreaterThan(0);
  return input;
}
async function completedTranscript(page: Page, api: WorkspaceApi) {
  api.finish();
  const at = new Date().toISOString();
  Object.assign(api.messages[0], { run_id: "fixture-run", seq: 1 });
  Object.assign(api.messages.at(-1)!, { content: finalReply, run_id: "fixture-run", seq: 3 });
  const event = (id: number, type: string, payload: Record<string, unknown> = {}) => ({ id, type, payload, run_id: "fixture-run", conversation_id: conversationId, created_at: at });
  const events = [event(100, "RUN_STARTED", { input_message_id: api.messages[0].id }), event(101, "MODEL_STARTED"), event(102, "MODEL_DELTA", { delta: progress }), event(103, "MODEL_COMPLETED", { message_id: "progress-message" }), ...Array.from({ length: 10 }, (_, i) => [event(104 + i * 2, "TOOL_STARTED", { tool: "read", arguments: { file_path: `reference-${i}.txt` } }), event(105 + i * 2, "TOOL_COMPLETED", { tool: "read", result: { content: "fixture" } })]).flat(), event(124, "CONTEXT_COMPACTING"), event(125, "CONTEXT_COMPACTION_FAILED", { prior_checkpoint_preserved: true }), event(126, "MODEL_STARTED"), event(127, "MODEL_DELTA", { delta: finalReply }), event(128, "MODEL_COMPLETED", { message_id: "fixture-assistant" }), event(129, "RUN_COMPLETED")];
  await page.context().route(`**/api/v1/conversations/${conversationId}/events/history`, route => route.fulfill({ json: { events } }));
  await page.reload();
  await expect(page.locator(".delivery-receipt")).toBeVisible();
  const close = page.locator(".computer-panel .close-files-panel");
  if (await close.isVisible()) await close.click();
  await expect(page.locator(".delivery-receipt")).toBeVisible();
}

test("completed transcript keeps the final reply primary and puts routine narration in execution details", async ({ page, api }, info) => {
  await start(page, api); await completedTranscript(page, api);
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill("保留我的反馈草稿");
  const stream = page.locator(".conversation-stream"), records = page.locator(".conversation-records");
  await expect(stream).not.toContainText(progress);
  await expect(stream.locator(".tool-activity")).toHaveCount(0);
  await expect(stream.locator(".chat-message.assistant")).toHaveCount(1);
  await expect(stream).toContainText("验证与限制");
  await expect(stream.getByRole("button", { name: "复制选项", exact: true })).toHaveCount(1);
  await expect(records).toBeVisible(); await expect(records).not.toHaveAttribute("open");
  await page.screenshot({ path: info.outputPath("conversation-reading.png") });
  await records.locator(":scope > summary").click();
  await expect(records).toContainText(progress);
  await expect(records).toContainText("保留原上下文，继续检查");
  await expect(records.locator(".tool-activity.completed")).toHaveCount(10);
  await expect(input).toHaveValue("保留我的反馈草稿");
  await page.screenshot({ path: info.outputPath("process-on-demand.png") });
  await records.locator(":scope > summary").click();
  await page.getByRole("button", { name: "更多会话选项", exact: true }).click();
  await page.getByRole("button", { name: "显示执行过程", exact: true }).click();
  await expect(stream).toContainText(progress);
  await expect(stream.locator(".tool-activity.completed")).toHaveCount(10);
  await expect(input).toHaveValue("保留我的反馈草稿");
  expect(api.mutations.filter(item => /respond|inputs/.test(item.path))).toHaveLength(0);
});

test("streaming handoff keeps one reply and an empty process stays hidden", async ({ page, api }) => {
  const input = await start(page, api), stream = page.locator(".conversation-stream");
  await input.fill("输出时还在写的草稿");
  api.modelEvent("MODEL_STARTED"); api.modelEvent("MODEL_DELTA", { delta: progress });
  await expect(stream.locator(".assistant-progress")).toHaveText(progress);
  await expect(page.locator(".conversation-records")).not.toBeVisible();
  api.modelEvent("MODEL_COMPLETED", { message_id: "hidden-progress" });
  api.modelEvent("MODEL_STARTED");
  await expect(stream.locator(".assistant-progress")).toHaveCount(0);
  await expect(page.locator(".conversation-records")).toBeVisible();
  api.modelEvent("MODEL_DELTA", { delta: "正在写最终回答" });
  await expect(stream.locator(".assistant-progress")).toHaveText("正在写最终回答");
  api.modelEvent("MODEL_DELTA", { delta: "，不会重复。" });
  await expect(stream.locator(".assistant-progress")).toHaveText("正在写最终回答，不会重复。");
  api.modelEvent("MODEL_COMPLETED", { message_id: "fixture-assistant" }); api.finish();
  api.messages.at(-1)!.content = "正在写最终回答，不会重复。";
  await expect(stream.locator(".chat-message.assistant")).toHaveText(/正在写最终回答，不会重复。/);
  await expect(stream.locator(".assistant-progress")).toHaveCount(0);
  await expect(stream.locator(".chat-message.assistant")).toHaveCount(1);
  await expect(stream).not.toContainText(progress);
  const records = page.locator(".conversation-records");
  await expect(records).not.toHaveAttribute("open");
  await records.locator(":scope > summary").click();
  await expect(records.locator(".assistant-progress")).toHaveText(progress);
  await expect(records).not.toContainText("正在写最终回答，不会重复。");
  await expect(input).toHaveValue("输出时还在写的草稿");
  expect(api.mutations.filter(item => /respond|inputs/.test(item.path))).toHaveLength(0);
});

test("an unfinished operation stays explicit after completion instead of showing a spinner or success", async ({ page, api }) => {
  await start(page, api); api.finish();
  const at = new Date().toISOString();
  const event = (id: number, type: string, payload: Record<string, unknown> = {}) => ({ id, type, payload, run_id: "fixture-run", conversation_id: conversationId, created_at: at });
  await page.context().route(`**/api/v1/conversations/${conversationId}/events/history`, route => route.fulfill({ json: { events: [event(100, "RUN_STARTED"), event(101, "TOOL_STARTED", { tool: "read", arguments: { file_path: "unknown.txt" } }), event(102, "RUN_COMPLETED")] } }));
  await page.reload();
  const row = page.locator(".conversation-stream .tool-activity.unknown");
  await expect(row).toBeVisible(); await expect(row.locator("summary")).toHaveText("操作结果尚未确认");
  await expect(row.locator(".tool-spinner")).toHaveCount(0);
  await expect(page.locator(".conversation-stream")).not.toContainText("已完成：读取文件");
});
