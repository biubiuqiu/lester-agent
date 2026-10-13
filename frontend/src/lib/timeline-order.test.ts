import { test } from "node:test";
import assert from "node:assert/strict";
import { orderedMessages, runSegments, segmentOrder } from "./timeline-order";
test("same-run adjustment anchors after its durable messages despite clock skew", () => {
  const messages = orderedMessages([
    { id: "adjust", role: "user", run_id: "run", seq: 3, created_at: "2026-01-01" },
    { id: "first", role: "user", run_id: "run", seq: 1, created_at: "2026-01-03" },
    { id: "reply", role: "assistant", run_id: "run", seq: 2, created_at: "2026-01-02" },
  ]);
  const event = (id: number, type: string, payload: Record<string, unknown> = {}) => ({ id, type, payload, run_id: "run", created_at: "2025-01-01" });
  const parts = runSegments([event(4, "MODEL_DELTA"), event(1, "RUN_STARTED", { input_message_id: "first" }), event(3, "RUN_INPUT_APPLIED", { message_ids: ["adjust"] }), event(2, "TOOL_STARTED")]);
  assert.deepEqual(messages.map(message => message.id), ["first", "reply", "adjust"]);
  assert.deepEqual(parts.map(part => part.map(event => event.id)), [[1, 2], [3, 4]]);
  assert.equal(segmentOrder(parts[0], messages), 1);
  assert.equal(segmentOrder(parts[1], messages), 21);
});
test("all adjustments precede their response", () => {
  const messages = ["one", "two", "three"].map(id => ({ id, role: "user", created_at: "2026-01-01" }));
  assert.equal(segmentOrder([{ id: 1, type: "RUN_INPUT_APPLIED", run_id: "run", created_at: "2025-01-01", payload: { message_ids: ["two", "three"] } }], messages), 21);
});
