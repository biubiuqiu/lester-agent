import { upload, type Attachment } from "./api";

// File identity survives client navigation with the draft. Keep only in-memory
// upload receipts: refreshing requires reselecting local files, and logout clears them.
const uploads = new Map<string, WeakMap<File, Promise<Attachment>>>();

export function clearAttachmentUploads() { uploads.clear(); }
export function clearConversationAttachmentUploads(conversationId: string) { uploads.delete(conversationId); }

export async function uploadConversationAttachments(conversationId: string, files: File[]): Promise<Attachment[]> {
  let receipts = uploads.get(conversationId);
  if (!receipts) {
    receipts = new WeakMap();
    uploads.set(conversationId, receipts);
    while (uploads.size > 50) uploads.delete(uploads.keys().next().value!);
  }
  const cache = receipts;
  const results = await Promise.allSettled(files.map(file => {
    const existing = cache.get(file);
    if (existing) return existing;
    const body = new FormData();
    body.append("file", file);
    const pending = upload<Attachment>(`/api/v1/conversations/${conversationId}/attachments`, body)
      .catch(error => { cache.delete(file); throw error; });
    cache.set(file, pending);
    return pending;
  }));
  // Wait for every upload before releasing the draft. Successful receipts are
  // reused on explicit retry, including a first-task failure followed by navigation.
  const failure = results.find(result => result.status === "rejected");
  if (failure?.status === "rejected") throw failure.reason;
  return results.map(result => (result as PromiseFulfilledResult<Attachment>).value);
}
