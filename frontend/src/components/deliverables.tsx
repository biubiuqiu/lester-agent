"use client";

import { useMemo, useState } from "react";
import { ArrowUpRight, FileText, Globe, MessageSquare, MoreHorizontal, RefreshCw } from "lucide-react";
import { deliverableKind } from "@/lib/deliverables";
import { FileDownload, useFileWorkspace } from "./file-workspace";
import { PublishFileButton } from "./artifact-manager";

export function Deliverables({ panel = false }: { panel?: boolean }) {
  const { files, running, runOutcome, loading, error, limited, changes, deliverables, deliverablesError, open, refresh, conversationId, setReference, setExpanded, setPanelOpen } = useFileWorkspace();
  const [count, setCount] = useState(6);
  const registered = useMemo(() => new Map(deliverables.map((item) => [item.entry_path, item])), [deliverables]);
  const results = useMemo(() => files.filter((file) => !file.is_dir && (registered.has(file.path) || deliverableKind(file))).toSorted((a, b) => Number(registered.has(b.path)) - Number(registered.has(a.path)) || b.modified_at.localeCompare(a.modified_at) || a.path.localeCompare(b.path)), [files, registered]);
  if (!panel && (running || loading || !results.length)) return null;
  const updated = new Set(changes.filter((change) => change.kind !== "deleted").map((change) => change.path));
  return <section className={`deliverables ${panel ? "deliverables-panel" : ""}`} aria-label={panel ? "会话成果" : "任务成果"}>
    <header><h2>会话成果{!running && results.length ? <small>{results.length}</small> : null}</h2>{panel ? <button type="button" className="deliverables-refresh" onClick={refresh} aria-label="刷新成果"><RefreshCw size={16} /></button> : null}</header>
    {error ? <p className="deliverables-notice" role="alert">成果同步失败：{error}<button type="button" onClick={refresh}>重新同步</button></p> : null}
    {deliverablesError ? <p className="deliverables-notice" role="alert">成果记录同步失败：{deliverablesError}。仍可查看已同步文件。<button type="button" onClick={refresh}>重试成果记录</button></p> : null}
    {limited ? <p className="deliverables-notice">目录较大或部分读取失败，仅展示已同步范围；其他文件可在文件面板查找。</p> : null}
    {running ? <p className="deliverables-empty" role="status">任务进行中，结束后整理成果。可在文件面板查看正在生成的文件。</p> : loading ? <p className="deliverables-empty" role="status">正在同步成果…</p> : <>
      {runOutcome ? <p className="deliverables-notice">{runOutcome === "failed" ? "上次任务执行失败" : "上次任务已停止"}。已有文件可查看，完整性与内容仍需检查。</p> : null}
      {!results.length ? <div className="deliverables-empty"><FileText size={24} /><strong>还没有网页或文档成果</strong><span>生成 HTML 网页或 Markdown 文档后，会在这里集中展示。</span></div> : <div className="deliverables-grid">{results.slice(0, count).map((file) => {
        const record = registered.get(file.path);
        const kind = record?.kind ?? deliverableKind(file);
        return <article className={`deliverable-card ${kind}`} key={record?.id ?? file.path}>
          <button type="button" className="deliverable-preview" onClick={() => open(file, "preview")} aria-label={`预览成果 ${file.name}`}>
            <span className="deliverable-cover" aria-hidden="true">{kind === "html" ? <Globe /> : <FileText />}</span>
            <span className="deliverable-info"><strong>{record?.title || file.name}</strong>{record?.summary ? <span className="deliverable-summary">{record.summary}</span> : null}<span className="deliverable-status"><span>{kind === "html" ? "网页" : "文档"}</span><span className="deliverable-review">待验收</span>{updated.has(file.path) ? <span>本轮更新</span> : null}</span></span>
            <ArrowUpRight className="deliverable-open-icon" aria-hidden="true" />
          </button>
          <div className="deliverable-actions">
            <button type="button" className="deliverable-modify" onClick={() => { setReference(file.path); setExpanded(false); setPanelOpen(false); }} aria-label={`继续修改 ${file.name}`}><MessageSquare size={15} />继续修改</button>
            <details className="deliverable-more" onKeyDown={(event) => { if (event.key === "Escape") { event.stopPropagation(); event.currentTarget.open = false; event.currentTarget.querySelector("summary")?.focus(); } }}>
              <summary aria-label={`${file.name} 的详情与更多操作`} title="详情与更多操作"><MoreHorizontal size={18} /></summary>
              <div className="deliverable-details">
                {record?.summary ? <p>{record.summary}</p> : null}
                <dl><div><dt>文件</dt><dd>{file.path}</dd></div><div><dt>大小</dt><dd>{formatSize(file.size)}</dd></div><div><dt>状态</dt><dd>{record ? "已登记，内容未验收" : "按文件类型发现，内容未验收"} · 当前版本</dd></div></dl>
                <div className="deliverable-secondary-actions"><FileDownload conversationId={conversationId} file={file} label="下载" />{kind === "html" ? <PublishFileButton conversationId={conversationId} path={file.path} /> : null}</div>
              </div>
            </details>
          </div>
        </article>;
      })}</div>}
      {results.length > count ? <button type="button" className="deliverables-more" onClick={() => setCount((value) => value + 12)}>显示更多成果（还有 {results.length - count} 个）</button> : null}
    </>}
  </section>;
}

function formatSize(size: number) {
  if (size < 1024) return `${size} B`;
  return size < 1048576 ? `${(size / 1024).toFixed(1)} KB` : `${(size / 1048576).toFixed(1)} MB`;
}
