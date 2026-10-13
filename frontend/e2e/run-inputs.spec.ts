import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";
import type { RunInput, RunInputRequest } from "../src/lib/run-input";
const prefix = `/api/v1/conversations/${conversationId}/inputs`;
const test = base.extend<{ api: WorkspaceApi }>({ api: async ({ context, page }, runFixture) => {
  const api = new WorkspaceApi(), errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await api.install(context);
  try { await runFixture(api); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); } finally { await api.close(); }
} });
async function start(page: Page, api: WorkspaceApi) {
  await page.goto("/app");
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill(prompt); await input.press("Enter");
  await expect(page).toHaveURL(`/app/c/${conversationId}`);
  await expect.poll(() => api.streamCount).toBeGreaterThan(0);
  return input;
}
function receipt(request: RunInputRequest): RunInput { return { id: request.id, after_run_id: request.run_id, kind: request.kind, status: request.kind === "adjust" ? "pending" : "queued", revision: 1, content: request.content, request, accepted_order: 1 }; }
test("typing preserves running drafts; adjustment is explicit and receipt loss never reposts", async ({ context, page, api }, info) => {
  let accepted: RunInput | undefined, posts = 0, reads = 0;
  await context.route(`**${prefix}`, route => {
    if (route.request().method() === "GET") return route.fulfill({ json: { inputs: accepted ? [accepted] : [] } });
    posts++; const request = route.request().postDataJSON() as RunInputRequest;
    expect(request.kind).toBe("adjust"); expect(request.content).toBe("只优化颜色"); expect(request.execution_mode).toBeUndefined();
    accepted = receipt(request);
    return route.fulfill({ status: 503, json: { error: "lost receipt" } });
  });
  await context.route(`**${prefix}/*`, route => { reads++; return route.fulfill({ json: accepted }); });
  const input = await start(page, api);
  await input.fill("只优化颜色");
  expect(posts).toBe(0); await expect(input).toHaveValue("只优化颜色");
  await page.getByRole("button", { name: "运行中输入", exact: true }).click();
  await expect(page.getByRole("button", { name: "调整当前任务", exact: true })).toBeVisible();
  await expect(page.locator(".composer-motion-ghost")).toHaveCount(0);
  await page.screenshot({ path: info.outputPath("running-input-menu.png") });
  await page.getByRole("button", { name: "调整当前任务", exact: true }).click();
  await expect(input).toHaveValue("");
  expect(posts).toBe(1); expect(reads).toBe(1);
  await page.locator(".run-input-receipts>summary").click();
  await expect(page.locator(".run-input-receipts")).toContainText("等待加入当前任务");
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeEnabled();
});
test("unknown acceptance preserves draft and shows an honest error", async ({ context, page, api }) => {
  let posts = 0;
  await context.route(`**${prefix}`, route => route.request().method() === "GET" ? route.fulfill({ json: { inputs: [] } }) : (posts++, route.fulfill({ status: 503, json: { error: "unknown" } })));
  await context.route(`**${prefix}/*`, route => route.fulfill({ status: 503, json: { error: "unknown" } }));
  const input = await start(page, api);
  await input.fill("不要丢掉这个调整");
  await page.getByRole("button", { name: "运行中输入", exact: true }).click();
  await page.getByRole("button", { name: "调整当前任务", exact: true }).click();
  await expect(page.locator(".compose-error")).toContainText("结果待确认");
  await expect(input).toHaveValue("不要丢掉这个调整");
  expect(posts).toBe(1);
});
test("queued task captures selected Plan mode without changing the running task", async ({ context, page, api }) => {
  let accepted: RunInput | undefined;
  await context.route(`**${prefix}`, route => {
    if (route.request().method() === "GET") return route.fulfill({ json: { inputs: accepted ? [accepted] : [] } });
    const request = route.request().postDataJSON() as RunInputRequest;
    expect(request.kind).toBe("next"); expect(request.execution_mode).toBe("plan");
    accepted = receipt(request); return route.fulfill({ status: 202, json: accepted });
  });
  const input = await start(page, api);
  await input.fill("接下来先审查代码");
  await page.getByRole("button", { name: "先规划，再执行", exact: true }).click();
  await page.getByRole("button", { name: "运行中输入", exact: true }).click();
  await page.getByRole("button", { name: "排队下一任务", exact: true }).click();
  await expect(input).toHaveValue("");
  await page.locator(".run-input-receipts>summary").click();
  await expect(page.locator(".run-input-receipts")).toContainText("等待下一任务");
  expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(1);
});

test("Enter and the queue send button accept follow-ups without stopping the current run", async ({ context, page, api }) => {
  const accepted: RunInput[] = [];
  await context.route(`**${prefix}`, route => {
    if (route.request().method() === "GET") return route.fulfill({ json: { inputs: accepted } });
    const request = route.request().postDataJSON() as RunInputRequest;
    expect(request.kind).toBe("next"); accepted.push({ ...receipt(request), accepted_order: accepted.length + 1 });
    return route.fulfill({ status: 202, json: accepted.at(-1) });
  });
  const input = await start(page, api);
  await input.fill("第一条排队消息"); await input.press("Enter"); await expect(input).toHaveValue("");
  await input.fill("第二条排队消息"); await page.getByRole("button", { name: "发送到队列", exact: true }).click(); await expect(input).toHaveValue("");
  expect(accepted.map(item => item.content)).toEqual(["第一条排队消息", "第二条排队消息"]);
  expect(accepted[0].id).not.toBe(accepted[1].id);
  expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(1);
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeEnabled();
});

test("a closed window's unfinished draft restores and an accepted queue survives reopening", async ({ context, page, api }) => {
  const accepted: RunInput[] = [];
  await context.route(`**${prefix}`, route => {
    if (route.request().method() === "GET") return route.fulfill({ json: { inputs: accepted } });
    const request = route.request().postDataJSON() as RunInputRequest; accepted.push(receipt(request)); return route.fulfill({ status: 202, json: accepted.at(-1) });
  });
  const input = await start(page, api);
  await input.fill("写了一半，还没有发");
  await page.locator('.composer input[type="file"]').setInputFiles({ name: "draft.txt", mimeType: "text/plain", buffer: Buffer.from("local attachment") });
  await page.close();
  const reopened = await context.newPage();
  await reopened.goto(`/app/c/${conversationId}`);
  const restored = reopened.getByRole("textbox", { name: "消息输入框", exact: true });
  await expect(restored).toHaveValue("写了一半，还没有发");
  await expect(reopened.locator(".draft-attachment-notice")).toContainText("draft.txt");
  expect(accepted).toHaveLength(0);
  await reopened.getByRole("button", { name: "知道了", exact: true }).click();
  await restored.press("Enter"); await expect(restored).toHaveValue(""); expect(accepted).toHaveLength(1);
  await reopened.close();
  const again = await context.newPage(); await again.goto(`/app/c/${conversationId}`);
  await expect(again.getByRole("textbox", { name: "消息输入框", exact: true })).toHaveValue("");
  await again.locator(".run-input-receipts>summary").click(); await expect(again.locator(".run-input-receipts")).toContainText("写了一半，还没有发"); expect(accepted).toHaveLength(1);
});

test("a lost queue receipt survives window closure and cannot become a duplicate idle task", async ({ context, page, api }) => {
  let accepted: RunInput | undefined, posted = 0, receiptAvailable = false;
  await context.route(`**${prefix}`, route => {
    if (route.request().method() === "GET") return route.fulfill({ json: { inputs: accepted ? [accepted] : [] } });
    posted++; accepted = receipt(route.request().postDataJSON()); return route.fulfill({ status: 503, json: { error: "lost receipt" } });
  });
  await context.route(`**${prefix}/*`, route => receiptAvailable ? route.fulfill({ json: accepted }) : route.fulfill({ status: 503, json: { error: "receipt unavailable" } }));
  const input = await start(page, api);
  await input.fill("受理不确定时保存的消息"); await input.press("Enter"); await expect(page.locator(".compose-error")).toContainText("结果待确认");
  await page.close();
  receiptAvailable = true; accepted!.status = "held"; api.failRun();
  const reopened = await context.newPage(); await reopened.goto(`/app/c/${conversationId}`);
  const restored = reopened.getByRole("textbox", { name: "消息输入框", exact: true }); await expect(restored).toHaveValue("受理不确定时保存的消息");
  await restored.press("Enter"); await expect(restored).toHaveValue("");
  expect(posted).toBe(1); expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(1);
  await reopened.locator(".run-input-receipts>summary").click(); await expect(reopened.locator(".run-input-receipts")).toContainText("已暂停，等待处理");
});
