import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";
import type { RunInput } from "../src/lib/run-input";

const prefix = `/api/v1/conversations/${conversationId}/permissions/inputs`;
const test = base.extend<{ api: WorkspaceApi }>({ api: [async ({ context, page }, runFixture) => {
  const api = new WorkspaceApi(), errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => { if (message.type() === "error" && !/Failed to load resource:.*status of (401|503)\b/.test(message.text())) errors.push(message.text()); });
  await api.install(context);
  try { await runFixture(api); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); } finally { await api.close(); }
}, { auto: true }] });

function item(index: number, values: Partial<RunInput> = {}): RunInput {
  return { id: `99999999-9999-4999-8999-${String(index).padStart(12, "0")}`, after_run_id: "fixture-run", kind: "next", status: "queued", revision: 1, content: `排队任务 ${index}`, accepted_order: index, ...values };
}
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
  return input;
}

test("the queue shows one accepted item, discloses the rest and keeps keyboard cancellation nearby", async ({ context, page, api }, info) => {
  const items = [item(1), item(2, { content: "第二条很长的任务。".repeat(50), request: { id: "fixture-request", run_id: "fixture-run", kind: "next", content: "second", permission_mode: "full" } }), item(3)];
  let writes = 0;
  await context.route(`**${prefix}`, route => route.fulfill({ json: { inputs: items } }));
  await context.route(`**${prefix}/*/action`, route => {
    expect(route.request().postDataJSON()).toEqual({ action: "cancel" }); writes++;
    const accepted = items.find(value => route.request().url().includes(value.id))!;
    accepted.status = "cancelled"; accepted.revision++;
    return route.fulfill({ json: accepted });
  });
  const input = await start(page, api); await input.fill("不发送的聊天草稿");
  const queue = page.locator(".composer-queue-list"), rows = queue.locator(".composer-queue-item"), remainder = queue.locator(".queue-remainder");
  await expect(rows).toHaveCount(3); await expect(rows.nth(0)).toBeVisible(); await expect(rows.nth(1)).not.toBeVisible();
  await expect(remainder.locator(":scope > summary")).toHaveText("另外 2 条");
  await expect(remainder).not.toHaveAttribute("open");
  await remainder.locator(":scope > summary").click(); await expect(rows.nth(1)).toBeVisible();
  expect(await rows.evaluateAll(elements => elements.map(element => element.getAttribute("data-input-id")))).toEqual(items.map(value => value.id));
  const message = rows.nth(1).locator(".queue-message");
  await message.locator(":scope > summary").click(); await expect(message.locator(".queue-permission")).toContainText("完全访问权限");
  expect(writes).toBe(0);
  const box = await page.locator(".compose-box").boundingBox(), bounds = await queue.boundingBox();
  expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(box!.y + 4);
  await expect(input).toBeInViewport(); await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeInViewport();
  await remainder.locator(":scope > summary").click();
  await page.screenshot({ path: info.outputPath("queue-one-item-and-count.png") });
  const cancel = rows.first().getByRole("button", { name: "取消", exact: true });
  await cancel.focus(); await page.keyboard.press("Enter");
  await expect(rows).toHaveCount(2); await expect(rows.first().locator(".queue-message > summary")).toBeFocused();
  await rows.first().getByRole("button", { name: "取消", exact: true }).focus(); await page.keyboard.press("Enter");
  await expect(rows).toHaveCount(1); await expect(queue.locator(".queue-remainder")).toHaveCount(0);
  await rows.first().getByRole("button", { name: "取消", exact: true }).focus(); await page.keyboard.press("Enter");
  await expect(queue).toHaveCount(0); await expect(input).toBeFocused(); await expect(input).toHaveValue("不发送的聊天草稿");
  expect(writes).toBe(3);
});

test("a held item keeps its real place and status while other accepted inputs remain discoverable", async ({ context, page, api }) => {
  const items = [item(1, { status: "held", content: "暂停中的第一项" }), item(2, { kind: "adjust", status: "pending", content: "当前任务的补充" }), item(3, { request: { id: "fixture-request", run_id: "fixture-run", kind: "next", content: "", attachment_ids: ["fixture-attachment"] }, content: "" })];
  let writes = 0;
  await context.route(`**${prefix}`, route => { if (route.request().method() !== "GET") writes++; return route.fulfill({ json: { inputs: items } }); });
  const input = await start(page, api); await input.fill("范围检查时的草稿");
  const queue = page.locator(".composer-queue-list"), rows = queue.locator(".composer-queue-item");
  await expect(rows.first()).toContainText("暂停中的第一项"); await expect(rows.first()).toContainText("已暂停，等待处理");
  await expect(rows.first().getByRole("button", { name: "继续排队", exact: true })).toBeDisabled();
  await queue.locator(".queue-remainder > summary").click();
  await expect(rows.nth(1)).toContainText("等待加入当前任务"); await expect(rows.nth(2)).toContainText("1 个附件");
  await expect(rows.nth(2)).toContainText("等待下一任务");
  await expect(rows.first()).not.toContainText("等待下一任务");
  await expect(input).toHaveValue("范围检查时的草稿"); expect(writes).toBe(0);
});

test("an uncertain cancellation reads its receipt once and does not steal a moved draft focus", async ({ context, page, api }) => {
  const accepted = item(1); let writes = 0, inspected = 0, release!: () => void;
  const ready = new Promise<void>(resolve => { release = resolve; });
  await context.route(`**${prefix}`, route => route.fulfill({ json: { inputs: [{ ...accepted, status: "queued", revision: 1 }] } }));
  await context.route(`**${prefix}/${accepted.id}/action`, async route => { writes++; await ready; return route.fulfill({ status: 503, json: { error: "fixture response lost" } }); });
  await context.route(`**${prefix}/${accepted.id}`, route => { inspected++; return route.fulfill({ json: { ...accepted, status: "cancelled", revision: 2 } }); });
  try {
    const input = await start(page, api), cancel = page.locator(".composer-queue").getByRole("button", { name: "取消", exact: true });
    await cancel.focus(); await page.keyboard.press("Enter"); await expect(cancel).toBeDisabled();
    await input.focus(); await input.fill("等待取消时继续写的内容");
    release();
    await expect(page.locator(".composer-queue-list")).toHaveCount(0); await expect(input).toBeFocused();
    await expect(input).toHaveValue("等待取消时继续写的内容"); expect(writes).toBe(1); expect(inspected).toBe(1);
  } finally { release(); }
});
