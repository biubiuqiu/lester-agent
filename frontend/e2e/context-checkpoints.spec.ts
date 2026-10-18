import { openConversationRecords } from "./conversation-controls";
import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";

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
  await expect.poll(() => api.streamCount).toBeGreaterThan(0);
  return input;
}

test("context organization reports real events, stays collapsed and preserves the active draft", async ({ page, api }, info) => {
  const input = await start(page, api);
  await input.fill("整理过程中保留的追问草稿");
  api.contextEvent("CONTEXT_COMPACTING", { request_index: 2 });
  const row = page.locator(".tool-activity").filter({ hasText: "完整聊天记录仍然保留" });
  await expect(row.locator("summary")).toHaveText("正在整理历史上下文");
  await expect(row).not.toHaveAttribute("open");
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeVisible();
  api.contextEvent("CONTEXT_COMPACTED", { checkpoint_id: "fixture-cp", through_seq: 20, source_count: 16, estimated_before: 54000, estimated_after: 18000, source: "estimated" });
  await expect(row.locator("summary")).toHaveText("历史已整理，继续任务");
  await openConversationRecords(page); await row.locator("summary").click();
  await expect(row).toContainText("请求输入估算：54000 → 18000 token；不是 Provider 精确计数。");
  await expect(input).toHaveValue("整理过程中保留的追问草稿");
  await expect(page.locator(".chat-message.user").filter({ hasText: prompt })).toHaveCount(1);
  expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(1);
  await page.screenshot({ path: info.outputPath("context-checkpoint-receipt.png") });
});

test("failed compaction reports the preserved boundary and interrupted work cannot stay in progress", async ({ page, api }) => {
  const input = await start(page, api);
  await input.fill("失败后仍然保留的草稿");
  api.contextEvent("CONTEXT_COMPACTING");
  api.contextEvent("CONTEXT_COMPACTION_FAILED", { prior_checkpoint_preserved: true, error_kind: "protocol" });
  const row = page.locator(".tool-activity").filter({ hasText: "完整聊天记录仍然保留" });
  await expect(row.locator("summary")).toHaveText("保留原上下文，继续检查");
  await openConversationRecords(page); await row.locator("summary").click();
  await expect(row).toContainText("本轮已停止自动整理。旧检查点保持不变，超出预算时任务会明确停止。");
  await expect(input).toHaveValue("失败后仍然保留的草稿");
  api.contextEvent("CONTEXT_COMPACTING");
  api.failRun("fixture interruption");
  await expect(row.locator("summary")).toHaveText("上下文整理已中断");
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toHaveCount(0);
  await expect(input).toHaveValue("失败后仍然保留的草稿");
});
