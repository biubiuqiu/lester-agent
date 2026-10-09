import type { FileEntry } from "./api";
import { relativeFilePath } from "./file-inventory";

type FileEvent = { id: number; conversation_id?: string; type: string; payload: Record<string, unknown> };
const internalDirectories = new Set([".agent", ".git", "node_modules", ".next", ".venv", "venv", "__pycache__", ".cache"]);
const version = (file: FileEntry) => `${file.modified_at}:${file.size}`;

// Live notifications can precede inventory visibility. Retain bounded requests,
// but never invent file metadata or reopen a tab from history/SSE replay.
export class AutoOpenFiles {
  private cursor: number;
  private pending = new Map<string, number>();
  private openedVersions = new Map<string, string>();

  constructor(private conversationId: string, initialEvents: FileEvent[] = []) {
    this.cursor = Math.max(0, ...initialEvents.map((event) => event.id));
  }

  receive(events: FileEvent[], now = Date.now()) {
    const paths: string[] = [];
    for (const event of events.toSorted((a, b) => a.id - b.id)) {
      if (event.id <= this.cursor) continue;
      this.cursor = event.id;
      if (event.conversation_id !== this.conversationId) continue;
      const raw = event.type === "FILE_UPDATED" ? event.payload.path : event.type === "DELIVERABLE_REGISTERED" ? event.payload.entry_path : undefined;
      if (typeof raw !== "string") continue;
      const path = this.enqueue(raw, now);
      if (path) paths.push(path);
    }
    return paths;
  }

  changed(files: FileEntry[], now = Date.now()) {
    for (const file of files) if (!file.is_dir && this.openedVersions.get(file.path) !== version(file) && !this.pending.has(file.path)) this.enqueue(file.path, now);
  }

  private enqueue(raw: string, now: number) {
    const path = relativeFilePath(this.conversationId, raw);
    if (!path || path.length > 1000 || path.split("/").some((part) => internalDirectories.has(part) || part === "" || part === ".") || /[\u0000-\u001f]/.test(path)) return "";
    this.pending.delete(path);
    this.pending.set(path, now + 60_000);
    while (this.pending.size > 64) this.pending.delete(this.pending.keys().next().value!);
    return path;
  }

  take(files: FileEntry[], now = Date.now()) {
    const inventory = new Map(files.filter((file) => !file.is_dir).map((file) => [file.path, file]));
    const ready: FileEntry[] = [];
    for (const [path, expires] of this.pending) {
      if (expires <= now) { this.pending.delete(path); continue; }
      const file = inventory.get(path);
      if (!file) continue;
      ready.push(file);
      this.pending.delete(path);
      this.openedVersions.delete(path);
      this.openedVersions.set(path, version(file));
    }
    while (this.openedVersions.size > 2000) this.openedVersions.delete(this.openedVersions.keys().next().value!);
    return ready;
  }
}

export function mergeFileTabs(tabs: FileEntry[], opened: FileEntry[]) {
  const next = new Map(tabs.map((file) => [file.path, file]));
  for (const file of opened) next.set(file.path, file);
  // Reopening an existing tab keeps its position, and it must survive the cap.
  const selected = opened.at(-1)?.path;
  while (next.size > 8) {
    const oldest = [...next.keys()].find((path) => path !== selected)!;
    next.delete(oldest);
  }
  return [...next.values()];
}
