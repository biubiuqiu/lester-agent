import assert from "node:assert/strict";
import test from "node:test";
import { artifactFiles } from "./artifact-files";
import type { FileEntry } from "./api";

const file = (path: string): FileEntry => ({ path, name: path, size: 100, is_dir: false, modified_at: "2026-09-06T00:00:00Z" });

test("streaming files stay in inventory but appear in the footer only after the run settles", () => {
  const files = [file("index.html")];
  const changes = [{ path: "index.html", kind: "updated" }];
  assert.deepEqual(artifactFiles(files, changes, true), []);
  assert.equal(files.length, 1);
  assert.equal(changes.length, 1);
  assert.deepEqual(artifactFiles(files, changes, false), files);
  // A new send or a restored active run must hide the footer again.
  assert.deepEqual(artifactFiles(files, changes, true), []);
});

test("settled runs show only verified, non-deleted files, including late inventory updates", () => {
  const changes = [{ path: "new.html", kind: "added" }, { path: "gone.txt", kind: "deleted" }];
  assert.deepEqual(artifactFiles([], changes, false), []);
  assert.deepEqual(artifactFiles([file("new.html"), file("gone.txt")], changes, false), [file("new.html")]);
});

test("the settled footer remains bounded to twelve files", () => {
  const files = Array.from({ length: 20 }, (_, i) => file(`${i}.txt`));
  assert.equal(artifactFiles(files, files.map(({ path }) => ({ path, kind: "added" })), false).length, 12);
});
