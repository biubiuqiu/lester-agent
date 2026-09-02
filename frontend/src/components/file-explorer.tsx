"use client";

/* eslint-disable @next/next/no-img-element -- Workspace images use authenticated runtime URLs and unknown dimensions. */

import { useCallback, useEffect, useState } from "react";
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
} from "lucide-react";
import {
  api,
  conversationFilePreviewURL,
  FileEntry,
  readConversationFile,
  readConversationFileBytes,
} from "@/lib/api";

type DirectoryState = {
  entries: FileEntry[];
  loading: boolean;
  error: string;
};

type PreviewState = {
  key: string;
  content: string;
  error: string;
};

type PreviewKind = "html" | "text" | "image" | "pdf" | "unsupported";

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
  const [directories, setDirectories] = useState<Record<string, DirectoryState>>({ "": { entries: [], loading: true, error: "" } });
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set([""]));
  const [selected, setSelected] = useState<FileEntry | null>(null);
  const [previewMode, setPreviewMode] = useState<"preview" | "source">("preview");
  const [preview, setPreview] = useState<PreviewState>({ key: "", content: "", error: "" });

  const loadDirectory = useCallback(async (path: string) => {
    setDirectories((current) => ({
      ...current,
      [path]: { entries: current[path]?.entries || [], loading: true, error: "" },
    }));
    try {
      const entries = await fetchDirectory(conversationId, path);
      setDirectories((current) => ({ ...current, [path]: { entries, loading: false, error: "" } }));
    } catch (reason) {
      const error = reason instanceof Error ? reason.message : "目录加载失败";
      setDirectories((current) => ({ ...current, [path]: { entries: [], loading: false, error } }));
    }
  }, [conversationId]);

  useEffect(() => {
    let active = true;
    fetchDirectory(conversationId, "")
      .then((entries) => { if (active) setDirectories({ "": { entries, loading: false, error: "" } }); })
      .catch((reason: Error) => { if (active) setDirectories({ "": { entries: [], loading: false, error: reason.message } }); });
    return () => { active = false; };
  }, [conversationId]);

  const selectedKind = selected ? previewKind(selected.path) : "unsupported";
  const needsTextContent = selectedKind === "text" || selectedKind === "html";
  const previewKey = selected ? `${selected.path}:${selectedKind === "html" ? previewMode : "source"}` : "";
  const previewTooLarge = Boolean(selected && needsTextContent && selected.size > maxTextPreviewBytes);
  const activePreview: PreviewState & { loading: boolean } = previewTooLarge
    ? { key: previewKey, content: "", error: `文件超过 ${formatBytes(maxTextPreviewBytes)}，请在 Terminal 中查看。`, loading: false }
    : preview.key === previewKey
      ? { ...preview, loading: false }
      : { key: previewKey, content: "", error: "", loading: needsTextContent };

  useEffect(() => {
    if (!selected || selected.is_dir || !needsTextContent || previewTooLarge) return;
    const controller = new AbortController();
    readConversationFile(conversationId, selected.path, controller.signal)
      .then(async (content) => selectedKind === "html" && previewMode === "preview"
        ? buildHTMLPreview(conversationId, selected.path, content, controller.signal)
        : content)
      .then((content) => setPreview({ key: previewKey, content, error: "" }))
      .catch((reason: Error) => {
        if (reason.name !== "AbortError") setPreview({ key: previewKey, content: "", error: reason.message });
      });
    return () => controller.abort();
  }, [conversationId, needsTextContent, previewKey, previewMode, previewTooLarge, selected, selectedKind]);

  const toggleDirectory = useCallback((path: string) => {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
    if (!directories[path]) void loadDirectory(path);
  }, [directories, loadDirectory]);

  const refresh = useCallback(() => {
    const visibleDirectories = [...expanded];
    setDirectories({});
    void Promise.all(visibleDirectories.map((path) => loadDirectory(path)));
  }, [expanded, loadDirectory]);

  const selectFile = useCallback((file: FileEntry) => {
    setSelected(file);
    setPreviewMode("preview");
  }, []);

  return <div className="file-explorer">
    <section className="file-tree-pane" aria-label="会话文件资源管理器">
      <header className="file-explorer-toolbar">
        <div>
          <strong>EXPLORER</strong>
          <span title={`/workspace/conversations/${conversationId}`}>{conversationId.slice(0, 8)}</span>
        </div>
        <button type="button" onClick={refresh} title="刷新文件" aria-label="刷新文件"><RefreshCw /></button>
      </header>
      <div className="file-workspace-root"><ChevronDown /><strong>CONVERSATION</strong></div>
      <div className="file-tree-scroll" role="tree" aria-label="Files">
        <FileTreeItems
          directoryPath=""
          depth={0}
          directories={directories}
          expanded={expanded}
          selectedPath={selected?.path || ""}
          onToggle={toggleDirectory}
          onSelect={selectFile}
        />
      </div>
    </section>
    <FilePreview
      conversationId={conversationId}
      file={selected}
      kind={selectedKind}
      mode={previewMode}
      preview={activePreview}
      onModeChange={setPreviewMode}
    />
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
  if (!state || state.loading) return <div className="file-tree-status" style={{ paddingLeft: 12 + depth * 14 }}><LoaderCircle />Loading…</div>;
  if (state.error) return <div className="file-tree-status error" style={{ paddingLeft: 12 + depth * 14 }}>{state.error}</div>;
  if (state.entries.length === 0) return <div className="file-tree-status" style={{ paddingLeft: 12 + depth * 14 }}>Empty folder</div>;

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
  conversationId,
  file,
  kind,
  mode,
  preview,
  onModeChange,
}: {
  conversationId: string;
  file: FileEntry | null;
  kind: PreviewKind;
  mode: "preview" | "source";
  preview: PreviewState & { loading: boolean };
  onModeChange: (mode: "preview" | "source") => void;
}) {
  if (!file) return <section className="file-preview empty"><Eye /><strong>选择文件进行预览</strong><p>支持代码、文本、图片、PDF 和 HTML 页面。</p></section>;
  const url = conversationFilePreviewURL(conversationId, file.path);
  return <section className="file-preview">
    <header className="file-preview-header">
      <div><FileGlyph file={file} /><span><strong>{file.name}</strong><small title={file.path}>{file.path}</small></span></div>
      {kind === "html" ? <a href={url} target="_blank" rel="noopener noreferrer" title="在新页面打开 HTML 预览" aria-label={`在新页面预览 ${file.name}`}><ExternalLink /></a> : null}
    </header>
    {kind === "html" ? <nav className="file-preview-tabs" aria-label="HTML 查看方式">
      <button type="button" className={mode === "preview" ? "active" : ""} onClick={() => onModeChange("preview")}><Eye />Preview</button>
      <button type="button" className={mode === "source" ? "active" : ""} onClick={() => onModeChange("source")}><Code2 />Source</button>
    </nav> : null}
    <div className={`file-preview-body ${kind}`}>
      {preview.loading ? <div className="file-preview-state"><LoaderCircle />正在读取文件…</div> : null}
      {preview.error ? <div className="file-preview-state error">{preview.error}</div> : null}
      {!preview.loading && !preview.error && kind === "html" && mode === "preview" ? <iframe title={`${file.name} preview`} srcDoc={preview.content} sandbox="allow-scripts" /> : null}
      {!preview.loading && !preview.error && kind === "image" ? <img src={url} alt={file.name} /> : null}
      {!preview.loading && !preview.error && kind === "pdf" ? <iframe title={`${file.name} PDF preview`} src={url} /> : null}
      {!preview.loading && !preview.error && (kind === "text" || (kind === "html" && mode === "source")) ? <SourcePreview content={preview.content} fileName={file.name} /> : null}
      {!preview.loading && !preview.error && kind === "unsupported" ? <div className="file-preview-state"><FileIcon /><strong>暂不支持预览</strong><span>{formatBytes(file.size)} · 可在 Terminal 中打开</span></div> : null}
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

async function fetchDirectory(conversationId: string, path: string) {
  const response = await api<{ files: FileEntry[] }>(`/api/v1/conversations/${conversationId}/files?path=${encodeURIComponent(path)}`);
  return [...response.files].sort((left, right) => {
    if (left.is_dir !== right.is_dir) return left.is_dir ? -1 : 1;
    return left.name.localeCompare(right.name, "zh-CN", { numeric: true, sensitivity: "base" });
  });
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
