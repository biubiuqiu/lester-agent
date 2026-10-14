import { test, expect } from "@playwright/test";
import { WorkspaceApi } from "./workspace-api";

test("settings navigation remains compact and reachable on narrow screens", async ({ page, context }, info) => {
  const api = new WorkspaceApi(); await api.install(context);
  await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", url: "http://127.0.0.1:13020" }]);
  await context.route("**/api/v1/me/identities", route => route.fulfill({ json: { identities: [] } }));
  const errors: string[] = []; page.on("pageerror", error => errors.push(error.message));
  try {
    if (info.project.name === "mobile") await page.setViewportSize({ width: 320, height: 740 });
    await page.goto("/app/settings/profile");
    await expect(page.getByRole("heading", { name: "个人资料与账号", exact: true })).toBeVisible();
    const navigation = page.locator(".settings-sidebar nav");
    for (const [label, route] of [["模型", "models"], ["Computer", "sandbox"], ["Skill 广场", "skills"], ["个人资料", "profile"]]) {
      await navigation.getByRole("button", { name: label, exact: true }).click();
      await expect(page).toHaveURL(`/app/settings/${route}`);
      await expect(navigation.getByRole("button", { name: label, exact: true })).toHaveAttribute("aria-current", "page");
      if (info.project.name === "mobile") {
        await expect.poll(async () => (await page.locator(".settings-sidebar").boundingBox())!.height).toBeLessThan(130);
        await expect.poll(async () => { const b = (await navigation.getByRole("button", { name: label, exact: true }).boundingBox())!; return b.x >= 0 && b.x + b.width <= 321; }).toBe(true);
      }
    }
    await page.getByRole("button", { name: "新手引导", exact: true }).click();
    await expect(page.locator(".guide-dialog")).toBeVisible();
    await page.getByRole("button", { name: "关闭新手引导", exact: true }).click();
    await expect(page.locator(".guide-dialog")).toHaveCount(0);
    await page.screenshot({ path: info.outputPath("settings-navigation.png") });
    await page.getByRole("button", { name: "返回工作区", exact: true }).click();
    await expect(page).toHaveURL("/app");
    expect(errors).toEqual([]); expect(api.unexpected).toEqual([]);
  } finally { await context.close(); await api.close(); }
});

test("skill loading and failed reads do not masquerade as an empty catalogue", async ({ page, context }, info) => {
  const api = new WorkspaceApi(); await api.install(context);
  await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", url: "http://127.0.0.1:13020" }]);
  let release!: () => void; const waiting = new Promise<void>(resolve => { release = resolve; }); let reads = 0;
  await context.route("**/api/v1/skills", async route => {
    reads++;
    if (reads === 1) { await waiting; return route.fulfill({ status: 503, json: { error: "测试服务暂时不可用" } }); }
    return route.fulfill({ json: { skills: [] } });
  });
  try {
    await page.goto("/app/settings/skills");
    await expect(page.getByRole("status")).toHaveText("正在加载 Skill");
    await expect(page.locator(".skill-market-empty")).toHaveCount(0);
    release();
    await expect(page.locator(".settings-main").getByRole("alert")).toContainText("测试服务暂时不可用");
    await expect(page.locator(".skill-market-empty")).toHaveCount(0);
    await page.getByRole("button", { name: "重试", exact: true }).click();
    await expect(page.getByRole("heading", { name: "还没有可用的 Skill", exact: true })).toBeVisible();
    await expect(page.locator(".settings-main").getByRole("alert")).toHaveCount(0);
    expect(reads).toBe(2);
    await page.screenshot({ path: info.outputPath("skills-empty.png") });
    expect(api.unexpected).toEqual([]);
  } finally { release(); await context.close(); await api.close(); }
});
