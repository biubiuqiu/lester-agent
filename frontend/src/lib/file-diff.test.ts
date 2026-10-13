import { test } from "node:test";
import assert from "node:assert/strict";
import { fileDiff } from "./file-diff";
test("actual diff retains original whitespace, duplicate lines and final newline", () => {
  for (const [before, after] of [["a\na\n", "a\nb\na\n"], ["  中文\n\n", "  日本語\n"], ["", "new"], ["last\n", "last"], ["a\nb", "b\na"]]) {
    const lines = fileDiff(before, after)!;
    assert.equal(lines.filter(line => line.kind !== "added").map(line => line.text).join("\n"), before);
    assert.equal(lines.filter(line => line.kind !== "removed").map(line => line.text).join("\n"), after);
  }
});
test("large comparisons retain text-pane fallback without fabricated differences", () => {
  assert.equal(fileDiff("line\n".repeat(1001), "changed"), null);
});
