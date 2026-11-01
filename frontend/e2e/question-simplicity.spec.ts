import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";

const path = `/api/v1/conversations/${conversationId}/interactions`;
const id = "77777777-7777-4777-8777-777777777777", digest = "b".repeat(64);
const question = { id: "format", prompt: "希望交付什么形式？", options: [{ id: "html", label: "网页", description: "可以直接预览", recommended: true }, { id: "document", label: "文档" }] };
const item = { id, run_id: "fixture-run", kind: "question", request_digest: digest, status: "pending", request: { kind: "question", questions: [question] } };
const test = base.extend<{ api: WorkspaceApi }>({ api: [async ({ context, page }, runFixture) => {
  const api = new WorkspaceApi(), errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => { if (message.type() === "error" && !/Failed to load resource:.*status of 401\b/.test(message.text())) errors.push(message.text()); });
  await api.install(context);
  try { await runFixture(api); expect(errors).toEqual([]); expect(api.unexpected).toEqual([]); } finally { await api.close(); }
}, { auto: true }] });

async function start(page: Page, api: WorkspaceApi) {
  await page.goto("/app/new"); await expect(page).toHaveTitle(/Lester/);
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill(prompt); await input.press("Enter");
  await expect(page).toHaveURL(`/app/c/${conversationId}`);
  await expect.poll(() => api.streamCount).toBeGreaterThan(0);
  await expect(page.locator(".composer-motion-ghost")).toHaveCount(0);
  await input.fill("回答问题时保留的聊天草稿");
  return input;
}

test("a simple choice keeps optional writing closed and does not take composer focus", async ({ page, context, api }, info) => {
  let responses = 0;
  await context.route(`**${path}`, route => route.fulfill({ json: { interactions: [item], execution_mode: "execute" } }));
  await context.route(`**${path}/${id}/respond`, route => {
    responses++;
    expect(route.request().postDataJSON()).toEqual({ request_digest: digest, answers: { format: { option_id: "html" } } });
    return route.fulfill({ json: { ...item, status: "answered", response: route.request().postDataJSON() } });
  });
  const input = await start(page, api), card = page.locator(".question-card");
  await expect(input).toBeFocused();
  await expect(card.getByRole("textbox", { name: "补充回答", exact: true })).toHaveCount(0);
  await expect(card.getByRole("button", { name: "添加补充", exact: true })).toHaveAttribute("aria-expanded", "false");
  await expect(card.locator("input:checked")).toHaveCount(0);
  await input.pressSequentially("123");
  await expect(card.locator("input:checked")).toHaveCount(0);
  await card.getByRole("radio", { name: /网页.*推荐.*可以直接预览/ }).check();
  await expect(card.getByRole("button", { name: "提交回答", exact: true })).toBeEnabled();
  await page.screenshot({ path: info.outputPath("question-simple-choice.png") });
  expect(responses).toBe(0);
  await card.getByRole("button", { name: "提交回答", exact: true }).click();
  await expect(card).toHaveCount(0); expect(responses).toBe(1);
  await expect(input).toHaveValue("回答问题时保留的聊天草稿123");
});

test("optional notes survive collapsing, answer changes and reload without being sent", async ({ page, context, api }, info) => {
  await context.route(`**${path}`, route => route.fulfill({ json: { interactions: [item], execution_mode: "execute" } }));
  const input = await start(page, api), card = page.locator(".question-card");
  await card.getByRole("button", { name: "添加补充", exact: true }).click();
  const notes = card.getByRole("textbox", { name: "补充回答", exact: true });
  await expect(notes).toBeFocused(); await notes.fill("网页也要方便打印。");
  await card.getByRole("button", { name: "收起补充", exact: true }).click();
  await expect(notes).toHaveCount(0);
  await card.getByRole("radio", { name: "自由回答", exact: true }).check();
  await expect(notes).toHaveValue("网页也要方便打印。");
  await card.getByRole("radio", { name: /网页.*推荐.*可以直接预览/ }).check();
  await expect(notes).toHaveValue("网页也要方便打印。");
  await card.getByRole("button", { name: "收起补充", exact: true }).click();
  await page.reload();
  await expect(notes).toHaveValue("网页也要方便打印。");
  await expect(card.getByRole("radio", { name: /网页.*推荐.*可以直接预览/ })).toBeChecked();
  await expect(input).toHaveValue("回答问题时保留的聊天草稿");
  expect(api.mutations.filter(value => value.path.endsWith("/respond"))).toHaveLength(0);
  await page.screenshot({ path: info.outputPath("question-notes-restored.png") });
});

test("free-text questions remain directly editable and multiple answers still require final review", async ({ page, context, api }, info) => {
  const request = { ...item, request: { kind: "question", questions: [question, { id: "requirements", header: "其他要求", prompt: "还有什么需要注意？" }] } };
  let responses = 0;
  await context.route(`**${path}`, route => route.fulfill({ json: { interactions: [request], execution_mode: "execute" } }));
  await context.route(`**${path}/${id}/respond`, route => {
    responses++;
    expect(route.request().postDataJSON()).toEqual({ request_digest: digest, answers: { format: { option_id: "html" }, requirements: { text: "保持简洁，先给我预览。" } } });
    return route.fulfill({ json: { ...request, status: "answered", response: route.request().postDataJSON() } });
  });
  const input = await start(page, api), card = page.locator(".question-card");
  await card.getByRole("radio", { name: /网页.*推荐.*可以直接预览/ }).focus();
  await page.keyboard.press("1");
  await card.getByRole("button", { name: "下一题", exact: true }).click();
  const notes = card.getByRole("textbox", { name: "补充回答", exact: true });
  await expect(notes).toBeVisible();
  await expect(card.getByRole("button", { name: "添加补充", exact: true })).toHaveCount(0);
  await notes.fill("保持简洁，先给我预览。");
  if (info.project.name === "mobile") await page.setViewportSize({ width: 393, height: 520 });
  await expect(card.getByRole("button", { name: "核对回答", exact: true })).toBeInViewport();
  await expect(input).toBeInViewport();
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeInViewport();
  await card.getByRole("button", { name: "核对回答", exact: true }).click();
  await expect(card).toContainText("保持简洁，先给我预览。");
  expect(responses).toBe(0);
  await card.getByRole("button", { name: "提交回答", exact: true }).click();
  await expect(card).toHaveCount(0); expect(responses).toBe(1);
  await expect(input).toHaveValue("回答问题时保留的聊天草稿");
});
