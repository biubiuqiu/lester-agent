import assert from "node:assert/strict";
import test from "node:test";
import { AutoOpenFiles, mergeFileTabs } from "./auto-open-files";
import type { FileEntry } from "./api";

const file = (path: string, modified_at = "1"): FileEntry => ({ path, name: path.split("/").at(-1)!, is_dir: false, size: 12, modified_at });
const event = (id: number, path: unknown, type = "FILE_UPDATED", conversation_id = "c1") => ({ id, conversation_id, type, payload: type === "DELIVERABLE_REGISTERED" ? { entry_path: path } : { path } });

test("live writes wait for inventory and consume once; history and replay cannot reopen closed tabs", () => {
  const queue = new AutoOpenFiles("c1", [event(1, "old.html")]);
  assert.deepEqual(queue.receive([event(1, "old.html"), event(2, "new.html")], 0), ["new.html"]);
  assert.deepEqual(queue.take([file("old.html")], 1), []);
  assert.deepEqual(queue.take([file("new.html")], 2).map((file) => file.path), ["new.html"]);
  queue.receive([event(2, "new.html")], 3);
  queue.changed([file("new.html")], 3);
  assert.deepEqual(queue.take([file("new.html")], 4), []);
  queue.receive([event(3, "new.html")], 5);
  assert.equal(queue.take([file("new.html")], 6).length, 1, "a genuine later edit opens even with unchanged size/mtime");
});

test("normalize own conversation paths and reject escapes, directories, internal files and other conversations", () => {
  const queue = new AutoOpenFiles("c1");
  const paths = ["/workspace/conversations/c1/site/index.html", "../outside.html", "/workspace/conversations/c2/private.html", ".agent/log.txt", "node_modules/a.js", "site/../escape.html", "bad\u0000.html", "folder", 123];
  assert.deepEqual(queue.receive(paths.map((path, index) => event(index + 1, path)), 0), ["site/index.html", "folder"]);
  queue.receive([event(20, "foreign.html", "FILE_UPDATED", "c2")], 0);
  assert.deepEqual(queue.take([file("site/index.html"), { ...file("folder"), is_dir: true }, file("foreign.html")], 1).map((file) => file.path), ["site/index.html"]);
});

test("registration opens the verified entry and coalesces writes in arrival order", () => {
  const queue = new AutoOpenFiles("c1");
  queue.receive([event(1, "a.html"), event(2, "b.md"), event(3, "a.html", "DELIVERABLE_REGISTERED")], 0);
  assert.deepEqual(queue.take([file("a.html"), file("b.md")], 1).map((file) => file.path), ["b.md", "a.html"]);
});

test("command supervisor files never auto-open, while user-authored state.json still opens", () => {
  const queue = new AutoOpenFiles("c1");
  const internal = [".lester/tasks/t1/state.json", ".lester/tasks/t1/output.log", ".lester/tasks/t1/owner.json", "nested/.lester/tasks/t2/state.json"];
  const outputs = ["state.json", "site/index.html"];
  assert.deepEqual(queue.receive([...internal, ...outputs].map((path, i) => event(i + 1, path)), 0), outputs);
  queue.changed([...internal, ...outputs].map(path => file(path)), 1);
  assert.deepEqual(queue.take([...internal, ...outputs].map(path => file(path)), 2).map(entry => entry.path), outputs);
  queue.changed(internal.map(path => file(path, "2")), 3);
  assert.deepEqual(queue.take(internal.map(path => file(path, "2")), 4), []);
});

test("shell inventory changes open new versions, but do not duplicate an explicit write", () => {
  const queue = new AutoOpenFiles("c1");
  queue.receive([event(1, "index.html")], 0);
  queue.changed([file("index.html"), file("report.md")], 0);
  assert.equal(queue.take([file("index.html"), file("report.md")], 1).length, 2);
  queue.changed([file("index.html"), file("report.md")], 2);
  assert.deepEqual(queue.take([file("index.html"), file("report.md")], 3), []);
  queue.changed([file("report.md", "2")], 4);
  assert.equal(queue.take([file("report.md", "2")], 5)[0].modified_at, "2");
});

test("missing notifications expire and burst queues are bounded", () => {
  const queue = new AutoOpenFiles("c1");
  queue.receive([event(1, "missing.html")], 0);
  assert.deepEqual(queue.take([file("missing.html")], 60_000), []);
  const files = Array.from({ length: 100 }, (_, i) => file(`file-${i}.txt`));
  queue.receive(files.map((file, i) => event(i + 2, file.path)), 70_000);
  assert.equal(queue.take(files, 70_001).length, 64);
});

test("tabs reuse paths, retain positions/metadata and cap without dropping the selected file", () => {
  const tabs = Array.from({ length: 8 }, (_, i) => file(`${i}.html`));
  const reopened = mergeFileTabs(tabs, [file("0.html", "2")]);
  assert.deepEqual(reopened.map((file) => file.path), tabs.map((file) => file.path));
  assert.equal(reopened[0].modified_at, "2");
  const next = mergeFileTabs(tabs, [file("new.md"), file("0.html", "3")]);
  assert.equal(next.length, 8);
  assert(next.some((file) => file.path === "new.md"));
  assert.equal(next[0].path, "0.html");
  assert.equal(next[0].modified_at, "3");
  const burst = mergeFileTabs(tabs, Array.from({ length: 12 }, (_, i) => file(`new-${i}.txt`)), "0.html");
  assert.equal(burst.length, 8);
  assert(burst.some(file => file.path === "0.html"), "background writes must not evict the file being read");
});

test("closing cancels delayed auto-opens until inventory settles, while later writes remain visible", () => {
  const queue = new AutoOpenFiles("c1");
  queue.receive([event(1, "notes.txt")], 0);
  queue.take([file("notes.txt")], 1);
  queue.receive([event(2, "notes.txt")], 2);
  queue.dismiss("notes.txt");
  queue.changed([file("notes.txt", "2")], 3);
  assert.deepEqual(queue.take([file("notes.txt", "2")], 4), []);
  queue.receive([event(2, "notes.txt")], 5);
  queue.changed([file("notes.txt", "2")], 5);
  assert.deepEqual(queue.take([file("notes.txt", "2")], 6), []);
  queue.changed([file("notes.txt", "3")], 7);
  assert.equal(queue.take([file("notes.txt", "3")], 8).length, 1);
  queue.dismiss("notes.txt");
  queue.receive([event(3, "notes.txt")], 9);
  assert.equal(queue.take([file("notes.txt", "3")], 10).length, 1, "a later explicit write remains visible even without a metadata change");
});
