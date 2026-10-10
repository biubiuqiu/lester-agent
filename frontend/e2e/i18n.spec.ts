import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test, expect } from "@playwright/test";
import { translator, type Locale } from "../src/lib/i18n";
import { startCommand } from "../src/lib/site";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";

const languages: Locale[] = ["zh-CN", "en", "ja", "ko", "fr", "es"];
for (const locale of languages) {
  test.describe(locale, () => {
    test.use({ locale });
    test("browser locale renders homepage and every help chapter", async ({ page, context }, testInfo) => {
      const t = translator(locale === "zh-CN" ? {} : JSON.parse(readFileSync(join(process.cwd(), `src/lib/i18n/messages/${locale}.json`), "utf8")));
      const errors: string[] = [], apiCalls: string[] = [];
      page.on("pageerror", error => errors.push(error.message));
      await context.route("**/api/v1/**", route => { apiCalls.push(route.request().url()); return route.abort(); });
      await page.goto("/");
      await expect(page.locator("html")).toHaveAttribute("lang", locale);
      await expect(page.locator("h1")).toHaveText(`${t("想清楚。")}${t("做出来。")}`);
      await expect(page).toHaveTitle(t("Lester Agent · 想清楚。做出来。"));
      if (!["zh-CN", "ja"].includes(locale)) {
        const introduction = (await page.locator(".site-home-copy > p").innerText()).replace(/\s+/g, " ").trim();
        expect(introduction).toBe(`${t("一个开源、可自托管的个人 AI 工作区。")} ${t("把目标交给 Lester，留下网页、文档与代码。")}`);
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)).toBe(false);
      await page.screenshot({ path: testInfo.outputPath(`${locale}-homepage.png`) });
      for (const chapter of ["", "/usage", "/models", "/deployment", "/troubleshooting"]) {
        await page.goto(`/docs${chapter}`);
        await expect(page.locator(".site-docs-article h1")).toBeVisible();
        await expect(page.locator("html")).toHaveAttribute("lang", locale);
        if (!["zh-CN", "ja"].includes(locale)) {
          const copy = await page.locator(".site-docs-article p, .site-docs-article li, .site-docs-article h1, .site-docs-article h2, .site-doc-note").allTextContents();
          expect(copy.join("\n")).not.toMatch(/[\u3400-\u9fff]/);
        }
        expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)).toBe(false);
        if (!chapter) await expect(page.locator("pre").filter({ hasText: startCommand })).toContainText(startCommand);
      }
      await page.screenshot({ path: testInfo.outputPath(`${locale}-help.png`) });
      expect(apiCalls).toEqual([]);
      expect(errors).toEqual([]);
    });
  });
}

test("manual switch persists through navigation and reload without losing a draft or attachments", async ({ page, context }, testInfo) => {
  const api = new WorkspaceApi();
  await api.install(context);
  try {
    await page.goto("/app");
    await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
    await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await page.getByRole("textbox", { name: "消息输入框", exact: true }).fill(prompt);
    await page.locator('.new-chat-composer input[type="file"]').setInputFiles({ name: "材料.txt", mimeType: "text/plain", buffer: Buffer.from("用户文件保持原样") });
    // Open the account menu and choose from the SVG language menu.
    if (!await page.locator(".user-menu-trigger").isVisible()) await page.getByRole("button", { name: "打开会话栏", exact: true }).click();
    await page.locator(".user-menu-trigger").click();
    await page.locator(".user-menu-popover .language-trigger").click();
    await page.getByRole("menuitemradio", { name: "Français", exact: true }).click();
    await expect(page.locator("html")).toHaveAttribute("lang", "fr");
    const composer = page.getByRole("textbox", { name: "Saisie du message", exact: true });
    await expect(composer).toHaveValue(prompt);
    await expect(page.locator(".pending-attachments")).toContainText("材料.txt");
    await page.getByRole("menuitem", { name: "Modèles", exact: true }).click();
    await expect(page).toHaveURL(/settings\/models/);
    await expect(page.locator(".settings-sidebar .language-trigger")).toContainText("Français");
    await page.goBack();
    await page.screenshot({ path: testInfo.outputPath("fr-workspace.png") });
    await expect(composer).toHaveValue(prompt);
    await expect(page.locator(".pending-attachments")).toContainText("材料.txt");
    await page.reload();
    await expect(page.locator("html")).toHaveAttribute("lang", "fr");
    await expect(composer).toHaveValue(prompt);
    await expect(page.locator(".draft-attachment-notice")).toContainText("材料.txt");
    expect(api.mutations.filter(item => item.path.startsWith("/api/v1/conversations"))).toEqual([]);
    expect(api.unexpected).toEqual([]);
  } finally { await api.close(); }
});

test("English auth and guides stay translated while task content stays as authored", async ({ page, context }, testInfo) => {
  const api = new WorkspaceApi();
  await api.install(context);
  await context.addCookies([{ name: "lester_locale", value: "en", url: "http://127.0.0.1:13020" }]);
  try {
    await page.goto("/login");
    await page.getByLabel("Email", { exact: true }).fill("browser@example.test");
    await page.getByLabel("Password", { exact: true }).fill("wrong");
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(page.locator(".login-card").getByRole("alert")).toHaveText("Incorrect email or password");
    await page.getByLabel("Password", { exact: true }).fill("fixture-password-123");
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await page.getByRole("button", { name: "Getting started guide", exact: true }).click();
    await page.locator(".guide-topic-grid button").filter({ hasText: "Your first time with Lester" }).click();
    await expect(page.locator("#guide-title")).toHaveText("Welcome. Give Lester an idea.");
    await page.getByRole("button", { name: "Start learning", exact: true }).click();
    await expect(page.locator("#guide-title")).toHaveText("Choose a model first");
    expect(api.mutations.filter(item => item.path.startsWith("/api/v1/conversations"))).toEqual([]);
    await page.getByRole("button", { name: "Close getting started guide", exact: true }).click();
    const composer = page.getByRole("textbox", { name: "Message input", exact: true });
    await composer.fill(prompt);
    await composer.press("Enter");
    await expect(page).toHaveURL(`/app/c/${conversationId}`);
    await expect.poll(() => api.streamCount).toBeGreaterThan(0);
    api.finish();
    const tabs = page.getByRole("tablist", { name: "Open files" });
    await expect(tabs.getByRole("tab", { name: "index.html", exact: true })).toBeVisible();
    await expect(page.frameLocator('iframe[title="index.html preview"]').getByRole("heading", { name: "浏览器回归产物" })).toBeVisible();
    await page.getByRole("button", { name: "Source", exact: true }).click();
    await expect(page.getByLabel("index.html source code", { exact: true })).toContainText("浏览器回归产物");
    expect(api.mutations.find(item => item.path.endsWith("/messages"))?.body.content).toBe(prompt);
    await page.screenshot({ path: testInfo.outputPath("en-workspace-source.png") });
    expect(api.unexpected).toEqual([]);
  } finally { await api.close(); }
});

test("public help has translated server HTML with JavaScript disabled", async ({ browser }, testInfo) => {
  const context = await browser.newContext({ baseURL: "http://127.0.0.1:13020", javaScriptEnabled: false, locale: "es-MX", viewport: testInfo.project.use.viewport });
  try {
    const page = await context.newPage();
    const response = await page.goto("/docs/usage");
    expect(response?.status()).toBe(200);
    await expect(page.locator("html")).toHaveAttribute("lang", "es");
    await expect(page.locator(".site-docs-article h1")).toHaveText("Completa tu trabajo con Lester");
    await expect(page.locator("#account + p")).toContainText("Accede con correo");
    await expect(page.locator(".site-docs-article")).toContainText("30 días");
  } finally { await context.close(); }
});
