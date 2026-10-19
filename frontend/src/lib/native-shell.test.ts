import assert from "node:assert/strict";
import test from "node:test";
import { nativeCommandPath, NativeRunFeedback } from "./native-shell";
import type { Conversation } from "./api";
const conversation = (status: string, run = "run-a") => ({ id: "conversation-a", run_id: run, run_status: status }) as Conversation;

test("native commands navigate only to known routes with strict identifiers", () => {
  const id = "11111111-1111-4111-8111-111111111111";
  assert.equal(nativeCommandPath({ action: "conversation", id }), `/app/c/${id}`);
  assert.equal(nativeCommandPath({ action: "home" }), "/app");
  assert.equal(nativeCommandPath({ action: "settings" }), "/app/settings/profile");
  assert.equal(nativeCommandPath({ action: "tasks" }), "/app/settings/automations");
  for (const value of [null, {}, { action: "send" }, { action: "approve" }, { action: "conversation", id: "../settings" }, { action: "project", id: "https://example.test" }]) assert.equal(nativeCommandPath(value), null);
});
test("completion haptics require an observed active task and are once per account/run", () => {
  const gate = new NativeRunFeedback();
  gate.reset("user:workspace", [conversation("completed")]);
  assert.equal(gate.complete("run-a", "conversation-a", 5), null);
  gate.reset("user:workspace", [conversation("running", "run-b")]);
  assert.deepEqual(gate.complete("run-b", "conversation-a", 6), { account: "user:workspace", run_id: "run-b", conversation_id: "conversation-a", event_id: 6 });
  gate.reset("user:workspace", [conversation("running", "run-b")]);
  assert.equal(gate.complete("run-b", "conversation-a", 7), null);
  gate.reset("other:workspace", [conversation("running", "run-b")]);
  assert.equal(gate.complete("run-b", "conversation-a", 8)?.account, "other:workspace");
});

test("returning to the foreground cannot vibrate for a completion from the background replay", () => {
  const gate = new NativeRunFeedback();
  gate.reset("user:workspace", [conversation("running")]);
  gate.foreground();
  gate.reset("user:workspace", [conversation("running")]);
  assert.equal(gate.complete("run-a", "conversation-a", 9, new Date(Date.now() - 60_000).toISOString()), null);
  gate.reset("user:workspace", [conversation("running")]);
  assert.equal(gate.complete("run-a", "conversation-a", 10, new Date().toISOString()), null);
});
