import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";

const prefix = `/api/v1/conversations/${conversationId}`;
const test = base.extend<{ api: WorkspaceApi }>({ api: async ({ context, page }, runFixture) => {
  const api = new WorkspaceApi(), errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => { if (message.type() === "error" && !/Failed to load resource:.*status of (401|503)\b/.test(message.text())) errors.push(message.text()); });
  await api.install(context);
  await context.route(`**${prefix}/tasks`, route => route.fulfill({ json: { tasks: Array.from({ length: 7 }, (_, i) => ({ task_id: `finished-${i}`, run_id: "fixture-run", status: "exited", revision: 1, exit_code: 0, cleanup_confirmed: true, description: `文档检查 ${i + 1}` })) } }));
  await context.route(`**${prefix}/file-changes`, route => route.fulfill({ json: { changes: ["index.html", "notes.txt", "review.md"].map((path, i) => ({ id: `change-${i}`, run_id: "fixture-run", path, status: "confirmed", operation: "write", before_exists: false, retained: true, rollback_status: "none" })) } }));
  await context.route(`**${prefix}/permissions`, route => route.fulfill({ json: { permissions: Array.from({ length: 9 }, (_, i) => ({ id: `approved-${i}`, run_id: "fixture-run", tool: "bash", status: "approved", request_digest: `digest-${i}`, decision: "once", allow_run: false, summary: { command: `check-document ${i}` } })) } }));
  await context.route(`**${prefix}/interactions`, route => route.fulfill({ json: { execution_mode: "execute", interactions: [{ id: "answered-question", run_id: "fixture-run", kind: "question", status: "answered", request_digest: "question-digest", request: { questions: [{ id: "format", prompt: "需要哪种文档格式？", options: [{ id: "html", label: "HTML 文档" }] }] }, response: { answers: { format: { option_id: "html" } } } }] } }));
  try { await runFixture(api); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); }
  finally { await api.close(); }
} });

async function start(page: Page, api: WorkspaceApi) {
  await page.goto("/app/new"); await expect(page).toHaveTitle(/Lester/);
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill(prompt); await input.press("Enter"); await expect(page).toHaveURL(`/app/c/${conversationId}`);
  await expect.poll(() => api.streamCount).toBeGreaterThan(0);
  return input;
}

test("completed conversations keep the answer and output primary, with one history disclosure", async ({ page, api }, info) => {
  const input = await start(page, api);
  api.finish();
  api.messages.at(-1)!.content = "已整理需求评审文档。\n\n- 目标：覆盖核心业务目标与衡量指标。\n- 功能范围：区分当前版本和后续迭代。\n- 验收标准：整理可核对的验收用例。\n\n文档已保存，请预览并确认业务细节。";
  for (let i = 0; i < 11; i++) api.writeFile(`support-${i}.txt`, "这是浏览器测试资料，不是真实模型输出。");
  await expect(page.locator(".open-file-tabs [role=tab]")).toHaveCount(8);
  const close = page.locator(".computer-panel .close-files-panel");
  if (await close.isVisible()) await close.click();
  await expect(page.locator(".delivery-receipt")).toBeVisible();
  const records = page.locator(".conversation-records");
  await expect(records).toBeVisible();
  await expect(records).not.toHaveAttribute("open");
  await expect(page.locator(".interaction-receipt")).not.toBeVisible();
  await expect(page.locator(".command-tasks")).not.toBeVisible();
  await expect(records.locator(".file-changes")).not.toBeVisible();
  await expect(page.locator(".permission-history")).not.toBeVisible();
  await expect(page.locator(".composer .permission-history")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "会话回顾", exact: true })).toHaveCount(0);
  await expect(page.locator(".session-awareness")).toHaveCount(0);
  await input.fill("保留尚未发送的反馈");
  const mutations = api.mutations.length;
  await page.screenshot({ path: info.outputPath("completed-conversation.png"), fullPage: false });
  await records.locator(":scope > summary").focus(); await page.keyboard.press("Enter");
  await expect(records).toHaveAttribute("open");
  await expect(records.locator(".command-tasks > summary")).toContainText("7");
  await expect(records.locator(".file-changes > summary")).toContainText("3");
  await expect(records.locator(".permission-history > summary")).toContainText("9");
  await expect(records.locator(".interaction-receipt > summary")).toHaveText("已回答");
  await records.locator(".permission-history > summary").click();
  await expect(records.getByText("check-document 0", { exact: true })).toBeVisible();
  await page.screenshot({ path: info.outputPath("execution-details.png"), fullPage: false });
  await records.locator(":scope > summary").click();
  await expect(input).toHaveValue("保留尚未发送的反馈");
  expect(api.mutations).toHaveLength(mutations);
  const width = page.viewportSize()!.width;
  const bounds = await page.locator(".composer-dock").boundingBox();
  expect(bounds!.x).toBeGreaterThanOrEqual(0); expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width + 1);
});

test("pending questions and approvals stay above the composer while history stays closed", async ({ page, context, api }, info) => {
  await context.route(`**${prefix}/interactions`, route => route.fulfill({ json: { execution_mode: "execute", interactions: [{ id: "pending-question", run_id: "fixture-run", kind: "question", status: "pending", request_digest: "pending-digest", request: { questions: [{ id: "scope", prompt: "本次优先处理哪个范围？", options: [{ id: "current", label: "当前页面" }] }] } }] } }));
  await context.route(`**${prefix}/permissions`, route => route.fulfill({ json: { permissions: [{ id: "pending-permission", run_id: "fixture-run", tool: "bash", status: "pending", request_digest: "permission-digest", allow_run: false, summary: { command: "npm test" } }] } }));
  const input = await start(page, api);
  await input.fill("还没发送的后续要求");
  await expect(page.locator(".conversation-records")).not.toHaveAttribute("open");
  await expect(page.locator(".question-dock").getByRole("article", { name: "等待你的回答", exact: true })).toBeVisible();
  await expect(page.locator(".composer-dock").getByRole("button", { name: "允许一次", exact: true })).toBeVisible();
  await expect(page.locator(".conversation-records .permission-card")).toHaveCount(0);
  await expect(page.locator(".conversation-records .question-card")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeEnabled();
  await page.screenshot({ path: info.outputPath("pending-actions-stay-visible.png") });
  await expect(input).toHaveValue("还没发送的后续要求");
  expect(api.mutations.filter(item => /respond|inputs/.test(item.path))).toHaveLength(0);
});

test("failed history reads stay visible without adding empty history or sending the draft", async ({ page, context, api }) => {
  let recovered = false;
  await context.route(`**${prefix}/tasks`, route => route.fulfill({ json: { tasks: [] } }));
  await context.route(`**${prefix}/interactions`, route => route.fulfill({ json: { interactions: [], execution_mode: "execute" } }));
  await context.route(`**${prefix}/permissions`, route => route.fulfill({ json: { permissions: [] } }));
  await context.route(`**${prefix}/file-changes`, route => recovered ? route.fulfill({ json: { changes: [] } }) : route.fulfill({ status: 503, json: { error: "fixture read unavailable" } }));
  const input = await start(page, api);
  await input.fill("同步失败时保留的草稿");
  const error = page.getByRole("alert").filter({ hasText: "文件审阅加载失败，请重试。" });
  await expect(error).toBeVisible();
  await expect(page.locator(".conversation-records")).not.toBeVisible();
  recovered = true;
  await error.getByRole("button", { name: "重试", exact: true }).click();
  await expect(error).toHaveCount(0);
  await expect(page.locator(".conversation-records")).not.toBeVisible();
  await expect(input).toHaveValue("同步失败时保留的草稿");
  expect(api.mutations.filter(item => /respond|inputs/.test(item.path))).toHaveLength(0);
});
