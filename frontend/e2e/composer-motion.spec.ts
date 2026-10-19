import { test as base, expect, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId, prompt } from "./workspace-api";

const test = base.extend<{ api: WorkspaceApi }>({
  api: async ({ context }, runFixture) => {
    const api = new WorkspaceApi();
    await api.install(context);
    try { await runFixture(api); expect(api.unexpected).toEqual([]); }
    finally { await api.close(); }
  },
});

async function home(page: Page) {
  await page.goto("/app/new");
  await page.getByLabel("邮箱", { exact: true }).fill("browser@example.test");
  await page.getByLabel("密码", { exact: true }).fill("fixture-password-123");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeVisible();
  await page.getByRole("textbox", { name: "消息输入框", exact: true }).fill(prompt);
}

async function sampleMotion(page: Page) {
  return page.locator(".new-chat-composer .compose-box").evaluate(async element => {
    const points: number[] = [];
    const start = performance.now();
    await new Promise<void>(resolve => {
      function frame() {
        points.push(element.getBoundingClientRect().y);
        if (performance.now() - start >= 430) resolve();
        else requestAnimationFrame(frame);
      }
      requestAnimationFrame(frame);
    });
    return points;
  });
}

test("first submit moves the live composer, bridges slow navigation and restores focus", async ({ page, api }, info) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  let create!: () => void, load!: () => void;
  api.waitForCreate = new Promise(resolve => { create = resolve; });
  api.waitForConversation = new Promise(resolve => { load = resolve; });
  try {
    await home(page);
    const box = page.locator(".new-chat-composer .compose-box");
    const initial = (await box.boundingBox())!;
    await page.screenshot({ path: info.outputPath("before-send.png") });
    await page.getByRole("textbox", { name: "消息输入框", exact: true }).press("Enter");
    await expect(page.getByRole("button", { name: "发送消息", exact: true })).toBeDisabled();
    await page.locator("form.new-chat-composer").evaluate(form => { (form as HTMLFormElement).requestSubmit(); (form as HTMLFormElement).requestSubmit(); });
    const points = await sampleMotion(page);
    const docked = (await box.boundingBox())!;
    expect(docked.y).toBeGreaterThan(initial.y + 60);
    expect(points.some(y => y > initial.y + 5 && y < docked.y - 5)).toBe(true);
    expect(Math.max(...points.slice(1).map((y, index) => Math.abs(y - points[index])))).toBeLessThan((docked.y - initial.y) * .6);
    await page.screenshot({ path: info.outputPath("pending-at-bottom.png") });
    create();
    await expect(page).toHaveURL(`/app/c/${conversationId}`);
    const ghost = page.locator(".composer-motion-ghost");
    await expect(ghost).toBeVisible();
    await expect(ghost).toHaveAttribute("aria-hidden", "true");
    expect(await ghost.evaluate(element => (element as HTMLElement).inert)).toBe(true);
    await page.screenshot({ path: info.outputPath("navigation-handoff.png") });
    load();
    const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
    await expect(input).toBeFocused();
    await expect(input).toHaveValue("");
    await expect(ghost).toHaveCount(0);
    await expect(page.locator(".thread")).toContainText(prompt);
    await input.fill("继续输入不会被动画清空");
    await expect(input).toHaveValue("继续输入不会被动画清空");
    await page.screenshot({ path: info.outputPath("conversation-at-bottom.png") });
    expect(api.mutations.filter(item => item.path === "/api/v1/conversations")).toHaveLength(1);
    expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(1);
    expect(errors).toEqual([]);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  } finally { create(); load(); }
});

test("failed creation smoothly returns the same draft to the centered composer", async ({ page, api }) => {
  let release!: () => void;
  api.waitForCreate = new Promise(resolve => { release = resolve; });
  api.failCreate = true;
  try {
    await home(page);
    const box = page.locator(".new-chat-composer .compose-box");
    const initial = (await box.boundingBox())!;
    await page.getByRole("textbox", { name: "消息输入框", exact: true }).press("Enter");
    await sampleMotion(page);
    const docked = (await box.boundingBox())!;
    release();
    await expect(page.locator(".new-chat-composer").getByRole("alert")).toHaveText("模拟会话创建失败");
    const points = await sampleMotion(page);
    const returned = (await box.boundingBox())!;
    expect(returned.y).toBeLessThan(docked.y - 60);
    expect(Math.abs(returned.y - initial.y)).toBeLessThan(40);
    expect(points.some(y => y < docked.y - 5 && y > returned.y + 5)).toBe(true);
    await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toHaveValue(prompt);
    await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeFocused();
    await expect(page.getByRole("button", { name: "发送消息", exact: true })).toBeEnabled();
    expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(0);
    await expect(page.locator(".composer-motion-ghost")).toHaveCount(0);
  } finally { release(); }
});

test("reduced motion skips travel and snapshots while preserving the first task", async ({ page, api }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  let release!: () => void;
  api.waitForCreate = new Promise(resolve => { release = resolve; });
  try {
    await home(page);
    await page.getByRole("textbox", { name: "消息输入框", exact: true }).press("Enter");
    expect(await page.locator(".new-chat-composer .compose-box").evaluate(element => element.getAnimations().length)).toBe(0);
    release();
    await expect(page).toHaveURL(`/app/c/${conversationId}`);
    await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeFocused();
    await expect(page.locator(".composer-motion-ghost")).toHaveCount(0);
    expect(await page.locator(".conversation-main > .composer .compose-box").evaluate(element => element.getAnimations().length)).toBe(0);
    expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(1);
  } finally { release(); }
});

test("an unavailable animation engine cannot misreport or repeat a confirmed send", async ({ page, api }) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await home(page);
  await page.evaluate(() => { Element.prototype.animate = () => { throw new Error("Fixture: animation unavailable"); }; });
  await page.getByRole("textbox", { name: "消息输入框", exact: true }).press("Enter");
  await expect(page).toHaveURL(`/app/c/${conversationId}`);
  await expect(page.locator(".thread")).toContainText(prompt);
  await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeFocused();
  await expect(page.locator(".composer-motion-ghost")).toHaveCount(0);
  await expect(page.locator(".compose-error")).toHaveCount(0);
  expect(api.mutations.filter(item => item.path.endsWith("/messages"))).toHaveLength(1);
  expect(errors).toEqual([]);
});
