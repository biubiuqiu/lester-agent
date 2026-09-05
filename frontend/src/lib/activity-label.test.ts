import assert from "node:assert/strict";
import test from "node:test";
import { activityLabel } from "./activity-label";

test("tool labels use complete real arguments, preserving Unicode and spaces", () => {
  assert.equal(activityLabel("running", { type: "TOOL_STARTED", payload: { tool: "write", arguments: JSON.stringify({ file_path: '/workspace/conversations/id/旅行 "计划".html', content: "large content" }) } }), '正在写入 旅行 "计划".html');
});
test("partial tool arguments and null arguments safely fall back", () => {
  for (const args of ['{"file_path":', "null", null]) assert.equal(activityLabel("running", { type: "TOOL_STARTED", payload: { tool: "edit", arguments: args } }), "正在修改 文件");
});
test("finished file operations never pretend to still be writing", () => {
  assert.equal(activityLabel("running", { type: "FILE_UPDATED", payload: {} }), "文件已更新，等待下一步");
  assert.equal(activityLabel("stopping"), "正在停止当前任务");
  assert.equal(activityLabel("running"), "等待任务进展");
});
