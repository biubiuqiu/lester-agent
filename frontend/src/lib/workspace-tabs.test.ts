import assert from "node:assert/strict";
import test from "node:test";
import { MAX_TERMINAL_TABS, fileWorkspaceTabId, mergeWorkspaceFiles, removeWorkspaceTab, restoreWorkspaceTabs, type WorkspaceTab } from "./workspace-tabs";

test("legacy layouts migrate file tabs while a deliberately empty new layout stays empty", () => {
  assert.deepEqual(restoreWorkspaceTabs(undefined, ["index.html", "notes.txt"]), [
    { id: "file:index.html", kind: "file", path: "index.html" },
    { id: "file:notes.txt", kind: "file", path: "notes.txt" },
  ]);
  assert.deepEqual(restoreWorkspaceTabs([], ["index.html"]), []);
});

test("restored tabs reject runtime paths, invented files, duplicate IDs and unbounded terminals", () => {
  const terminals = Array.from({ length: 7 }, (_, index) => ({ id: `terminal:t${index + 1}`, kind: "terminal", serial: index + 1 }));
  const restored = restoreWorkspaceTabs([
    { id: "files", kind: "files" }, { id: "files", kind: "files" },
    { id: "file:index.html", kind: "file", path: "index.html" },
    { id: "file:outside.txt", kind: "file", path: "outside.txt" },
    { id: "file:.lester/tasks/a/state.json", kind: "file", path: ".lester/tasks/a/state.json" },
    { id: "file:../escape", kind: "file", path: "../escape" },
    { id: "arbitrary-id", kind: "results" }, { id: "terminal:bad/path", kind: "terminal", serial: 10 },
    ...terminals,
  ], ["index.html", ".lester/tasks/a/state.json", "../escape"]);
  assert.deepEqual(restored.slice(0, 2), [{ id: "files", kind: "files" }, { id: "file:index.html", kind: "file", path: "index.html" }]);
  assert.equal(restored.length, 2 + MAX_TERMINAL_TABS);
  assert.deepEqual(restored.filter(tab => tab.kind === "terminal").map(tab => tab.serial), [1, 2, 3, 4]);
});

test("file-cap reconciliation retains tool ordering and live terminal descriptors", () => {
  const tabs: WorkspaceTab[] = [
    { id: "file:old.txt", kind: "file", path: "old.txt" },
    { id: "terminal:first", kind: "terminal", serial: 1 },
    { id: "results", kind: "results" },
    { id: "file:kept.txt", kind: "file", path: "kept.txt" },
  ];
  assert.deepEqual(mergeWorkspaceFiles(tabs, ["kept.txt", "new.txt"]), [
    tabs[1], tabs[2], tabs[3], { id: fileWorkspaceTabId("new.txt"), kind: "file", path: "new.txt" },
  ]);
});

test("closing a background tab preserves the active tab; closing active selects its neighbor", () => {
  const tabs: WorkspaceTab[] = [{ id: "files", kind: "files" }, { id: "terminal:first", kind: "terminal", serial: 1 }, { id: "results", kind: "results" }];
  assert.equal(removeWorkspaceTab(tabs, "terminal:first", "files").active, "terminal:first");
  assert.equal(removeWorkspaceTab(tabs, "terminal:first", "terminal:first").active, "results");
  assert.equal(removeWorkspaceTab(tabs, "results", "results").active, "terminal:first");
  assert.deepEqual(removeWorkspaceTab([tabs[0]], "files", "files"), { tabs: [], active: null });
  assert.deepEqual(removeWorkspaceTab(tabs, null, "missing"), { tabs, active: null });
});
