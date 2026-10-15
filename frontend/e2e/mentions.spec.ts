import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";

const sourceId = "33333333-3333-4333-8333-333333333333";
const contextId = "44444444-4444-4444-8444-444444444444";
const skillId = "55555555-5555-4555-8555-555555555555";
const test = base.extend<{ api: WorkspaceApi }>({
  api: async ({ context, page }, runFixture) => {
    const api = new WorkspaceApi(), errors: string[] = [];
    page.on("pageerror", error => errors.push(error.message));
    api.contextEntries = [{ id: contextId, title: "项目背景", description: "交付要求", content: "必须支持中文与英文", version: 1, updated_at: "2026-01-01T00:00:00Z" }];
    api.referenceConversations = [{ id: sourceId, title: "产品 讨论", project_id: "fixture-project", workspace_id: "fixture-workspace", created_by: "fixture-user", agent_slug: "lester", agent_name: "Lester", model_deployment_id: "fixture-model", pinned: false, run_status: "completed", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" }];
    api.referenceMessages[sourceId] = Array.from({ length: 6 }, (_, i) => ({ id: `source-${i}`, role: i % 2 ? "assistant" : "user", content: `讨论消息 ${i + 1}：先完成主页，再讨论发布。`, created_at: "2026-01-01T00:00:00Z" }));
    api.skillCatalog = [{ id: skillId, slug: "conversation-reader", name: "Conversation Reader", description: "读取明确引用的会话，整理背景、已确认的决定和待办。", version: "1.0.0", source: "builtin", size_bytes: 1000 }];
    await api.install(context);
    try { await runFixture(api); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); }
    finally { await api.close(); }
  },
});
async function login(page: Page) {
  await page.goto("/app");
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeVisible();
}
async function choose(page: Page, title: string) {
  await page.getByRole("button", { name: "添加内容", exact: true }).click();
  await page.getByRole("button", { name: "@ 添加引用", exact: true }).click();
  await page.getByRole("option", { name: new RegExp(title) }).click();
}
async function pickerExposed(page: Page) {
  const option = page.getByRole("listbox", { name: "引用对象" }).getByRole("option").first();
  await expect.poll(() => option.evaluate(node => {
    const r = node.getBoundingClientRect(), top = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
    return r.top >= 0 && r.bottom <= innerHeight && r.left >= 0 && r.right <= innerWidth && !!top && node.contains(top);
  })).toBe(true);
}

test("mixed references preview, persist and send only typed IDs on explicit submit", async ({ page, api }, info) => {
  await login(page);
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  const options = page.getByRole("listbox", { name: "引用对象" }).getByRole("option");
  await input.fill("请整理 @会话 产品 讨论");
  await expect(options).toHaveCount(1);
  await pickerExposed(page);
  await page.screenshot({ path: info.outputPath("conversation-search.png") });
  await input.press("Enter");
  await expect(input).toHaveValue("请整理 ");
  await choose(page, "Conversation Reader");
  await choose(page, "项目背景");
  await expect(page.locator(".context-references > span")).toHaveCount(3);
  expect(api.mutations.filter(item => item.path.startsWith("/api/v1/conversations"))).toEqual([]);
  const source = page.getByRole("button", { name: "查看会话引用 产品 讨论", exact: true });
  await source.click();
  const preview = page.getByRole("dialog", { name: "查看会话引用 产品 讨论", exact: true });
  await expect(preview).toContainText("讨论消息 5");
  await expect(preview).not.toContainText("讨论消息 6");
  await expect(preview.getByRole("link", { name: "打开原会话", exact: true })).toHaveAttribute("href", `/app/c/${sourceId}`);
  await page.screenshot({ path: info.outputPath("conversation-inspection.png") });
  await page.getByRole("button", { name: "关闭引用预览", exact: true }).press("Escape");
  await expect(source).toBeFocused();
  await page.getByRole("button", { name: "查看 Skill 引用 Conversation Reader", exact: true }).click();
  await expect(page.locator(".context-reference-preview")).toContainText("当前版本 · v1.0.0");
  await expect(page.locator(".context-reference-preview")).toContainText("发送后将尚未安装的 Skill");
  await page.getByRole("button", { name: "关闭引用预览", exact: true }).click();
  await page.reload();
  await expect(input).toHaveValue("请整理 ");
  await expect(page.locator(".context-references > span")).toHaveCount(3);
  await page.getByRole("button", { name: "添加内容", exact: true }).click();
  await page.getByRole("button", { name: "@ 添加引用", exact: true }).click();
  await expect(options).toHaveCount(0); // selected items cannot be duplicated
  await input.press("Escape");
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  await expect(page).toHaveURL(`/app/c/${conversationId}`);
  await expect(page.locator(".message-reference-list")).toContainText("产品 讨论");
  await expect(page.locator(".message-reference-list")).toContainText("Conversation Reader");
  await expect(page.locator(".message-context")).toContainText("项目背景");
  const sent = api.mutations.filter(item => item.path.endsWith("/messages"));
  expect(sent).toHaveLength(1);
  expect(sent[0].body).toEqual({ content: "请整理", permission_mode: "standard", attachment_ids: [], context_ids: [contextId], references: [{ type: "conversation", id: sourceId }, { type: "skill", id: skillId }], submission_id: expect.stringMatching(/^[0-9a-f-]{36}$/) });
});

test("failed first send keeps all reference drafts; new-task grants exclude the current chat", async ({ page, api }, info) => {
  await login(page);
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill(prompt);
  await choose(page, "产品 讨论"); await choose(page, "Conversation Reader"); await choose(page, "项目背景");
  api.failSend = true;
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  await expect(page).toHaveURL(`/app/c/${conversationId}`);
  await expect(page.getByText(/会话已创建，但首条消息未确认发送成功/)).toBeVisible();
  await expect(page.locator(".context-references > span")).toHaveCount(3);
  await page.reload();
  await expect(input).toHaveValue(prompt);
  await expect(page.locator(".context-references > span")).toHaveCount(3);
  expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(1);
  // Delete a focused chip without modifying the request, and inspect the remaining category.
  await page.getByRole("button", { name: "查看会话引用 产品 讨论", exact: true }).press("Delete");
  await page.getByRole("button", { name: "添加内容", exact: true }).click();
  await page.getByRole("button", { name: "@ 添加引用", exact: true }).click();
  await page.getByRole("group", { name: "引用类型", exact: true }).getByRole("button", { name: "会话", exact: true }).click();
  await expect(page.getByRole("listbox", { name: "引用对象" }).getByRole("option")).toHaveCount(1);
  await expect(page.getByRole("listbox", { name: "引用对象" }).getByRole("option")).toContainText("产品 讨论");
  await pickerExposed(page);
  await page.screenshot({ path: info.outputPath("conversation-filter.png") });
  await input.press("Enter");
  api.failSend = false;
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeVisible();
  await input.fill("运行期间准备下一轮");
  await choose(page, "产品 讨论");
  await input.press("Enter");
  await expect(input).toHaveValue("运行期间准备下一轮");
  await expect(page.locator(".context-references")).toContainText("产品 讨论");
  expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(2);
});

test("reference search retries safely, composition does not select, and caret suffix is retained", async ({ page, api }, info) => {
  api.failReferenceSearch = true;
  await login(page);
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill("保留草稿");
  await page.getByRole("button", { name: "添加内容", exact: true }).click();
  await page.getByRole("button", { name: "@ 添加引用", exact: true }).click();
  await expect(page.locator(".mention-picker [role=alert]")).toContainText("引用对象加载失败");
  api.failReferenceSearch = false;
  await page.locator(".mention-picker").getByRole("button", { name: "重试", exact: true }).click();
  await expect(page.getByRole("listbox", { name: "引用对象" }).getByRole("option")).toHaveCount(3);
  await pickerExposed(page);
  await page.screenshot({ path: info.outputPath("all-references.png") });
  await input.press("Escape");
  const text = "前缀 @skill Conversation Reader 后缀";
  await input.fill(text);
  await input.evaluate((node: HTMLTextAreaElement, caret) => { node.setSelectionRange(caret, caret); node.click(); }, text.indexOf(" 后缀"));
  await expect(page.getByRole("listbox", { name: "引用对象" }).getByRole("option")).toHaveCount(1);
  await input.dispatchEvent("keydown", { key: "Enter", isComposing: true });
  await expect(page.locator(".context-references > span")).toHaveCount(0);
  await input.press("Enter");
  await expect(input).toHaveValue("前缀  后缀");
  expect(await input.evaluate((node: HTMLTextAreaElement) => node.selectionStart)).toBe(3);
  await expect(page.locator(".context-references")).toContainText("Conversation Reader");
  expect(api.mutations.filter(item => item.path.startsWith("/api/v1/conversations"))).toEqual([]);
});

test("installed Skill panel refreshes from a confirmed installation event", async ({ page, api }) => {
  await login(page);
  await page.getByRole("textbox", { name: "消息输入框", exact: true }).fill("请根据引用整理已确认的决定");
  await choose(page, "产品 讨论"); await choose(page, "Conversation Reader");
  await page.getByRole("button", { name: "发送消息", exact: true }).click();
  await expect(page).toHaveURL(`/app/c/${conversationId}`);
  await expect.poll(() => api.streamCount).toBeGreaterThan(0);
  const workspace = page.getByRole("button", { name: "工作区", exact: true });
  if (await workspace.isVisible()) await workspace.click();
  else await page.getByRole("button", { name: "成果", exact: true }).click();
  await page.locator(".workspace-tools summary").click();
  await page.locator(".workspace-tools-menu").getByRole("button", { name: "技能", exact: true }).click();
  const skill = page.locator(".conversation-skill").filter({ hasText: "Conversation Reader" });
  await expect(skill.getByRole("button", { name: "安装", exact: true })).toBeVisible();
  // This fixture event tests UI refresh only; PostgreSQL/tool tests verify real installation.
  api.skillInstalled("conversation-reader");
  await expect(skill.getByRole("button", { name: "卸载", exact: true })).toBeVisible();
  expect(api.mutations.some(item => item.path.endsWith("/install"))).toBe(false);
});

test("language switching and model navigation preserve typed reference drafts", async ({ page, api }, info) => {
  await login(page);
  await page.getByRole("textbox", { name: "消息输入框", exact: true }).fill("保留我的请求");
  await choose(page, "项目背景"); await choose(page, "产品 讨论"); await choose(page, "Conversation Reader");
  if (!await page.locator(".user-menu-trigger").isVisible()) await page.getByRole("button", { name: "打开会话栏", exact: true }).click();
  await page.locator(".user-menu-trigger").click();
  await page.locator(".user-menu-popover .language-trigger").click();
  await page.getByRole("menuitemradio", { name: "Français", exact: true }).click();
  await page.getByRole("menuitem", { name: "Modèles", exact: true }).click();
  await expect(page).toHaveURL(/settings\/models/);
  await page.goBack(); await page.reload();
  await expect(page.locator("html")).toHaveAttribute("lang", "fr");
  await expect(page.getByRole("textbox", { name: "Saisie du message", exact: true })).toHaveValue("保留我的请求");
  await expect(page.locator(".context-references > span")).toHaveCount(3);
  await page.getByRole("button", { name: "Examiner la référence de Skill Conversation Reader", exact: true }).click();
  await expect(page.locator(".context-reference-preview")).toContainText("Lire les conversations explicitement référencées");
  await page.screenshot({ path: info.outputPath("fr-skill-reference.png") });
  expect(api.mutations.filter(item => item.path.startsWith("/api/v1/conversations"))).toEqual([]);
});
