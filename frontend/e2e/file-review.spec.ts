import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";
const changeId = "55555555-5555-4555-8555-555555555555";
const prefix = `/api/v1/conversations/${conversationId}/file-changes`;
const change = { id: changeId, run_id: "fixture-run", path: "note.txt", operation: "edit", status: "confirmed", before_exists: true, before_sha256: "a".repeat(64), after_sha256: "b".repeat(64), retained: true, rollback_status: "none", before_content: "old heading\n  keep indentation\n", after_content: "new heading\n  keep indentation\n" };
const test = base.extend<{ api: WorkspaceApi }>({ api: async ({ context, page }, runFixture) => {
  const api = new WorkspaceApi(), errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message)); await api.install(context);
  try { await runFixture(api); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); } finally { await api.close(); }
} });
async function start(page: Page, api: WorkspaceApi) {
  await page.goto("/app");
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test"); await page.getByLabel("密码", { exact: true }).fill("fixture-password-123"); await page.getByRole("button", { name: "登录", exact: true }).click();
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true }); await input.fill(prompt); await input.press("Enter"); await expect(page).toHaveURL(`/app/c/${conversationId}`); await expect.poll(() => api.streamCount).toBeGreaterThan(0); return input;
}
test("actual review is lazy, exports original records and restores only after confirmation", async ({ context, page, api }, info) => {
  let loaded = 0, writes = 0, inspected = 0, rollbackId = "", restored = false;
  await context.route(`**${prefix}`, route => route.fulfill({ json: { changes: [{ ...change, rollback_status: restored ? "completed" : "none", before_content: undefined, after_content: undefined }] } }));
  await context.route(`**${prefix}/${changeId}`, route => { loaded++; return route.fulfill({ json: { ...change, rollback_status: restored ? "completed" : "none" } }); });
  await context.route(`**${prefix}/${changeId}/rollback`, route => { writes++; rollbackId = route.request().postDataJSON().id; restored = true; return route.fulfill({ status: 503, json: { error: "lost HTTP receipt" } }); });
  await context.route(`**${prefix}/${changeId}/rollback/*`, route => { inspected++; return route.fulfill({ json: { id: rollbackId, file_change_id: changeId, status: "completed" } }); });
  const input = await start(page, api); expect(loaded).toBe(0);
  await page.locator(".file-changes>summary").click(); await page.locator(".file-change-card>summary").click();
  await expect(page.locator(".diff-added")).toHaveText("+new heading"); await expect(page.locator(".diff-removed")).toHaveText("−old heading");
  await expect(page.getByRole("button", { name: "撤回此文件修改", exact: true })).toBeDisabled();
  const download = page.waitForEvent("download"); await page.getByRole("button", { name: "导出审阅记录", exact: true }).click(); expect((await download).suggestedFilename()).toBe(`lester-file-review-${changeId}.json`);
  api.finish(); await expect(page.getByRole("button", { name: "撤回此文件修改", exact: true })).toBeEnabled();
  await expect(page.locator(".open-file-tabs [role=tab]").filter({ hasText: "index.html" })).toHaveCount(1);
  if (info.project.name === "mobile") await expect(page.locator("#conversation-workspace-panel")).not.toHaveAttribute("open");
  await input.fill("审阅时保留的草稿"); await page.getByRole("button", { name: "撤回此文件修改", exact: true }).click(); expect(writes).toBe(0);
  await expect(page.locator(".composer-motion-ghost")).toHaveCount(0); await page.screenshot({ path: info.outputPath("file-review-confirm.png") });
  await page.getByRole("button", { name: "确认恢复", exact: true }).click(); await expect(page.getByText("已恢复修改前的文件内容", { exact: true })).toBeVisible(); await expect(input).toHaveValue("审阅时保留的草稿"); expect(writes).toBe(1); expect(inspected).toBe(1);
});
test("unconfirmed writes never display intended bytes and an open review follows confirmation", async ({ context, page, api }) => {
  let confirmed = false;
  await context.route(`**${prefix}`, route => route.fulfill({ json: { changes: [{ ...change, status: confirmed ? "confirmed" : "unknown" }] } }));
  await context.route(`**${prefix}/${changeId}`, route => route.fulfill({ json: { ...change, status: confirmed ? "confirmed" : "unknown", before_content: confirmed ? change.before_content : undefined, after_content: confirmed ? change.after_content : undefined } }));
  await start(page, api); await page.locator(".file-changes>summary").click(); await page.locator(".file-change-card>summary").click();
  await expect(page.getByText("原工具写入尚未确认，不能显示实际差异或恢复文件。", { exact: true })).toBeVisible(); await expect(page.locator(".file-diff")).toHaveCount(0); await expect(page.getByRole("button", { name: "撤回此文件修改", exact: true })).toHaveCount(0); confirmed = true; await expect(page.locator(".diff-added")).toHaveText("+new heading");
});
test("project rules preserve stale editor drafts and expose accepted source/version", async ({ context, page, api }, info) => {
  let saves = 0, stale = true;
  const rules = { project_id: "fixture-project", project_name: "个人项目", source: "project_settings", version: 2, sha256: "c".repeat(64), content: "Use the existing design tokens" };
  await context.route("**/api/v1/projects/fixture-project/rules", route => {
    if (route.request().method() === "GET") return route.fulfill({ json: rules });
    saves++; expect(route.request().postDataJSON()).toEqual({ content: "Keep spacing consistent", version: 2 });
    return stale ? route.fulfill({ status: 409, json: { error: "changed" } }) : route.fulfill({ json: { ...rules, version: 3, content: "Keep spacing consistent" } });
  });
  const input = await start(page, api); api.messages[0].metadata = { ...api.messages[0].metadata, project_rules: rules };
  await input.fill("设置规则时保留的任务草稿");
  if (info.project.name === "mobile") await page.getByRole("button", { name: "打开会话栏", exact: true }).click();
  await page.getByRole("button", { name: "项目 个人项目 的操作", exact: true }).click(); await page.getByRole("button", { name: "项目规则", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "项目规则", exact: true }); const editor = dialog.getByRole("textbox", { name: "项目规则内容", exact: true });
  await expect(editor).toHaveValue(rules.content); api.finish(); await expect(page.locator(".open-file-tabs [role=tab]").filter({ hasText: "index.html" })).toHaveCount(1); await expect(page.locator("#conversation-workspace-panel")).not.toHaveAttribute("open"); await editor.fill("Keep spacing consistent"); await dialog.getByRole("button", { name: "保存", exact: true }).click(); await expect(dialog.getByRole("alert")).toContainText("草稿保留"); await expect(editor).toHaveValue("Keep spacing consistent");
  stale = false; await dialog.getByRole("button", { name: "保存", exact: true }).click(); await expect(dialog).toContainText("v3"); await page.screenshot({ path: info.outputPath("project-rules-editor.png") });
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  if (info.project.name === "mobile") await page.locator(".mobile-close").click();
  await expect(input).toHaveValue("设置规则时保留的任务草稿"); await expect(page.locator(".chat-message .message-context>summary").filter({ hasText: "项目规则" })).toContainText("v2"); expect(saves).toBe(2);
});
