import { test } from "node:test";
import assert from "node:assert/strict";
import { CommandLogDecoder } from "./command-log-decoder";

test("raw log pages preserve Chinese and emoji across every byte boundary", () => {
  const text = "任务完成🙂\n", bytes = Buffer.from(text);
  for (let boundary = 1; boundary < bytes.length; boundary++) {
    const decoder = new CommandLogDecoder();
    const first = decoder.decode({ data: bytes.subarray(0, boundary).toString("base64"), offset: 0, next_offset: boundary, has_more: true }, true);
    const second = decoder.decode({ data: bytes.subarray(boundary).toString("base64"), offset: boundary, next_offset: bytes.length, has_more: false }, true);
    assert.equal(first + second, text);
  }
});
test("restarting a log discards a pending partial character", () => {
  const decoder = new CommandLogDecoder();
  decoder.decode({ data: Buffer.from("🙂").subarray(0, 2).toString("base64"), offset: 0, next_offset: 2, has_more: true }, true);
  assert.equal(decoder.decode({ data: Buffer.from("fresh").toString("base64"), offset: 0, next_offset: 5, has_more: false }, true), "fresh");
});
