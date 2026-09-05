"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { Download, FileText, FolderOpen, X } from "lucide-react";
import { type FileEntry, readConversationFileBytes } from "@/lib/api";
import { listDirectory, relativeFilePath, scanFiles, wasPathCovered, type FileState } from "@/lib/file-inventory";
import { readView, updateView } from "@/lib/conversation-view-state";
import type { RunEvent } from "./tool-timeline";

type Change = { path: string; kind: "added" | "updated" | "deleted" };
type FileWorkspaceValue = FileState & {
  conversationId: string;
  storageKey: string;
  loading: boolean;
  changes: Change[];
  tabs: FileEntry[];
  selected: FileEntry | null;
  reference: string | null;
  expanded: boolean;
  panelOpen: boolean;
  panelTab: "files" | "terminal" | "skills";
  setPanelTab: (tab: "files" | "terminal" | "skills") => void;
  open: (file: FileEntry) => void;
  close: (path: string) => void;
  refresh: () => void;
  loadDirectory: (path: string) => Promise<void>;
  setExpanded: (value: boolean) => void;
  setPanelOpen: (value: boolean) => void;
  setReference: (path: string | null) => void;
};

const Context = createContext<FileWorkspaceValue | null>(null);
const emptyState: FileState = { directories: {}, files: [], signature: "", limited: false, error: "" };
const refreshEvents = new Set(["FILE_UPDATED", "TOOL_COMPLETED", "TOOL_FAILED", "RUN_COMPLETED", "RUN_FAILED", "RUN_CANCELLED"]);

export function useFileWorkspace() {
  const context = useContext(Context);
  if (!context) throw new Error("File workspace is unavailable");
  return context;
}

export function FileWorkspaceProvider({ conversationId, storageKey, events, runId, running, children }: {
  conversationId?: string; storageKey: string; events: RunEvent[]; runId?: string; running: boolean; children: React.ReactNode;
}) {
  const [state, setState] = useState<FileState>(emptyState);
  const [loading, setLoading] = useState(true);
  const [tabs, setTabs] = useState<FileEntry[]>(() => readView(storageKey).tabs.map((path) => ({ path, name: path.split("/").at(-1) ?? path, is_dir: false, size: 0, modified_at: "" })));
  const [selectedPath, setSelectedPath] = useState<string | null>(() => readView(storageKey).selected);
  const [reference, setReferenceState] = useState<string | null>(() => readView(storageKey).reference);
  const setReference = useCallback((value: string | null) => { updateView(storageKey, { reference: value }); setReferenceState(value); }, [storageKey]);
  const [expanded, setExpanded] = useState(false);
  const [panelOpen, setPanelOpen] = useState(false);
  const [panelTab, setPanelTab] = useState<"files" | "terminal" | "skills">("files");
  const [observed, setObserved] = useState<{ runId?: string; changes: Change[] }>({ changes: [] });
  const refreshRef = useRef<() => void>(() => {});
  const watchedDirectories = useRef(new Set<string>([...readView(storageKey).directories, ...readView(storageKey).tabs.map((path) => path.split("/").slice(0, -1).join("/"))]));
  const runRef = useRef(runId);
  const runningRef = useRef(running);
  useEffect(() => { runRef.current = runId; runningRef.current = running; }, [runId, running]);
  const revision = events.findLast((event) => refreshEvents.has(event.type))?.id ?? 0;

  useEffect(() => {
    if (!conversationId) return;
    const controller = new AbortController();
    let stopped = false;
    let pending = false;
    let queued = false;
    let previous: FileState | null = null;
    let timer: ReturnType<typeof setTimeout>;
    const scan = async () => {
      if (stopped || document.hidden) return;
      if (pending) { queued = true; return; }
      clearTimeout(timer);
      pending = true;
      const owner = runRef.current;
      try {
        const next = await scanFiles(conversationId, controller.signal, [...watchedDirectories.current]);
        if (stopped) return;
        if (previous && owner === runRef.current) {
          const old = new Map(previous.files.map((file) => [file.path, file]));
          const current = new Map(next.files.map((file) => [file.path, file]));
          const changes: Change[] = [];
          for (const [path, file] of current) {
            const before = old.get(path);
            if (!before && !previous.limited && !path.startsWith(".agent/") && wasPathCovered(path, previous.directories)) changes.push({ path, kind: "added" });
            else if (before && !path.startsWith(".agent/") && (file.size !== before.size || file.modified_at !== before.modified_at)) changes.push({ path, kind: "updated" });
          }
          // A partial/failed listing cannot prove that a file was deleted.
          if (!next.limited && !previous.limited) for (const path of old.keys()) {
            if (!current.has(path) && !path.startsWith(".agent/")) changes.push({ path, kind: "deleted" });
          }
          if (changes.length) setObserved((value) => {
            const merged = new Map((value.runId === owner ? value.changes : []).map((item) => [item.path, item]));
            for (const change of changes) {
              const before = merged.get(change.path);
              merged.set(change.path, before?.kind === "added" && change.kind === "updated" ? before : change);
            }
            return { runId: owner, changes: [...merged.values()].slice(-200) };
          });
        }
        previous = next;
        setState((value) => value.signature === next.signature && value.error === next.error && value.limited === next.limited ? value : next);
      } catch (reason) {
        if (!stopped) setState((value) => ({ ...value, error: reason instanceof Error ? reason.message : "文件同步失败" }));
      } finally {
        pending = false;
        if (!stopped) {
          setLoading(false);
          timer = setTimeout(() => {
            void scan();
          }, queued ? 350 : runningRef.current ? 5000 : 15000);
          queued = false;
        }
      }
    };
    refreshRef.current = () => { void scan(); };
    const onVisible = () => { if (!document.hidden) void scan(); };
    document.addEventListener("visibilitychange", onVisible);
    void scan();
    return () => { stopped = true; controller.abort(); clearTimeout(timer); document.removeEventListener("visibilitychange", onVisible); refreshRef.current = () => {}; };
  }, [conversationId]);

  useEffect(() => {
    if (!revision) return;
    const timer = setTimeout(() => refreshRef.current(), 400);
    return () => clearTimeout(timer);
  }, [revision]);

  const refresh = useCallback(() => refreshRef.current(), []);
  const loadDirectory = useCallback(async (path: string) => {
    if (!conversationId) return;
    watchedDirectories.current.add(path);
    try {
      const entries = await listDirectory(conversationId, path);
      setState((value) => ({ ...value, directories: { ...value.directories, [path]: { entries, error: "" } } }));
    } catch (reason) {
      setState((value) => ({ ...value, directories: { ...value.directories, [path]: { entries: [], error: reason instanceof Error ? reason.message : "目录加载失败" } } }));
    }
  }, [conversationId]);
  const open = useCallback((file: FileEntry) => {
    const paths = [...readView(storageKey).tabs.filter((path) => path !== file.path), file.path].slice(-8);
    updateView(storageKey, { tabs: paths, selected: file.path });
    setTabs((value) => value.some((item) => item.path === file.path) ? value : [...value, file].slice(-8));
    setSelectedPath(file.path); setPanelTab("files"); setPanelOpen(true);
  }, [storageKey]);
  const close = (path: string) => {
    const next = tabs.filter((file) => file.path !== path);
    setTabs(next);
    updateView(storageKey, { tabs: next.map((file) => file.path), selected: selectedPath === path ? next.at(-1)?.path ?? null : selectedPath });
    if (selectedPath === path) setSelectedPath(next.at(-1)?.path ?? null);
  };
  const changes = useMemo(() => {
    const entries = new Map<string, Change>();
    for (const event of events) {
      if (event.run_id !== runId || event.type !== "FILE_UPDATED") continue;
      const path = relativeFilePath(conversationId ?? "", String(event.payload.path ?? ""));
      if (path && !path.startsWith(".agent/")) entries.set(path, { path, kind: "updated" });
    }
    if (observed.runId === runId) for (const change of observed.changes) entries.set(change.path, change);
    return [...entries.values()].slice(-200);
  }, [events, runId, observed, conversationId]);
  const selected = state.files.find((file) => file.path === selectedPath)
    ?? Object.values(state.directories).flatMap((directory) => directory.entries).find((file) => !file.is_dir && file.path === selectedPath)
    ?? null;
  return <Context.Provider value={{ ...state, storageKey, conversationId: conversationId ?? "", loading, tabs, selected, changes, reference, expanded, panelOpen, panelTab, setPanelTab, open, close, refresh, loadDirectory, setExpanded, setPanelOpen, setReference }}>{children}</Context.Provider>;
}

export function OpenFilesButton() {
  const { setPanelOpen, setPanelTab } = useFileWorkspace();
  return <button type="button" className="open-files-button" onClick={() => { setPanelTab("files"); setPanelOpen(true); }}><FolderOpen />文件</button>;
}

export function FileReferenceChip() {
  const { reference, setReference } = useFileWorkspace();
  return reference ? <div className="file-reference"><FileText /><span title={reference}>{reference}</span><button type="button" onClick={() => setReference(null)} aria-label="移除文件引用"><X /></button></div> : null;
}

export function ArtifactCards() {
  const { files, changes, open, conversationId } = useFileWorkspace();
  const verified = changes.filter((change) => change.kind !== "deleted").flatMap((change) => {
    const file = files.find((item) => item.path === change.path);
    return file ? [file] : [];
  }).slice(0, 12);
  if (!verified.length) return null;
  return <section className="artifact-cards" aria-label="任务文件"><small title="本轮任务涉及的文件，打开查看当前版本">相关文件</small>{verified.map((file) => <div className="artifact-card" key={file.path}>
    <button type="button" onClick={() => open(file)} title={file.path} aria-label={`查看 ${file.name}`}><FileText /><span><strong>{file.name}</strong><small>{file.name.includes(".") ? file.name.split(".").at(-1)?.toUpperCase() : "文件"} · {file.size < 1024 ? `${file.size} B` : file.size < 1048576 ? `${(file.size / 1024).toFixed(1)} KB` : `${(file.size / 1048576).toFixed(1)} MB`}</small></span><span>查看</span></button>
    <FileDownload conversationId={conversationId} file={file} />
  </div>)}</section>;
}

export function FileDownload({ conversationId, file }: { conversationId: string; file: FileEntry }) {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  async function download() {
    setPending(true); setError("");
    try {
      const bytes = await readConversationFileBytes(conversationId, file.path);
      const url = URL.createObjectURL(new Blob([new Uint8Array(bytes)]));
      const link = document.createElement("a"); link.href = url; link.download = file.name; link.hidden = true;
      document.body.append(link); link.click(); link.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (reason) { setError(reason instanceof Error ? reason.message : "下载失败"); }
    finally { setPending(false); }
  }
  return <span className="file-download"><button type="button" disabled={pending} onClick={() => void download()} aria-label={`下载 ${file.name}`} title="下载"><Download /></button>{error ? <span role="alert">{error}</span> : null}</span>;
}
