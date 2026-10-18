import { test, expect, type BrowserContext } from "@playwright/test";
import { WorkspaceApi, conversationId } from "./workspace-api";
import type { FileEntry } from "../src/lib/api";

const source = `/app/c/${conversationId}`;
const view = `lester.view.v1.22222222-2222-4222-8222-222222222222.fixture-workspace.${conversationId}`;
const file = (path: string): FileEntry => ({ path, name: path.split("/").at(-1)!, is_dir: false, size: 100, modified_at: "2026-10-18T00:00:00Z" });

async function fixture(context: BrowserContext, running = true) {
  const api = new WorkspaceApi(), reads: string[] = [];
  api.conversation = { id: conversationId, workspace_id: "fixture-workspace", created_by: "22222222-2222-4222-8222-222222222222", project_id: "fixture-project", agent_slug: "lester", agent_name: "Lester", model_deployment_id: "fixture-model", title: "文件自动预览回归", pinned: false, run_status: running ? "running" : "completed", created_at: "2026-10-18T00:00:00Z", updated_at: "2026-10-18T00:00:00Z" };
  await api.install(context);
  await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", url: "http://127.0.0.1:13020" }, { name: "lester_locale", value: "zh-CN", url: "http://127.0.0.1:13020" }]);
  await context.route(`**/api/v1/conversations/${conversationId}/files?*`, route => {
    const path = new URL(route.request().url()).searchParams.get("path") || "";
    reads.push(path);
    // Model actual directory responses: internal files appear only if the
    // client traverses .lester, rather than leaking through a flat fixture.
    const prefix = path ? `${path}/` : "", entries = new Map<string, FileEntry>();
    for (const entry of api.files) {
      if (!entry.path.startsWith(prefix)) continue;
      const relative = entry.path.slice(prefix.length), name = relative.split("/")[0];
      if (!name) continue;
      const direct = `${prefix}${name}`;
      entries.set(direct, relative.includes("/") ? { ...file(direct), is_dir: true } : entry);
    }
    return route.fulfill({ json: { files: [...entries.values()] } });
  });
  return { api, reads };
}

test("command bookkeeping leaves the panel closed and actual outputs still open and refresh", async ({ page, context }, info) => {
  const { api, reads } = await fixture(context), errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  try {
    await page.goto(source);
    await expect(page).toHaveTitle(/Lester/i);
    await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeVisible();
    await expect.poll(() => api.streamCount).toBeGreaterThan(0);
    await expect.poll(() => reads.length).toBeGreaterThan(0);
    const internal = [".lester/tasks/t1/state.json", ".lester/tasks/t1/output.log", ".lester/tasks/t2/state.json", ".lester/tasks/t2/request.json"];
    api.files.push(...internal.map(path => file(path)));
    const scanned = page.waitForResponse(response => new URL(response.url()).pathname.endsWith("/files"));
    // Exercise the same scan used by polling without waiting for its timer.
    await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
    await (await scanned).finished();
    await expect(page.locator(".computer-panel")).toBeHidden();
    await page.screenshot({ path: info.outputPath("runtime-files-panel-closed.png") });

    // Even an explicit notification must not turn runtime files into outputs.
    api.writeFile(".lester/tasks/t3/state.json", '{"status":"running"}');
    api.writeFile("state.json", '{"user_document":true}');
    const panel = page.locator(".computer-panel"), tabs = panel.getByRole("tablist", { name: "已打开文件", exact: true });
    await expect(tabs.getByRole("tab", { name: "state.json", exact: true })).toBeVisible();
    await expect(tabs.getByRole("tab")).toHaveCount(1);
    await expect(panel.locator(".file-source-scroll")).toContainText('"user_document":true');
    api.writeFile("index.html", "<!doctype html><html><body><h1>实际输出</h1></body></html>");
    await expect(tabs.getByRole("tab")).toHaveCount(2);
    await tabs.getByRole("tab", { name: "index.html", exact: true }).click();
    await expect(panel.locator('iframe[title="index.html preview"]')).toBeVisible();
    api.writeFile("state.json", '{"user_document":"updated"}');
    await tabs.getByRole("tab", { name: "state.json", exact: true }).click();
    await expect(panel.locator(".file-source-scroll")).toContainText('"user_document":"updated"');
    await expect(tabs.getByRole("tab")).toHaveCount(2);
    expect(reads.some(path => path.split("/").includes(".lester"))).toBe(false);
    await page.screenshot({ path: info.outputPath("only-actual-output-tabs.png") });
    expect(errors).toEqual([]); expect(api.unexpected).toEqual([]); expect(api.mutations).toEqual([]);
  } finally { await api.close(); }
});

test("reopening a conversation clears stale runtime tabs and watches while keeping the draft and user files", async ({ page, context }, info) => {
  const { api, reads } = await fixture(context, false);
  api.files = [file(".lester/tasks/t1/state.json"), file(".lester/tasks/t1/output.log"), file("state.json")];
  await context.addInitScript(({ view }) => sessionStorage.setItem(view, JSON.stringify({ version: 1,
    tabs: [".lester/tasks/t1/state.json", ".lester/tasks/t1/output.log", "state.json"],
    selected: ".lester/tasks/t1/state.json", directories: ["", ".lester/tasks/t1"],
    closedTabs: [".lester/tasks/t1/output.log"], unreadFiles: [".lester/tasks/t1/state.json"], text: "保留我的输入",
  })), { view });
  try {
    await page.goto(source);
    await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toHaveValue("保留我的输入");
    await expect.poll(() => reads.length).toBeGreaterThan(0);
    await expect(page.locator(".computer-panel")).toBeHidden();
    if (info.project.name === "mobile") await page.getByRole("button", { name: "成果", exact: true }).click();
    else await page.locator(".workspace-panel-button").click();
    const panel = page.locator(".computer-panel");
    await panel.locator(".computer-tabs").getByRole("button", { name: "文件", exact: true }).click();
    const tabs = panel.getByRole("tablist", { name: "已打开文件", exact: true });
    await expect(tabs.getByRole("tab")).toHaveCount(1);
    await tabs.getByRole("tab", { name: "state.json", exact: true }).click();
    await expect(panel.locator(".file-source-scroll")).toBeVisible();
    expect(reads.some(path => path.split("/").includes(".lester"))).toBe(false);
    await expect(panel.getByRole("button", { name: "重新打开刚关闭的标签", exact: true })).toBeDisabled();
    await page.screenshot({ path: info.outputPath("restored-user-file-only.png") });
    expect(api.unexpected).toEqual([]); expect(api.mutations).toEqual([]);
  } finally { await api.close(); }
});
