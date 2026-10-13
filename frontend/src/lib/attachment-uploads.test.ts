import assert from "node:assert/strict";
import test from "node:test";
import { clearAttachmentUploads, uploadConversationAttachments } from "./attachment-uploads";

test("partial upload failure keeps successful receipts and retries only failed files", async t => {
  clearAttachmentUploads();
  const counts = new Map<string, number>();
  t.mock.method(globalThis, "fetch", async (_path: string, init: RequestInit) => {
    const file = (init.body as FormData).get("file") as File;
    const count = (counts.get(file.name) || 0) + 1;
    counts.set(file.name, count);
    if (file.name === "retry.txt" && count === 1) return Response.json({ error: "upload failed" }, { status: 503 });
    return Response.json({ id: file.name });
  });
  const files = [new File(["a"], "saved.txt"), new File(["b"], "retry.txt")];
  await assert.rejects(uploadConversationAttachments("partial", files), /upload failed/);
  assert.deepEqual((await uploadConversationAttachments("partial", files)).map(item => item.id), ["saved.txt", "retry.txt"]);
  assert.deepEqual(Object.fromEntries(counts), { "saved.txt": 1, "retry.txt": 2 });
});

test("receipts are conversation-scoped and cleared with the browser session", async t => {
  clearAttachmentUploads();
  const paths: string[] = [];
  t.mock.method(globalThis, "fetch", async (path: string) => {
    paths.push(path); return Response.json({ id: `receipt-${paths.length}` });
  });
  const file = new File(["same local file"], "notes.txt");
  await uploadConversationAttachments("a", [file]);
  await uploadConversationAttachments("a", [file]);
  await uploadConversationAttachments("b", [file]);
  clearAttachmentUploads();
  await uploadConversationAttachments("a", [file]);
  assert.deepEqual(paths, ["/api/v1/conversations/a/attachments", "/api/v1/conversations/b/attachments", "/api/v1/conversations/a/attachments"]);
});
