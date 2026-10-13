import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { commandOutcome } from "./command-outcome";

describe("command outcomes", () => {
  it("distinguishes execution from success, including legacy and serialized results", () => {
    assert.equal(commandOutcome({ exit_code: 7 }), "failed");
    assert.equal(commandOutcome('{"status":"exited","exit_code":0,"exit_code_known":true}'), "completed");
    assert.equal(commandOutcome({ status: "running", task_id: "build", exit_code: 0 }), "background");
  });
  it("never manufactures success after timeout, cancellation, or unknown process cleanup", () => {
    assert.equal(commandOutcome({ status: "timed_out", exit_code: 0, exit_code_known: false }), "unknown");
    assert.equal(commandOutcome({ status: "cancelled", cleanup_confirmed: true, exit_code: 0 }), "cancelled");
    assert.equal(commandOutcome({ status: "cancelled", cleanup_confirmed: false }), "unknown");
    assert.equal(commandOutcome({ status: "unknown", exit_code: 0 }), "unknown");
    assert.equal(commandOutcome({ exit_code: null }), null);
    assert.equal(commandOutcome("not a result"), null);
  });
});
