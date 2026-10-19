import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";

type NativeMessage = { version: number; type: string; payload: Record<string, unknown> };
const test = base.extend<{ api: WorkspaceApi; nativeMessages: NativeMessage[] }>({
  api: async ({ context }, run) => {
    const api = new WorkspaceApi(); await api.install(context);
    try { await run(api); expect(api.unexpected).toEqual([]); } finally { await api.close(); }
  },
  nativeMessages: async ({ context }, run, info) => {
    const messages: NativeMessage[] = [];
    await context.exposeBinding("captureNative", (_, message: NativeMessage) => messages.push(message));
    await context.addInitScript((platform: string) => {
      if (window.top !== window) return;
      Object.defineProperty(window, "__LESTER_NATIVE__", { value: Object.freeze({ version: 1, platform }) });
      if (platform === "android") Object.defineProperty(window, "LesterAndroid", { value: { postMessage: (value: string) => { void (window as unknown as { captureNative: (value: unknown) => Promise<void> }).captureNative(JSON.parse(value)); } } });
      else Object.defineProperty(window, "webkit", { value: { messageHandlers: { lester: { postMessage: (value: unknown) => { void (window as unknown as { captureNative: (value: unknown) => Promise<void> }).captureNative(value); } } } } });
    }, info.project.name === "mobile" ? "android" : "ios");
    await run(messages);
  },
});
async function authenticate(page: Page) {
  await page.goto("/login?returnTo=%2Fapp%2Fnew");
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeVisible();
}

test("native phone and tablet keep task, results and drafts with desktop tools removed", async ({ page, api, nativeMessages }, info) => {
  // Both iPhone-sized and iPad-sized viewports exercise the production bundle.
  await page.setViewportSize(info.project.name === "mobile" ? { width: 393, height: 852 } : { width: 1024, height: 768 });
  const errors: string[] = []; page.on("pageerror", error => errors.push(error.message));
  await authenticate(page);
  await expect(page.locator("html")).toHaveAttribute("data-lester-native", info.project.name === "mobile" ? "android" : "ios");
  await expect(page.locator(".conversation-sidebar")).toBeHidden();
  await expect(page.getByRole("button", { name: "打开会话栏", exact: true })).toBeHidden();
  const composer = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await composer.fill(prompt); await composer.press("Enter");
  await expect(page).toHaveURL(`/app/c/${conversationId}`);
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeVisible();
  await expect.poll(() => nativeMessages.filter(m => m.type === "workspace" && (m.payload.conversations as { run_status: string }[]).some(c => c.run_status === "running")).length).toBeGreaterThan(0);
  await expect.poll(() => api.streamCount).toBe(1);
  api.finish();
  await expect.poll(() => nativeMessages.filter(m => m.type === "run_completed").length).toBe(1);
  const snapshot = nativeMessages.findLast(m => m.type === "workspace")!;
  expect(snapshot.payload).toMatchObject({ display_name: "浏览器测试", projects: [{ id: "fixture-project", name: "个人项目" }] });
  expect(snapshot.payload.conversations).toEqual(expect.arrayContaining([expect.objectContaining({ id: conversationId, project_name: "个人项目" })]));
  const allPayloads = JSON.stringify(nativeMessages);
  expect(allPayloads).not.toContain("fixture-password"); expect(allPayloads).not.toContain("lester_access_token"); expect(allPayloads).not.toContain("fixture-assistant");
  if (info.project.name === "mobile") {
    await page.getByRole("button", { name: "成果", exact: true }).click();
    await page.getByRole("button", { name: "预览成果 index.html", exact: true }).click();
  }
  await expect(page.getByRole("tab", { name: "index.html", exact: true })).toBeVisible();
  await expect(page.frameLocator('iframe[title="index.html preview"]').getByRole("heading", { name: "浏览器回归产物" })).toBeVisible();
  await expect(page.locator(".workspace-tools")).toHaveCount(0);
  await page.screenshot({ path: info.outputPath("native-workspace.png") });
  if (info.project.name === "mobile") await page.getByRole("button", { name: "返回对话", exact: true }).click();
  await composer.fill("还没发送的修改");
  const mutations = api.mutations.length;
  await page.evaluate(() => window.dispatchEvent(new CustomEvent("lester:native-command", { detail: { action: "home" } })));
  await expect(page).toHaveURL(/\/app$/);
  await page.evaluate(id => window.dispatchEvent(new CustomEvent("lester:native-command", { detail: { action: "conversation", id } })), conversationId);
  await expect(composer).toHaveValue("还没发送的修改");
  await page.reload(); await expect(composer).toHaveValue("还没发送的修改");
  expect(nativeMessages.filter(m => m.type === "run_completed")).toHaveLength(1);
  expect(api.mutations).toHaveLength(mutations); expect(errors).toEqual([]);
});

test("a URL parameter does not enable native mode in an ordinary browser", async ({ page, api }) => {
  await authenticate(page);
  await page.goto("/app/new?native=1");
  await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeVisible();
  await expect(page.locator("html")).not.toHaveAttribute("data-lester-native", "ios");
  expect(api.mutations.filter(item => item.path.startsWith("/api/v1/conversations"))).toEqual([]);
});

test("native sign-in confirmation is explicit and rejects invalid handoff parameters", async ({ page, context, api }, info) => {
  await authenticate(page);
  let handoffs = 0;
  await context.route("**/api/v1/auth/native/handoff", route => { handoffs++; return route.fulfill({ status: 503, json: { error: "fixture unavailable" } }); });
  await page.goto("/app/native-auth?challenge=invalid&state=invalid");
  const button = page.getByRole("button", { name: "登录到 App", exact: true });
  await expect(button).toBeEnabled(); expect(handoffs).toBe(0);
  await button.click(); await expect(page.locator(".login-card").getByRole("alert")).toContainText("App 登录请求无效"); expect(handoffs).toBe(0);
  const valid = "a".repeat(43);
  await page.goto(`/app/native-auth?challenge=${valid}&state=${valid}`);
  await expect(button).toBeEnabled(); expect(handoffs).toBe(0);
  await page.screenshot({ path: info.outputPath("native-sign-in.png") });
  await button.click(); await expect(page.locator(".login-card").getByRole("alert")).toContainText("App 登录未完成"); expect(handoffs).toBe(1);
  expect(api.mutations.filter(item => item.path.startsWith("/api/v1/conversations"))).toEqual([]);
});
