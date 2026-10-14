import assert from "node:assert/strict";
import test from "node:test";
import { clearMessageSubmissions, sendConversationMessage } from "./message-submission";

test("an unconfirmed explicit retry reuses its submission ID; a later task gets a new ID", async t => {
  clearMessageSubmissions();
  const sent: Record<string, unknown>[] = [];
  t.mock.method(globalThis, "fetch", async (path: string, init: RequestInit) => {
    if (path.includes("/submissions/")) return Response.json({ error: "not found" }, { status: 404 });
    sent.push(JSON.parse(String(init.body)));
    if (sent.length === 1) throw new Error("response lost");
    return Response.json({ run_id: "accepted" });
  });
  const payload = { content: "continue", context_ids: [], attachment_ids: ["saved-upload"] };
  await assert.rejects(sendConversationMessage("c", payload), /response lost/);
  assert.equal(sent.length, 1, "a failed request must never automatically replay a mutation");
  await sendConversationMessage("c", payload);
  await sendConversationMessage("c", payload);
  assert.match(String(sent[0].submission_id), /^[0-9a-f-]{36}$/);
  assert.equal(sent[0].submission_id, sent[1].submission_id);
  assert.notEqual(sent[1].submission_id, sent[2].submission_id);
});

test("edited content, attachments, references, conversation or logout starts a different attempt", async t => {
  clearMessageSubmissions();
  const ids: string[] = [];
  t.mock.method(globalThis, "fetch", async (path: string, init: RequestInit) => {
    if (path.includes("/submissions/")) return Response.json({ error: "not found" }, { status: 404 });
    ids.push(JSON.parse(String(init.body)).submission_id);
    throw new Error("not confirmed");
  });
  const payload = { content: "same", context_ids: [], attachment_ids: [] };
  await assert.rejects(sendConversationMessage("c", payload));
  await assert.rejects(sendConversationMessage("c", { ...payload, content: "edited" }));
  await assert.rejects(sendConversationMessage("c", { ...payload, attachment_ids: ["new"] }));
  await assert.rejects(sendConversationMessage("c", { ...payload, context_ids: ["context"] }));
  await assert.rejects(sendConversationMessage("other", payload));
  clearMessageSubmissions();
  await assert.rejects(sendConversationMessage("c", payload));
  assert.equal(new Set(ids).size, ids.length);
});

test("a lost POST response is reconciled with one read, without repeating the mutation", async t => {
  clearMessageSubmissions();
  const calls: { path: string; id?: string }[] = [];
  t.mock.method(globalThis, "fetch", async (path: string, init: RequestInit) => {
    if (path.includes("/submissions/")) { calls.push({ path }); return Response.json({ run_id: "saved-run", status: "cancelling" }); }
    calls.push({ path, id: JSON.parse(String(init.body)).submission_id });
    throw new Error("POST response lost");
  });
  assert.deepEqual(await sendConversationMessage("reconciled", { content: "hello", attachment_ids: [] }), { run_id: "saved-run", status: "cancelling" });
  assert.equal(calls.length, 2);
  assert.equal(calls[1].path, `/api/v1/conversations/reconciled/submissions/${calls[0].id}`);
});

test("failed status lookup stays unconfirmed rather than claiming the message was rejected", async t => {
  clearMessageSubmissions();
  let posts = 0, lookups = 0;
  t.mock.method(globalThis, "fetch", async (path: string) => {
    if (path.includes("/submissions/")) { lookups++; return Response.json({ error: "lookup unavailable" }, { status: 503 }); }
    posts++; throw new Error("connection lost");
  });
  await assert.rejects(sendConversationMessage("unknown", { content: "hello", attachment_ids: [] }), /消息发送结果尚未确认/);
  assert.equal(posts, 1); assert.equal(lookups, 1);
});

test("HTTP LAN without secure crypto reuses only exactly matching canonical payloads", async t => {
  clearMessageSubmissions();
  const original = Object.getOwnPropertyDescriptor(globalThis, "crypto")!;
  const nativeCrypto = crypto;
  Object.defineProperty(globalThis, "crypto", { configurable: true, value: { getRandomValues: nativeCrypto.getRandomValues.bind(nativeCrypto) } });
  t.after(() => Object.defineProperty(globalThis, "crypto", original));
  const ids: string[] = [];
  t.mock.method(globalThis, "fetch", async (path: string, init: RequestInit) => {
    if (path.includes("/submissions/")) return Response.json({ error: "not found" }, { status: 404 });
    ids.push(JSON.parse(String(init.body)).submission_id);
    throw new Error("unconfirmed");
  });
  await assert.rejects(sendConversationMessage("http", { content: "same", attachment_ids: [] }));
  await assert.rejects(sendConversationMessage("http", { attachment_ids: [], content: "same" }));
  await assert.rejects(sendConversationMessage("http", { content: "different", attachment_ids: [] }));
  assert.equal(ids[0], ids[1]);
  assert.notEqual(ids[1], ids[2]);
  assert.match(ids[0], /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
});

test("oversized HTTP fallback attempts stay out of storage and quota failures never block sending", async t => {
  clearMessageSubmissions();
  const originalCrypto = Object.getOwnPropertyDescriptor(globalThis, "crypto")!;
  const originalWindow = Object.getOwnPropertyDescriptor(globalThis, "window");
  const nativeCrypto = crypto;
  Object.defineProperty(globalThis, "crypto", { configurable: true, value: { getRandomValues: nativeCrypto.getRandomValues.bind(nativeCrypto) } });
  let writes = 0;
  Object.defineProperty(globalThis, "window", { configurable: true, value: { sessionStorage: {
    getItem: () => null,
    setItem: () => { writes++; throw new Error("quota exceeded"); },
    removeItem: () => {},
  } } });
  t.after(() => {
    Object.defineProperty(globalThis, "crypto", originalCrypto);
    if (originalWindow) Object.defineProperty(globalThis, "window", originalWindow);
    else Reflect.deleteProperty(globalThis, "window");
  });
  let posts = 0;
  t.mock.method(globalThis, "fetch", async () => { posts++; return Response.json({ run_id: "accepted" }); });
  await sendConversationMessage("large-http", { content: "a".repeat(100_001), attachment_ids: [] });
  assert.equal(writes, 0);
  await sendConversationMessage("quota-http", { content: "hello", attachment_ids: [] });
  assert.equal(writes, 1); assert.equal(posts, 2);
});

test("permission-aware sends never fall back to an API that ignores permissions", async t => {
  clearMessageSubmissions();const paths:string[]=[];
  t.mock.method(globalThis,"fetch",async(path:string)=>{paths.push(path);return Response.json({error:"old API"},{status:404});});
  await assert.rejects(sendConversationMessage("protocol",{content:"private task",attachment_ids:[],permission_mode:"standard"}),/权限服务不可用/);
  assert.deepEqual(paths,["/api/v1/conversations/protocol/permissions/messages"]);
});
test("default permission preserves legacy retry identity but changing the mode does not",async t=>{
  clearMessageSubmissions();const ids:string[]=[];
  t.mock.method(globalThis,"fetch",async(path:string,init?:RequestInit)=>{
    if(path.includes("/submissions/"))return Response.json({error:"missing"},{status:404});
    ids.push(JSON.parse(String(init?.body)).submission_id);return Response.json({error:"lost"},{status:503});
  });
  const payload={content:"same task",attachment_ids:[]};
  await assert.rejects(sendConversationMessage("mode-identity",payload));
  await assert.rejects(sendConversationMessage("mode-identity",{...payload,permission_mode:"standard"}));
  await assert.rejects(sendConversationMessage("mode-identity",{...payload,permission_mode:"full"}));
  assert.equal(ids[0],ids[1]);assert.notEqual(ids[1],ids[2]);
});
