import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test, expect } from "@playwright/test";
import { translator, type Locale } from "../src/lib/i18n";
import { WorkspaceApi, prompt } from "./workspace-api";

for (const locale of ["zh-CN", "en", "ja", "ko", "fr", "es"] as Locale[]) {
  test.describe(locale, () => {
    test.use({ locale });
    test("focused task, stable file selection and accessible result panel", async ({ page, context }, testInfo) => {
      const t = translator(locale === "zh-CN" ? {} : JSON.parse(readFileSync(join(process.cwd(), `src/lib/i18n/messages/${locale}.json`), "utf8")));
      const api = new WorkspaceApi();
      await api.install(context);
      const errors: string[] = [];
      page.on("pageerror", error => errors.push(error.message));
      const compact = testInfo.project.name === "mobile";
      try {
        await page.goto("/app");
        await page.getByLabel(t("邮箱"), { exact: true }).fill("browser@example.test");
        await page.getByLabel(t("密码"), { exact: true }).fill("fixture-password-123");
        await page.getByRole("button", { name: t("登录"), exact: true }).click();
        const composer = page.getByRole("textbox", { name: t("消息输入框"), exact: true });
        await expect(composer).toBeVisible();
        if (compact) {
          const initialViewport = page.viewportSize()!;
          await page.setViewportSize({ width: 320, height: initialViewport.height });
          const toolbar = page.locator(".new-chat-composer .compose-actions");
          for (const control of await toolbar.locator("button, select").all()) {
            const bounds = await control.boundingBox();
            expect(bounds!.width).toBeGreaterThan(25);
            expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(321);
          }
          await page.setViewportSize(initialViewport);
        }
        await page.screenshot({ path: testInfo.outputPath("new-task.png") });
        await page.getByRole("button", { name: t("选择 Agent"), exact: true }).click();
        await expect(page.locator(".composer-agent-popover")).toBeVisible();
        await page.getByRole("combobox", { name: t("选择 Agent"), exact: true }).press("Escape");
        await expect(page.locator(".composer-agent-popover")).toHaveCount(0);
        await composer.fill(prompt);
        await composer.press("Enter");
        await expect(page.getByRole("button", { name: t("停止生成"), exact: true })).toBeVisible();
        await expect(page.locator(".computer-panel")).toBeHidden();
        await expect(page.locator(".agent-heading strong")).toHaveText(prompt);
        await expect.poll(() => api.streamCount).toBeGreaterThan(0);
        await page.screenshot({ path: testInfo.outputPath("focused-task.png") });
        if (!compact) await composer.fill("阅读时保留的草稿");
        api.finish();
        const panel = page.locator(".computer-panel");
        const tabs = panel.getByRole("tablist", { name: t("已打开文件") });
        await expect(tabs.getByRole("tab", { name: "index.html", exact: true })).toBeVisible();
        await expect(panel.locator('iframe[title="index.html preview"]')).toBeVisible();
        await expect(panel.locator(".file-tree-pane")).toBeHidden();
        if (!compact) {
          await expect(composer).toHaveValue("阅读时保留的草稿");
          await expect(composer).toBeFocused();
          const bounds = await panel.boundingBox();
          expect(bounds!.width).toBeGreaterThan(600);
          expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(page.viewportSize()!.width + 1);
        } else {
          await expect(panel).toHaveAttribute("aria-modal", "true");
          expect(await panel.evaluate(element => element.matches(":modal"))).toBe(true);
          for (let i = 0; i < 14; i++) {
            await page.keyboard.press("Tab");
            expect(await page.evaluate(() => !!document.activeElement?.closest(".computer-panel"))).toBe(true);
          }
        }
        const viewport = page.viewportSize()!;
        await page.setViewportSize({ width: compact ? 320 : 1100, height: viewport.height });
        if (compact) {
          const controls = panel.locator(".computer-panel-header button, .workspace-tools > summary");
          for (const control of await controls.all()) {
            if (!await control.isVisible()) continue;
            const bounds = await control.boundingBox();
            expect(bounds!.x).toBeGreaterThanOrEqual(0);
            expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(321);
          }
        } else {
          const bounds = await panel.boundingBox();
          expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(1101);
          await panel.getByRole("button", { name: t("专注预览"), exact: true }).click();
          await expect.poll(() => panel.evaluate(element => element.matches(":modal"))).toBe(true);
          await page.keyboard.press("Escape");
          await expect(panel).toBeHidden();
          await page.locator(".conversation-header").getByRole("button", { name: t("工作区"), exact: true }).click();
          await expect(panel).toBeVisible();
          expect(await panel.evaluate(element => element.matches(":modal"))).toBe(false);
        }
        await page.setViewportSize(viewport);
        await panel.locator(".workspace-tools > summary").click();
        await expect(panel.locator(".workspace-tools-menu").getByRole("button", { name: t("终端"), exact: true })).toBeVisible();
        await panel.locator(".workspace-tools > summary").press("Escape");
        await expect(panel.locator(".workspace-tools-menu")).toBeHidden();
        await expect(panel).toBeVisible();
        await panel.locator(".workspace-tools > summary").click();
        await panel.locator(".workspace-tools-menu").getByRole("button", { name: t("技能"), exact: true }).click();
        await expect(panel.locator(".conversation-skills")).toBeVisible();
        await panel.locator(".computer-panel-header").getByRole("button", { name: t("文件"), exact: true }).click();
        await expect(page.frameLocator('iframe[title="index.html preview"]').getByRole("heading", { name: "浏览器回归产物", exact: true })).toBeVisible();
        await page.screenshot({ path: testInfo.outputPath("artifact-preview.png") });
        await panel.getByRole("button", { name: t("部署 HTML 站点"), exact: true }).click();
        await expect(page.locator(".deploy-dialog")).toBeVisible();
        await page.keyboard.press("Escape");
        await expect(page.locator(".deploy-dialog")).toHaveCount(0);
        await expect(panel).toBeVisible();
        await tabs.getByRole("tab", { name: "notes.txt", exact: true }).click();
        await expect(panel.getByLabel(t("{0} 源代码", ["notes.txt"]), { exact: true })).toContainText("请人工验收");
        api.writeFile("index.html", "<!doctype html><h1>后台更新</h1>");
        api.writeFile("example.ts", "export const answer = 42;");
        await expect(tabs.getByRole("tab", { name: "example.ts", exact: true })).toBeVisible();
        await expect(tabs.getByRole("tab", { name: "notes.txt", exact: true })).toHaveAttribute("aria-selected", "true");
        await tabs.getByRole("tab", { name: "example.ts", exact: true }).click();
        await expect(panel.getByLabel(t("{0} 源代码", ["example.ts"]), { exact: true })).toContainText("answer = 42");
        await tabs.getByRole("tab", { name: "notes.txt", exact: true }).click();
        await expect(panel.getByLabel(t("{0} 源代码", ["notes.txt"]), { exact: true })).toContainText("请人工验收");
        await panel.locator(".computer-panel-header").getByRole("button", { name: t("成果"), exact: true }).click();
        await expect(panel.locator(".deliverable-card")).toHaveCount(1);
        await expect(panel.locator(".deliverable-entry")).toHaveText("index.html");
        await panel.getByRole("button", { name: t("预览成果 {0}", ["index.html"]), exact: true }).click();
        await expect(panel.locator('iframe[title="index.html preview"]')).toBeVisible();
        await panel.locator(".computer-panel-header").getByRole("button", { name: t("成果"), exact: true }).click();
        await page.screenshot({ path: testInfo.outputPath("results-panel.png") });
        const more = panel.locator(".deliverable-more > summary");
        await more.click();
        await more.press("Escape");
        await expect(panel.locator(".deliverable-details")).toBeHidden();
        await expect(panel).toBeVisible();
        await panel.getByRole("button", { name: compact ? t("返回对话") : t("关闭工作区面板"), exact: true }).click();
        await expect(panel).toBeHidden();
        await expect(page.locator(".thread .deliverable-card")).toHaveCount(0);
        await expect(page.locator(".delivery-receipt")).toContainText(t("待验收"));
        await expect(page.locator(".delivery-receipt-files button")).toHaveCount(1);
        api.writeFile("later.txt", "后台写入不重新打开已关闭的面板");
        await expect(page.locator(".related-files > summary")).toContainText("3");
        await expect(panel).toBeHidden();
        const opener = page.locator(".conversation-header").getByRole("button", { name: t(compact ? "成果" : "工作区"), exact: true });
        await opener.click();
        await expect(panel).toBeVisible();
        if (compact) {
          await page.keyboard.press("Escape");
          await expect(panel).toBeHidden();
          await expect(opener).toBeFocused();
          await opener.click();
        }
        await panel.getByRole("button", { name: t("继续修改 {0}", ["index.html"]), exact: true }).click();
        await expect(panel).toBeHidden();
        await expect(composer).toBeFocused();
        await expect(page.locator(".file-reference")).toContainText("index.html");
        expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)).toBe(false);
        expect(errors).toEqual([]);
        expect(api.unexpected).toEqual([]);
      } finally { await api.close(); }
    });
  });
}
