"use client";

import { useEffect, useId, useRef, useState, type RefObject, type TextareaHTMLAttributes } from "react";
import Link from "next/link";
import { BookOpen, X } from "lucide-react";
import { api, type ContextEntry, type ContextReference } from "@/lib/api";
import { readView, updateView } from "@/lib/conversation-view-state";

export function useContextReferences(key: string) {
  const [references, setReferences] = useState<ContextReference[]>(() => readView(key).contexts);
  function change(next: ContextReference[]) { updateView(key, { contexts: next }); setReferences(next); }
  return [references, change] as const;
}

type Props = Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, "onChange" | "value"> & {
  value: string; onText: (text: string) => void; references: ContextReference[]; onReferences: (items: ContextReference[]) => void; inputRef?: RefObject<HTMLTextAreaElement | null>;
};
export function ContextInput({ value, onText, references, onReferences, inputRef, onKeyDown, ...props }: Props) {
  const listId = useId();
  const ownRef = useRef<HTMLTextAreaElement>(null); const input = inputRef || ownRef;
  const [query, setQuery] = useState<string | null>(null);
  const [entries, setEntries] = useState<ContextEntry[]>([]);
  const [error, setError] = useState(""); const [loading, setLoading] = useState(false);
  const [index, setIndex] = useState(0); const [retry, setRetry] = useState(0);
  const mention = useRef<{ start: number; end: number } | null>(null);
  const open = query !== null;
  useEffect(() => {
    if (!open) return;
    let active = true;
    api<{ entries: ContextEntry[] }>("/api/v1/contexts").then((r) => { if (active) setEntries(r.entries); }).catch((e: unknown) => { if (active) setError(e instanceof Error ? e.message : "上下文加载失败"); }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [open, retry]);
  const matches = entries.filter((e) => !references.some((r) => r.id === e.id) && `${e.title} ${e.description}`.toLowerCase().includes((query || "").toLowerCase())).slice(0, 8);
  function track(text: string, caret: number) {
    const match = /(?:^|\s)@([^\s@]{0,80})$/.exec(text.slice(0, caret));
    mention.current = match ? { start: caret - match[1].length - 1, end: caret } : null;
    if (match && !open) { setLoading(true); setError(""); }
    setQuery(match ? match[1] : null); setIndex(0);
  }
  function choose(entry: ContextEntry) {
    if (references.length >= 8) { setError("每条消息最多引用 8 个词条"); return; }
    onReferences([...references, { id: entry.id, title: entry.title }]);
    const range = mention.current;
    if (range) onText(value.slice(0, range.start) + value.slice(range.end));
    setQuery(null); mention.current = null;
    input.current?.focus();
  }
  return <div className="context-input">
    <div className="context-references"><button type="button" className="context-add" disabled={props.disabled} onClick={() => { mention.current = null; if (!open) { setLoading(true); setError(""); } setQuery(open ? null : ""); setIndex(0); input.current?.focus(); }}><BookOpen size={14} />@ 引用上下文</button>{references.map((r) => <span key={r.id}>@{r.title}<button type="button" disabled={props.disabled} aria-label={`移除上下文 ${r.title}`} onClick={() => onReferences(references.filter((item) => item.id !== r.id))}><X size={12} /></button></span>)}</div>
    {open && <div className="context-picker">
      <header><strong>上下文库</strong><Link href="/app/contexts">管理词条</Link><button type="button" aria-label="关闭上下文选择" onClick={() => setQuery(null)}><X size={14} /></button></header>
      {error ? <p role="alert">{error}<button type="button" onClick={() => { setLoading(true); setError(""); setRetry((n) => n + 1); }}>重试</button></p> : loading ? <p role="status">正在加载…</p> : <div id={listId} role="listbox" aria-label="上下文词条">{matches.map((entry, i) => <button type="button" role="option" id={`${listId}-${i}`} aria-selected={i === index} key={entry.id} className={i === index ? "selected" : ""} onMouseDown={(e) => e.preventDefault()} onClick={() => choose(entry)}><strong>{entry.title}</strong><small>{entry.description || "无简介"}</small></button>)}</div>}
      {!loading && !error && !matches.length && <p>{entries.length ? "没有匹配的可选词条" : "还没有词条，先在上下文库中创建。"}</p>}
      <small className="context-picker-hint">输入 @名称 搜索 · ↑↓ 选择 · Enter 引用 · Esc 关闭</small>
    </div>}
    <textarea {...props} aria-autocomplete="list" aria-controls={open ? listId : undefined} aria-activedescendant={open && matches[index] ? `${listId}-${index}` : undefined} ref={input} value={value} onChange={(e) => { onText(e.target.value); track(e.target.value, e.target.selectionStart); }} onClick={(e) => track(value, e.currentTarget.selectionStart)} onKeyDown={(e) => {
      if (e.nativeEvent.isComposing) return;
      if (open) {
        if (e.key === "Escape") { e.preventDefault(); setQuery(null); return; }
        if (e.key === "ArrowDown" || e.key === "ArrowUp") { e.preventDefault(); setIndex((n) => matches.length ? (n + (e.key === "ArrowDown" ? 1 : matches.length - 1)) % matches.length : 0); return; }
        if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); if (!loading && !error && matches[index]) choose(matches[index]); return; }
      }
      onKeyDown?.(e);
    }} />
  </div>;
}
