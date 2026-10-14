import { test, expect, type Page } from "@playwright/test";
import { writeFile } from "node:fs/promises";
import { WorkspaceApi, conversationId } from "../e2e/workspace-api";
import type { RunInput, RunInputRequest } from "../src/lib/run-input";

// Controlled REST/SSE data; every control is the production UI. No live model calls.
const html = `<!doctype html><html lang="en"><meta charset="utf-8"><title>Lester — Make it happen</title><style>body{margin:0;background:#f6f8f4;color:#243c30;font-family:system-ui}main{padding:72px 40px}small{letter-spacing:.2em}h1{font-size:60px;line-height:1.1;font-weight:500;letter-spacing:-3px;margin:55px 0 25px}p{max-width:370px;line-height:1.8;color:#66736a}a{display:inline-block;margin-top:25px;padding:14px 22px;background:#345b43;color:white;border-radius:30px;text-decoration:none}footer{margin-top:100px;font-size:12px;color:#66736a}</style><main><small>LESTER / YOUR PERSONAL AGENT</small><h1>Think it through.<br>Make it happen.</h1><p>A quiet workspace for ambitious ideas. Plan, create, and refine — with your files right beside you.</p><a href="https://github.com/biubiuqiu/lester-agent">View on GitHub ↗</a><footer id="details">Open source. Self hosted. Yours.</footer></main></html>`;

test("record the current workspace story", async ({ context, page }, info) => {
  const api = new WorkspaceApi(), errors: string[] = [], chapters: { seconds: number; title: string }[] = [];
  const started = Date.now();
  const observe = (p: Page) => p.on("pageerror", e => errors.push(e.message));
  observe(page);
  await api.install(context);
  await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", url: "http://127.0.0.1:13020", httpOnly: true }, { name: "lester_locale", value: "zh-CN", url: "http://127.0.0.1:13020" }]);
  // Caption outside the application layout; stays visible after tab recovery.
  await context.addInitScript(() => {
    if (window !== window.top) return;
    addEventListener("DOMContentLoaded", () => {
      const bar = document.createElement("div"); bar.id = "walkthrough-caption";
      bar.style.cssText = "position:fixed;bottom:12px;left:50%;transform:translateX(-50%);z-index:2147483647;pointer-events:none;background:#183527f2;color:#fff;padding:12px 22px;border-radius:12px;font:16px system-ui;box-shadow:0 4px 20px #0002;text-align:center;white-space:nowrap";
      bar.innerHTML = '<span id="walkthrough-title">Lester · From an idea to a result</span><span style="display:block;font-size:10px;letter-spacing:.12em;margin-top:5px;opacity:.7">ILLUSTRATIVE TASK · SAMPLE DATA · NO LIVE MODEL</span>';
      document.body.append(bar);
    });
  });
  const caption = async (p: Page, title: string) => {
    chapters.push({ seconds: (Date.now() - started) / 1000, title });
    await p.locator("#walkthrough-title").evaluate((el, text) => { el.textContent = text; }, title);
  };
  // Delays below are reading time for the recording, never readiness assertions.
  const hold = (p: Page, ms = 4500) => p.waitForTimeout(ms);
  const prefix = `/api/v1/conversations/${conversationId}`;
  const plan = { id: "44444444-4444-4444-8444-444444444444", run_id: "fixture-run", kind: "plan", request_digest: "a".repeat(64), status: "pending", request: { kind: "plan", title: "制作 Lester 极简介绍页", steps: ["梳理主标题与项目定位", "制作自适应 HTML，使用鼠尾草绿", "检查预览和页面结构"] } };
  let approved = false;
  const inputs: RunInput[] = [];
  await context.route(`**${prefix}/interactions`, r => r.fulfill({ json: { interactions: [approved ? { ...plan, status: "approved", response: { decision: "approve", request_digest: plan.request_digest } } : plan], execution_mode: approved ? "execute" : "plan" } }));
  await context.route(`**${prefix}/interactions/${plan.id}/respond`, r => { approved = true; return r.fulfill({ json: { ...plan, status: "approved", response: r.request().postDataJSON() } }); });
  await context.route(`**${prefix}/inputs`, r => {
    if (r.request().method() === "GET") return r.fulfill({ json: { inputs } });
    const request = r.request().postDataJSON() as RunInputRequest;
    const item: RunInput = { id: request.id, after_run_id: request.run_id, kind: request.kind, status: request.kind === "next" ? "queued" : "pending", revision: 1, content: request.content, request, accepted_order: inputs.length + 1 };
    inputs.push(item); return r.fulfill({ status: 202, json: item });
  });
  const change = { id: "55555555-5555-4555-8555-555555555555", run_id: "fixture-run", path: "notes.txt", operation: "edit", status: "confirmed", before_exists: true, before_sha256: "a".repeat(64), after_sha256: "b".repeat(64), retained: true, rollback_status: "none", before_content: "Palette: neutral gray\nLayout: generous spacing\n", after_content: "Palette: sage green\nLayout: generous spacing\n" };
  await context.route(`**${prefix}/file-changes`, r => r.fulfill({ json: { changes: api.conversation?.run_status === "completed" ? [{ ...change, before_content: undefined, after_content: undefined }] : [] } }));
  await context.route(`**${prefix}/file-changes/${change.id}`, r => r.fulfill({ json: change }));
  try {
    await page.goto("/app");
    const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
    await expect(input).toBeVisible();
    await caption(page, "01 / Start with a goal. Choose Plan before execution.");
    await page.getByRole("button", { name: "先规划，再执行", exact: true }).click();
    await input.pressSequentially("制作一个 Lester 的极简介绍页：大标题、项目定位、GitHub 入口。", { delay: 55 });
    await hold(page, 2500); await input.press("Enter");
    await expect(page).toHaveURL(`/app/c/${conversationId}`);
    await expect.poll(() => api.streamCount).toBeGreaterThan(0);
    await expect(page.getByRole("button", { name: "批准并执行", exact: true })).toBeVisible();
    await caption(page, "02 / Review the plan. Execution starts with your approval.");
    await hold(page); await page.getByRole("button", { name: "批准并执行", exact: true }).click();
    await expect(page.locator(".interaction-receipt>summary")).toContainText("计划已批准");
    await hold(page, 2000);
    await caption(page, "03 / While it works, press Enter to queue the next task.");
    await input.pressSequentially("下一步：补充部署说明和帮助文档入口。", { delay: 65 });
    await input.press("Enter"); await expect.poll(() => inputs.length).toBe(1);
    await page.locator(".run-input-receipts>summary").click(); await hold(page);
    await caption(page, "04 / Or explicitly add a clarification to the current task.");
    await input.pressSequentially("当前页面用鼠尾草绿色，保持留白。", { delay: 65 });
    await page.getByRole("button", { name: "运行中输入", exact: true }).click(); await hold(page, 2000);
    await page.getByRole("button", { name: "调整当前任务", exact: true }).click();
    await expect.poll(() => inputs.length).toBe(2); await expect(page.locator(".run-input-receipts")).toContainText("等待加入当前任务"); await hold(page);
    await caption(page, "05 / The result opens beside the conversation, in its own tab.");
    api.finish();
    api.messages.at(-1)!.content = "示例成果：index.html 和 notes.txt 已更新，可以在右侧预览并查看修改。";
    api.writeFile("index.html", html); api.writeFile("notes.txt", change.after_content);
    await expect(page.locator(".open-file-tabs [role=tab]").filter({ hasText: "index.html" })).toBeVisible();
    await page.locator(".open-file-tabs [role=tab]").filter({ hasText: "index.html" }).click();
    await page.getByRole("button", { name: "预览", exact: true }).click(); await hold(page, 6000);
    await page.screenshot({ path: info.outputPath("poster.png") });
    await caption(page, "06 / Switch between the rendered page and its source.");
    await page.getByRole("button", { name: "源码", exact: true }).click(); await hold(page);
    await page.getByRole("button", { name: "预览", exact: true }).click(); await hold(page, 2000);
    await caption(page, "07 / Review retained before-and-after file changes.");
    await page.locator(".file-changes>summary").filter({ hasText: "文件修改审阅" }).click(); await page.locator(".file-change-card>summary").click();
    await expect(page.locator(".diff-added")).toHaveText("+Palette: sage green");
    await expect(page.locator(".diff-removed")).toHaveText("−Palette: neutral gray"); await page.screenshot({ path: info.outputPath("file-review.png") }); await hold(page, 6000);
    await caption(page, "08 / An unfinished message stays a draft, even after closing the tab.");
    const draft = "明天继续：添加一个常见问题区，保持极简。";
    await input.pressSequentially(draft, { delay: 75 }); await hold(page, 3000);
    await page.close();
    await page.video()!.saveAs(info.outputPath("part-01.webm"));
    const restored = await context.newPage(); observe(restored);
    await restored.goto(`/app/c/${conversationId}`);
    await expect(restored.getByRole("textbox", { name: "消息输入框", exact: true })).toHaveValue(draft);
    await caption(restored, "Your draft is still here. Continue when you are ready."); await restored.screenshot({ path: info.outputPath("draft-restored.png") }); await hold(restored, 6000);
    await restored.close();
    await restored.video()!.saveAs(info.outputPath("part-02.webm"));
    expect(api.unexpected).toEqual([]); expect(errors).toEqual([]);
    await writeFile(info.outputPath("chapters.json"), JSON.stringify(chapters, null, 2));
  } finally { await api.close(); }
});
