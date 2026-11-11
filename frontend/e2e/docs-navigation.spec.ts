import { test, expect, type Page } from "@playwright/test";

const origin = "http://127.0.0.1:13020";
const chapters = [
  ["/docs", "开始使用 Lester", "first-task"],
  ["/docs/models", "连接你选择的模型", "connection"],
  ["/docs/usage", "和 Lester 一起完成工作", "conversation"],
  ["/docs/personal-assistant", "个人助理与记忆", "personal-assistant"],
  ["/docs/projects-and-files", "管理项目，查看工作成果", "deliverables"],
  ["/docs/agents-and-context", "让 Agent 理解你的工作", "context"],
  ["/docs/connections", "连接你的工作资料", "github-code"],
  ["/docs/automations", "安排定时任务与外部触发", "automations"],
  ["/docs/speech", "把语音变成可编辑的草稿", "speech"],
  ["/docs/mobile", "在桌面、手机与平板上使用 Lester", "android"],
  ["/docs/deployment", "部署在自己的环境里", "backup"],
  ["/docs/troubleshooting", "遇到问题时，从这里排查", "preview"],
] as const;
const movedSections = [
  ["personal-assistant", "personal-assistant"],
  ["project-workbench", "projects-and-files"],
  ["computer", "projects-and-files"],
  ["deliverables", "projects-and-files"],
  ["publish", "projects-and-files"],
  ["skills", "agents-and-context"],
  ["agents", "agents-and-context"],
  ["context", "agents-and-context"],
  ["app-connections", "connections"],
  ["github-code", "connections"],
  ["automations", "automations"],
  ["speech", "speech"],
  ["ios", "mobile"],
  ["android", "mobile"],
] as const;

async function observePublicHelp(page: Page) {
  const errors: string[] = [], apiCalls: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => { if (message.type() === "error") errors.push(message.text()); });
  await page.route("**/api/**", route => { apiCalls.push(route.request().url()); return route.abort(); });
  await page.emulateMedia({ reducedMotion: "reduce" });
  return { errors, apiCalls };
}

async function chapterNavigation(page: Page) {
  const compact = page.locator(".site-docs-mobile-nav");
  if (await compact.isVisible()) {
    if (await compact.getAttribute("open") === null) await compact.locator("summary").click();
    return compact.locator(".site-docs-nav");
  }
  return page.locator(".site-docs-sidebar .site-docs-nav");
}

test("anonymous readers can find each shorter chapter and follow its local contents and neighboring pages", async ({ page, context }, info) => {
  const observed = await observePublicHelp(page);
  await context.addCookies([{ name: "lester_locale", value: "zh-CN", url: origin }]);
  await page.goto("/docs");
  for (const [path, title, anchor] of chapters) {
    const navigation = await chapterNavigation(page);
    await expect(navigation.getByRole("group", { name: "开始使用", exact: true })).toBeVisible();
    await expect(navigation.getByRole("group", { name: "日常使用", exact: true })).toBeVisible();
    await expect(navigation.getByRole("group", { name: "部署与维护", exact: true })).toBeVisible();
    await navigation.locator(`a[href="${path}"]`).click();
    await expect(page).toHaveURL(`${origin}${path}`);
    await expect(page.locator(".site-docs-article h1")).toHaveText(title);
    await expect(page.locator(`.site-docs-sidebar a[href="${path}"]`)).toHaveAttribute("aria-current", "page");
    await expect(page.locator(`h2#${anchor}`)).toHaveCount(1);
    const contents = page.locator(".site-docs-toc");
    const contentsLink = contents.locator(`a[href="#${anchor}"]`);
    await expect(contentsLink).toHaveCount(1);
    if (await contents.isVisible()) {
      await contentsLink.click();
      await expect(page).toHaveURL(`${origin}${path}#${anchor}`);
      await expect(page.locator(`h2#${anchor}`)).toBeInViewport();
    } else {
      await page.locator(`h2#${anchor}`).scrollIntoViewIfNeeded();
      await expect(page.locator(`h2#${anchor}`)).toBeInViewport();
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  }
  await page.locator(".site-docs-pagination").getByRole("link", { name: "部署与升级", exact: true }).click();
  await expect(page).toHaveURL(`${origin}/docs/deployment`);
  await page.locator(".site-docs-pagination").getByRole("link", { name: "常见问题", exact: true }).click();
  await expect(page).toHaveURL(`${origin}/docs/troubleshooting`);
  await page.screenshot({ path: info.outputPath("shorter-help-chapters.png") });
  expect(observed.apiCalls).toEqual([]);
  expect(observed.errors).toEqual([]);
});

test("old usage bookmarks reach the moved section and retain queries without redirecting retained or unknown anchors", async ({ page, context }) => {
  const observed = await observePublicHelp(page);
  await context.addCookies([{ name: "lester_locale", value: "zh-CN", url: origin }]);
  for (const [anchor, slug] of movedSections) {
    await page.goto(`/docs/usage?source=bookmark#${anchor}`);
    await expect(page).toHaveURL(`${origin}/docs/${slug}?source=bookmark#${anchor}`);
    await expect(page.locator(`h2#${anchor}`)).toBeVisible();
  }
  for (const anchor of ["account", "guides", "conversation", "permissions", "cloud-tasks", "recap", "unknown-bookmark"]) {
    await page.goto(`/docs/usage#${anchor}`);
    await expect(page.locator(".site-docs-article h1")).toHaveText("和 Lester 一起完成工作");
    await expect(page).toHaveURL(`${origin}/docs/usage#${anchor}`);
    if (anchor !== "unknown-bookmark") await expect(page.locator(`h2#${anchor}`)).toBeVisible();
  }
  await page.evaluate(() => { location.hash = "computer"; });
  await expect(page).toHaveURL(`${origin}/docs/projects-and-files#computer`);
  await expect(page.locator("h2#computer")).toBeVisible();
  expect(observed.apiCalls).toEqual([]);
  expect(observed.errors).toEqual([]);
});

test("chapter selection stays reachable by keyboard on short screens and compact menus scroll within the viewport", async ({ page, context }, info) => {
  const observed = await observePublicHelp(page);
  await context.addCookies([{ name: "lester_locale", value: "zh-CN", url: origin }]);
  await page.setViewportSize(info.project.name === "mobile" ? { width: 320, height: 640 } : { width: 1440, height: 620 });
  await page.goto("/docs");
  const compact = page.locator(".site-docs-mobile-nav");
  if (await compact.isVisible()) {
    await compact.locator("summary").focus();
    await page.keyboard.press("Enter");
    await expect(compact).toHaveAttribute("open", "");
    const bounds = await compact.locator(".site-docs-nav").boundingBox();
    expect(bounds!.height).toBeLessThanOrEqual(640 * .7);
  }
  const navigation = await chapterNavigation(page);
  const last = navigation.getByRole("link", { name: "常见问题", exact: true });
  await last.focus();
  await expect(last).toBeFocused();
  await expect(last).toBeInViewport();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(`${origin}/docs/troubleshooting`);
  await expect(page.locator(".site-docs-article h1")).toHaveText("遇到问题时，从这里排查");
  await page.screenshot({ path: info.outputPath("short-screen-help-navigation.png") });
  expect(observed.apiCalls).toEqual([]);
  expect(observed.errors).toEqual([]);
});

test("without JavaScript a moved bookmark offers a readable link to its complete server-rendered chapter", async ({ browser }, info) => {
  const context = await browser.newContext({ baseURL: origin, javaScriptEnabled: false, locale: "zh-CN", viewport: info.project.use.viewport });
  try {
    const page = await context.newPage(), observed = await observePublicHelp(page);
    await context.addCookies([{ name: "lester_locale", value: "zh-CN", url: origin }]);
    for (const [anchor, slug] of [["context", "agents-and-context"], ["app-connections", "connections"]]) {
      const response = await page.goto(`/docs/usage#${anchor}`);
      expect(response?.status()).toBe(200);
      const fallback = page.locator(`#${anchor}`).getByRole("link");
      await expect(fallback).toBeVisible();
      await expect(fallback).toHaveAttribute("href", `/docs/${slug}#${anchor}`);
      await fallback.click();
      await expect(page).toHaveURL(`${origin}/docs/${slug}#${anchor}`);
      await expect(page.locator(`h2#${anchor}`)).toBeVisible();
      await expect(page.locator(`section[aria-labelledby="${anchor}"] p`)).not.toHaveCount(0);
    }
    expect(observed.apiCalls).toEqual([]);
    expect(observed.errors).toEqual([]);
  } finally { await context.close(); }
});
