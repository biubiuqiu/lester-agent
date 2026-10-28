import { test as base, expect, type BrowserContext, type Page } from "@playwright/test";
import { WorkspaceApi, conversationId } from "./workspace-api";

const origin = "http://127.0.0.1:13020";
const test = base.extend<{ api: WorkspaceApi }>({ api: async ({ context, page }, run) => {
  const api = new WorkspaceApi(), errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => { if (message.type() === "error" && !/Failed to load resource:.*status of (404|409|410|503)\b/.test(message.text())) errors.push(message.text()); });
  await api.install(context);
  await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", url: origin }, { name: "lester_locale", value: "zh-CN", url: origin }]);
  try { await run(api); expect(api.unexpected).toEqual([]); expect(errors).toEqual([]); } finally { await api.close(); }
} });
type MediaMode = "allow" | "deny" | "hold" | "empty" | "mp4";
type MediaProbe = { mode: MediaMode; requests: number; stopped: number; recorderTypes: string[]; release: () => void };
type VoiceWindow = Window & { __voiceMedia: MediaProbe };

// These are explicit browser-media protocol fixtures, not recordings from real
// hardware and not evidence of a provider's recognition accuracy or codecs.
async function media(context: BrowserContext, mode: MediaMode = "allow") {
  await context.addInitScript(mode => {
    const probe: MediaProbe = { mode, requests: 0, stopped: 0, recorderTypes: [], release: () => {} };
    (window as unknown as VoiceWindow).__voiceMedia = probe;
    function stream() {
      const track = Object.assign(new EventTarget(), {
        kind: "audio", readyState: "live",
        stop() { if (this.readyState === "live") { this.readyState = "ended"; probe.stopped++; } },
      });
      const value = new MediaStream();
      Object.defineProperties(value, { getTracks: { value: () => [track] }, getAudioTracks: { value: () => [track] } });
      return value;
    }
    Object.defineProperty(navigator, "mediaDevices", { configurable: true, value: { getUserMedia: async () => {
      probe.requests++;
      if (probe.mode === "deny") throw new DOMException("fixture permission denied", "NotAllowedError");
      if (probe.mode === "hold") return new Promise<MediaStream>(resolve => { probe.release = () => resolve(stream()); });
      return stream();
    } } });
    class Recorder extends EventTarget {
      static isTypeSupported(type: string) { return probe.mode === "mp4" ? type === "audio/mp4" : type.startsWith("audio/webm"); }
      state = "inactive";
      mimeType: string;
      ondataavailable: ((event: BlobEvent) => void) | null = null;
      onstop: (() => void) | null = null;
      onerror: (() => void) | null = null;
      constructor(_stream: MediaStream, options?: MediaRecorderOptions) { super(); this.mimeType = options?.mimeType || "audio/webm"; probe.recorderTypes.push(this.mimeType); }
      start() { this.state = "recording"; }
      stop() {
        if (this.state === "inactive") return;
        this.state = "inactive";
        queueMicrotask(() => {
          const data = new Blob(probe.mode === "empty" ? [] : [new Uint8Array([0x1a, 0x45, 0xdf, 0xa3]), "fixture recording bytes".repeat(8)], { type: this.mimeType });
          this.ondataavailable?.(new BlobEvent("dataavailable", { data }));
          this.onstop?.();
        });
      }
    }
    Object.defineProperty(window, "MediaRecorder", { configurable: true, writable: true, value: Recorder });
  }, mode);
}
async function capabilities(context: BrowserContext, overrides: Record<string, unknown> = {}) {
  await context.route("**/api/v1/speech/capabilities", route => route.fulfill({ json: { enabled: true, provider: "openai", revision: 3, max_audio_bytes: 10485760, max_duration_seconds: 120, ...overrides } }));
}
function requestId(body: string) {
  const id = /name="request_id"\r\n\r\n([0-9a-f-]{36})/i.exec(body)?.[1];
  expect(id).toMatch(/^[0-9a-f-]{36}$/);
  return id!;
}
async function recorder(page: Page) {
  await page.getByRole("button", { name: "语音输入", exact: true }).click();
  await expect(page.getByText("正在录音", { exact: true })).toBeVisible();
}
async function accountMenu(page: Page) {
  if (!await page.locator(".user-menu-trigger").isVisible()) await page.getByRole("button", { name: "打开会话栏", exact: true }).click();
  await page.locator(".user-menu-trigger").click();
}
function currentConversation(api: WorkspaceApi) {
  const now = new Date().toISOString();
  api.conversation = { id: conversationId, workspace_id: "fixture-workspace", created_by: "22222222-2222-4222-8222-222222222222", project_id: "fixture-project", agent_slug: "lester", agent_name: "Lester", model_deployment_id: "fixture-model", title: "运行中的任务", run_status: "running", run_id: "fixture-run", pinned: false, created_at: now, updated_at: now };
  api.messages = [{ id: "accepted-goal", role: "user", content: "已接受的任务", created_at: now }];
}

test("microphone policy permits the app origin and excludes public and standalone preview pages", async ({ page, api }) => {
  for (const path of ["/", "/login", `/preview/${conversationId}?path=index.html`]) {
    const response = await page.request.get(path);
    expect(response.headers()["permissions-policy"]).toBe("microphone=()");
  }
  const app = await page.request.get("/app/new");
  expect(app.headers()["permissions-policy"]).toBe("microphone=(self)");
  expect(api.mutations).toEqual([]);
});

test("configured speech appends to the latest draft, preserves attachments and sends only after explicit confirmation", async ({ page, context, api }, info) => {
  await media(context); await capabilities(context);
  let posts = 0, release!: () => void;
  const held = new Promise<void>(resolve => { release = resolve; });
  await context.route("**/api/v1/speech/transcriptions", async route => {
    posts++;
    const body = route.request().postData()!, id = requestId(body);
    expect(route.request().headers()["content-type"]).toContain("multipart/form-data");
    expect(body).toContain('filename="recording.webm"');
    expect(body).toContain('name="configuration_revision"\r\n\r\n3');
    await held; await route.fulfill({ json: { id, status: "completed", text: "语音补充 GitHub API 的细节" } });
  });
  try {
    await page.goto("/app/new");
    const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
    await input.fill("原来的草稿");
    await page.locator('.new-chat-composer input[type="file"]').setInputFiles({ name: "brief.txt", mimeType: "text/plain", buffer: Buffer.from("fixture attachment") });
    await recorder(page);
    await page.getByRole("button", { name: "停止并转写", exact: true }).click();
    await expect(page.getByText("正在转写…", { exact: true })).toBeVisible();
    await expect.poll(() => posts).toBe(1);
    await input.fill("转写过程中继续输入的内容");
    release();
    await expect(input).toHaveValue("转写过程中继续输入的内容\n语音补充 GitHub API 的细节");
    await expect(page.locator(".pending-attachments")).toContainText("brief.txt");
    await expect(page.getByText("转写已加入草稿，请确认后发送。", { exact: true })).toBeVisible();
    await expect.poll(() => page.evaluate(() => (window as unknown as VoiceWindow).__voiceMedia.stopped)).toBe(1);
    expect(api.mutations).toEqual([]);
    await page.screenshot({ path: info.outputPath("speech-text-is-an-editable-draft.png") });
    await page.reload();
    await expect(input).toHaveValue("转写过程中继续输入的内容\n语音补充 GitHub API 的细节");
    expect(posts).toBe(1);
    expect(api.mutations).toEqual([]);
    await expect(page).toHaveTitle(/Lester/); await expect(page.locator("nextjs-portal")).toHaveCount(0);
  } finally { release(); }
});

test("cancel and backgrounding stop microphone tracks without uploading or discarding ordinary text", async ({ page, context, api }) => {
  await media(context); await capabilities(context);
  let posts = 0;
  await context.route("**/api/v1/speech/transcriptions", route => { posts++; return route.fulfill({ status: 500, json: { error: "unexpected upload" } }); });
  await page.goto("/app/new");
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill("保留未发送内容");
  await recorder(page);
  await page.getByRole("button", { name: "取消录音", exact: true }).click();
  await expect.poll(() => page.evaluate(() => (window as unknown as VoiceWindow).__voiceMedia.stopped)).toBe(1);
  await expect(input).toHaveValue("保留未发送内容");
  await recorder(page);
  await page.evaluate(() => { Object.defineProperty(document, "visibilityState", { configurable: true, value: "hidden" }); document.dispatchEvent(new Event("visibilitychange")); });
  await expect.poll(() => page.evaluate(() => (window as unknown as VoiceWindow).__voiceMedia.stopped)).toBe(2);
  await expect(page.getByText("正在录音", { exact: true })).toHaveCount(0);
  await page.evaluate(() => { Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" }); document.dispatchEvent(new Event("visibilitychange")); });
  expect(posts).toBe(0); expect(api.mutations).toEqual([]);
});

test("navigation rejects a late microphone permission result and retains the original task draft", async ({ page, context, api }) => {
  await media(context, "hold"); await capabilities(context);
  await page.goto("/app/new");
  const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
  await input.fill("不应丢失的任务草稿");
  await page.getByRole("button", { name: "语音输入", exact: true }).click();
  await expect(page.getByText("正在请求麦克风…", { exact: true })).toBeVisible();
  await accountMenu(page); await page.getByRole("menuitem", { name: "模型", exact: true }).click();
  await expect(page).toHaveURL(/\/app\/settings\/models/);
  await page.evaluate(() => (window as unknown as VoiceWindow).__voiceMedia.release());
  await expect.poll(() => page.evaluate(() => (window as unknown as VoiceWindow).__voiceMedia.stopped)).toBe(1);
  await page.getByRole("link", { name: "返回工作区", exact: true }).first().click();
  await expect(input).toHaveValue("不应丢失的任务草稿");
  expect(api.mutations).toEqual([]);
});

test("lost transcription response reads the original receipt instead of uploading twice", async ({ page, context, api }) => {
  await media(context); await capabilities(context);
  let posts = 0, reads = 0, original = "";
  await context.route("**/api/v1/speech/transcriptions**", route => {
    if (route.request().method() === "POST") { posts++; original = requestId(route.request().postData()!); return route.fulfill({ status: 503, json: { error: "fixture lost response" } }); }
    reads++; expect(route.request().url()).toContain(original);
    return route.fulfill({ json: { id: original, status: "completed", text: "恢复原来的转写结果" } });
  });
  await page.goto("/app/new"); await recorder(page);
  await page.getByRole("button", { name: "停止并转写", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toHaveValue("恢复原来的转写结果");
  expect(posts).toBe(1); expect(reads).toBe(1); expect(api.mutations).toEqual([]);
});

test("unknown receipts offer read-only inspection and a confirmed missing request retries with its original identity", async ({ page, context, api }) => {
  await media(context); await capabilities(context);
  let mode: "unknown" | "completed" | "missing" | "retry" = "unknown", posts = 0, reads = 0;
  const ids: string[] = [];
  await context.route("**/api/v1/speech/transcriptions**", route => {
    if (route.request().method() === "POST") {
      posts++; ids.push(requestId(route.request().postData()!));
      return mode === "retry" ? route.fulfill({ json: { id: ids.at(-1), status: "completed", text: "明确重试后的文字" } }) : route.fulfill({ status: 503, json: { error: "fixture upload lost" } });
    }
    reads++;
    if (mode === "missing") return route.fulfill({ status: 404, json: { error: "not accepted" } });
    return route.fulfill({ json: { id: ids.at(-1), status: mode, text: mode === "completed" ? "核对回执得到的文字" : undefined } });
  });
  await page.goto("/app/new"); await recorder(page);
  await page.getByRole("button", { name: "停止并转写", exact: true }).click();
  await expect(page.locator(".speech-feedback").getByRole("alert")).toContainText("不会自动重复上传");
  await expect(page.getByRole("button", { name: "重试转写", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "检查转写状态", exact: true }).click();
  await expect.poll(() => reads).toBe(2); expect(posts).toBe(1);
  mode = "completed";
  await page.getByRole("button", { name: "检查转写状态", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toHaveValue("核对回执得到的文字");
  mode = "missing";
  await recorder(page); await page.getByRole("button", { name: "停止并转写", exact: true }).click();
  await expect(page.getByRole("button", { name: "重试转写", exact: true })).toBeVisible();
  mode = "retry";
  await page.getByRole("button", { name: "重试转写", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toHaveValue("核对回执得到的文字\n明确重试后的文字");
  expect(posts).toBe(3); expect(ids[1]).toBe(ids[2]); expect(ids[0]).not.toBe(ids[1]); expect(api.mutations).toEqual([]);
});

for (const kind of ["expired", "configuration"] as const) {
  test(`${kind} transcription responses stay actionable without offering a futile upload retry`, async ({ page, context, api }) => {
    await media(context); await capabilities(context);
    let posts = 0, reads = 0;
    const message = kind === "expired" ? "语音转写记录已过期，请重新录音" : "语音配置或请求已变化，请刷新状态后重试";
    await context.route("**/api/v1/speech/transcriptions**", route => {
      if (route.request().method() === "POST") {
        posts++;
        requestId(route.request().postData()!);
        return route.fulfill({ status: kind === "expired" ? 503 : 409, json: { error: message } });
      }
      reads++;
      return route.fulfill({ status: kind === "expired" ? 410 : 404, json: { error: message } });
    });
    await page.goto("/app/new");
    const input = page.getByRole("textbox", { name: "消息输入框", exact: true });
    await input.fill("转写失败仍要保留的文字");
    await recorder(page); await page.getByRole("button", { name: "停止并转写", exact: true }).click();
    await expect(page.locator(".speech-feedback").getByRole("alert")).toContainText(message);
    await expect(page.getByRole("button", { name: "重试转写", exact: true })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "检查转写状态", exact: true })).toHaveCount(0);
    await expect(input).toHaveValue("转写失败仍要保留的文字");
    expect(posts).toBe(1); expect(reads).toBe(1); expect(api.mutations).toEqual([]);
  });
}

test("personal dictation stays in its own draft and minimizing cancels an unfinished recording", async ({ page, context, api }) => {
  await media(context); await capabilities(context);
  let posts = 0;
  await context.route("**/api/v1/speech/transcriptions", route => { posts++; return route.fulfill({ json: { id: requestId(route.request().postData()!), status: "completed", text: "先讨论我的工作安排" } }); });
  await page.goto("/app/new");
  const task = page.getByRole("textbox", { name: "消息输入框", exact: true }); await task.fill("独立的工程任务草稿");
  await page.locator(".personal-dock-launcher").click();
  const panel = page.getByRole("dialog", { name: "个人助理", exact: true });
  const personal = panel.getByRole("textbox", { name: "个人助理消息", exact: true });
  await personal.fill("个人对话草稿");
  await panel.getByRole("button", { name: "语音输入", exact: true }).click();
  await panel.getByRole("button", { name: "停止并转写", exact: true }).click();
  await expect(personal).toHaveValue("个人对话草稿\n先讨论我的工作安排");
  await expect(task).toHaveValue("独立的工程任务草稿");
  expect(api.mutations).toEqual([]);
  await panel.getByRole("button", { name: "语音输入", exact: true }).click();
  await expect(panel.getByText("正在录音", { exact: true })).toBeVisible();
  await panel.getByRole("button", { name: "收起个人助理", exact: true }).click();
  await expect.poll(() => page.evaluate(() => (window as unknown as VoiceWindow).__voiceMedia.stopped)).toBe(2);
  await page.locator(".personal-dock-launcher").click();
  await expect(personal).toHaveValue("个人对话草稿\n先讨论我的工作安排");
  expect(posts).toBe(1); expect(api.mutations).toEqual([]);
});

test("dictation during a run does not send or interrupt until the user explicitly queues the draft", async ({ page, context, api }) => {
  await media(context); await capabilities(context); currentConversation(api);
  await context.route("**/api/v1/speech/transcriptions", route => route.fulfill({ json: { id: requestId(route.request().postData()!), status: "completed", text: "下一步增加测试覆盖" } }));
  const accepted: Record<string, unknown>[] = [];
  await context.route(`**/api/v1/conversations/${conversationId}/permissions/inputs`, route => {
    if (route.request().method() === "GET") return route.fulfill({ json: { inputs: accepted } });
    const body = route.request().postDataJSON();
    accepted.push({ id: body.id, after_run_id: body.run_id, kind: body.kind, status: "queued", revision: 1, content: body.content, request: body, accepted_order: 1 });
    return route.fulfill({ json: accepted[0] });
  });
  await page.goto(`/app/c/${conversationId}`);
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeEnabled();
  await recorder(page); await page.getByRole("button", { name: "停止并转写", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toHaveValue("下一步增加测试覆盖");
  expect(accepted).toEqual([]); expect(api.mutations).toEqual([]);
  await expect(page.getByRole("button", { name: "停止生成", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "发送到队列", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toHaveValue("");
  expect(accepted).toHaveLength(1); expect(accepted[0]).toMatchObject({ kind: "next", content: "下一步增加测试覆盖" });
});

for (const [mode, message] of [["deny", "无法访问麦克风，请检查浏览器和系统权限。"], ["empty", "没有录到声音，请重新录音。"]] as const) {
  test(`${mode} recording reports an actionable error and keeps the draft`, async ({ page, context, api }) => {
    await media(context, mode); await capabilities(context);
    await page.goto("/app/new");
    const input = page.getByRole("textbox", { name: "消息输入框", exact: true }); await input.fill("失败时保留输入");
    await page.getByRole("button", { name: "语音输入", exact: true }).click();
    if (mode === "empty") { await expect(page.getByText("正在录音", { exact: true })).toBeVisible(); await page.getByRole("button", { name: "停止并转写", exact: true }).click(); }
    await expect(page.locator(".speech-feedback").getByRole("alert")).toContainText(message);
    await expect(input).toHaveValue("失败时保留输入"); expect(api.mutations).toEqual([]);
  });
}

test("disabled speech stays hidden and MP4-capable devices use a matching upload filename", async ({ page, context, api }) => {
  await media(context, "mp4");
  let enabled = false, posts = 0, capabilityReads = 0;
  await context.route("**/api/v1/speech/capabilities", route => { capabilityReads++; return route.fulfill({ json: { enabled, provider: "elevenlabs", revision: 3, max_audio_bytes: 10485760, max_duration_seconds: 120 } }); });
  await context.route("**/api/v1/speech/transcriptions", route => {
    posts++; const body = route.request().postData()!;
    expect(body).toContain('filename="recording.m4a"'); expect(body).toContain("Content-Type: audio/mp4");
    return route.fulfill({ json: { id: requestId(body), status: "completed", text: "来自 MP4 录音格式的文字" } });
  });
  await page.goto("/app/new");
  await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toBeVisible();
  await expect.poll(() => capabilityReads).toBeGreaterThan(0);
  await expect(page.getByRole("button", { name: "语音输入", exact: true })).toHaveCount(0);
  expect(await page.evaluate(() => (window as unknown as VoiceWindow).__voiceMedia.requests)).toBe(0);
  enabled = true; await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await recorder(page); await page.getByRole("button", { name: "停止并转写", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "消息输入框", exact: true })).toHaveValue("来自 MP4 录音格式的文字");
  expect(posts).toBe(1); expect(api.mutations).toEqual([]);
});

test("logout releases an active microphone and clears account-scoped drafts", async ({ page, context, api }) => {
  await media(context); await capabilities(context);
  await context.route("**/api/v1/auth/logout", route => route.fulfill({ json: {} }));
  await page.goto("/app/new");
  await page.getByRole("textbox", { name: "消息输入框", exact: true }).fill("退出后清理的草稿");
  await recorder(page); await accountMenu(page);
  await page.getByRole("menuitem", { name: "退出登录", exact: true }).click();
  await expect(page).toHaveURL(/\/login$/);
  await expect.poll(() => page.evaluate(() => (window as unknown as VoiceWindow).__voiceMedia.stopped)).toBe(1);
  expect(await page.evaluate(() => Object.keys(localStorage).some(key => key.startsWith("lester.draft.v1.")))).toBe(false);
  expect(api.mutations).toEqual([]);
});
