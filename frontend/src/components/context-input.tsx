"use client";
import { T, useT } from "@/components/i18n";


import { useEffect, useId, useRef, useState, type ReactNode, type RefObject, type TextareaHTMLAttributes } from "react";
import Link from "next/link";
import { FloatingPopover } from "./floating-popover";
import { AtSign, X } from "lucide-react";
import { api, type ContextEntry, type ContextReference } from "@/lib/api";
import { readView, updateView } from "@/lib/conversation-view-state";
import { useComposerSize } from "./use-composer-size";

export function useContextReferences(key: string) {
  const [references, setReferences] = useState<ContextReference[]>(() => readView(key).contexts);
  function change(next: ContextReference[]) { updateView(key, { contexts: next }); setReferences(next); }
  return [references, change] as const;
}

type Props = Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, "onChange" | "value"> & {
  value: string; onText: (text: string) => void; references: ContextReference[]; onReferences: (items: ContextReference[]) => void; inputRef?: RefObject<HTMLTextAreaElement | null>; tools?: ReactNode;
};
export function ContextInput({ value, onText, references, onReferences, inputRef, onKeyDown, tools, ...props }: Props) {
  const t = useT();

  const listId = useId();
  const anchor = useRef<HTMLDivElement>(null);
  const ownRef = useRef<HTMLTextAreaElement>(null); const input = inputRef || ownRef;
  useComposerSize(input, value);
  const previewAnchor = useRef<HTMLButtonElement | null>(null);
  const chips = useRef<HTMLDivElement>(null);
  const [previewReference, setPreviewReference] = useState<ContextReference | null>(null);
  const [previewEntry, setPreviewEntry] = useState<ContextEntry | null>(null);
  const [previewError, setPreviewError] = useState("");
  const [previewLoading, setPreviewLoading] = useState(false);
  const [previewRetry, setPreviewRetry] = useState(0);
  const [query, setQuery] = useState<string | null>(null);
  const [entries, setEntries] = useState<ContextEntry[]>([]);
  const [error, setError] = useState(""); const [loading, setLoading] = useState(false);
  const [index, setIndex] = useState(0); const [retry, setRetry] = useState(0);
  const mention = useRef<{ start: number; end: number } | null>(null);
  const open = query !== null;
  useEffect(() => {
    if (!previewReference) return;
    const controller = new AbortController();
    api<{ entries: ContextEntry[] }>("/api/v1/contexts", { signal: controller.signal }).then(result => {
      if (controller.signal.aborted) return;
      const entry = result.entries.find(item => item.id === previewReference.id);
      if (!entry) setPreviewError("此上下文已不可用，请移除后重新选择。");
      else setPreviewEntry(entry);
    }).catch(reason => { if (!controller.signal.aborted) setPreviewError(reason instanceof Error ? reason.message : "上下文加载失败"); }).finally(() => { if (!controller.signal.aborted) setPreviewLoading(false); });
    return () => controller.abort();
  }, [previewReference, previewRetry]);
  function removeReference(id: string) {
    if (props.disabled) return;
    const index = references.findIndex(item => item.id === id);
    const next = references.filter(item => item.id !== id);
    onReferences(next);
    setPreviewReference(null);
    requestAnimationFrame(() => {
      const target = chips.current?.querySelectorAll<HTMLButtonElement>(".context-chip-preview")[Math.min(index, next.length - 1)];
      if (target) target.focus(); else input.current?.focus();
    });
  }
  useEffect(() => {
    if (!open) return;
    let active = true;
    api<{ entries: ContextEntry[] }>("/api/v1/contexts").then((r) => { if (active) setEntries(r.entries); }).catch((e: unknown) => { if (active) setError(e instanceof Error ? e.message : "上下文加载失败"); }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [open, retry]);
  const matches = entries.filter((e) => !references.some((r) => r.id === e.id) && `${e.title} ${e.description}`.toLowerCase().includes((query || "").toLowerCase())).slice(0, 8);
  useEffect(() => {
    if (open) document.getElementById(`${listId}-${index}`)?.scrollIntoView({ block: "nearest" });
  }, [open, index, query, loading, listId]);
  function track(text: string, caret: number) {
    const match = /(?:^|\s)@([^\s@]{0,80})$/.exec(text.slice(0, caret));
    mention.current = match ? { start: caret - match[1].length - 1, end: caret } : null;
    if (match && !open) { setLoading(true); setError(""); }
    setQuery(match ? match[1] : null); setIndex(0);
    if (match) setPreviewReference(null);
  }
  function choose(entry: ContextEntry) {
    if (references.length >= 8) { setError("每条消息最多引用 8 个词条"); return; }
    onReferences([...references, { id: entry.id, title: entry.title }]);
    const range = mention.current;
    if (range) onText(value.slice(0, range.start) + value.slice(range.end));
    setQuery(null); mention.current = null;
    input.current?.focus();
  }
  return <div className="context-input" ref={anchor}>
    {references.length ? <div className="context-references" ref={chips}>{references.map((r) => <span key={r.id} onKeyDown={event => {
      if ((event.key === "Delete" || event.key === "Backspace") && !props.disabled) { event.preventDefault(); removeReference(r.id); }
    }}><button type="button" className="context-chip-preview" aria-label={t("查看上下文 {0}", [r.title])} aria-expanded={previewReference?.id === r.id} onClick={event => {
      const closing = previewReference?.id === r.id;
      previewAnchor.current = event.currentTarget; setQuery(null); setPreviewEntry(null); setPreviewError(""); setPreviewLoading(!closing); setPreviewReference(closing ? null : r);
    }}>@{r.title}</button><button type="button" disabled={props.disabled} aria-label={t("移除上下文 {0}", [r.title])} onClick={() => removeReference(r.id)}><X size={12} /></button></span>)}</div> : null}
    {previewReference ? <FloatingPopover className="context-reference-preview" anchor={previewAnchor} width={420} onClose={reason => { setPreviewReference(null); if (reason === "escape") previewAnchor.current?.focus(); }}>
      <header><strong>{previewEntry?.title || previewReference.title}</strong><button type="button" aria-label={t("关闭上下文预览")} onClick={() => { setPreviewReference(null); previewAnchor.current?.focus(); }}><X size={15} /></button></header>
      {previewLoading ? <p role="status">{t("正在加载…")}</p> : previewError ? <p role="alert">{t(previewError)}<button type="button" onClick={() => { setPreviewLoading(true); setPreviewError(""); setPreviewRetry(value => value + 1); }}>{t("重试")}</button></p> : previewEntry ? <><small>{t("当前版本 · v{0}", [previewEntry.version])}</small>{previewEntry.description ? <p>{previewEntry.description}</p> : null}<pre tabIndex={0}>{previewEntry.content}</pre><small>{t("发送时使用当前内容；历史消息中的引用保持原样。")}</small></> : null}
    </FloatingPopover> : null}
    {open && <FloatingPopover className="context-picker" anchor={anchor} matchAnchor onClose={reason => { setQuery(null); if (reason === "escape") input.current?.focus(); }}>
      <header><strong><T>{"上下文库"}</T></strong><Link href="/app/contexts"><T>{"管理词条"}</T></Link><button type="button" aria-label={t("关闭上下文选择")} onClick={() => setQuery(null)}><X size={14} /></button></header>
      {error ? <p role="alert">{t(error)}<button type="button" onClick={() => { setLoading(true); setError(""); setRetry((n) => n + 1); }}><T>{"重试"}</T></button></p> : loading ? <p role="status"><T>{"正在加载…"}</T></p> : <div id={listId} role="listbox" aria-label={t("上下文词条")}>{matches.map((entry, i) => <button type="button" role="option" id={`${listId}-${i}`} aria-selected={i === index} key={entry.id} className={i === index ? "selected" : ""} onMouseDown={(e) => e.preventDefault()} onClick={() => choose(entry)}><strong>{entry.title}</strong><small>{entry.description || t("无简介")}</small></button>)}</div>}
      {!loading && !error && !matches.length && <p>{entries.length ? t("没有匹配的可选词条") : t("还没有词条，先在上下文库中创建。")}</p>}
      <small className="context-picker-hint"><T>{"输入 @名称 搜索 · ↑↓ 选择 · Enter 引用 · Esc 关闭"}</T></small>
    </FloatingPopover>}
    <textarea {...props} aria-autocomplete="list" aria-controls={open ? listId : undefined} aria-activedescendant={open && matches[index] ? `${listId}-${index}` : undefined} ref={input} value={value} onChange={(e) => { onText(e.target.value); track(e.target.value, e.target.selectionStart); }} onClick={(e) => track(value, e.currentTarget.selectionStart)} onKeyDown={(e) => {
      if (e.nativeEvent.isComposing) return;
      if (e.key === "Backspace" && !value && references.length && !open) { e.preventDefault(); chips.current?.querySelectorAll<HTMLButtonElement>(".context-chip-preview").item(references.length - 1)?.focus(); return; }
      if (open) {
        if (e.key === "Escape") { e.preventDefault(); setQuery(null); return; }
        if (e.key === "ArrowDown" || e.key === "ArrowUp") { e.preventDefault(); setIndex((n) => matches.length ? (n + (e.key === "ArrowDown" ? 1 : matches.length - 1)) % matches.length : 0); return; }
        if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); if (!loading && !error && matches[index]) choose(matches[index]); return; }
      }
      onKeyDown?.(e);
    }} />
    <div className="compose-actions"><button type="button" className="context-add" aria-label={t("@ 引用上下文")} title={t("@ 引用上下文")} disabled={props.disabled} onClick={() => { mention.current = null; setPreviewReference(null); if (!open) { setLoading(true); setError(""); } setQuery(open ? null : ""); setIndex(0); input.current?.focus(); }}><AtSign size={17} /></button>{tools}</div>
  </div>;
}
