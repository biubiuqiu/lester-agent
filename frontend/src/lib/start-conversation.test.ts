import assert from "node:assert/strict";
import test from "node:test";
import { startConversation } from "./start-conversation";

test("first submit creates Lester then sends exactly once with the chosen model", async (t) => {
  const calls: { path: string; body: Record<string, unknown> }[] = [];
  t.mock.method(globalThis, "fetch", async (path: string, init: RequestInit) => {
    calls.push({ path, body: JSON.parse(String(init.body)) });
    return Response.json(path.endsWith("/messages") ? { run_id: "r" } : { id: "c" });
  });
  let created = false;
  const result = await startConversation("  hello  ", [], "model", () => { assert.equal(calls.length, 1); created = true; });
  assert.equal(created, true);
  assert.equal(result.id, "c");
  assert.deepEqual(calls, [
    { path: "/api/v1/conversations", body: { agent_slug: "lester", model_deployment_id: "model", title: "hello" } },
    { path: "/api/v1/conversations/c/messages", body: { content: "hello", attachment_ids: [] } },
  ]);
});

test("blank drafts or missing models never create a conversation", async (t) => {
  t.mock.method(globalThis, "fetch", () => { throw new Error("unexpected network call"); });
  await assert.rejects(startConversation(" ", [], "model", () => {}), /请输入/);
  await assert.rejects(startConversation("hello", [], "", () => {}), /请输入/);
});

test("send failure exposes the created conversation for recovery and never retries", async (t) => {
  let requests = 0;
  t.mock.method(globalThis, "fetch", async () => {
    requests++;
    if (requests === 1) return Response.json({ id: "saved" });
    throw new Error("connection lost");
  });
  let saved = "";
  await assert.rejects(startConversation("keep my draft", [], "model", (item) => { saved = item.id; }), /connection lost/);
  assert.equal(saved, "saved");
  assert.equal(requests, 2);
});

test("attachments upload into the new conversation before sending only their IDs", async (t) => {
  const paths: string[] = [];
  t.mock.method(globalThis, "fetch", async (path: string, init: RequestInit) => {
    paths.push(path);
    if (path.endsWith("/attachments")) { assert.ok(init.body instanceof FormData); return Response.json({ id: "attachment" }); }
    if (path.endsWith("/messages")) { assert.deepEqual(JSON.parse(String(init.body)), { content: "", attachment_ids: ["attachment"] }); return Response.json({ run_id: "r" }); }
    return Response.json({ id: "c" });
  });
  await startConversation("", [new File(["not injected"], "notes.txt")], "model", () => {});
  assert.deepEqual(paths, ["/api/v1/conversations", "/api/v1/conversations/c/attachments", "/api/v1/conversations/c/messages"]);
});


test("project selection is sent when creating the conversation", async (t) => {
  const calls: {path: string; body: Record<string, unknown>}[] = [];
  t.mock.method(globalThis, "fetch", async (path: string, init: RequestInit) => {
    calls.push({path, body: JSON.parse(String(init.body))});
    return Response.json(path.endsWith("/messages") ? {run_id:"r"} : {id:"c"});
  });
  await startConversation("project draft", [], "model", () => {}, "selected-project");
  assert.equal(calls[0].body.project_id, "selected-project");
  assert.equal(calls.length, 2);
});
