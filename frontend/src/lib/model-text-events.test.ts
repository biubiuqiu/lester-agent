import { test } from "node:test";
import assert from "node:assert/strict";
import { withoutPersistedModelText } from "./model-text-events";

test("same-run adjustment retains new streaming text without duplicating persisted prior replies", () => {
  const event = (id: number, type: string, payload: Record<string, unknown> = {}) => ({ id, type, payload, run_id: "one" });
  const events = [event(1, "MODEL_STARTED"), event(2, "MODEL_DELTA", { delta: "first reply" }), event(3, "MODEL_COMPLETED", { message_id: "saved" }), event(4, "RUN_INPUT_APPLIED"), event(5, "MODEL_STARTED"), event(6, "MODEL_DELTA", { delta: "new reply" }), event(7, "TOOL_STARTED")];
  assert.deepEqual(withoutPersistedModelText(events, [{ id: "saved", role: "assistant" }]).map(item => item.id), [1, 3, 4, 5, 6, 7]);
  assert.equal(withoutPersistedModelText(events, []).length, events.length);
  assert.equal(withoutPersistedModelText(events, [{ id: "saved", role: "user" }]).length, events.length);
});
