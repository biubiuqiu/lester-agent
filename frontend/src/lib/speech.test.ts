import assert from "node:assert/strict";
import test from "node:test";
import { appendSpeechText, readSpeechReceipt, speechAccountScope, speechFileName } from "./speech";

test("voice text appends to the latest draft without rewriting its contents or truncating", () => {
  assert.deepEqual(appendSpeechText("typed while recording", "  fix GitHub 登录  ", 100), { text: "typed while recording\nfix GitHub 登录", exceeded: false });
  assert.deepEqual(appendSpeechText("latest cross-tab text\n", "second paragraph", 100), { text: "latest cross-tab text\nsecond paragraph", exceeded: false });
  assert.deepEqual(appendSpeechText("draft", "voice", 5), { text: "draft\nvoice", exceeded: true });
  assert.deepEqual(appendSpeechText("do not clear", "  ", 100), { text: "do not clear", exceeded: false });
});

test("audio filename follows the actual browser recorder container", () => {
  assert.equal(speechFileName("audio/mp4; codecs=mp4a.40.2"), "recording.m4a");
  assert.equal(speechFileName("audio/webm;codecs=opus"), "recording.webm");
  assert.equal(speechFileName("audio/ogg;codecs=opus"), "recording.ogg");
});

test("speech receipts stay bound to the original request and explicit server outcome", () => {
  assert.equal(readSpeechReceipt({ id: "original", status: "unknown" }, "original").status, "unknown");
  assert.throws(() => readSpeechReceipt({ id: "other", status: "completed", text: "late account result" }, "original"));
  assert.throws(() => readSpeechReceipt({ id: "original", status: "ok", text: "invented success" }, "original"));
  assert.throws(() => readSpeechReceipt({ id: "original", status: "completed", text: "a".repeat(20_001) }, "original"));
  assert.equal(speechAccountScope("user.workspace.new.project"), "user.workspace.");
  assert.equal(speechAccountScope(""), "");
});
