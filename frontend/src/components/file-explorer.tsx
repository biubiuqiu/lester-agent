"use client";

/* eslint-disable @next/next/no-img-element -- Workspace images use authenticated runtime URLs and unknown dimensions. */

import { useCallback, useEffect, useRef, useState } from "react";
import dynamic from "next/dynamic";
import {
  ChevronDown,
  ChevronRight,
  Code2,
  Eye,
  ExternalLink,
  File as FileIcon,
  FileCode2,
  FileImage,
  FileJson,
  FileText,
  Folder,
  FolderOpen,
  LoaderCircle,
  RefreshCw,
  Maximize2, Minimize2, X, MessageSquare,
} from "lucide-react";
import {
  conversationFilePreviewURL,
  FileEntry,
  readConversationFile,
  readConversationFileBytes,
} from "@/lib/api";

import { useFileWorkspace, FileDownload } from "./file-workspace";
import { readView, updateView } from "@/lib/conversation-view-state";
import { usePreviewScroll } from "./use-preview-scroll";
import { FileError } from "./file-error";
import { MessageContent } from "./message-content";

type DirectoryState = {
  entries: FileEntry[];
  loading?: boolean;
  error: string;
};

type PreviewState = {
  key: string;
  identity?: string;
  updatedAt?: number;
  content: string;
  error: string;
};

type PreviewKind = "html" | "markdown" | "text" | "image" | "pdf" | "unsupported";

const textExtensions = new Set([
  "c", "cc", "conf", "cpp", "css", "csv", "env", "go", "h", "hpp", "ini", "java", "js", "jsx",
  "json", "log", "md", "mjs", "php", "properties", "py", "rb", "rs", "scss", "sh", "sql", "svg",
  "toml", "ts", "tsx", "txt", "xml", "yaml", "yml",
]);
const imageExtensions = new Set(["avif", "bmp", "gif", "ico", "jpeg", "jpg", "png", "webp"]);
const codeExtensions = new Set(["c", "cc", "cpp", "css", "go", "h", "hpp", "java", "js", "jsx", "mjs", "php", "py", "rb", "rs", "scss", "sh", "sql", "ts", "tsx"]);
const ignoredScriptAttributes = new Set(["src", "integrity", "crossorigin"]);
const maxTextPreviewBytes = 768 * 1024;
const SourcePreview = dynamic(
  () => import("@/components/source-preview").then((module) => module.SourcePreview),
  {
    ssr: false,
    loading: () => <div className="file-preview-state"><LoaderCircle />正在加载代码预览…</div>,
  },
);

export function FileExplorer({ conversationId }: { conversationId: string }) {
  const { storageKey, directories, signature, selected, tabs, changes, files, loading, error, limited, open, close, refresh, loadDirectory, expanded: enlarged, setExpanded, setReference, setPanelOpen } = useFileWorkspace();
  const [expanded, setTreeExpanded] = useState<Set<string>>(() => new Set(readView(storageKey).directories));
  const [treeOpen, setTreeOpenState] = useState(() => readView(storageKey).treeOpen);
  const setTreeOpen = (value: boolean) => { updateView(storageKey, { treeOpen: value }); setTreeOpenState(value); };
  const changesMenu = useRef<HTMLDetailsElement>(null);
  useEffect(() => {
    const closeOutside = (event: PointerEvent) => { if (changesMenu.current && !changesMenu.current.contains(event.target as Node)) changesMenu.current.open = false; };
    document.addEventListener("pointerdown", closeOutside);
    return () => document.removeEventListener("pointerdown", closeOutside);
  }, []);
  const [modes, setModes] = useState(() => readView(storageKey).modes);
  const previewMode = selected ? modes[selected.path] ?? "preview" : "preview";
  const setPreviewMode = (mode: "preview" | "source") => {
    if (!selected) return;
    const next = Object.fromEntries(Object.entries({ ...modes, [selected.path]: mode }).slice(-16));
    updateView(storageKey, { modes: next }); setModes(next);
  };
  const [previewAttempt, setPreviewAttempt] = useState(0);
  const [preview, setPreview] = useState<PreviewState>({ key: "", content: "", error: "" });

  const selectedKind = selected ? previewKind(selected.path) : "unsupported";
  const selectedPath = selected?.path;
  const needsTextContent = selectedKind === "text" || selectedKind === "html" || selectedKind === "markdown";
  // HTML can embed local assets: rebuild when their directory metadata changes too.
  const assetRevision = selectedKind === "html" && previewMode === "preview" ? signature : "";
  const previewKey = selected ? `${selected.path}:${selected.modified_at}:${selected.size}:${previewMode}:${assetRevision}` : "";
  const previewTooLarge = Boolean(selected && needsTextContent && selected.size > maxTextPreviewBytes);
  const identity = JSON.stringify([selectedPath, previewMode]);
  const activePreview: PreviewState & { loading: boolean } = previewTooLarge
    ? { key: previewKey, content: "", error: `文件超过 ${formatBytes(maxTextPreviewBytes)}，请在 Terminal 中查看。`, loading: false }
    : preview.key === previewKey
      ? { ...preview, loading: false }
      : { key: previewKey, content: preview.identity === identity ? preview.content : "", error: "", loading: needsTextContent };

  useEffect(() => {
    if (!selectedPath || !needsTextContent || previewTooLarge) return;
    const controller = new AbortController();
    readConversationFile(conversationId, selectedPath, controller.signal)
      .then(async (content) => selectedKind === "html" && previewMode === "preview"
        ? buildHTMLPreview(conversationId, selectedPath, content, controller.signal)
        : content)
      .then((content) => { if (!controller.signal.aborted) setPreview((previous) => ({ key: previewKey, identity, content, error: "", updatedAt: previous.identity === identity && previous.key !== previewKey ? Date.now() : undefined })); })
      .catch((reason: Error) => {
        if (reason.name !== "AbortError") setPreview({ key: previewKey, content: "", error: reason.message });
      });
    return () => controller.abort();
  }, [conversationId, identity, needsTextContent, previewAttempt, previewKey, previewMode, previewTooLarge, selectedPath, selectedKind]);

  const toggleDirectory = useCallback((path: string) => {
    setTreeExpanded((current) => {
      const next = new Set(current);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      updateView(storageKey, { directories: [...next].slice(-64) });
      return next;
    });
    if (!directories[path]) void loadDirectory(path);
  }, [directories, loadDirectory, storageKey]);

  const selectFile = useCallback((file: FileEntry) => {
    open(file);
  }, [open]);

  return <div className="file-explorer">
    <section className={`file-tree-pane ${treeOpen ? "" : "collapsed"} ${selected ? "has-preview" : ""}`} aria-label="会话文件资源管理器">
      <header className="file-explorer-toolbar">
        <button type="button" onClick={() => setTreeOpen(!treeOpen)} aria-expanded={treeOpen}>{treeOpen ? <ChevronDown /> : <ChevronRight />}<strong>目录</strong></button>
        {changes.length || limited ? <details ref={changesMenu} className="file-changes" onKeyDown={(event) => { if (event.key === "Escape") { event.stopPropagation(); event.currentTarget.open = false; event.currentTarget.querySelector("summary")?.focus(); } }}><summary aria-label="查看文件变化">变化{changes.length ? ` · ${changes.length}` : ""}</summary>
      <div className="file-changes-popover"><strong>文件变化</strong><p>包含本轮文件操作及本次打开期间检测到的变化；不是完整历史或回滚快照。{limited ? "目录较大或部分读取失败，仅展示已同步范围。" : ""}</p>
      <div>{changes.map((change) => <button key={change.path} type="button" disabled={change.kind === "deleted" || !files.some((file) => file.path === change.path)} onClick={() => { const file = files.find((file) => file.path === change.path); if (file) selectFile(file); if (changesMenu.current) changesMenu.current.open = false; }}><span data-kind={change.kind}>{change.kind === "added" ? "新增" : change.kind === "deleted" ? "删除" : "更新"}</span><span>{change.path}</span></button>)}</div>
    </div></details> : null}
        <span className="file-sync-label" title="工具执行后同步；运行时每 5 秒、空闲时每 15 秒检查">自动同步</span>
        <button type="button" onClick={refresh} title="刷新文件" aria-label="刷新文件"><RefreshCw /></button>
        <button type="button" onClick={() => { setExpanded(!enlarged); if (!enlarged) setTreeOpen(false); }} title={enlarged ? "还原布局" : "专注预览"} aria-label={enlarged ? "还原布局" : "专注预览"}>{enlarged ? <Minimize2 /> : <Maximize2 />}</button>
      </header>
      {error ? <FileError detail={error} onRetry={refresh} /> : null}
      {treeOpen ? <div className="file-tree-scroll" role="tree" aria-label="Files">
        {loading ? <div className="file-tree-status">正在同步文件…</div> : <FileTreeItems directoryPath="" depth={0} directories={directories} expanded={expanded} selectedPath={selected?.path || ""} onToggle={toggleDirectory} onSelect={selectFile} />}
      </div> : null}
    </section>
    {tabs.length ? <nav className="open-file-tabs" aria-label="已打开文件">{tabs.map((file) => <div key={file.path} className={selected?.path === file.path ? "active" : ""}><button type="button" onClick={() => selectFile(file)} title={file.path} aria-pressed={selected?.path === file.path}>{file.name}</button><button type="button" onClick={() => close(file.path)} aria-label={`关闭 ${file.name}`}><X /></button></div>)}</nav> : null}
    <FilePreview key={`${selected?.path ?? "empty"}:${previewMode}`} onRetry={() => { setPreview((value) => ({ ...value, key: "", error: "" })); setPreviewAttempt((value) => value + 1); }} storageKey={storageKey} conversationId={conversationId} file={selected} kind={selectedKind} mode={previewMode} preview={activePreview} onModeChange={setPreviewMode} onReference={() => { if (selected) setReference(selected.path); setExpanded(false); setPanelOpen(false); }} />
  </div>;
}

function FileTreeItems({
  directoryPath,
  depth,
  directories,
  expanded,
  selectedPath,
  onToggle,
  onSelect,
}: {
  directoryPath: string;
  depth: number;
  directories: Record<string, DirectoryState>;
  expanded: Set<string>;
  selectedPath: string;
  onToggle: (path: string) => void;
  onSelect: (file: FileEntry) => void;
}) {
  const state = directories[directoryPath];
  if (!state || state.loading) return <div className="file-tree-status" style={{ paddingLeft: 12 + depth * 14 }}><LoaderCircle />正在读取…</div>;
  if (state.error) return <div className="file-tree-status error" style={{ paddingLeft: 12 + depth * 14 }}>{state.error}</div>;
  if (state.entries.length === 0) return <div className="file-tree-status" style={{ paddingLeft: 12 + depth * 14 }}>空目录</div>;

  return state.entries.map((entry) => {
    const path = normalizePath(entry.path);
    const open = entry.is_dir && expanded.has(path);
    return <div className="file-tree-node" key={path}>
      <button
        type="button"
        role="treeitem"
        aria-expanded={entry.is_dir ? open : undefined}
        aria-selected={!entry.is_dir && selectedPath === path}
        className={`file-tree-row ${selectedPath === path ? "selected" : ""}`}
        style={{ paddingLeft: 8 + depth * 14 }}
        title={`${path}${entry.is_dir ? "" : ` · ${formatBytes(entry.size)}`}`}
        onClick={() => entry.is_dir ? onToggle(path) : onSelect({ ...entry, path })}
      >
        <span className="file-tree-chevron">{entry.is_dir ? open ? <ChevronDown /> : <ChevronRight /> : null}</span>
        <FileGlyph file={entry} open={open} />
        <span className="file-tree-name">{entry.name}</span>
        {!entry.is_dir ? <small>{formatBytes(entry.size)}</small> : null}
      </button>
      {open ? <FileTreeItems
        directoryPath={path}
        depth={depth + 1}
        directories={directories}
        expanded={expanded}
        selectedPath={selectedPath}
        onToggle={onToggle}
        onSelect={onSelect}
      /> : null}
    </div>;
  });
}

function FilePreview({
  onRetry,
  storageKey,
  conversationId,
  file,
  kind,
  mode,
  preview,
  onModeChange,
  onReference,
}: {
  onRetry: () => void;
  storageKey: string;
  conversationId: string;
  file: FileEntry | null;
  kind: PreviewKind;
  mode: "preview" | "source";
  preview: PreviewState & { loading: boolean };
  onModeChange: (mode: "preview" | "source") => void;
  onReference: () => void;
}) {
  const markdownScroll = usePreviewScroll(storageKey, `markdown:${file?.path}`);
  if (!file) return <section className="file-preview empty"><Eye /><strong>选择文件进行预览</strong><p>支持代码、文本、图片、PDF 和 HTML 页面。</p></section>;
  const url = `${conversationFilePreviewURL(conversationId, file.path)}?v=${encodeURIComponent(file.modified_at + ":" + file.size)}`;
  return <section className="file-preview">
    <header className="file-preview-header" title={file.path}>
      {kind === "html" || kind === "markdown" ? <nav className="file-preview-tabs" aria-label="文件查看方式">
        <button type="button" aria-pressed={mode === "preview"} className={mode === "preview" ? "active" : ""} onClick={() => onModeChange("preview")}><Eye />预览</button>
        <button type="button" aria-pressed={mode === "source"} className={mode === "source" ? "active" : ""} onClick={() => onModeChange("source")}><Code2 />源码</button>
      </nav> : <span className="file-preview-format"><FileGlyph file={file} />{fileExtension(file.name).toUpperCase() || "文本"}<small>{formatBytes(file.size)}</small></span>}
      <div className="file-preview-actions">{preview.updatedAt ? <RecentFileUpdate key={preview.updatedAt} /> : null}<button type="button" className="file-ask-agent" onClick={onReference} title="让 Agent 修改此文件" aria-label="让 Agent 修改此文件"><MessageSquare /><span>修改</span></button><FileDownload conversationId={conversationId} file={file} />{kind === "html" ? <a href={url} target="_blank" rel="noopener noreferrer" title="在新页面打开 HTML 预览" aria-label={`在新页面预览 ${file.name}`}><ExternalLink /></a> : null}</div>
    </header>
    <div ref={kind === "markdown" && mode === "preview" ? markdownScroll : undefined} className={`file-preview-body ${kind}`}>
      {preview.loading && preview.content ? <span className="preview-syncing" role="status">正在同步最新版本…</span> : null}
      {preview.loading && !preview.content ? <div className="file-preview-state"><LoaderCircle />正在读取文件…</div> : null}
      {preview.error ? <FileError detail={preview.error} onRetry={onRetry} /> : null}
      {(!preview.loading || Boolean(preview.content)) && !preview.error && kind === "html" && mode === "preview" ? <iframe title={`${file.name} preview`} srcDoc={preview.content} sandbox="allow-scripts" /> : null}
      {(!preview.loading || Boolean(preview.content)) && !preview.error && kind === "image" ? <img src={url} alt={file.name} /> : null}
      {(!preview.loading || Boolean(preview.content)) && !preview.error && kind === "pdf" ? <iframe title={`${file.name} PDF preview`} src={url} /> : null}
      {(!preview.loading || Boolean(preview.content)) && !preview.error && (kind === "text" || ((kind === "html" || kind === "markdown") && mode === "source")) ? <SourcePreview key={file.path} content={preview.content} fileName={file.name} filePath={file.path} storageKey={storageKey} /> : null}
      {(!preview.loading || Boolean(preview.content)) && !preview.error && kind === "markdown" && mode === "preview" ? <div className="file-markdown"><MessageContent content={preview.content} /></div> : null}
      {(!preview.loading || Boolean(preview.content)) && !preview.error && kind === "unsupported" ? <div className="file-preview-state"><FileIcon /><strong>暂不支持预览</strong><span>{formatBytes(file.size)} · 可在 Terminal 中打开</span></div> : null}
    </div>
  </section>;
}

function FileGlyph({ file, open = false }: { file: Pick<FileEntry, "name" | "is_dir">; open?: boolean }) {
  if (file.is_dir) return open ? <FolderOpen className="file-kind folder" /> : <Folder className="file-kind folder" />;
  const extension = fileExtension(file.name);
  if (imageExtensions.has(extension)) return <FileImage className="file-kind image" />;
  if (extension === "json") return <FileJson className="file-kind json" />;
  if (extension === "html" || extension === "htm" || codeExtensions.has(extension)) return <FileCode2 className="file-kind code" />;
  if (textExtensions.has(extension)) return <FileText className="file-kind text" />;
  return <FileIcon className="file-kind" />;
}

function previewKind(path: string): PreviewKind {
  const extension = fileExtension(path);
  if (extension === "html" || extension === "htm") return "html";
  if (imageExtensions.has(extension)) return "image";
  if (extension === "pdf") return "pdf";
  if (extension === "md") return "markdown";
  if (textExtensions.has(extension) || !extension) return "text";
  return "unsupported";
}

function fileExtension(path: string) {
  const name = path.split("/").at(-1) || "";
  const dot = name.lastIndexOf(".");
  return dot > 0 ? name.slice(dot + 1).toLowerCase() : "";
}

function normalizePath(path: string) {
  return path.replaceAll("\\", "/").replace(/^\.\//, "");
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

async function buildHTMLPreview(conversationId: string, filePath: string, source: string, signal: AbortSignal) {
  const document = new DOMParser().parseFromString(source, "text/html");
  document.querySelectorAll('meta[http-equiv="Content-Security-Policy" i]').forEach((element) => element.remove());
  const policy = document.createElement("meta");
  policy.httpEquiv = "Content-Security-Policy";
  policy.content = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; connect-src 'none'; media-src data: blob:; object-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'";
  document.head.prepend(policy);

  const tasks: Promise<void>[] = [];
  const stylesheets = [...document.querySelectorAll<HTMLLinkElement>('link[rel~="stylesheet"][href]')].slice(0, 20);
  for (const link of stylesheets) {
    const assetPath = resolveWorkspaceReference(filePath, link.getAttribute("href") || "");
    if (!assetPath) continue;
    tasks.push(readConversationFile(conversationId, assetPath, signal).then((content) => {
      if (content.length > 512 * 1024) return;
      const style = document.createElement("style");
      style.textContent = content;
      link.replaceWith(style);
    }));
  }

  const scripts = [...document.querySelectorAll<HTMLScriptElement>("script[src]")].slice(0, 20);
  for (const script of scripts) {
    const assetPath = resolveWorkspaceReference(filePath, script.getAttribute("src") || "");
    if (!assetPath) continue;
    tasks.push(readConversationFile(conversationId, assetPath, signal).then((content) => {
      if (content.length > 512 * 1024) return;
      const inline = document.createElement("script");
      for (const attribute of [...script.attributes]) {
        if (!ignoredScriptAttributes.has(attribute.name)) inline.setAttribute(attribute.name, attribute.value);
      }
      inline.textContent = content;
      script.replaceWith(inline);
    }));
  }

  const images = [...document.querySelectorAll<HTMLImageElement>("img[src]")].slice(0, 20);
  for (const image of images) {
    const assetPath = resolveWorkspaceReference(filePath, image.getAttribute("src") || "");
    if (!assetPath) continue;
    tasks.push(readConversationFileBytes(conversationId, assetPath, signal).then((content) => {
      if (content.byteLength > 4 * 1024 * 1024) return;
      image.src = bytesToDataURL(content, imageMIMEType(assetPath));
      image.removeAttribute("srcset");
    }));
  }

  await Promise.allSettled(tasks);
  if (signal.aborted) throw new DOMException("Aborted", "AbortError");
  return `<!doctype html>\n${document.documentElement.outerHTML}`;
}

function resolveWorkspaceReference(baseFile: string, reference: string) {
  const value = reference.trim();
  if (!value || value.startsWith("#") || /^(?:[a-z][a-z0-9+.-]*:|\/\/)/i.test(value)) return "";
  const pathOnly = value.split(/[?#]/, 1)[0];
  const parts = value.startsWith("/") ? [] : normalizePath(baseFile).split("/").slice(0, -1);
  for (const part of normalizePath(pathOnly).split("/")) {
    if (!part || part === ".") continue;
    if (part === "..") {
      if (parts.length === 0) return "";
      parts.pop();
    } else {
      try {
        parts.push(decodeURIComponent(part));
      } catch {
        return "";
      }
    }
  }
  return parts.join("/");
}

function imageMIMEType(path: string) {
  const extension = fileExtension(path);
  if (extension === "jpg" || extension === "jpeg") return "image/jpeg";
  if (extension === "svg") return "image/svg+xml";
  if (extension === "ico") return "image/x-icon";
  return `image/${extension || "png"}`;
}

function bytesToDataURL(bytes: Uint8Array, mimeType: string) {
  let binary = "";
  for (let offset = 0; offset < bytes.length; offset += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000));
  }
  return `data:${mimeType};base64,${btoa(binary)}`;
}

function RecentFileUpdate() {
  const [visible, setVisible] = useState(true);
  useEffect(() => { const timer = setTimeout(() => setVisible(false), 3000); return () => clearTimeout(timer); }, []);
  return visible ? <span className="file-updated-notice" role="status">已更新</span> : null;
}
