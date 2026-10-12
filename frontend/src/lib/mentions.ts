import { api, type ContextEntry, type ContextReference, type MentionItem, type MentionInput, type ReferenceType } from "./api";

export const referenceType = (reference: ContextReference): ReferenceType => reference.type || "context";
export const referenceKey = (reference: ContextReference) => `${referenceType(reference)}:${reference.id}`;
export function mentionPayload(references: ContextReference[]) {
  const context_ids = references.filter(item => referenceType(item) === "context").map(item => item.id);
  const selected: MentionInput[] = references.filter(item => referenceType(item) !== "context").map(item => ({ type: referenceType(item), id: item.id }));
  return { context_ids, ...(selected.length ? { references: selected } : {}) };
}

export function searchMentions(kind: ReferenceType | "all", query: string, conversationId: string | undefined, signal: AbortSignal) {
  const params = new URLSearchParams({ type: kind, q: query });
  if (conversationId) params.set("exclude_conversation", conversationId);
  return api<{ items: MentionItem[] }>(`/api/v1/references?${params}`, { signal });
}

export type MentionPreview = { title: string; description: string; version?: string; content?: string; slug?: string; source?: string; messages?: { role: string; content: string }[]; hasMore?: boolean };
export async function inspectMention(reference: ContextReference, signal: AbortSignal): Promise<MentionPreview> {
  const id = encodeURIComponent(reference.id);
  switch (referenceType(reference)) {
    case "context": {
      const item = await api<ContextEntry>(`/api/v1/contexts/${id}`, { signal });
      return { title: item.title, description: item.description, content: item.content, version: String(item.version) };
    }
    case "conversation": {
      const item = await api<{ conversation: { title: string; project: string }; messages: { role: string; content: string }[]; has_more: boolean }>(`/api/v1/references/conversations/${id}`, { signal });
      return { title: item.conversation.title, description: item.conversation.project, messages: item.messages, hasMore: item.has_more };
    }
    case "skill": {
      const item = await api<MentionItem>(`/api/v1/references/skills/${id}`, { signal });
      return { title: item.title, description: item.description, version: item.version, slug: item.slug, source: item.source };
    }
  }
}

/** Optional type words select a category; free text still searches all kinds. */
export function parseMentionQuery(value: string): { kind?: ReferenceType; query: string } {
  const match = /^(会话|conversation|chat|上下文|context|skill|技能)(?:\s+|:|：|$)(.*)$/i.exec(value);
  if (!match) return { query: value.trim().slice(0, 80) };
  const kind = /会话|conversation|chat/i.test(match[1]) ? "conversation" : /上下文|context/i.test(match[1]) ? "context" : "skill";
  return { kind, query: match[2].trim().slice(0, 80) };
}
