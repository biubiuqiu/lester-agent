import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";

const prefix = `/api/v1/conversations/${conversationId}/interactions`;
const itemId = "44444444-4444-4444-8444-444444444444";
const digest = "a".repeat(64);
const test = base.extend<{ api: WorkspaceApi }>({
  api: async ({ context, page }, runFixture) => {
    const api = new WorkspaceApi(), errors: string[] = [];
    page.on("pageerror", error => errors.push(error.message));
    await api.install(context);
    try { await runFixture(api); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); }
    finally { await api.close(); }
  },
});
async function start(page: Page, api: WorkspaceApi, plan = false) {
  await page.goto("/app");
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  if (plan) { await page.getByRole("button", { name: "先规划，再执行", exact: true }).click(); await expect(page.getByRole("button", { name: "先规划，再执行", exact: true })).toHaveAttribute("aria-pressed", "true"); }
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill(prompt); await input.press("Enter");
  await expect(page).toHaveURL(`/app/c/${conversationId}`);
  await expect.poll(() => api.streamCount).toBeGreaterThan(0);
  return input;
}
const planItem = { id: itemId, run_id: "fixture-run", kind: "plan", request_digest: digest, status: "pending", request: { kind: "plan", title: "先审阅再修改", steps: ["读取当前文件", "完成明确的修改", "运行验证"] } };
const questionItem = { id: itemId, run_id: "fixture-run", kind: "question", request_digest: digest, status: "pending", request: { kind: "question", questions: [{ id: "format", prompt: "希望交付什么形式？", options: [{ id: "html", label: "网页", description: "可直接预览" }, { id: "document", label: "文档" }] }] } };

test("Plan submission is explicit, exact approval survives stale polling and keeps the composer draft", async ({ page, context, api }, info) => {
  let lists = 0, approvals = 0;
  await context.route(`**${prefix}`, route => { lists++; return route.fulfill({ json: { interactions: [planItem], execution_mode: "plan" } }); });
  await context.route(`**${prefix}/${itemId}/respond`, route => {
    approvals++; expect(route.request().postDataJSON()).toEqual({ request_digest: digest, decision: "approve" });
    return route.fulfill({ json: { ...planItem, status: "approved", response: { decision: "approve", request_digest: digest } } });
  });
  const input = await start(page, api, true);
  expect(api.mutations.find(item => item.path.endsWith("/messages"))?.body.execution_mode).toBe("plan");
  await expect(page.getByRole("button", { name: "批准并执行", exact: true })).toBeVisible();
  await input.fill("批准过程中保留的追问");
  await expect(page.locator(".composer-motion-ghost")).toHaveCount(0);
  await page.screenshot({ path: info.outputPath("pending-plan-review.png") });
  await page.getByRole("button", { name: "批准并执行", exact: true }).click();
  await expect(page.locator(".interaction-receipt>summary")).toHaveText("计划已批准 · 先审阅再修改");
  await expect(page.getByText("Plan · 仅查看与规划", { exact: true })).toHaveCount(0);
  const afterApprove = lists;
  await expect.poll(() => lists).toBeGreaterThan(afterApprove);
  await expect(page.locator(".interaction-receipt>summary")).toHaveText("计划已批准 · 先审阅再修改");
  await expect(input).toHaveValue("批准过程中保留的追问");
  expect(approvals).toBe(1);
  await page.screenshot({ path: info.outputPath("approved-plan-receipt.png") });
});

test("a lost question response confirms by scoped receipt and never resubmits", async ({ page, context, api }, info) => {
  let responses = 0, inspected = 0;
  const answer = { request_digest: digest, answers: { format: { option_id: "html", text: "保留极简风格" } } };
  await context.route(`**${prefix}`, route => route.fulfill({ json: { interactions: [questionItem], execution_mode: "execute" } }));
  await context.route(`**${prefix}/${itemId}/respond`, route => {
    responses++; expect(route.request().postDataJSON()).toEqual(answer);
    return route.fulfill({ status: 503, json: { error: "fixture receipt lost" } });
  });
  await context.route(`**${prefix}/${itemId}`, route => { inspected++; return route.fulfill({ json: { ...questionItem, status: "answered", response: { answers: { format: { text: "保留极简风格", option_id: "html" } }, request_digest: digest } } }); });
  const input = await start(page, api);
  await input.fill("回答时保留的草稿");
  await page.getByRole("radio", { name: "网页 可直接预览" }).check();
  await page.getByRole("textbox", { name: "补充回答", exact: true }).fill("保留极简风格");
  await page.screenshot({ path: info.outputPath("pending-question-review.png") });
  await page.getByRole("button", { name: "提交回答", exact: true }).click();
  await expect(page.locator(".interaction-receipt>summary")).toHaveText("已回答");
  await page.locator(".interaction-receipt>summary").click();
  await expect(page.locator(".interaction-content")).toContainText("保留极简风格");
  await expect(page.getByRole("button", { name: "批准并执行", exact: true })).toHaveCount(0);
  await expect(input).toHaveValue("回答时保留的草稿");
  expect(responses).toBe(1); expect(inspected).toBe(1);
});

test("plan revisions require explicit feedback and keep execution read-only", async ({ page, context, api }) => {
  let responded = 0;
  await context.route(`**${prefix}`, route => route.fulfill({ json: { interactions: [planItem], execution_mode: "plan" } }));
  await context.route(`**${prefix}/${itemId}/respond`, route => {
    responded++;
    expect(route.request().postDataJSON()).toEqual({ request_digest: digest, decision: "request_changes", feedback: "先保留旧文件，只生成新文件" });
    return route.fulfill({ json: { ...planItem, status: "changes_requested", response: route.request().postDataJSON() } });
  });
  const input = await start(page, api, true);
  await expect(page.getByRole("button", { name: "调整计划", exact: true })).toBeDisabled();
  await input.fill("保留聊天草稿");
  await page.getByRole("textbox", { name: "调整意见", exact: true }).fill("先保留旧文件，只生成新文件");
  await page.getByRole("button", { name: "调整计划", exact: true }).click();
  await expect(page.locator(".interaction-receipt>summary")).toHaveText("请调整计划 · 先审阅再修改");
  await expect(page.getByText("Plan · 仅查看与规划", { exact: true })).toBeVisible();
  await expect(input).toHaveValue("保留聊天草稿");
  expect(responded).toBe(1);
});

test("pending answers survive refresh, then cancellation removes all decision controls", async ({ page, context, api }) => {
  let cancelled = false;
  await context.route(`**${prefix}`, route => route.fulfill({ json: { interactions: [{ ...questionItem, status: cancelled ? "cancelled" : "pending" }], execution_mode: "execute" } }));
  await start(page, api);
  await page.getByRole("textbox", { name: "补充回答", exact: true }).fill("刷新后仍保留的回答");
  await page.reload();
  await expect(page.getByRole("textbox", { name: "补充回答", exact: true })).toHaveValue("刷新后仍保留的回答");
  await expect.poll(() => api.streamCount).toBeGreaterThan(0);
  cancelled = true;
  api.failRun("fixture owner interrupted");
  await expect(page.getByRole("button", { name: "提交回答", exact: true })).toHaveCount(0);
  await expect(page.locator(".interaction-receipt>summary")).toHaveText("已取消");
  await page.locator(".interaction-receipt>summary").click();
  await expect(page.locator(".interaction-content")).toContainText("此请求已撤销，旧回答或批准不会启动任务。");
});
