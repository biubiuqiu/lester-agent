import { api } from "./api";
export type PersonalMemory = { id: string; project_id: string | null; kind: "preference" | "project" | "work"; topic: string; content: string; evidence: string; source_turn_id: string | null; automatic: boolean; deleted: boolean; revision: number; updated_at: string };
export type PersonalMemoryInput = Pick<PersonalMemory, "topic" | "content" | "kind" | "project_id">;
export type PersonalMemoryReceipt = { memory: PersonalMemory; change_id: string };
export const createPersonalMemory = (id: string, input: PersonalMemoryInput, signal?: AbortSignal) => api<PersonalMemoryReceipt>(`/api/v1/personal/memories/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(input), signal });
export const editPersonalMemory = (id: string, revision: number, input: PersonalMemoryInput, signal?: AbortSignal) => api<PersonalMemoryReceipt>(`/api/v1/personal/memories/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify({ revision, content: input.content, details: { topic: input.topic, kind: input.kind, project_id: input.project_id } }), signal });
export const forgetPersonalMemory = (id: string, revision: number, signal?: AbortSignal) => api<PersonalMemoryReceipt>(`/api/v1/personal/memories/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify({ revision, content: "", forget: true }), signal });
export const readPersonalMemory = (id: string, signal?: AbortSignal) => api<PersonalMemory>(`/api/v1/personal/memories/${encodeURIComponent(id)}`, { signal });
export type MemoryChange = { id: string; memory_id: string; topic: string; deleted: boolean; automatic: boolean; undone: boolean };
export type DelegatedTask = { conversation_id: string; project_id: string; title: string; status: "prepared" | "dispatching" | "started" | "queued" | "failed" | "unknown"; run_id: string | null; input_id: string | null };
export type PersonalTurn = { id: string; seq: number; content: string; reply: string; memory_notice?: string; status: "pending" | "completed" | "failed" | "interrupted"; created_at: string; changes: MemoryChange[]; task?: DelegatedTask };
export type PersonalTask = { id: string; project_id: string; project_name: string; title: string; status: string; run_id: string | null; reply: string; updated_at: string; latest_reply_id?: string | null; latest_reply_seq?: number; reply_at?: string | null; unread?: boolean };
export type PersonalInbox = { tasks: PersonalTask[]; total: number; next_cursor: string };
export type PersonalState = { turns: PersonalTurn[]; has_more: boolean; memories: PersonalMemory[]; tasks: PersonalTask[]; auto_memory: boolean; inbox?: PersonalInbox };
export const readPersonal = (before?: number, signal?: AbortSignal) => api<PersonalState>(`/api/v1/personal${before ? `?before=${before}` : ""}`, { signal });
export const readPersonalInbox = (cursor = "", signal?: AbortSignal) => api<PersonalInbox>(`/api/v1/personal/unread${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`, { signal });
export function mergePersonalTurns(previous: PersonalTurn[], incoming: PersonalTurn[]) {
  const items = new Map(previous.map(item => [item.id, item]));
  for (const item of incoming) items.set(item.id, item);
  return [...items.values()].toSorted((a, b) => a.seq - b.seq);
}
