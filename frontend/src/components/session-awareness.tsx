"use client";
import { useEffect, useMemo, useRef, useState } from "react";
import { BookOpen, Layers, ListTree, X } from "lucide-react";
import type { ContextReference, Message } from "@/lib/api";
import { readView, updateView } from "@/lib/conversation-view-state";
import { recapAwayMs, recapChanged, recapExcerpt, shouldOfferRecap } from "@/lib/session-recap";
import { orderedMessages } from "@/lib/timeline-order";
import type { RunEvent } from "./tool-timeline";
import { useFileWorkspace } from "./file-workspace";
import { ProjectRulesReceipt } from "./project-rules";
import { useT } from "./i18n";
import { ConversationTools } from "./conversation-presentation";

export function useSessionRecap(storageKey: string, messages: Message[], events: RunEvent[], draft: boolean) {
  const [open, setOpen] = useState(false), [available, setAvailable] = useState(false);
  const [enabled, setEnabled] = useState(() => readView(storageKey).recap.enabled);
  const ordered = useMemo(() => orderedMessages(messages), [messages]);
  const messageId = ordered.at(-1)?.id || "";
  const eventId = useMemo(() => events.reduce((maximum, event) => Math.max(maximum, event.id), 0), [events]);
  const latest = { messageId, eventId };
  const current = useRef({ ...latest, draft });
  useEffect(() => { current.current = { messageId, eventId, draft }; }, [eventId, messageId, draft]);
  useEffect(() => {
    let away = false;
    const leave = () => {
      if (away) return;
      away = true;
      updateView(storageKey, { recap: { ...readView(storageKey).recap, seenAt: Date.now(), eventId: current.current.eventId, messageId: current.current.messageId } });
    };
    const arrive = () => {
      away = false;
      const saved = readView(storageKey).recap;
      if (saved.enabled && saved.seenAt > 0 && Date.now() - saved.seenAt >= recapAwayMs && recapChanged(saved, current.current)) setAvailable(true);
      if (shouldOfferRecap(saved, current.current, Date.now(), current.current.draft)) setOpen(true);
    };
    const visibility = () => { if (document.hidden) leave(); else arrive(); };
    const frame = requestAnimationFrame(arrive);
    document.addEventListener("visibilitychange", visibility);
    window.addEventListener("pagehide", leave);
    return () => { cancelAnimationFrame(frame); leave(); document.removeEventListener("visibilitychange", visibility); window.removeEventListener("pagehide", leave); };
  }, [storageKey]);
  const show = () => {
    setOpen(true); setAvailable(false);
    updateView(storageKey, { recap: { ...readView(storageKey).recap, seenAt: Date.now(), ...latest } });
  };
  return { open, available, enabled, show, close: () => { setOpen(false); setAvailable(false); }, toggle: (value: boolean) => { setEnabled(value); updateView(storageKey, { recap: { ...readView(storageKey).recap, enabled: value } }); } };
}

export function SessionAwareness({ messages, events, references, attachmentNames, fileReference, recap, detailed, onDetail, status, onPrepare, recoverable, working }: {
  messages: Message[]; events: RunEvent[]; references: ContextReference[]; attachmentNames: string[]; fileReference: string | null;
  recap: ReturnType<typeof useSessionRecap>; detailed: boolean; onDetail: () => void; status: string; onPrepare: (text: string) => void; recoverable: boolean; working: boolean;
}) {
  const t = useT(), [contextOpen, setContextOpen] = useState(false);
  const recapButton = useRef<HTMLButtonElement>(null), contextButton = useRef<HTMLButtonElement>(null);
  const closeRecap = () => { recap.close(); (recapButton.current || document.querySelector<HTMLButtonElement>(".conversation-options-button"))?.focus(); };
  const closeContext = () => { setContextOpen(false); (contextButton.current || document.querySelector<HTMLButtonElement>(".conversation-options-button"))?.focus(); };
  const workspace = useFileWorkspace();
  const ordered = useMemo(() => orderedMessages(messages), [messages]);
  const userIndex = ordered.findLastIndex(message => message.role === "user");
  const user = ordered[userIndex], reply = ordered.slice(userIndex + 1).findLast(message => message.role === "assistant");
  const metadata = user?.metadata;
  const materials = (metadata?.contexts?.length || 0) + (metadata?.conversations?.length || 0) + (metadata?.skills?.length || 0) + (metadata?.attachments?.length || 0) + Number(Boolean(metadata?.project_rules?.content));
  const runId = user?.run_id || events.at(-1)?.run_id;
  const changedPaths = new Set(events.filter(event => event.run_id === runId && event.type === "FILE_UPDATED" && typeof event.payload.path === "string").map(event => String(event.payload.path)));
  const files = workspace.files.filter(file => !file.is_dir && changedPaths.has(file.path)).slice(0, 8);
  const compacted = events.findLast(event => event.type.startsWith("CONTEXT_COMPACT"));
  return <>
    <ConversationTools><div className="session-awareness-tools">
      <button ref={recapButton} type="button" onClick={() => { setContextOpen(false); if (recap.open) recap.close(); else recap.show(); }} aria-expanded={recap.open} title={recap.available ? t("离开后有新进展") : "/recap"}><BookOpen /><span>{t("会话回顾")}</span>{recap.available ? <i aria-hidden="true" /> : null}</button>
      <button ref={contextButton} type="button" aria-label={t("上下文概览")} onClick={() => { recap.close(); setContextOpen(value => !value); }} aria-expanded={contextOpen}><Layers /><span>{t("上下文概览")}</span>{materials + references.length + attachmentNames.length + Number(Boolean(fileReference)) > 0 ? <small aria-hidden="true">{materials + references.length + attachmentNames.length + Number(Boolean(fileReference))}</small> : null}</button>
      <button type="button" onClick={onDetail} aria-pressed={detailed}><ListTree /><span>{t(detailed ? "详细过程" : "简洁过程")}</span></button>
    </div></ConversationTools>
    {recap.open || contextOpen ? <section className="session-awareness" aria-label={t("会话导航")}>
    {recap.open ? <section className="session-recap" onKeyDown={event => { if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); closeRecap(); } }} aria-label={t("会话回顾")}>
      <header><strong>{t("会话回顾")}</strong><span>{status}</span><button type="button" aria-label={t("关闭回顾")} onClick={closeRecap}><X /></button></header>
      {user ? <><small>{t("最近目标")}</small><p>{recapExcerpt(user.content, 160)}</p></> : <p>{t("暂无可回顾的消息")}</p>}
      {reply ? <><small>{t("最近回复")}</small><p>{recapExcerpt(reply.content)}</p></> : null}
      {files.length ? <div className="recap-files"><small>{t("近期文件更新")}</small>{files.map(file => <button type="button" key={file.path} onClick={() => workspace.open(file, "preview")}>{file.path}</button>)}</div> : null}
      <div className="recap-actions">{workspace.files.length ? <button type="button" onClick={() => { workspace.setPanelTab("results"); workspace.setPanelOpen(true); }}>{t("查看会话成果")}</button> : null}{recoverable ? <button type="button" onClick={() => onPrepare(t("请检查当前会话目录里已经产生的结果，从上次中断处继续；不要重复已经成功完成的步骤。"))}>{t("从当前结果继续")}</button> : null}</div>
      <p className="recap-note">{t("回顾不会执行任务，也不会批准操作。")}</p>
      <label className="recap-preference"><input type="checkbox" checked={recap.enabled} onChange={event => recap.toggle(event.target.checked)} />{t("自动回顾")}</label>
      <p className="recap-note">{t("离开两分钟后有新进展时显示；正在编写草稿时不自动展开。")}</p>
    </section> : null}
    {contextOpen ? <section className="session-context" onKeyDown={event => { if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); closeContext(); } }} aria-label={t("上下文概览")}>
      <header><strong>{t("上下文概览")}</strong><button type="button" aria-label={t("关闭上下文概览")} onClick={closeContext}><X /></button></header>
      <p className="recap-note">{t("这里只展示已受理材料与整理事件，不是完整模型请求。")}</p>
      <h3>{t("最近消息的材料")}</h3>
      <ul>{metadata?.contexts?.map(entry => <li key={entry.id}><details><summary>@{entry.title} · v{entry.version}</summary><pre>{entry.content}</pre></details></li>)}{metadata?.conversations?.map(entry => <li key={entry.id}>@{entry.title} · {t("会话引用 · 截至消息序号 {0}", [entry.max_seq])}</li>)}{metadata?.skills?.map(entry => <li key={entry.id}>Skill · {entry.name} · v{entry.version}</li>)}{metadata?.attachments?.map(entry => <li key={entry.id}>{entry.original_name}</li>)}</ul>
      <ProjectRulesReceipt rules={metadata?.project_rules} />
      {!materials ? <p className="recap-note">{t("没有附加材料")}</p> : null}
      <h3>{t("待发送的材料")}</h3><ul>{references.map(entry => <li key={`${entry.type}:${entry.id}`}>@{entry.title}</li>)}{attachmentNames.map((name, index) => <li key={`${name}:${index}`}>{name}</li>)}{fileReference ? <li>{fileReference}</li> : null}</ul>
      <p className="recap-note">{t("引用不等于已读取；Skill 的实际加载与文件读取请查看执行过程。")}</p>
      {compacted ? <p>{t(compacted.type === "CONTEXT_COMPACTED" ? "历史已整理，继续任务" : compacted.type === "CONTEXT_COMPACTING" ? working ? "正在整理历史上下文" : "上下文整理已中断" : "保留原上下文，继续检查")}</p> : null}
    </section> : null}
  </section> : null}</>;
}
