import { clearRunInputs } from "./run-input";
import type { ContextReference } from "./api";
import { clearAttachmentUploads } from "./attachment-uploads";
import { clearMessageSubmissions } from "./message-submission";
import type { QuestionAnswer } from "./user-question";
import { clearProjectViews, resumeProjectViews } from "./project-view-state";
import { isInternalRuntimePath } from "./file-path-policy";

// View layout remains tab-local; account-scoped drafts survive closing tabs/windows.
export type ViewState = {
  recap: { enabled: boolean; seenAt: number; eventId: number; messageId: string };
  detailedProcess: boolean;
  contexts: ContextReference[];
  agentSlug: string;
  executionMode: "execute" | "plan";
  permissionMode: "standard" | "auto" | "full";
  interactionDrafts: Record<string, { answers: Record<string, QuestionAnswer>; feedback: string }>;
  sendNotice: string;
  text: string;
  files: File[];
  missingFiles: string[];
  reference: string | null;
  tabs: string[];
  closedTabs: string[];
  unreadFiles: string[];
  selected: string | null;
  treeOpen: boolean;
  directories: string[];
  modes: Record<string, "preview" | "source">;
  positions: Record<string, { top: number; left: number }>;
  thread: { top: number; following: boolean };
};
const prefix = "lester.view.v1.";
const draftPrefix = "lester.draft.v1.";
const draftFields = ["recap", "detailedProcess", "text", "contexts", "reference", "executionMode", "permissionMode", "agentSlug", "interactionDrafts", "sendNotice", "files", "missingFiles"] as const;
function stored(read: () => string | null) { try { return JSON.parse(read() || "null"); } catch { return null; } }
const cache = new Map<string, ViewState>();
const draftSnapshots = new Map<string, string | null>();
let writeScope: string | null = "";
export function resumeViewState(scope: string) { writeScope = scope; resumeProjectViews(scope); }
const defaults = (): ViewState => ({ recap: { enabled: true, seenAt: 0, eventId: 0, messageId: "" }, detailedProcess: false, contexts: [], agentSlug: "lester", executionMode: "execute", permissionMode: "standard", interactionDrafts: {}, sendNotice: "", text: "", files: [], missingFiles: [], reference: null, tabs: [], closedTabs: [], unreadFiles: [], selected: null, treeOpen: true, directories: [""], modes: {}, positions: {}, thread: { top: 0, following: true } });
export const viewKey = (userId: string, workspaceId: string, conversationId: string) => `${userId}.${workspaceId}.${conversationId}`;

function persistDraft(key: string, value: ViewState) {
  try {
    window.localStorage.setItem(draftPrefix + key, JSON.stringify({ version: 1,
      text: value.text.slice(0, 100_000), recap: value.recap, detailedProcess: value.detailedProcess, contexts: value.contexts, reference: value.reference,
      executionMode: value.executionMode, permissionMode: value.permissionMode, agentSlug: value.agentSlug,
      interactionDrafts: value.interactionDrafts, sendNotice: value.sendNotice,
      attachmentNames: [...value.files.map(file => file.name), ...value.missingFiles],
    }));
  } catch { /* Browser storage restrictions must not break editing or sending. */ }
}

export function readView(key: string): ViewState {
  if (!key || typeof window === "undefined" || writeScope === null || !key.startsWith(writeScope)) return defaults();
  const cached = cache.get(key);
  let draftRaw: string | null = null;
  try { draftRaw = window.localStorage.getItem(draftPrefix + key); } catch { if (cached) return cached; }
  if (cached && draftSnapshots.get(key) === draftRaw) return cached;
  const value = defaults();
  try {
    // Another tab may have changed the draft while this tab kept its layout in
    // memory. Refresh shared fields before applying a reference/recap patch.
    const session = cached ? { ...cached, version: 1, attachmentNames: [...cached.files.map(file => file.name), ...cached.missingFiles] } : stored(() => window.sessionStorage.getItem(prefix + key));
    const draft = stored(() => draftRaw);
    const saved = draft?.version === 1 ? { ...(session?.version === 1 ? session : {}), ...draft } : session;
    if (saved && saved.version === 1) {
      if (saved.recap && typeof saved.recap === "object") value.recap = { enabled: saved.recap.enabled !== false, seenAt: Number.isSafeInteger(saved.recap.seenAt) && saved.recap.seenAt > 0 ? saved.recap.seenAt : 0, eventId: Number.isSafeInteger(saved.recap.eventId) && saved.recap.eventId >= 0 ? saved.recap.eventId : 0, messageId: typeof saved.recap.messageId === "string" ? saved.recap.messageId.slice(0, 100) : "" };
      value.detailedProcess = saved.detailedProcess === true;
      value.contexts = Array.isArray(saved.contexts) ? saved.contexts.filter((v: unknown) => v && typeof v === "object" && "id" in v && typeof v.id === "string" && "title" in v && typeof v.title === "string" && (!("type" in v) || v.type === "context" || v.type === "conversation" || v.type === "skill")).map((item: ContextReference) => ({ id: item.id, title: item.title, ...(item.type ? { type: item.type } : {}) })).slice(0, 8) : [];
      value.agentSlug = typeof saved.agentSlug === "string" && saved.agentSlug.length <= 50 ? saved.agentSlug : "lester";
      value.executionMode = saved.executionMode === "plan" ? "plan" : "execute";
      value.permissionMode = saved.permissionMode === "full" || saved.permissionMode === "auto" ? saved.permissionMode : "standard";
      if (saved.interactionDrafts && typeof saved.interactionDrafts === "object") for (const [id, raw] of Object.entries(saved.interactionDrafts).slice(-8)) {
        if (!/^[0-9a-f-]{36}$/i.test(id) || !raw || typeof raw !== "object") continue;
        const draft = raw as { answers?: Record<string, QuestionAnswer>; feedback?: string };
        const answers: Record<string, QuestionAnswer> = {};
        if (draft.answers && typeof draft.answers === "object") for (const [key, answer] of Object.entries(draft.answers).slice(0, 3)) if (/^[a-zA-Z0-9][a-zA-Z0-9_-]{0,39}$/.test(key) && answer && typeof answer === "object") answers[key] = { option_id: typeof answer.option_id === "string" ? answer.option_id.slice(0, 40) : "", ...(Array.isArray(answer.option_ids)?{option_ids:[...new Set(answer.option_ids.filter((id:unknown)=>typeof id==="string"&&/^[a-zA-Z0-9][a-zA-Z0-9_-]{0,39}$/.test(id)))].slice(0,6)}:{}), text: typeof answer.text === "string" ? answer.text.slice(0, 6000) : "" };
        value.interactionDrafts[id] = { answers, feedback: typeof draft.feedback === "string" ? draft.feedback.slice(0, 6000) : "" };
      }
      value.sendNotice = typeof saved.sendNotice === "string" ? saved.sendNotice.slice(0, 2000) : "";
      value.text = typeof saved.text === "string" ? saved.text.slice(0, 100_000) : "";
      value.reference = typeof saved.reference === "string" ? saved.reference : null;
      // Older clients auto-opened command supervisor state/logs. Do not restore
      // those tabs, unread badges or watches when the conversation is revisited.
      value.tabs = Array.isArray(saved.tabs) ? saved.tabs.filter((p: unknown) => typeof p === "string" && !isInternalRuntimePath(p)).slice(-8) : [];
      value.closedTabs = Array.isArray(saved.closedTabs) ? saved.closedTabs.filter((p: unknown) => typeof p === "string" && !isInternalRuntimePath(p)).slice(-8) : [];
      value.unreadFiles = Array.isArray(saved.unreadFiles) ? saved.unreadFiles.filter((p: unknown) => typeof p === "string" && !isInternalRuntimePath(p)).slice(-200) : [];
      value.selected = value.tabs.includes(saved.selected) ? saved.selected : null;
      value.treeOpen = saved.treeOpen !== false;
      value.directories = Array.isArray(saved.directories) ? saved.directories.filter((p: unknown) => typeof p === "string" && !isInternalRuntimePath(p)).slice(-64) : [""];
      value.missingFiles = Array.isArray(saved.attachmentNames) ? saved.attachmentNames.filter((p: unknown) => typeof p === "string").slice(0, 100) : [];
      for (const file of cached?.files || []) {
        const index = value.missingFiles.indexOf(file.name);
        if (index >= 0) { value.files.push(file); value.missingFiles.splice(index, 1); }
      }
      if (saved.modes && typeof saved.modes === "object") for (const [path, mode] of Object.entries(saved.modes).slice(-16)) {
        if (mode === "preview" || mode === "source") value.modes[path] = mode;
      }
      if (saved.positions && typeof saved.positions === "object") for (const [path, position] of Object.entries(saved.positions).slice(-24)) {
        const p = position as { top?: number; left?: number } | null;
        if (p && Number.isFinite(p.top) && Number.isFinite(p.left)) value.positions[path] = { top: Math.max(0, p.top!), left: Math.max(0, p.left!) };
      }
      if (saved.thread && Number.isFinite(saved.thread.top)) value.thread = { top: Math.max(0, saved.thread.top), following: saved.thread.following !== false };
      if (draft?.version !== 1 && session?.version === 1) persistDraft(key, value);
    }
  } catch { /* Storage may be unavailable; in-memory navigation still works. */ }
  cache.set(key, value);
  draftSnapshots.set(key, draftRaw);
  return value;
}

export function updateView(key: string, patch: Partial<ViewState>) {
  if (!key || typeof window === "undefined" || writeScope === null || !key.startsWith(writeScope)) return;
  const next = { ...readView(key), ...patch };
  cache.delete(key);
  cache.set(key, next);
  while (cache.size > 50) { const old = cache.keys().next().value!; cache.delete(old); draftSnapshots.delete(old); }
  // Layout/scroll updates must not overwrite another tab's more recent draft.
  if (draftFields.some(field => Object.hasOwn(patch, field))) persistDraft(key, next);
  try { draftSnapshots.set(key, window.localStorage.getItem(draftPrefix + key)); } catch { /* Keep the last readable snapshot. */ }
  try {
    const { files, missingFiles, ...serializable } = next;
    window.sessionStorage.setItem(prefix + key, JSON.stringify({ ...serializable, text: next.text.slice(0, 100_000), version: 1, attachmentNames: [...files.map((file) => file.name), ...missingFiles] }));
    const keys = Object.keys(window.sessionStorage).filter((item) => item.startsWith(prefix));
    for (const old of keys.slice(0, Math.max(0, keys.length - 50))) if (old !== prefix + key) window.sessionStorage.removeItem(old);
  } catch { /* Quota/private mode must never break sending or navigation. */ }
}

export function clearViewState() {
  // Unmount/pagehide callbacks after logout must not write the cleared draft back.
  writeScope = null;
  cache.clear();
  draftSnapshots.clear();
  clearProjectViews();
  clearAttachmentUploads();
  clearMessageSubmissions();
  clearRunInputs();
  if (typeof window === "undefined") return;
  try { for (const key of Object.keys(window.sessionStorage)) if (key.startsWith(prefix)) window.sessionStorage.removeItem(key); } catch { /* best effort */ }
  try { for (const key of Object.keys(window.localStorage)) if (key.startsWith(draftPrefix)) window.localStorage.removeItem(key); } catch { /* best effort */ }
}
