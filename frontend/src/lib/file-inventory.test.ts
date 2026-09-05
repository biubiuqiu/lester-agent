import test from "node:test";
import assert from "node:assert/strict";
import { relativeFilePath, scanFiles, wasPathCovered } from "./file-inventory";
import type { FileEntry } from "./api";

const file = (path: string, is_dir = false): FileEntry => ({ name: path.split("/").at(-1)!, path, is_dir, size: 10, modified_at: "2026-09-05T00:00:00Z" });

test("newly expanded excluded directories are not reported as new files", () => {
  const directories = { "": { entries: [file("node_modules", true)], error: "" } };
  assert.equal(wasPathCovered("node_modules/existing.txt", directories), false);
  assert.equal(wasPathCovered("new-folder/new.txt", directories), true);
  assert.equal(wasPathCovered("new.txt", directories), true);
  assert.equal(wasPathCovered("broken/x", { "": { entries: [], error: "offline" } }), false);
});

test("paths retain spaces, Unicode and quotes but reject traversal", () => {
  assert.equal(relativeFilePath("c", '/workspace/conversations/c/a 中文 "quote".md'), 'a 中文 "quote".md');
  assert.equal(relativeFilePath("c", "./dir\\file.py"), "dir/file.py");
  for (const path of ["../x", "x/../../y", "/etc/passwd", "/workspace/conversations/other/x", ""]) assert.equal(relativeFilePath("c", path), "");
});

test("inventory discovers nested files and explicitly opened excluded directories", async (context) => {
  const calls: string[] = [];
  const tree: Record<string, FileEntry[]> = { "": [file("report.md"), file("src", true), file("node_modules", true)], src: [file("src/a.ts")], node_modules: [file("node_modules/info.txt")] };
  context.mock.method(globalThis, "fetch", async (input: string | URL | Request) => {
    const path = new URL(input instanceof Request ? input.url : input, "http://localhost").searchParams.get("path")!;
    calls.push(path);
    return Response.json({ files: tree[path] ?? [] });
  });
  const first = await scanFiles("c", new AbortController().signal, []);
  assert.deepEqual(first.files.map((entry) => entry.path), ["report.md", "src/a.ts"]);
  assert.equal(first.limited, false);
  assert.ok(!calls.includes("node_modules"));
  const second = await scanFiles("c", new AbortController().signal, ["node_modules"]);
  assert.ok(second.files.some((entry) => entry.path === "node_modules/info.txt"));
  const repeated = await scanFiles("c", new AbortController().signal, ["node_modules"]);
  assert.equal(second.signature, repeated.signature);
});

test("directory scans are bounded and disclose incomplete coverage", async (context) => {
  let calls = 0;
  context.mock.method(globalThis, "fetch", async (input: string | URL | Request) => {
    calls++;
    const path = new URL(input instanceof Request ? input.url : input, "http://localhost").searchParams.get("path");
    return Response.json({ files: path ? [] : Array.from({ length: 100 }, (_, i) => file(`dir${i}`, true)) });
  });
  const result = await scanFiles("c", new AbortController().signal, []);
  assert.equal(calls, 64);
  assert.equal(result.limited, true);
});

test("file count and depth are bounded", async (context) => {
  const mock = context.mock.method(globalThis, "fetch", async () => Response.json({ files: Array.from({ length: 2100 }, (_, i) => file(`f${i}.txt`)) }));
  const result = await scanFiles("c", new AbortController().signal, []);
  assert.equal(result.files.length, 2000);
  assert.equal(result.limited, true);
  mock.mock.mockImplementation(async (input: string | URL | Request) => {
    const path = new URL(input instanceof Request ? input.url : input, "http://localhost").searchParams.get("path");
    return Response.json({ files: [file(path ? `${path}/next` : "next", true)] });
  });
  const deep = await scanFiles("c", new AbortController().signal, []);
  assert.equal(Object.keys(deep.directories).length, 6);
  assert.equal(deep.limited, true);
});

test("subdirectory failure is partial, root failure and cancellation reject", async (context) => {
  const mock = context.mock.method(globalThis, "fetch", async (input: string | URL | Request) => {
    const path = new URL(input instanceof Request ? input.url : input, "http://localhost").searchParams.get("path");
    return path ? Response.json({ error: "unavailable" }, { status: 500 }) : Response.json({ files: [file("broken", true)] });
  });
  const partial = await scanFiles("c", new AbortController().signal, []);
  assert.equal(partial.limited, true);
  assert.ok(partial.directories.broken.error);
  mock.mock.mockImplementation(async () => { throw new DOMException("Aborted", "AbortError"); });
  await assert.rejects(scanFiles("c", new AbortController().signal, []), { name: "AbortError" });
});
