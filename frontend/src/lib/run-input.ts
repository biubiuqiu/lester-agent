import { readAttemptStorage, writeAttemptStorage, removeAttemptStorage, clearAttemptStorage } from "./attempt-storage";
import { api, ApiError, type MentionInput } from "./api";
import { randomId } from "./random-id";
export type RunInputRequest = { id: string; run_id: string; kind: "adjust" | "next"; content: string; attachment_ids?: string[]; context_ids?: string[]; references?: MentionInput[]; execution_mode?: "plan"; permission_mode?: "standard" | "auto" | "full" };
export type RunInput = { id: string; after_run_id: string; kind: "adjust" | "next"; status: "pending" | "queued" | "applied" | "held" | "started" | "cancelled"; revision: number; content: string; request?: RunInputRequest; accepted_order: number };
const prefix = "lester.run-input.v1.";
const attempts = new Map<string, RunInputRequest>();
let epoch = 0;
export class RunInputUnconfirmed extends Error {
  constructor() { super("任务输入结果待确认；草稿已保留，请检查状态后再操作。"); }
}
export function sameRunInput(a: RunInputRequest, b: RunInputRequest): boolean {
  const normalize = (value: RunInputRequest) => JSON.stringify({ id: value.id, run_id: value.run_id, kind: value.kind, content: value.content, attachment_ids: value.attachment_ids || [], context_ids: value.context_ids || [], references: (value.references || []).map(item => ({ type: item.type, id: item.id })), execution_mode: value.execution_mode || "", permission_mode: value.permission_mode || "standard" });
  return normalize(a) === normalize(b);
}
function readAttempt(key: string): RunInputRequest | undefined {
  const cached = attempts.get(key);
  if (cached) return cached;
  try {
    const value = JSON.parse(readAttemptStorage(prefix + key) || "null");
    if (value && typeof value.id === "string" && /^[0-9a-f-]{36}$/i.test(value.id) && typeof value.content === "string" && (value.kind === "adjust" || value.kind === "next")) return value;
  } catch { /* In-memory receipt tracking remains available. */ }
}
function forget(key: string) {
  attempts.delete(key);
  removeAttemptStorage(prefix + key);
}
export function clearRunInputs() {
  epoch++;
  attempts.clear();
  clearAttemptStorage(prefix);
}
export async function acceptRunInput(conversationId: string, storageKey: string, payload: Omit<RunInputRequest, "id">): Promise<RunInput> {
  const startedEpoch = epoch;
  const previous = readAttempt(storageKey);
  const proposed = { ...payload, id: previous?.id || randomId() };
  const request = previous && sameRunInput(previous, proposed) ? previous : proposed;
  const path = `/api/v1/conversations/${conversationId}/permissions/inputs`;
  // A changed draft cannot replace a request whose mutation result is unknown.
  if (previous && !sameRunInput(previous, request)) {
    try { const receipt = await api<RunInput>(`${path}/${previous.id}`); if (!receipt.request || !sameRunInput(receipt.request, previous)) throw new RunInputUnconfirmed(); if (epoch === startedEpoch) forget(storageKey); if (sameRunInput(previous, { ...request, run_id: previous.run_id })) return receipt; } catch { throw new RunInputUnconfirmed(); }
    throw new RunInputUnconfirmed();
  }
  attempts.set(storageKey, request);
  writeAttemptStorage(prefix + storageKey, JSON.stringify(request));
  let receipt: RunInput;
  try {
    receipt = await api<RunInput>(path, { method: "POST", body: JSON.stringify(request) });
  } catch (error) {
    if (error instanceof ApiError && error.status < 500 && error.status !== 408) {
      if (epoch === startedEpoch) forget(storageKey);
      throw error;
    }
    try { receipt = await api<RunInput>(`${path}/${request.id}`); } catch { throw new RunInputUnconfirmed(); }
  }
  if (receipt.id !== request.id || !receipt.request || !sameRunInput(receipt.request, request)) throw new RunInputUnconfirmed();
  if (epoch === startedEpoch) forget(storageKey);
  return receipt;
}

// Resolve a preserved unknown input before an idle composer starts a fresh run.
// An accepted/held input is never silently converted into a second task.
export async function confirmPendingRunInput(conversationId: string, storageKey: string, payload: Omit<RunInputRequest, "id" | "run_id" | "kind">): Promise<RunInput | null> {
  const previous = readAttempt(storageKey);
  if (!previous) return null;
  const startedEpoch = epoch;
  let receipt: RunInput;
  try { receipt = await api<RunInput>(`/api/v1/conversations/${conversationId}/permissions/inputs/${previous.id}`); } catch { throw new RunInputUnconfirmed(); }
  if (!receipt.request || !sameRunInput(receipt.request, previous)) throw new RunInputUnconfirmed();
  if (epoch === startedEpoch) forget(storageKey);
  const current = { ...payload, id: previous.id, run_id: previous.run_id, kind: previous.kind };
  if (previous.kind === "adjust") { delete current.execution_mode; delete current.permission_mode; }
  if (!sameRunInput(previous, current)) throw new RunInputUnconfirmed();
  return receipt;
}
