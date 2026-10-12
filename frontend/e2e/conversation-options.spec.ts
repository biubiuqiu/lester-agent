import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test, expect } from "@playwright/test";
import { translator } from "../src/lib/i18n";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";

for (const locale of ["zh-CN", "fr"] as const) {
  test.describe(locale, () => {
    test.use({ locale });
    test("conversation options float without moving the header or preview", async ({ page, context }, info) => {
      const t = translator(locale === "zh-CN" ? {} : JSON.parse(readFileSync(join(process.cwd(), `src/lib/i18n/messages/${locale}.json`), "utf8")));
      const api = new WorkspaceApi();
      api.modelName = "deepseek-v4-flash-vision-exp-long-provider-name";
      await api.install(context);
      const errors: string[] = [];
      page.on("pageerror", error => errors.push(error.message));
      try {
        await page.goto("/app");
        await page.getByLabel(t("邮箱"), { exact: true }).fill("browser@example.test");
        await page.getByLabel(t("密码"), { exact: true }).fill("fixture-password-123");
        await page.getByRole("button", { name: t("登录"), exact: true }).click();
        page.on("console", message => { if (message.type() === "error") errors.push(message.text()); });
        const composer = page.getByRole("textbox", { name: t("消息输入框"), exact: true });
        await composer.fill(prompt);
        await composer.press("Enter");
        await expect(page).toHaveURL(`/app/c/${conversationId}`);
        await expect(page.locator(".sidebar-footer")).toContainText("browser@example.test");
        await expect(page.getByRole("button", { name: t("停止生成"), exact: true })).toBeVisible();
        await expect.poll(() => api.streamCount).toBeGreaterThan(0);
        api.finish();
        const panel = page.locator(".computer-panel");
        await expect(panel.locator('iframe[title="index.html preview"]')).toBeVisible();
        const compact = info.project.name === "mobile";
        if (compact) {
          await panel.getByRole("button", { name: t("返回对话"), exact: true }).click();
          await page.setViewportSize({ width: 320, height: 839 });
        } else await page.setViewportSize({ width: 1100, height: 960 });
        await composer.fill("保留正在写的草稿");
        await page.locator(".workspace-shell").evaluate(async element => {
          await Promise.all(element.getAnimations().map(animation => animation.finished.catch(() => {})));
        });
        const header = page.locator(".conversation-header");
        const before = (await header.boundingBox())!;
        expect((await header.locator(".agent-heading small").boundingBox())!.height).toBeLessThanOrEqual(20);
        expect(await header.locator(".conversation-run-label").evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
        if (!compact) {
          await expect(header.getByRole("button", { name: t("工作区"), exact: true })).toBeVisible();
          await expect(header.getByRole("button", { name: t("成果"), exact: true })).toHaveCount(0);
          await expect(header.getByRole("button", { name: t("文件"), exact: true })).toHaveCount(0);
          await expect(header.locator(".conversation-agent-name")).toHaveText("Lester");
        }
        const previewBefore = compact ? null : await panel.boundingBox();
        const trigger = page.getByRole("button", { name: t("更多会话选项"), exact: true });
        const popup = page.getByRole("dialog", { name: t("更多会话选项"), exact: true });
        await trigger.click();
        await expect(popup).toBeVisible();
        await popup.evaluate(async element => { await Promise.all(element.getAnimations().map(animation => animation.finished.catch(() => {}))); });
        await expect(trigger).toHaveAttribute("aria-expanded", "true");
        const select = popup.getByRole("combobox", { name: t("切换模型"), exact: true });
        await expect(select).toBeFocused();
        expect(await popup.evaluate(element => element.parentElement === document.body && getComputedStyle(element).position === "fixed")).toBe(true);
        const bounds = (await popup.boundingBox())!;
        expect(bounds.x).toBeGreaterThanOrEqual(7);
        expect(bounds.x + bounds.width).toBeLessThanOrEqual(page.viewportSize()!.width - 7);
        expect(bounds.y).toBeGreaterThanOrEqual(before.y + before.height - 8);
        const after = (await header.boundingBox())!;
        expect(after).toEqual(before);
        if (previewBefore) expect(await panel.boundingBox()).toEqual(previewBefore);
        expect(await popup.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
        expect(await popup.getByRole("link").evaluate(element => getComputedStyle(element).textDecorationLine)).toBe("none");
        await page.screenshot({ path: info.outputPath("options-open.png") });
        await select.press("Escape");
        await expect(popup).toHaveCount(0);
        await expect(trigger).toBeFocused();
        await trigger.click();
        await composer.click();
        await expect(popup).toHaveCount(0);
        await expect(composer).toHaveValue("保留正在写的草稿");
        await trigger.click();
        await popup.getByRole("combobox", { name: t("切换模型"), exact: true }).selectOption("fixture-model");
        await expect(popup).toHaveCount(0);
        await expect.poll(() => api.mutations.filter(item => item.path === `/api/v1/conversations/${conversationId}`).length).toBe(1);
        await trigger.click();
        await popup.getByRole("button", { name: t("新手引导"), exact: true }).click();
        await expect(popup).toHaveCount(0);
        await expect(page.locator(".guide-dialog")).toBeVisible();
        await page.getByRole("button", { name: t("关闭新手引导"), exact: true }).click();
        await expect(trigger).toBeFocused();
        await trigger.click();
        await popup.getByRole("button", { name: t("查看会话文件"), exact: true }).click();
        await expect(popup).toHaveCount(0);
        await expect(panel).toBeVisible();
        await expect(panel.locator('iframe[title="index.html preview"]')).toBeVisible();
        expect(api.unexpected).toEqual([]);
        expect(errors).toEqual([]);
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      } finally { await api.close(); }
    });
  });
}
