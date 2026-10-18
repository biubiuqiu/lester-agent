import { openConversationRecords } from "./conversation-controls";
import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId } from "./workspace-api";

const permissions = `/api/v1/conversations/${conversationId}/permissions`;
const modePath = `/api/v1/conversations/${conversationId}/runs/fixture-run/permissions`;
const command = { id: "99999999-9999-4999-8999-999999999999", run_id: "fixture-run", tool: "bash", request_digest: "c".repeat(64), permission_revision: 0, allow_run: false, summary: { command: "npm test" } };
const test = base.extend<{ api: WorkspaceApi }>({ api: async ({ context, page }, runFixture) => {
  const api = new WorkspaceApi(), errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message)); await api.install(context);
  try { await runFixture(api); expect(errors).toEqual([]); expect(api.unexpected).toEqual([]); }
  finally { await api.close(); }
} });
async function start(page: Page, api: WorkspaceApi, auto = false) {
  await page.goto("/app"); await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test"); await page.getByLabel("密码", { exact: true }).fill("fixture-password-123"); await page.getByRole("button", { name: "登录", exact: true }).click();
  if (auto) { await page.getByRole("button", { name: "权限设置", exact: true }).click(); await page.getByRole("group", { name: "权限设置", exact: true }).getByRole("button").filter({ hasText: "自动审批" }).click(); }
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true }); await input.fill("运行测试并检查结果"); await input.press("Enter");
  await expect(page).toHaveURL(`/app/c/${conversationId}`); await expect.poll(() => api.streamCount).toBeGreaterThan(0);
  await expect(page.getByRole("button", { name: "权限设置", exact: true })).toBeEnabled();
  return input;
}

test("live full access releases the current approval and retains the composer draft", async ({ page, context, api }, info) => {
  await context.route(`**${permissions}`, route => route.fulfill({ json: { permissions: [{ ...command, status: api.currentPermissionMode === "full" ? "approved" : "pending", decision: api.currentPermissionMode === "full" ? "full" : undefined, permission_revision: api.permissionRevision }] } }));
  const input = await start(page, api); await input.fill("保留还没发送的补充");
  await expect(page.getByRole("button", { name: "允许一次", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "权限设置", exact: true }).click();
  await page.getByRole("group", { name: "权限设置", exact: true }).getByRole("button").filter({ hasText: "完全访问权限" }).click();
  await expect(page.locator(".permission-card")).toHaveCount(0); await expect(input).toHaveValue("保留还没发送的补充");
  await expect(page.getByRole("button", { name: "权限设置", exact: true })).toContainText("完全访问权限");
  expect(api.mutations.filter(item => item.path === modePath)).toHaveLength(1);
  expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(1);
  await page.screenshot({ path: info.outputPath("live-full-access.png") });
});

test("the command card grants full access for the active run and synchronizes its selector", async ({ page, context, api }, info) => {
  let approved = false;
  await context.route(`**${permissions}`, route => route.fulfill({ json: { permissions: [{ ...command, status: approved ? "approved" : "pending", ...(approved ? { decision: "full", permission_revision: 1 } : {}) }] } }));
  await context.route(`**${permissions}/${command.id}/respond`, route => {
    expect(route.request().postDataJSON()).toEqual({ decision: "full", request_digest: command.request_digest }); approved = true; api.changePermissionMode("full");
    return route.fulfill({ json: { ...command, status: "approved", decision: "full", permission_revision: 1 } });
  });
  const input = await start(page, api); await input.fill("不要清空我的草稿");
  await page.getByRole("button", { name: "本次任务完全访问", exact: true }).click();
  await expect(page.locator(".permission-card")).toHaveCount(0); await expect(page.getByRole("button", { name: "权限设置", exact: true })).toContainText("完全访问权限"); await expect(input).toHaveValue("不要清空我的草稿");
  await page.screenshot({ path: info.outputPath("full-access-from-approval.png") });
});

for (const decision of ["allow", "deny"] as const) test(`independent AI ${decision} updates approval history without a user click`, async ({ page, context, api }, info) => {
  let reviewing = true;
  await context.route(`**${permissions}`, route => route.fulfill({ json: { permissions: [{ ...command, status: reviewing ? "reviewing" : decision === "allow" ? "approved" : "rejected", decision_source: "llm", review_decision: reviewing ? undefined : decision, review_reason: reviewing ? "" : "Fixture review of the exact command.", decision: reviewing ? undefined : decision === "allow" ? "once" : "reject" }] } }));
  const input = await start(page, api, true); await input.fill("审查过程中继续写草稿");
  await expect(page.locator(".permission-reviewing")).toContainText("AI 正在审批命令");
  await expect(page.getByRole("button", { name: "允许一次", exact: true })).toHaveCount(0); await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeEnabled();
  reviewing = false; api.permissionEvent("PERMISSION_REVIEW_COMPLETED", { permission_id: command.id, decision });
  await expect(page.locator(".permission-reviewing")).toHaveCount(0); await openConversationRecords(page); await page.locator(".permission-history > summary").click();
  await expect(page.locator(".permission-history")).toContainText("AI 审批"); await expect(page.locator(".permission-history")).toContainText(decision === "allow" ? "已允许" : "已拒绝"); await expect(input).toHaveValue("审查过程中继续写草稿");
  expect(api.mutations.filter(item => item.path.endsWith("/respond"))).toHaveLength(0);
  await page.screenshot({ path: info.outputPath(`ai-${decision}.png`) });
});

test("AI ask_user shows its reason and preserves an explicit once-only decision", async ({ page, context, api }, info) => {
  let approved = false;
  const item = { ...command, decision_source: "llm", review_decision: "ask_user", review_reason: "自动审批不可用，请你确认这次操作。" };
  await context.route(`**${permissions}`, route => route.fulfill({ json: { permissions: [{ ...item, status: approved ? "approved" : "pending", ...(approved ? { decision: "once" } : {}) }] } }));
  await context.route(`**${permissions}/${command.id}/respond`, route => { expect(route.request().postDataJSON().decision).toBe("once"); approved = true; return route.fulfill({ json: { ...item, status: "approved", decision: "once", decision_source: "user" } }); });
  const input = await start(page, api, true); await input.fill("保留我的补充");
  await expect(page.locator(".permission-card")).toContainText(item.review_reason);
  await page.screenshot({ path: info.outputPath("ai-asks-user.png") });
  await page.getByRole("button", { name: "允许一次", exact: true }).click();
  await expect(page.locator(".permission-card")).toHaveCount(0); await expect(input).toHaveValue("保留我的补充"); expect(api.currentPermissionMode).toBe("auto");
});

test("a lost permission change response reads current state without replaying the mutation", async ({ page, context, api }) => {
  let writes = 0;
  await context.route(`**${modePath}`, async route => {
    if (route.request().method() !== "PUT") return route.fallback();
    writes++; api.changePermissionMode("full"); return route.fulfill({ status: 503, json: { error: "fixture lost mode response" } });
  });
  const input = await start(page, api); await input.fill("保留不确定响应时的草稿");
  await page.getByRole("button", { name: "权限设置", exact: true }).click(); await page.getByRole("group", { name: "权限设置", exact: true }).getByRole("button").filter({ hasText: "完全访问权限" }).click();
  await expect(page.getByRole("button", { name: "权限设置", exact: true })).toContainText("完全访问权限"); await expect(input).toHaveValue("保留不确定响应时的草稿"); expect(writes).toBe(1);
});

test("permission changes synchronize across browser windows through workspace events", async ({ page, context, api }) => {
  await start(page, api);
  const other = await context.newPage(); await other.goto(`/app/c/${conversationId}`);
  await expect(other.getByRole("button", { name: "权限设置", exact: true })).toBeEnabled(); await expect.poll(() => api.streamCount).toBeGreaterThan(1);
  await page.getByRole("button", { name: "权限设置", exact: true }).click(); await page.getByRole("group", { name: "权限设置", exact: true }).getByRole("button").filter({ hasText: "完全访问权限" }).click();
  await expect(other.getByRole("button", { name: "权限设置", exact: true })).toContainText("完全访问权限"); await other.close();
});
