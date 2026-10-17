import type { ProjectResultPage } from "./project-workbench";

export type ProjectView = {
  tab: "results" | "conversations"; search: string; kind: string;
  conversationLimit: number; scroll: number;
  feed: { key: string; data: ProjectResultPage } | null; paged: boolean;
};
const prefix = "lester.project-view.v1.";
const views = new Map<string, ProjectView>();
let scope: string | null = "";
export function resumeProjectViews(value: string) { scope = value; }
export function readProjectView(key: string): ProjectView {
  const value: ProjectView = { tab: "results", search: "", kind: "", conversationLimit: 24, scroll: 0, feed: null, paged: false };
  if (scope === null || !key.startsWith(scope)) return value;
  const cached = views.get(key);
  if (cached) return cached;
  try {
    const saved = JSON.parse(window.sessionStorage.getItem(prefix + key) || "null");
    if (saved?.tab === "conversations") value.tab = "conversations";
    if (typeof saved?.search === "string") value.search = saved.search.slice(0, 120);
    if (["html", "markdown"].includes(saved?.kind)) value.kind = saved.kind;
    if (Number.isSafeInteger(saved?.conversationLimit)) value.conversationLimit = Math.max(24, Math.min(240, saved.conversationLimit));
    if (Number.isFinite(saved?.scroll)) value.scroll = Math.max(0, saved.scroll);
  } catch { /* Navigation still works when browser storage is unavailable. */ }
  return value;
}
export function saveProjectView(key: string, value: ProjectView, persist = false) {
  if (scope === null || !key.startsWith(scope)) return;
  // Retain loaded pages for ordinary back navigation without persisting server
  // records to disk. Refresh remains explicit after paging, as in the catalogue.
  views.delete(key);
  views.set(key, value.feed && value.feed.data.deliverables.length > 240 ? { ...value, feed: null, paged: false } : value);
  while (views.size > 20) views.delete(views.keys().next().value!);
  if (!persist) return;
  try {
    const layout = { tab: value.tab, search: value.search, kind: value.kind, conversationLimit: value.conversationLimit, scroll: value.scroll };
    window.sessionStorage.setItem(prefix + key, JSON.stringify(layout));
    const keys = Object.keys(window.sessionStorage).filter(item => item.startsWith(prefix));
    for (const old of keys.slice(0, Math.max(0, keys.length - 20))) if (old !== prefix + key) window.sessionStorage.removeItem(old);
  } catch { /* best effort */ }
}
export function clearProjectViews() {
  scope = null;
  views.clear();
  try { for (const key of Object.keys(window.sessionStorage)) if (key.startsWith(prefix)) window.sessionStorage.removeItem(key); } catch { /* best effort */ }
}
