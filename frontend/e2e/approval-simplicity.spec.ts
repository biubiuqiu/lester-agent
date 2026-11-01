import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId } from "./workspace-api";

const path = `/api/v1/conversations/${conversationId}/permissions`;
const first = { id: "99999999-9999-4999-8999-999999999999", run_id: "fixture-run", tool: "bash", status: "pending", request_digest: "c".repeat(64), permission_revision: 0, allow_run: false, summary: { command: "npm test" } };
const test = base.extend<{ api: WorkspaceApi }>({ api: [async ({ context, page }, runFixture) => {
  const api = new WorkspaceApi(), errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => { if (message.type() === "error" && !/Failed to load resource:.*status of (401|503)\b/.test(message.text())) errors.push(message.text()); });
  await api.install(context);
  try { await runFixture(api); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); } finally { await api.close(); }
}, { auto: true }] });

async function start(page: Page, api: WorkspaceApi) {
  await page.goto("/app/new"); await expect(page).toHaveTitle(/Lester/);
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill("运行测试并检查结果"); await input.press("Enter");
  await expect(page).toHaveURL(`/app/c/${conversationId}`);
  await expect.poll(() => api.streamCount).toBeGreaterThan(0);
  await expect(page.locator(".composer-motion-ghost")).toHaveCount(0);
  return input;
}

test("approval has one primary action and broader authority requires opening its scope", async ({ page, context, api }, info) => {
  let approved = false, posts = 0;
  await context.route(`**${path}`, route => route.fulfill({ json: { permissions: [{ ...first, status: approved ? "approved" : "pending", ...(approved ? { decision: "full", permission_revision: 1 } : {}) }] } }));
  await context.route(`**${path}/${first.id}/respond`, route => {
    posts++; expect(route.request().postDataJSON()).toEqual({ request_digest: first.request_digest, decision: "full" });
    approved = true; api.changePermissionMode("full");
    return route.fulfill({ json: { ...first, status: "approved", decision: "full", permission_revision: 1 } });
  });
  const input = await start(page, api); await input.fill("保留我的后续要求");
  const card = page.locator(".permission-card"), allow = card.getByRole("button", { name: "允许一次", exact: true });
  await expect(allow).toHaveClass(/permission-primary/);
  await expect(card.getByRole("button", { name: "拒绝", exact: true })).toBeVisible();
  await expect(card.getByRole("button", { name: "本次任务完全访问", exact: true })).toHaveCount(0);
  await expect(card.getByText("以下授权仅用于当前任务。", { exact: true })).not.toBeVisible();
  await card.screenshot({ path: info.outputPath("approval-compact.png") });
  await card.getByText("更多授权", { exact: true }).click();
  await expect(card.getByText("以下授权仅用于当前任务。", { exact: true })).toBeVisible();
  expect(posts).toBe(0);
  await card.getByRole("button", { name: "本次任务完全访问", exact: true }).click();
  await expect(card).toHaveCount(0);
  await expect(input).toHaveValue("保留我的后续要求");
  await expect(page.getByRole("button", { name: "权限设置", exact: true })).toContainText("完全访问权限");
  expect(posts).toBe(1);
});

test("keyboard approval hands focus back without repeating Enter into a queued message", async ({ page, context, api }) => {
  let approved = false, posts = 0;
  await context.route(`**${path}`, route => route.fulfill({ json: { permissions: [{ ...first, status: approved ? "approved" : "pending", ...(approved ? { decision: "once" } : {}) }] } }));
  await context.route(`**${path}/${first.id}/respond`, route => { approved = true; posts++; return route.fulfill({ json: { ...first, status: "approved", decision: "once" } }); });
  const input = await start(page, api); await input.fill("尚未发送的文字");
  const button = page.getByRole("button", { name: "允许一次", exact: true });
  await button.focus(); await page.keyboard.down("Enter");
  await expect(page.locator(".permission-card")).toHaveCount(0);
  await expect(input).toBeFocused();
  await page.keyboard.down("Enter");
  await expect(input).toHaveValue("尚未发送的文字");
  expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(1);
  expect(api.mutations.filter(item => item.path.endsWith("/inputs"))).toHaveLength(0);
  await page.keyboard.up("Enter"); expect(posts).toBe(1);
});

test("a pending approval does not steal focus when the user keeps typing elsewhere", async ({ page, context, api }) => {
  let release!: () => void, started!: () => void, approved = false;
  const held = new Promise<void>(resolve => { release = resolve; }), accepted = new Promise<void>(resolve => { started = resolve; });
  await context.route(`**${path}`, route => route.fulfill({ json: { permissions: [{ ...first, status: approved ? "approved" : "pending", ...(approved ? { decision: "once" } : {}) }] } }));
  await context.route(`**${path}/${first.id}/respond`, async route => { started(); await held; approved = true; return route.fulfill({ json: { ...first, status: "approved", decision: "once" } }); });
  const input = await start(page, api);
  try {
    await page.getByRole("button", { name: "允许一次", exact: true }).click(); await accepted;
    await input.fill("审批过程中继续写的草稿");
    release();
    await expect(page.locator(".permission-card")).toHaveCount(0);
    await expect(input).toBeFocused(); await expect(input).toHaveValue("审批过程中继续写的草稿");
  } finally { release(); }
});

test("new approvals stay behind the current decision and require viewing the next item", async ({ page, context, api }, info) => {
  const next = { ...first, id: "77777777-7777-4777-8777-777777777777", request_digest: "d".repeat(64), summary: { command: "npm run build" } };
  const incoming = { ...first, id: "66666666-6666-4666-8666-666666666666", request_digest: "e".repeat(64), summary: { command: "git status" } };
  let items = [first, next], posts = 0;
  await context.route(`**${path}`, route => route.fulfill({ json: { permissions: items } }));
  await context.route(`**${path}/${first.id}/respond`, route => {
    posts++; items = items.map(item => item.id === first.id ? { ...item, status: "approved", decision: "once" } : item);
    return route.fulfill({ json: { ...first, status: "approved", decision: "once" } });
  });
  const input = await start(page, api); await input.fill("审批队列中保留的草稿");
  await expect(page.locator(".permission-card")).toHaveCount(1);
  await expect(page.locator(".permission-card pre")).toHaveText("npm test");
  await expect(page.locator(".permission-card")).toContainText("另有 1 项待确认");
  items = [incoming, ...items]; api.permissionEvent("PERMISSION_REQUESTED", { permission_id: incoming.id, tool: "bash" });
  await expect(page.locator(".permission-card")).toContainText("另有 2 项待确认");
  await expect(page.locator(".permission-card pre")).toHaveText("npm test");
  await expect(input).toBeFocused();
  await page.getByRole("button", { name: "允许一次", exact: true }).click();
  await expect(page.locator(".permission-card")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "允许一次", exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "查看下一项", exact: true })).toBeVisible();
  await page.screenshot({ path: info.outputPath("approval-next-request.png") });
  expect(posts).toBe(1);
  await page.getByRole("button", { name: "查看下一项", exact: true }).click();
  await expect(page.locator(".permission-card pre")).toHaveText("git status");
  await expect(page.locator(".permission-grants")).not.toHaveAttribute("open");
  await expect(input).toHaveValue("审批队列中保留的草稿"); expect(posts).toBe(1);
});

test("GitHub publication has no broader authorization choice", async ({ page, context, api }) => {
  const publish = { ...first, tool: "github_publish", summary: { repository: "example/project", branch: "lester/review", summary: "更新已预览的首页" } };
  await context.route(`**${path}`, route => route.fulfill({ json: { permissions: [publish] } }));
  await start(page, api);
  const card = page.locator(".permission-card");
  await expect(card).toContainText("example/project"); await expect(card).toContainText("lester/review");
  await expect(card.getByRole("button", { name: "允许一次", exact: true })).toBeVisible();
  await expect(card.getByText("更多授权", { exact: true })).toHaveCount(0);
  await expect(card.getByRole("button")).toHaveCount(2);
});
