import assert from "node:assert/strict";
import test from "node:test";
import { formatMessage, loadMessages, resolveLocale, supportedLocale, translator } from "./i18n";

test("manual locale wins; browser preferences honor weights, regional variants and exclusions", () => {
  assert.equal(resolveLocale("fr", "ja-JP"), "fr");
  assert.equal(resolveLocale(undefined, "de;q=1, es-MX;q=0.9, en;q=0.4"), "es");
  assert.equal(resolveLocale("invalid", "ko-KR, en;q=0.8"), "ko");
  assert.equal(resolveLocale(undefined, "zh-TW;q=0, ja;q=0.5"), "ja");
  assert.equal(resolveLocale(undefined, "de-DE"), "en");
  assert.equal(resolveLocale(undefined), "en");
  assert.equal(supportedLocale("zh-Hans-CN"), "zh-CN");
  assert.equal(supportedLocale("EN_us"), "en");
  assert.equal(supportedLocale("english"), undefined);
});

test("interpolation permits reordered values and treats filenames and user input as data", () => {
  const t = translator({ "第 {0} 行 · 第 {1} 列": "Column {1}, line {0}", "查看文件 {0}": "View {0}", "{0} {1}": "{0} {1}" });
  assert.equal(t("第 {0} 行 · 第 {1} 列", [3, 9]), "Column 9, line 3");
  assert.equal(t("查看文件 {0}", ['文件 {1} <script>$&</script>.txt']), 'View 文件 {1} <script>$&</script>.txt');
  assert.equal(t("user-provided content"), "user-provided content");
  assert.equal(t("文件"), "文件");
  assert.equal(formatMessage("a {0} b {1}", [0]), "a 0 b {1}");
});

test("stored source notices can change language without changing embedded details", () => {
  const t = translator({ "发送失败：{0}。草稿已保留。": "Draft kept. Send failed: {0}.", "错误 a+b：{0}": "Detail: {0}" });
  assert.equal(t("发送失败：HTTP 503\ninvalid credentials。草稿已保留。"), "Draft kept. Send failed: HTTP 503\ninvalid credentials.");
  assert.equal(t("错误 a+b：x[0]"), "Detail: x[0]");
  assert.equal(t("错误 aab：x[0]"), "错误 aab：x[0]");
});

test("all requested languages load independent dictionaries with translated help copy", async () => {
  assert.deepEqual(await loadMessages("zh-CN"), {});
  const [english, japanese, korean, french, spanish] = await Promise.all([loadMessages("en"), loadMessages("ja"), loadMessages("ko"), loadMessages("fr"), loadMessages("es")]);
  assert.equal(english["想清楚。"], "Think it through.");
  assert.equal(japanese["帮助文档"], "ヘルプドキュメント");
  assert.equal(korean["帮助文档"], "도움말 문서");
  assert.equal(french["帮助文档"], "Documentation");
  assert.equal(spanish["帮助文档"], "Documentación");
  const message = "access token 有效 2 小时，refresh token 有效 30 天。持续使用会自动轮换并重新计算 30 天，可持续续期；30 天未续期需重新登录。退出登录或重设密码会撤销令牌。";
  for (const dictionary of [english, japanese, korean, french, spanish]) assert.ok(dictionary[message]);
});
