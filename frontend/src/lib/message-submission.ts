import { api, ApiError, type ConversationRunStatus, type MentionInput } from "./api";
import { clearConversationAttachmentUploads } from "./attachment-uploads";
import { randomId } from "./random-id";

type Submission = { id: string; fingerprint: string };
export type MessageSubmission = { content: string; context_ids?: string[]; references?: MentionInput[]; attachment_ids: string[] };
export type AcceptedSubmission = { run_id: string; status?: ConversationRunStatus };
export class SubmissionUnconfirmed extends Error {
  constructor() { super("消息发送结果尚未确认，请先重新同步状态；草稿已保留。"); }
}
const prefix = "lester.submission.v1.";
const maximumStoredAttemptCharacters = 100_000;
const attempts = new Map<string, Submission>();
let sessionEpoch = 0;

function readAttempt(conversationId: string) {
  const cached = attempts.get(conversationId);
  if (cached) return cached;
  try {
    const stored = typeof window !== "undefined" ? JSON.parse(window.sessionStorage.getItem(prefix + conversationId) || "null") : null;
    if (stored && /^[0-9a-f-]{36}$/i.test(stored.id) && typeof stored.fingerprint === "string"
      && (/^sha256:[0-9a-f]{64}$/.test(stored.fingerprint) || (stored.fingerprint.startsWith("json:") && stored.fingerprint.length <= maximumStoredAttemptCharacters))) return stored as Submission;
  } catch { /* Keep in-memory retry protection when browser storage is unavailable. */ }
}

export function clearMessageSubmissions() {
  sessionEpoch++;
  attempts.clear();
  if (typeof window === "undefined") return;
  try { for (const key of Object.keys(window.sessionStorage)) if (key.startsWith(prefix)) window.sessionStorage.removeItem(key); } catch { /* best effort */ }
}

export async function sendConversationMessage(conversationId: string, payload: MessageSubmission): Promise<AcceptedSubmission> {
  const epoch = sessionEpoch;
  const canonical = JSON.stringify(payload, (_key, value) => value && typeof value === "object" && !Array.isArray(value)
    ? Object.fromEntries(Object.keys(value).sort().map(key => [key, value[key]])) : value);
  let fingerprint: string;
  if (crypto.subtle) {
    const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(canonical));
    fingerprint = `sha256:${Array.from(new Uint8Array(digest), value => value.toString(16).padStart(2, "0")).join("")}`;
  } else {
    // Exact equality on HTTP LAN origins; never use a collision-prone hash or
    // mistake this local draft fingerprint for server-side authorization.
    fingerprint = `json:${canonical}`;
  }
  if (epoch !== sessionEpoch) throw new Error("登录已过期，请重新登录");
  const previous = readAttempt(conversationId);
  const attempt = previous?.fingerprint === fingerprint ? previous : { id: randomId(), fingerprint };
  attempts.set(conversationId, attempt);
  while (attempts.size > 50) attempts.delete(attempts.keys().next().value!);
  try {
    if (typeof window !== "undefined") {
      const serialized = JSON.stringify(attempt);
      if (serialized.length <= maximumStoredAttemptCharacters) window.sessionStorage.setItem(prefix + conversationId, serialized);
      else window.sessionStorage.removeItem(prefix + conversationId);
      const keys = Object.keys(window.sessionStorage).filter(key => key.startsWith(prefix));
      for (const key of keys.slice(0, Math.max(0, keys.length - 50))) if (key !== prefix + conversationId) window.sessionStorage.removeItem(key);
    }
  } catch { /* Receipt remains in memory for client navigation and explicit retry. */ }
  let started: AcceptedSubmission;
  try {
    started = await api<AcceptedSubmission>(`/api/v1/conversations/${conversationId}/messages`, { method: "POST", body: JSON.stringify({ ...payload, submission_id: attempt.id }) });
  } catch (error) {
    // Definite validation/access failures cannot have started a run. For a lost
    // response, perform one scoped read, never an automatic mutation retry.
    if (error instanceof ApiError && error.status < 500 && error.status !== 408) throw error;
    try {
      started = await api<AcceptedSubmission>(`/api/v1/conversations/${conversationId}/submissions/${attempt.id}`);
    } catch (lookupError) {
      if (lookupError instanceof ApiError && lookupError.status === 404) throw error;
      throw new SubmissionUnconfirmed();
    }
    if (typeof started.run_id !== "string" || !started.run_id) throw new SubmissionUnconfirmed();
  }
  if (attempts.get(conversationId)?.id === attempt.id) {
    attempts.delete(conversationId);
    try { if (typeof window !== "undefined") window.sessionStorage.removeItem(prefix + conversationId); } catch { /* best effort */ }
  }
  clearConversationAttachmentUploads(conversationId);
  return started;
}
