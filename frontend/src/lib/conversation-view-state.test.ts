import assert from "node:assert/strict";
import test from "node:test";
import { clearViewState, readView, updateView, viewKey, resumeViewState } from "./conversation-view-state";

const values: Record<string, string> = {};
const storage = Object.defineProperties(values, {
  getItem: { value: (key: string) => values[key] ?? null },
  setItem: { value: (key: string, value: string) => { values[key] = value; }, writable: true },
  removeItem: { value: (key: string) => { delete values[key]; } },
}) as unknown as Storage;
Object.defineProperty(globalThis, "window", { value: { sessionStorage: storage }, configurable: true });

test("drafts and view state are isolated by user, workspace and conversation", () => {
  clearViewState();
  resumeViewState("");
  const a = viewKey("u1", "w1", "c1");
  updateView(a, { text: "draft", reference: "hello world.py", tabs: ["hello world.py"], selected: "hello world.py", thread: { top: 350, following: false } });
  assert.equal(readView(a).text, "draft");
  for (const key of [viewKey("u2", "w1", "c1"), viewKey("u1", "w2", "c1"), viewKey("u1", "w1", "c2")]) assert.equal(readView(key).text, "");
  assert.deepEqual(readView(a).thread, { top: 350, following: false });
});
test("switching retains File objects, persistence contains only attachment names", () => {
  const file = new File(["private"], "report.txt");
  updateView("attachments", { files: [file] });
  assert.equal(readView("attachments").files[0], file);
  const saved = JSON.parse(values["lester.view.v1.attachments"]);
  assert.deepEqual(saved.attachmentNames, ["report.txt"]);
  assert.equal(saved.files, undefined);
  values["lester.view.v1.reloaded"] = values["lester.view.v1.attachments"];
  assert.deepEqual(readView("reloaded").missingFiles, ["report.txt"]);
  assert.deepEqual(readView("reloaded").files, []);
});
test("corrupt storage and disabled storage do not break drafts", () => {
  values["lester.view.v1.corrupt"] = "{";
  assert.equal(readView("corrupt").text, "");
  const original = storage.setItem;
  storage.setItem = () => { throw Error("quota"); };
  updateView("blocked", { text: "still editable" });
  assert.equal(readView("blocked").text, "still editable");
  storage.setItem = original;
});

test("logout clears persisted state and rejects late cleanup writes", () => {
  updateView("logout", { text: "private" });
  clearViewState();
  updateView("logout", { text: "late write" });
  assert.equal(values["lester.view.v1.logout"], undefined);
  resumeViewState("u2.w2.");
  updateView("u1.w1.c1", { text: "stale account" });
  assert.equal(readView("u1.w1.c1").text, "");
  updateView("u2.w2.c1", { text: "new account" });
  assert.equal(readView("u2.w2.c1").text, "new account");
});
