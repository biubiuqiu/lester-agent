import assert from "node:assert/strict";
import test from "node:test";
import { clearViewState, readView, updateView, viewKey, resumeViewState } from "./conversation-view-state";

const values: Record<string, string> = {};
const storage = Object.defineProperties(values, {
  getItem: { value: (key: string) => values[key] ?? null },
  setItem: { value: (key: string, value: string) => { values[key] = value; }, writable: true },
  removeItem: { value: (key: string) => { delete values[key]; } },
}) as unknown as Storage;
Object.defineProperty(globalThis, "window", { value: { sessionStorage: storage, localStorage: storage }, configurable: true });

test("drafts and view state are isolated by user, workspace and conversation", () => {
  clearViewState();
  resumeViewState("");
  const a = viewKey("u1", "w1", "c1");
  updateView(a, { text: "draft", reference: "hello world.py", tabs: ["hello world.py"], selected: "hello world.py", thread: { top: 350, following: false } });
  assert.equal(readView(a).text, "draft");
  for (const key of [viewKey("u2", "w1", "c1"), viewKey("u1", "w2", "c1"), viewKey("u1", "w1", "c2")]) assert.equal(readView(key).text, "");
  assert.deepEqual(readView(a).thread, { top: 350, following: false });
});
test("restoring a conversation drops old runtime tabs without dropping actual user files or drafts", () => {
  values["lester.view.v1.runtime-tabs"] = JSON.stringify({
    version: 1, text: "keep my draft", tabs: [".lester/tasks/t1/state.json", "state.json", "index.html"],
    selected: ".lester/tasks/t1/state.json", closedTabs: ["notes.txt", ".lester/tasks/t1/output.log"],
    directories: ["", ".lester/tasks/t1", "site"], unreadFiles: [".lester/tasks/t1/state.json", "state.json"],
  });
  const restored = readView("runtime-tabs");
  assert.deepEqual(restored.tabs, ["state.json", "index.html"]);
  assert.equal(restored.selected, null);
  assert.deepEqual(restored.closedTabs, ["notes.txt"]);
  assert.deepEqual(restored.directories, ["", "site"]);
  assert.deepEqual(restored.unreadFiles, ["state.json"]);
  assert.equal(restored.text, "keep my draft");
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
  assert.deepEqual(JSON.parse(values["lester.draft.v1.reloaded"]).attachmentNames, ["report.txt"]);
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

test("durable draft restores after session storage disappears, without restoring File bytes", () => {
  resumeViewState("u2.w2.");
  const key = "u2.w2.reopened";
  values["lester.draft.v1." + key] = JSON.stringify({ version: 1, text: "关窗前打了一半", contexts: [{ id: "context", title: "背景" }], reference: "note.txt", executionMode: "plan", attachmentNames: ["photo.png"] });
  assert.equal(readView(key).text, "关窗前打了一半");
  assert.equal(readView(key).executionMode, "plan");
  assert.equal(readView(key).reference, "note.txt");
  assert.deepEqual(readView(key).missingFiles, ["photo.png"]);
  assert.deepEqual(readView(key).files, []);
  values["lester.draft.v1." + key] = JSON.stringify({ version: 1, text: "another window's new draft" });
  updateView(key, { thread: { top: 20, following: false } });
  assert.equal(JSON.parse(values["lester.draft.v1." + key]).text, "another window's new draft");
  clearViewState();
  assert.equal(values["lester.draft.v1." + key], undefined);
});

test("reference and recap updates retain another tab's latest draft and local File objects", () => {
  clearViewState(); resumeViewState("u.w.");
  const key = "u.w.shared", file = new File(["local"], "report.txt");
  updateView(key, { text: "old", files: [file], tabs: ["old.html"], selected: "old.html" });
  const remote = JSON.parse(values["lester.draft.v1." + key]);
  values["lester.draft.v1." + key] = JSON.stringify({ ...remote, text: "newer draft from another tab", permissionMode: "auto" });
  updateView(key, { reference: "index.html", selected: null });
  assert.equal(readView(key).text, "newer draft from another tab");
  assert.equal(readView(key).permissionMode, "auto");
  assert.equal(readView(key).files[0], file);
  assert.deepEqual(readView(key).tabs, ["old.html"]);
  assert.equal(readView(key).selected, null);
  const saved = JSON.parse(values["lester.draft.v1." + key]);
  assert.equal(saved.text, "newer draft from another tab");
  assert.equal(saved.reference, "index.html");
  values["lester.draft.v1." + key] = JSON.stringify({ ...saved, text: "latest", attachmentNames: ["remote.png"] });
  updateView(key, { recap: { enabled: true, seenAt: 100, eventId: 1, messageId: "m" } });
  assert.equal(readView(key).text, "latest");
  assert.deepEqual(readView(key).files, []);
  assert.deepEqual(readView(key).missingFiles, ["remote.png"]);
  clearViewState();
});
