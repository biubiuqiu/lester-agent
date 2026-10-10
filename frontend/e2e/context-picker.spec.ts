import { test, expect, type Locator, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";

async function login(page: Page) {
  await page.goto("/app");
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeVisible();
}

async function exposed(option: Locator) {
  await expect.poll(() => option.evaluate(element => {
    const rect = element.getBoundingClientRect();
    const front = document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2);
    return rect.top >= 0 && rect.bottom <= innerHeight && rect.left >= 0 && rect.right <= innerWidth && !!front && element.contains(front);
  })).toBe(true);
}

for (const existing of [false, true]) {
  test(`context picker is clickable and keyboard accessible in ${existing ? "existing" : "new"} conversations`, async ({ page, context }, testInfo) => {
    const api = new WorkspaceApi();
    api.contextEntries = Array.from({ length: 8 }, (_, i) => ({ id: `context-${i}`, title: `项目背景${i + 1}`, description: "目标、受众和交付要求。", content: "固定测试资料", version: 1, updated_at: "2026-01-01T00:00:00Z" }));
    await api.install(context);
    const errors: string[] = [], consoleErrors: string[] = [];
    page.on("console", message => { if (message.type() === "error" && !message.text().includes("status of 401")) consoleErrors.push(message.text()); });
    page.on("pageerror", error => errors.push(error.message));
    try {
      await login(page);
      const composer = page.getByRole("textbox", { name: "消息输入框", exact: true });
      if (existing) {
        await composer.fill(prompt); await composer.press("Enter");
        await expect(page).toHaveURL(`/app/c/${conversationId}`);
        // A shorter viewport exercises the space available when a mobile keyboard opens.
        await page.setViewportSize({ width: testInfo.project.use.viewport!.width, height: 480 });
      }
      await composer.fill("保留这个草稿 ");
      await page.getByRole("button", { name: "@ 引用上下文", exact: true }).click();
      const options = page.getByRole("listbox", { name: "上下文词条" }).getByRole("option");
      await expect(options).toHaveCount(8);
      await page.screenshot({ path: testInfo.outputPath("context-picker.png") });
      await exposed(options.first());
      if (!existing) {
        await page.setViewportSize({ width: testInfo.project.use.viewport!.width, height: 600 });
        await exposed(options.first());
      }
      if (existing) {
        for (let i = 0; i < 7; i++) await composer.press("ArrowDown");
        await expect(options.last()).toHaveAttribute("aria-selected", "true");
        await exposed(options.last());
        await composer.press("Enter");
      } else {
        await options.last().scrollIntoViewIfNeeded(); await exposed(options.last());
        await options.last().click();
      }
      await expect(page.locator(".context-references")).toContainText("@项目背景8");
      await expect(composer).toHaveValue("保留这个草稿 ");
      await expect(options).toHaveCount(0);
      await composer.fill("保留这个草稿 @项目背景2");
      await expect(options).toHaveCount(1); await exposed(options.first());
      await composer.press("Escape");
      await expect(options).toHaveCount(0);
      await expect(composer).toHaveValue("保留这个草稿 @项目背景2");
      await page.getByRole("button", { name: "@ 引用上下文", exact: true }).click();
      await expect(options).toHaveCount(7);
      await page.locator(".conversation-header").click({ position: { x: 1, y: 1 } });
      await expect(options).toHaveCount(0);
      expect(consoleErrors).toEqual([]);
      expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(existing ? 1 : 0);
      expect(api.unexpected).toEqual([]); expect(errors).toEqual([]);
    } finally { await api.close(); }
  });
}
