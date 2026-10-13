import { test } from "node:test";
import assert from "node:assert/strict";
import { sameInteractionResponse } from "./interaction-response";

test("lost reply receipts compare canonical fields without granting a changed answer", () => {
  const intended = { request_digest: "exact-plan", answers: { choice: { text: "", option_id: "html" } } };
  assert.equal(sameInteractionResponse(intended, { answers: { choice: { option_id: "html" } }, request_digest: "exact-plan" }), true);
  assert.equal(sameInteractionResponse(intended, { request_digest: "other-plan", answers: { choice: { option_id: "html" } } }), false);
  assert.equal(sameInteractionResponse(intended, { request_digest: "exact-plan", answers: { choice: { option_id: "document" } } }), false);
  assert.equal(sameInteractionResponse({ feedback: " do this " }, { feedback: "do this" }), false);
});
