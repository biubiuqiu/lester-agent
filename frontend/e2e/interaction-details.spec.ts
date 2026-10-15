import { test as base, expect, type Locator, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";

const test = base.extend<{ api: WorkspaceApi }>({
  api: async ({ context, page }, runFixture) => {
    const api = new WorkspaceApi();
    const errors: string[] = [];
    page.on("pageerror", error => errors.push(error.message));
    page.on("console", message => { if (message.type() === "error" && !message.text().includes("status of 401")) errors.push(message.text()); });
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
async function start(page: Page, api: WorkspaceApi) {
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill(prompt); await input.press("Enter");
  await expect(page).toHaveURL(`/app/c/${conversationId}`);
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeVisible();
  await expect.poll(() => api.streamCount).toBeGreaterThan(0);
}
async function closePanel(page: Page) {
  const close = page.locator(".computer-panel .close-files-panel");
  if (await close.isVisible()) await close.click();
  else await page.getByRole("button", { name: "工作区", exact: true }).click();
}
async function selectText(locator: Locator) {
  await locator.evaluate(node => {
    const range = document.createRange(); range.selectNodeContents(node);
    const selection = window.getSelection()!; selection.removeAllRanges(); selection.addRange(range);
    node.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
  });
}

test("composer expands and bounds long drafts; typing active-run follow-ups never submits", async ({ page, api }, info) => {
  await login(page);
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  const empty = (await input.boundingBox())!.height;
  await input.fill("第一行\n第二行\n第三行\n第四行\n第五行");
  await expect.poll(async () => (await input.boundingBox())!.height).toBeGreaterThan(empty);
  await input.fill(Array.from({ length: 30 }, (_, i) => `第 ${i + 1} 行`).join("\n"));
  expect(await input.evaluate(node => node.scrollHeight > node.clientHeight && node.clientHeight < 240)).toBe(true);
  await page.emulateMedia({ reducedMotion: "reduce" });
  await input.fill("");
  // Chromium may differ by a fraction of a CSS pixel after layout/animation.
  await expect.poll(async () => (await input.boundingBox())!.height).toBeCloseTo(empty, 1);
  await start(page, api);
  await input.fill("请稍后继续处理这份材料");
  await expect(page.getByText("发送将排队，当前任务完成后开始；也可选择调整当前任务。", { exact: true })).toBeVisible();
  await expect(input).toHaveValue("请稍后继续处理这份材料");
  await page.locator('.composer input[type="file"]').setInputFiles({ name: "下一轮.txt", mimeType: "text/plain", buffer: Buffer.from("draft material") });
  await expect(page.locator(".pending-attachments")).toContainText("下一轮.txt");
  expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(1);
  expect(await input.evaluate(node => node.getBoundingClientRect().bottom <= innerHeight)).toBe(true);
  await page.screenshot({ path: info.outputPath("draft-running.png") });
});

test("selected context inspection uses current contents and keyboard removal preserves the draft", async ({ page, api }, info) => {
  api.contextEntries = [{ id: "context-a", title: "项目背景", description: "测试资料", content: "原始背景", version: 1, updated_at: "2026-01-01T00:00:00Z" }];
  await login(page);
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill("保留草稿 ");
  await page.getByRole("button", { name: "添加内容", exact: true }).click();
  await page.getByRole("button", { name: "@ 添加引用", exact: true }).click();
  await page.getByRole("option", { name: /项目背景/ }).click();
  api.contextEntries[0] = { ...api.contextEntries[0], version: 2, content: "更新后的背景资料" };
  const chip = page.getByRole("button", { name: "查看上下文 项目背景", exact: true });
  await chip.click();
  await expect(page.locator(".context-reference-preview")).toContainText("当前版本 · v2");
  await expect(page.locator(".context-reference-preview pre")).toHaveText("更新后的背景资料");
  await expect(input).toHaveValue("保留草稿 ");
  await page.screenshot({ path: info.outputPath("context-inspection.png") });
  await chip.press("Escape");
  await expect(chip).toBeFocused();
  await chip.press("Delete");
  await expect(chip).toHaveCount(0); await expect(input).toBeFocused();
  await expect(input).toHaveValue("保留草稿 ");
  expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(0);
});

test("local image previews, duplicate skipping and oversized rejection do not submit tasks", async ({ page, api }, info) => {
  await login(page);
  const fileInput = page.locator('.new-chat-composer input[type="file"]');
  await fileInput.setInputFiles({ name: "截图.png", mimeType: "image/png", buffer: Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+j/a8AAAAASUVORK5CYII=", "base64") });
  const preview = page.getByRole("button", { name: "预览图片 截图.png", exact: true });
  await expect(preview).toBeVisible();
  await expect.poll(() => preview.locator("img").evaluate(node => (node as HTMLImageElement).naturalWidth)).toBe(1);
  await preview.click();
  await expect(page.getByRole("dialog", { name: "图片预览", exact: true })).toBeVisible();
  await page.screenshot({ path: info.outputPath("attachment-preview.png") });
  await page.getByRole("button", { name: "关闭图片预览", exact: true }).press("Escape");
  await expect(preview).toBeFocused();
  // Input is cleared after selection. Repeat an explicitly identical File twice.
  await fileInput.evaluate((node: HTMLInputElement) => {
    const file = new File(["same"], "重复.txt", { type: "text/plain", lastModified: 1 });
    const transfer = new DataTransfer(); transfer.items.add(file); transfer.items.add(file); node.files = transfer.files;
    node.dispatchEvent(new Event("change", { bubbles: true }));
  });
  await expect(page.getByText("已忽略重复附件：重复.txt", { exact: true })).toBeVisible();
  await expect(page.locator(".pending-attachment")).toHaveCount(2);
  await fileInput.evaluate((node: HTMLInputElement) => {
    const transfer = new DataTransfer(); transfer.items.add(new File([new Uint8Array((25 << 20) + 1)], "超大.bin")); node.files = transfer.files;
    node.dispatchEvent(new Event("change", { bubbles: true }));
  });
  await expect(page.getByText("超大.bin 超过 25 MiB，请选择更小的文件。", { exact: true })).toBeVisible();
  await expect(page.locator(".pending-attachment")).toHaveCount(2);
  expect(api.mutations.filter(item => item.path.startsWith("/api/v1/conversations"))).toHaveLength(0);
});

test("response copying confirms real clipboard writes; quoting appends without sending", async ({ page, context, api }, info) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  const errors: string[] = []; page.on("pageerror", error => errors.push(error.message));
  await login(page); await start(page, api); api.finish();
  api.messages[1].content = "## 测试标题\n\n这段结论用于追问。\n\n```python\nprint('Lester')\n```";
  await expect(page.getByRole("tab", { name: "index.html", exact: true })).toBeVisible();
  await closePanel(page);
  const reply = page.locator(".chat-message.assistant");
  await expect(reply).toContainText("测试标题");
  await reply.getByRole("button", { name: "复制代码", exact: true }).click();
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe("print('Lester')\n");
  await reply.getByRole("button", { name: "保留格式复制", exact: true }).click();
  let rich = "";
  await expect.poll(async () => {
    rich = await page.evaluate(async () => {
      const [item] = await navigator.clipboard.read();
      return item?.types.includes("text/html") ? (await item.getType("text/html")).text() : "";
    });
    return rich;
  }).toContain("<h2>");
  expect(rich).not.toContain("复制代码");
  await page.screenshot({ path: info.outputPath("copy-success.png") });
  await page.evaluate(() => Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async () => { throw Error("fixture clipboard denied"); } } }));
  await reply.getByRole("button", { name: "复制回答", exact: true }).click();
  await expect(reply.locator(".copy-error")).toContainText("复制失败");
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill("已有草稿");
  // Resizing the composer can scroll the thread and dismiss a text selection.
  await expect.poll(() => input.evaluate(node => node.getAnimations().length)).toBe(0);
  await selectText(reply.locator(".markdown-content p").first());
  await page.getByRole("button", { name: "引用并追问", exact: true }).click();
  await expect(input).toHaveValue("已有草稿\n\n> 引用回答\n> 这段结论用于追问。\n\n");
  await expect(input).toBeFocused();
  expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(1);
  expect(errors).toEqual([]);
  await expect.poll(() => input.evaluate(node => node.getAnimations().length)).toBe(0);
  await page.screenshot({ path: info.outputPath("quoted-draft.png") });
});

test("file updates preserve selection; reopening restores source reading position", async ({ page, api }, info) => {
  await login(page); await start(page, api); api.finish();
  await expect(page.getByRole("tab", { name: "index.html", exact: true })).toBeVisible();
  api.writeFile("notes.txt", Array.from({ length: 180 }, (_, i) => `说明 ${i + 1}`).join("\n"));
  await page.getByRole("tab", { name: "notes.txt", exact: true }).click();
  const source = page.locator(".file-source-scroll");
  await expect(source).toContainText("说明 180");
  await source.evaluate(node => { node.scrollTop = 420; });
  await page.getByRole("button", { name: "关闭 notes.txt", exact: true }).click();
  await page.reload();
  await expect(page.getByRole("tab", { name: "index.html", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "重新打开刚关闭的标签", exact: true }).click();
  await expect(page.getByRole("tab", { name: "notes.txt", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect.poll(() => source.evaluate(node => node.scrollTop)).toBe(420);
  // Reload creates a new SSE connection; do not emit the update before it exists.
  await expect.poll(() => api.streamCount).toBeGreaterThan(0);
  api.writeFile("index.html", "<html><body>更新后的网页</body></html>");
  const htmlTab = page.getByRole("tab", { name: "index.html", exact: true });
  await expect(htmlTab.locator(".file-unread-dot")).toBeVisible();
  await expect(page.getByRole("tab", { name: "notes.txt", exact: true })).toHaveAttribute("aria-selected", "true");
  await page.screenshot({ path: info.outputPath("file-update-dot.png") });
  await htmlTab.click();
  await expect(page.frameLocator(".file-preview-body iframe").getByText("更新后的网页")).toBeVisible();
  await expect(htmlTab.locator(".file-unread-dot")).toHaveCount(0);
});

test("source selection remains clickable inside modal previews and quotes the path and lines", async ({ page, api }, info) => {
  await login(page); await start(page, api); api.finish();
  await expect(page.getByRole("tab", { name: "index.html", exact: true })).toBeVisible();
  api.writeFile("notes.txt", "first line\nsecond line\nthird line");
  await page.getByRole("tab", { name: "notes.txt", exact: true }).click();
  await expect(page.locator(".file-source-code")).toContainText("first line");
  if (info.project.name === "desktop") await page.getByRole("button", { name: "专注预览", exact: true }).click();
  await page.locator(".file-source-code").evaluate(node => {
    const codes = node.querySelectorAll(".file-source-line code");
    const range = document.createRange(); range.setStart(codes[0], 0); range.setEnd(codes[1], codes[1].childNodes.length);
    const selection = window.getSelection()!; selection.removeAllRanges(); selection.addRange(range);
    node.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
  });
  const quote = page.getByRole("button", { name: "引用并追问", exact: true });
  await expect(quote).toBeVisible();
  expect(await quote.evaluate(node => {
    const rect = node.getBoundingClientRect();
    return node.contains(document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2));
  })).toBe(true);
  await page.screenshot({ path: info.outputPath("source-quote.png") });
  await quote.click();
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await expect(input).toHaveValue("> notes.txt:1–2\n> first line\n> second line\n\n");
  await expect(input).toBeFocused();
  expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(1);
});
