import { test } from "node:test";
import assert from "node:assert/strict";
import { sameRunInput } from "./run-input";
test("receipt matching tolerates omitted empty arrays but binds the full accepted task", () => {
  const request = { id: "id", run_id: "run", kind: "next" as const, content: "target", attachment_ids: [] };
  assert.equal(sameRunInput(request, { ...request, attachment_ids: undefined, context_ids: [] }), true);
  for (const changed of [{ ...request, content: "changed" }, { ...request, run_id: "other" }, { ...request, execution_mode: "plan" as const }, { ...request, references: [{ type: "skill" as const, id: "skill" }] }]) assert.equal(sameRunInput(request, changed), false);
});
