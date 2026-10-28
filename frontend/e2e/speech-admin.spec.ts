import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test as base, expect, type BrowserContext } from "@playwright/test";
import { translator, type Locale } from "../src/lib/i18n";
import { WorkspaceApi } from "./workspace-api";

const origin = "http://127.0.0.1:13020";
const test = base.extend<{ api: WorkspaceApi }>({ api: [async ({ context }, run) => {
  const api = new WorkspaceApi(); await api.install(context);
  await context.addCookies([{ name: "lester_access_token", value: "browser-test-access", url: origin }, { name: "lester_locale", value: "zh-CN", url: origin }]);
  await context.route("**/api/v1/me", route => route.fulfill({ json: { user_id: "22222222-2222-4222-8222-222222222222", workspace_id: "fixture-workspace", display_name: "管理测试", email: "admin@example.test", role: "admin", avatar_key: "forest" } }));
  try { await run(api); expect(api.unexpected).toEqual([]); } finally { await api.close(); }
}, { auto: true }] });
type Settings = { enabled: boolean; provider: string; endpoint: string; model_id: string; has_credential: boolean; revision: number };
function initial(): Settings { return { enabled: false, provider: "openai", endpoint: "https://api.openai.com/v1", model_id: "gpt-4o-transcribe", has_credential: false, revision: 1 }; }
async function configure(context: BrowserContext, settings = initial()) {
  const writes: Record<string, unknown>[] = [];
  await context.route("**/api/v1/admin/speech", route => {
    if (route.request().method() === "GET") return route.fulfill({ json: settings });
    const body = route.request().postDataJSON(); writes.push(body);
    const scopeChanged = body.provider !== settings.provider || new URL(String(body.endpoint)).origin !== new URL(settings.endpoint).origin;
    settings = { ...settings, enabled: body.enabled, provider: body.provider, endpoint: body.endpoint, model_id: body.model_id, has_credential: body.clear_credential ? false : !!body.credential || !scopeChanged && settings.has_credential, revision: settings.revision + 1 };
    return route.fulfill({ json: settings });
  });
  return writes;
}

test("speech configuration preserves masked keys, requires a new provider key and explicitly clears credentials", async ({ page, context, api }, info) => {
  const writes = await configure(context);
  await page.goto("/admin/speech");
  await expect(page.getByRole("heading", { name: "语音输入", exact: true })).toBeVisible();
  await expect(page.locator('.speech-admin-enable input')).not.toBeChecked();
  await expect(page.getByLabel("转写模型", { exact: true })).toHaveValue("gpt-4o-transcribe");
  await page.locator('.speech-admin-enable input').check();
  await page.getByLabel("API 密钥", { exact: true }).fill("fixture-openai-secret");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("语音配置已保存");
  await expect(page.getByLabel("API 密钥", { exact: true })).toHaveValue("");
  await expect(page.getByLabel("API 密钥", { exact: true })).toHaveAttribute("placeholder", "已保存密钥，留空保留");
  expect(writes[0]).toMatchObject({ enabled: true, provider: "openai", model_id: "gpt-4o-transcribe", credential: "fixture-openai-secret", revision: 1 });
  await expect(page.locator(".speech-admin")).not.toContainText("fixture-openai-secret");
  await page.getByLabel("转写模型", { exact: true }).fill("gpt-4o-mini-transcribe");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("语音配置已保存");
  expect(writes[1].credential).toBeUndefined();
  await page.getByLabel("转写服务", { exact: true }).selectOption("elevenlabs");
  await expect(page.getByLabel("转写模型", { exact: true })).toHaveValue("scribe_v2");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.locator(".speech-admin").getByRole("alert")).toContainText("开启语音输入前，请填写服务商、模型和密钥。");
  expect(writes).toHaveLength(2);
  await page.getByLabel("API 密钥", { exact: true }).fill("fixture-eleven-secret");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("语音配置已保存");
  expect(writes[2]).toMatchObject({ provider: "elevenlabs", model_id: "scribe_v2", endpoint: "https://api.elevenlabs.io/v1", credential: "fixture-eleven-secret" });
  await page.getByRole("button", { name: "清除已保存的密钥", exact: true }).click();
  await expect(page.locator('.speech-admin-enable input')).not.toBeChecked();
  await expect(page.getByLabel("API 密钥", { exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("语音配置已保存");
  expect(writes[3]).toMatchObject({ enabled: false, clear_credential: true });
  expect(writes[3].credential).toBeUndefined();
  expect(api.mutations).toEqual([]);
  await expect(page).toHaveTitle(/Lester/);
  await expect(page.locator("nextjs-portal")).toHaveCount(0);
  await page.screenshot({ path: info.outputPath("speech-admin-explicit-configuration.png") });
});

test("admin read errors retry by read only and conflicting saves retain edits until the latest revision is reviewed", async ({ page, context }) => {
  let reads = 0, failRead = true;
  const writes: Record<string, unknown>[] = [];
  let settings = { ...initial(), enabled: true, has_credential: true };
  await context.route("**/api/v1/admin/speech", route => {
    if (route.request().method() === "GET") { reads++; return route.fulfill(failRead ? { status: 503, json: { error: "语音配置加载失败，请重试。" } } : { json: settings }); }
    const body = route.request().postDataJSON(); writes.push(body);
    if (writes.length === 1) { settings = { ...settings, model_id: "other-admin-model", revision: 7 }; return route.fulfill({ status: 409, json: { error: "conflict" } }); }
    settings = { ...settings, ...body, revision: settings.revision + 1 }; return route.fulfill({ json: settings });
  });
  await page.goto("/admin/speech");
  await expect(page.locator(".speech-admin").getByRole("alert")).toContainText("语音配置加载失败，请重试。");
  await expect(page.locator(".speech-admin-form")).toHaveCount(0);
  failRead = false;
  await page.getByRole("button", { name: "重试", exact: true }).click();
  await expect(page.getByLabel("转写模型", { exact: true })).toHaveValue("gpt-4o-transcribe");
  await page.getByLabel("转写模型", { exact: true }).fill("keep-my-model-edit");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.locator(".speech-admin").getByRole("alert")).toContainText("语音配置已被其他管理员修改");
  await expect(page.getByLabel("转写模型", { exact: true })).toHaveValue("keep-my-model-edit");
  await expect(page.getByRole("button", { name: "保存", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "读取最新版本", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("编辑内容已保留");
  await expect(page.getByLabel("转写模型", { exact: true })).toHaveValue("keep-my-model-edit");
  expect(writes).toHaveLength(1);
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("语音配置已保存");
  expect(writes[1]).toMatchObject({ revision: 7, model_id: "keep-my-model-edit" });
  expect(reads).toBe(3);
});

test("changing a compatible endpoint cannot reuse a saved key or retain a key typed for another origin", async ({ page, context }) => {
  const writes = await configure(context, { ...initial(), enabled: true, has_credential: true });
  await page.goto("/admin/speech");
  await expect(page.getByLabel("API 密钥", { exact: true })).toHaveAttribute("placeholder", "已保存密钥，留空保留");
  await page.getByLabel("API 密钥", { exact: true }).fill("fixture-old-host-key");
  await page.getByText("高级设置", { exact: true }).click();
  await page.getByLabel("接口基础地址", { exact: true }).fill("https://asr.example.test/v1");
  await expect(page.getByLabel("API 密钥", { exact: true })).toHaveValue("");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.locator(".speech-admin").getByRole("alert")).toContainText("开启语音输入前，请填写服务商、模型和密钥。");
  expect(writes).toHaveLength(0);
  await page.locator('.speech-admin-enable input').uncheck();
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("语音配置已保存");
  expect(writes[0]).toMatchObject({ enabled: false, endpoint: "https://asr.example.test/v1" });
  expect(writes[0].credential).toBeUndefined();
});

for (const locale of ["zh-CN", "en", "ja", "ko", "fr", "es"] as Locale[]) {
  test(`${locale} speech administration stays localized and usable at 320px`, async ({ page, context }, info) => {
    const t = translator(locale === "zh-CN" ? {} : JSON.parse(readFileSync(join(process.cwd(), `src/lib/i18n/messages/${locale}.json`), "utf8")));
    await context.addCookies([{ name: "lester_locale", value: locale, url: origin }]);
    await configure(context);
    await page.setViewportSize({ width: 320, height: 760 });
    await page.goto("/admin/speech");
    await expect(page.locator("html")).toHaveAttribute("lang", locale);
    await expect(page.getByRole("heading", { name: t("语音输入"), exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: t("语音输入"), exact: true })).toHaveAttribute("aria-current", "page");
    await expect(page.getByLabel(t("转写模型"), { exact: true })).toHaveValue("gpt-4o-transcribe");
    await page.getByLabel(t("转写模型"), { exact: true }).fill("keep-model-name");
    await page.getByRole("button", { name: t("保存"), exact: true }).click();
    await expect(page.getByRole("status")).toContainText(t("语音配置已保存；实际可用性需通过录音验证。"));
    await expect(page.getByLabel(t("转写模型"), { exact: true })).toHaveValue("keep-model-name");
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)).toBe(false);
    await expect(page.locator("nextjs-portal")).toHaveCount(0);
    await page.screenshot({ path: info.outputPath(`${locale}-speech-admin.png`) });
  });
}
