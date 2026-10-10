"use client";
import { T, useT } from "@/components/i18n";


import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { Download, FileText, FolderOpen, X } from "lucide-react";
import { api, type Deliverable, type FileEntry, readConversationFileBytes } from "@/lib/api";
import { listDirectory, relativeFilePath, scanFiles, wasPathCovered, type FileState } from "@/lib/file-inventory";
import { readView, updateView } from "@/lib/conversation-view-state";
import { artifactFiles } from "@/lib/artifact-files";
import { deliverableKind } from "@/lib/deliverables";
import { AutoOpenFiles, mergeFileTabs } from "@/lib/auto-open-files";
import type { RunEvent } from "./tool-timeline";

type Change = { path: string; kind: "added" | "updated" | "deleted" };
type FileWorkspaceValue = FileState & {
  conversationId: string;
  storageKey: string;
  loading: boolean;
  running: boolean;
  runOutcome?: "failed" | "cancelled";
  deliverables: Deliverable[];
  deliverablesError: string;
  modes: Record<string, "preview" | "source">;
  fileRevisions: Record<string, number>;
  setPreviewMode: (path: string, mode: "preview" | "source") => void;
  changes: Change[];
  tabs: FileEntry[];
  selected: FileEntry | null;
  reference: string | null;
  referenceRevision: number;
  expanded: boolean;
  treeOpen: boolean;
  setTreeOpen: (value: boolean) => void;
  panelOpen: boolean;
  panelTab: "files" | "results" | "terminal" | "skills" | "agent";
  setPanelTab: (tab: "files" | "results" | "terminal" | "skills" | "agent") => void;
  open: (file: FileEntry, mode?: "preview") => void;
  close: (path: string) => void;
  refresh: () => void;
  loadDirectory: (path: string) => Promise<void>;
  setExpanded: (value: boolean) => void;
  setPanelOpen: (value: boolean) => void;
  setReference: (path: string | null) => void;
};

const Context = createContext<FileWorkspaceValue | null>(null);
const emptyState: FileState = { directories: {}, files: [], signature: "", limited: false, error: "" };
const refreshEvents = new Set(["FILE_UPDATED", "DELIVERABLE_REGISTERED", "TOOL_COMPLETED", "TOOL_FAILED", "RUN_COMPLETED", "RUN_FAILED", "RUN_CANCELLED"]);

export function useFileWorkspace() {
  const context = useContext(Context);
  if (!context) throw new Error("File workspace is unavailable");
  return context;
}

export function useOptionalFileWorkspace() {
  return useContext(Context);
}

export function FileWorkspaceProvider({ conversationId, storageKey, events, liveFileEvents, runId, running, runOutcome, children }: {
  conversationId?: string; storageKey: string; events: RunEvent[]; liveFileEvents: RunEvent[]; runId?: string; running: boolean; runOutcome?: "failed" | "cancelled"; children: React.ReactNode;
}) {

  const [requestedPreview] = useState(() => {
    if (!conversationId || typeof window === "undefined" || window.location.pathname !== `/app/c/${conversationId}`) return "";
    const path = new URLSearchParams(window.location.search).get("preview") || "";
    return path.length <= 1000 ? relativeFilePath(conversationId, path) : "";
  });
  const [state, setState] = useState<FileState>(emptyState);
  const [loading, setLoading] = useState(true);
  const [deliverables, setDeliverables] = useState<Deliverable[]>([]);
  const [deliverablesError, setDeliverablesError] = useState("");
  const [tabs, setTabs] = useState<FileEntry[]>(() => [...new Set([...readView(storageKey).tabs, ...(requestedPreview ? [requestedPreview] : [])])].slice(-8).map((path) => ({ path, name: path.split("/").at(-1) ?? path, is_dir: false, size: 0, modified_at: "" })));
  const tabsRef = useRef(tabs);
  const autoOpen = useRef<AutoOpenFiles | null>(null);
  if (!autoOpen.current) autoOpen.current = new AutoOpenFiles(conversationId ?? "");
  const liveScanCursor = useRef(0);
  const pendingRunScans = useRef(new Map<string, number>());
  const [fileRevisions, setFileRevisions] = useState<Record<string, number>>({});
  const [selectedPath, setSelectedPath] = useState<string | null>(() => requestedPreview || readView(storageKey).selected);
  const [reference, setReferenceState] = useState<string | null>(() => readView(storageKey).reference);
  const [referenceRevision, setReferenceRevision] = useState(0);
  const setReference = useCallback((value: string | null) => { updateView(storageKey, { reference: value }); setReferenceState(value); setReferenceRevision((revision) => revision + 1); }, [storageKey]);
  const [expanded, setExpanded] = useState(Boolean(requestedPreview));
  const [treeOpen, setTreeOpenState] = useState(() => requestedPreview ? false : readView(storageKey).treeOpen);
  const setTreeOpen = useCallback((value: boolean) => { updateView(storageKey, { treeOpen: value }); setTreeOpenState(value); }, [storageKey]);
  const [panelOpen, setPanelOpen] = useState(Boolean(requestedPreview));
  const [panelTab, setPanelTab] = useState<"files" | "results" | "terminal" | "skills" | "agent">("files");
  const [modes, setModes] = useState<Record<string, "preview" | "source">>(() => ({ ...readView(storageKey).modes, ...(requestedPreview ? { [requestedPreview]: "preview" } : {}) }));
  const setPreviewMode = useCallback((path: string, mode: "preview" | "source") => {
    const next = Object.fromEntries(Object.entries({ ...readView(storageKey).modes, [path]: mode }).slice(-16));
    updateView(storageKey, { modes: next }); setModes(next);
  }, [storageKey]);
  const [observed, setObserved] = useState<{ runId?: string; changes: Change[] }>({ changes: [] });
  const refreshRef = useRef<() => void>(() => {});
  const watchedDirectories = useRef(new Set<string>([...readView(storageKey).directories, ...tabs.map((file) => file.path.split("/").slice(0, -1).join("/"))].slice(-63)));
  const watchDirectory = useCallback((path: string) => {
    const watched = watchedDirectories.current;
    watched.delete(path); watched.add(path);
    // scanFiles always adds the root; retain space for each recent target.
    while (watched.size > 63) watched.delete(watched.keys().next().value!);
  }, []);
  const runRef = useRef(runId);
  const runningRef = useRef(running);
  useEffect(() => { runRef.current = runId; runningRef.current = running; }, [runId, running]);
  const revision = events.findLast((event) => refreshEvents.has(event.type))?.id ?? 0;
  const openBatch = useCallback((files: FileEntry[]) => {
    const selected = files.at(-1);
    if (!selected) return;
    const next = mergeFileTabs(tabsRef.current, files);
    tabsRef.current = next;
    for (const file of files) watchDirectory(file.path.split("/").slice(0, -1).join("/"));
    updateView(storageKey, { tabs: next.map((file) => file.path), selected: selected.path });
    setTabs(next); setSelectedPath(selected.path); setPanelTab("files"); setPanelOpen(true);
  }, [storageKey, watchDirectory]);

  useEffect(() => {
    const paths = autoOpen.current!.receive(liveFileEvents);
    let needsScan = paths.length > 0;
    for (const event of liveFileEvents) {
      if (event.id <= liveScanCursor.current) continue;
      liveScanCursor.current = event.id;
      if (event.conversation_id !== conversationId || !event.run_id || !refreshEvents.has(event.type)) continue;
      pendingRunScans.current.set(event.run_id, event.id);
      while (pendingRunScans.current.size > 4) pendingRunScans.current.delete(pendingRunScans.current.keys().next().value!);
      needsScan = true;
    }
    if (!needsScan) return;
    // Explicit writes can target deep directories beyond the normal scan.
    for (const path of paths) watchDirectory(path.split("/").slice(0, -1).join("/"));
    const timer = setTimeout(() => refreshRef.current(), 400);
    return () => clearTimeout(timer);
  }, [liveFileEvents, conversationId, watchDirectory]);

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
      const wasRunning = runningRef.current;
      const pendingEvent = owner ? pendingRunScans.current.get(owner) : undefined;
      try {
        const [inventory, registered] = await Promise.allSettled([
          scanFiles(conversationId, controller.signal, [...watchedDirectories.current]),
          api<{ deliverables: Deliverable[] }>(`/api/v1/conversations/${conversationId}/deliverables`, { signal: controller.signal }),
        ]);
        if (stopped) return;
        if (registered.status === "fulfilled") {
          if (!Array.isArray(registered.value.deliverables)) setDeliverablesError("成果记录返回格式不正确");
          else {
            setDeliverables((value) => JSON.stringify(value) === JSON.stringify(registered.value.deliverables) ? value : registered.value.deliverables);
            setDeliverablesError("");
          }
        } else setDeliverablesError(registered.reason instanceof Error ? registered.reason.message : "成果记录加载失败");
        if (inventory.status === "rejected") throw inventory.reason;
        const next = inventory.value;
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
          // Shell-created outputs have no FILE_UPDATED notification. Open only
          // proved changes observed during this run, never the initial listing.
          if (owner && (wasRunning || pendingEvent !== undefined)) {
            const changedPaths = new Set(changes.filter((change) => change.kind !== "deleted").map((change) => change.path));
            autoOpen.current!.changed(next.files.filter((file) => changedPaths.has(file.path)));
          }
        }
        if (owner && pendingEvent !== undefined && pendingRunScans.current.get(owner) === pendingEvent) pendingRunScans.current.delete(owner);
        const ready = autoOpen.current!.take(next.files);
        if (ready.length) {
          openBatch(ready);
          // An explicit edit can keep both size and timestamp unchanged.
          setFileRevisions((value) => Object.fromEntries(Object.entries({ ...value, ...Object.fromEntries(ready.map((file) => [file.path, (value[file.path] ?? 0) + 1])) }).slice(-2000)));
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
  }, [conversationId, openBatch]);

  useEffect(() => {
    if (!revision) return;
    const timer = setTimeout(() => refreshRef.current(), 400);
    return () => clearTimeout(timer);
  }, [revision]);

  const refresh = useCallback(() => refreshRef.current(), []);
  const loadDirectory = useCallback(async (path: string) => {
    if (!conversationId) return;
    watchDirectory(path);
    try {
      const entries = await listDirectory(conversationId, path);
      setState((value) => ({ ...value, directories: { ...value.directories, [path]: { entries, error: "" } } }));
    } catch (reason) {
      setState((value) => ({ ...value, directories: { ...value.directories, [path]: { entries: [], error: reason instanceof Error ? reason.message : "目录加载失败" } } }));
    }
  }, [conversationId, watchDirectory]);
  const open = useCallback((file: FileEntry, mode?: "preview") => {
    if (mode) { setPreviewMode(file.path, mode); setTreeOpen(false); }
    openBatch([file]);
  }, [openBatch, setPreviewMode, setTreeOpen]);
  const close = (path: string) => {
    const index = tabsRef.current.findIndex((file) => file.path === path);
    const next = tabsRef.current.filter((file) => file.path !== path);
    const replacement = next[Math.min(index, next.length - 1)]?.path ?? null;
    tabsRef.current = next;
    setTabs(next);
    updateView(storageKey, { tabs: next.map((file) => file.path), selected: selectedPath === path ? replacement : selectedPath });
    if (selectedPath === path) setSelectedPath(replacement);
    if (!next.length) setTreeOpen(true);
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
  return <Context.Provider value={{ ...state, storageKey, conversationId: conversationId ?? "", loading, running, runOutcome, deliverables, deliverablesError, modes, fileRevisions, setPreviewMode, tabs, selected, changes, reference, referenceRevision, expanded, treeOpen, setTreeOpen, panelOpen, panelTab, setPanelTab, open, close, refresh, loadDirectory, setExpanded, setPanelOpen, setReference }}>{children}</Context.Provider>;
}

export function OpenFilesButton({ menu = false }: { menu?: boolean }) {
  const t = useT();

  const { setPanelOpen, setPanelTab } = useFileWorkspace();
  return <button type="button" className={menu ? "open-files-menu-button" : "open-files-button"} onClick={(event) => { setPanelTab("files"); setPanelOpen(true); const details = event.currentTarget.closest("details"); if (details) details.open = false; }}><FolderOpen />{menu ? t("查看会话文件") : t("文件")}</button>;
}

export function FileReferenceChip() {
  const t = useT();

  const { reference, setReference } = useFileWorkspace();
  return reference ? <div className="file-reference"><FileText /><span title={reference}>{reference}</span><button type="button" onClick={() => setReference(null)} aria-label={t("移除文件引用")}><X /></button></div> : null;
}

export function ArtifactCards() {
  const t = useT();

  const { files, changes, open, conversationId, running, deliverables } = useFileWorkspace();
  const registeredPaths = new Set(deliverables.map((item) => item.entry_path));
  const verified = artifactFiles(files.filter((file) => !deliverableKind(file) && !registeredPaths.has(file.path)), changes, running);
  if (!verified.length) return null;
  return <details className="related-files"><summary><T>{"其他相关文件 ·"}</T>{verified.length}</summary><section className="artifact-cards" aria-label={t("任务文件")}>{verified.map((file) => <div className="artifact-card" key={file.path}>
    <button type="button" onClick={() => open(file)} title={file.path} aria-label={t("查看 {0}", [file.name])}><FileText /><span><strong>{file.name}</strong><small>{file.name.includes(".") ? file.name.split(".").at(-1)?.toUpperCase() : t("文件")} · {file.size < 1024 ? `${file.size} B` : file.size < 1048576 ? `${(file.size / 1024).toFixed(1)} KB` : `${(file.size / 1048576).toFixed(1)} MB`}</small></span><span><T>{"查看"}</T></span></button>
    <FileDownload conversationId={conversationId} file={file} />
  </div>)}</section></details>;
}

export function FileDownload({ conversationId, file, label }: { conversationId: string; file: FileEntry; label?: string }) {
  const t = useT();

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
  return <span className="file-download"><button type="button" disabled={pending} onClick={() => void download()} aria-label={t("下载 {0}", [file.name])} title={t("下载")}><Download />{label ? <span>{pending ? t("下载中…") : label}</span> : null}</button>{error ? <span role="alert">{t(error)}</span> : null}</span>;
}
