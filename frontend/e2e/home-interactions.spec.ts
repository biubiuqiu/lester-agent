import { test, expect } from "@playwright/test";
import { WorkspaceApi } from "./workspace-api";

for (const javaScriptEnabled of [true, false]) {
  test.describe(`public home: JavaScript ${javaScriptEnabled ? "on" : "off"}`, () => {
    test.use({ javaScriptEnabled });
    test("illustrative file choices work anonymously and with reduced motion", async ({ page }, info) => {
      const errors: string[] = [], apiCalls: string[] = [];
      page.on("pageerror", error => errors.push(error.message));
      page.on("console", message => { if (message.type() === "error") errors.push(message.text()); });
      await page.route("**/api/**", route => { apiCalls.push(route.request().url()); return route.abort(); });
      await page.goto("/");
      await expect(page).toHaveTitle("Lester Agent · 想清楚。做出来。");
      await expect(page.getByRole("heading", { level: 1 })).toContainText("想清楚。");
      const sketch = page.getByRole("group", { name: "成果示例", exact: true });
      await expect(sketch.getByText("index.html", { exact: true })).toBeVisible();
      await expect(sketch.locator(".site-sample-web")).toBeVisible();
      await sketch.getByRole("radio", { name: "文档", exact: true }).check();
      await expect(sketch.getByText("brief.md", { exact: true })).toBeVisible();
      await expect(sketch.getByText("产品需求报告", { exact: true })).toBeVisible();
      await expect(sketch.getByText("index.html", { exact: true })).toBeHidden();
      await sketch.getByRole("radio", { name: "文档", exact: true }).press("ArrowRight");
      await expect(sketch.getByRole("radio", { name: "代码", exact: true })).toBeChecked();
      await expect(sketch.getByText("main.py", { exact: true })).toBeVisible();
      await expect(sketch.locator(".site-sample-code")).toContainText("def summarize");
      await page.emulateMedia({ reducedMotion: "reduce" });
      await sketch.getByRole("radio", { name: "网页", exact: true }).check();
      await expect(sketch.getByText("index.html", { exact: true })).toBeVisible();
      expect(await page.locator(".site-home").evaluate(element => element.getAnimations({ subtree: true }).length)).toBe(0);
      if (info.project.name === "mobile") await page.setViewportSize({ width: 320, height: 839 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await page.screenshot({ path: info.outputPath("landing-home.png"), fullPage: true });
      await page.getByRole("link", { name: "关于 Lester", exact: true }).click();
      await expect(page).toHaveURL(/#why-lester$/);
      await expect(page.getByRole("heading", { name: "为什么叫 Lester？", exact: true })).toBeInViewport();
      expect(apiCalls).toEqual([]);
      expect(errors).toEqual([]);
    });
  });
}

test("new-task entry, example disclosure and focus feedback preserve explicit submission", async ({ page, context }, info) => {
  const api = new WorkspaceApi();
  await api.install(context);
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  try {
    await page.goto("/app/new");
    await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
    await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
    await page.getByRole("button", { name: "登录", exact: true }).click();
    const composer = page.getByRole("textbox", { name: "消息输入框", exact: true });
    await expect(composer).toBeVisible();
    expect(await page.locator(".new-chat-content h1").evaluate(element => getComputedStyle(element).animationName)).toBe("workspace-home-enter");
    const suggestions = page.getByRole("group", { name: "任务示例", exact: true });
    await expect(suggestions.getByRole("button")).toHaveCount(3);
    await composer.fill("保留我的输入草稿");
    await expect(suggestions).toHaveCount(0);
    const disclosure = page.locator(".task-examples");
    const before = (await disclosure.boundingBox())!.height;
    // Start at the actual pointer event: Playwright may wait for the entrance
    // animation to stabilize before clicking, longer than the sampling window.
    // Still use a real click and require an observed intermediate height.
    await disclosure.evaluate(element => {
      const target = element as HTMLElement & { transitionSamples?: Promise<{ points: number[]; supported: boolean }> };
      target.transitionSamples = new Promise(resolve => {
        element.querySelector("summary")!.addEventListener("pointerdown", () => {
          const points: number[] = [], start = performance.now();
          function frame() {
            points.push(element.getBoundingClientRect().height);
            if (performance.now() - start >= 500) resolve({ points, supported: CSS.supports("interpolate-size", "allow-keywords") });
            else requestAnimationFrame(frame);
          }
          requestAnimationFrame(frame);
        }, { once: true });
      });
    });
    await disclosure.locator("summary").click();
    const heights = await disclosure.evaluate(element => (element as HTMLElement & { transitionSamples: Promise<{ points: number[]; supported: boolean }> }).transitionSamples);
    const expanded = (await disclosure.boundingBox())!.height;
    expect(expanded).toBeGreaterThan(before);
    if (heights.supported) expect(heights.points.some(height => height > before + 1 && height < expanded - 1)).toBe(true);
    await page.getByRole("button", { name: "整理需求文档", exact: true }).click();
    await expect(composer).toBeFocused();
    await expect(composer).toHaveValue(/验收标准/);
    await expect(disclosure).not.toHaveAttribute("open", "");
    expect(api.mutations.filter(item => item.path.startsWith("/api/v1/conversations"))).toEqual([]);
    await disclosure.locator("summary").press("Escape");
    await expect(disclosure).not.toHaveAttribute("open", "");
    await page.emulateMedia({ reducedMotion: "reduce" });
    // Media-query change events are delivered asynchronously to Web Animations listeners.
    await expect.poll(() => page.locator(".new-chat-content").evaluate(element => element.getAnimations({ subtree: true }).length)).toBe(0);
    await disclosure.locator("summary").click();
    await page.getByRole("button", { name: "制作一个网页", exact: true }).click();
    await expect(composer).toHaveValue("帮我制作一个产品介绍网页，先和我确认目标用户、内容和视觉风格。");
    await page.locator('.new-chat-composer input[type="file"]').setInputFiles({ name: "材料.txt", mimeType: "text/plain", buffer: Buffer.from("示例材料") });
    await expect(page.locator(".pending-attachments")).toContainText("材料.txt");
    await composer.fill("");
    await expect(suggestions).toHaveCount(0);
    await page.getByRole("button", { name: "移除 材料.txt", exact: true }).click();
    await expect(suggestions.getByRole("button")).toHaveCount(3);
    if (info.project.name === "mobile") await page.setViewportSize({ width: 320, height: 839 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: info.outputPath("workspace-home.png") });
    expect(api.mutations.filter(item => item.path.startsWith("/api/v1/conversations"))).toEqual([]);
    expect(api.unexpected).toEqual([]);
    expect(errors).toEqual([]);
  } finally { await api.close(); }
});
