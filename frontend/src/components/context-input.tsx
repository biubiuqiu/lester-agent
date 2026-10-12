"use client";

import { useEffect, useId, useRef, useState, type ReactNode, type RefObject, type TextareaHTMLAttributes } from "react";
import Link from "next/link";
import { AtSign, FileText, MessageSquare, Puzzle, X } from "lucide-react";
import { useT } from "./i18n";
import { FloatingPopover } from "./floating-popover";
import { useComposerSize } from "./use-composer-size";
import type { ContextReference, MentionItem, ReferenceType } from "@/lib/api";
import { inspectMention, parseMentionQuery, referenceKey, referenceType, searchMentions, type MentionPreview } from "@/lib/mentions";
import { readView, updateView } from "@/lib/conversation-view-state";

export function useContextReferences(key: string) {
  const [references, setReferences] = useState<ContextReference[]>(() => readView(key).contexts);
  function change(next: ContextReference[]) { updateView(key, { contexts: next }); setReferences(next); }
  return [references, change] as const;
}

type Props = Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, "onChange" | "value"> & {
  value: string; onText: (text: string) => void; references: ContextReference[]; onReferences: (items: ContextReference[]) => void;
  inputRef?: RefObject<HTMLTextAreaElement | null>; tools?: ReactNode; conversationId?: string;
};
const kinds = ["all", "context", "conversation", "skill"] as const;
const kindLabels = { all: "全部", context: "上下文", conversation: "会话", skill: "Skill" };
function ReferenceIcon({ kind }: { kind: ReferenceType }) {
  const Icon = kind === "conversation" ? MessageSquare : kind === "skill" ? Puzzle : FileText;
  return <Icon size={13} aria-hidden="true" />;
}

export function ContextInput({ value, onText, references, onReferences, inputRef, onKeyDown, tools, conversationId, ...props }: Props) {
  const t = useT();
  const listId = useId();
  const anchor = useRef<HTMLDivElement>(null), chips = useRef<HTMLDivElement>(null);
  const ownRef = useRef<HTMLTextAreaElement>(null), input = inputRef || ownRef;
  useComposerSize(input, value);
  const previewAnchor = useRef<HTMLButtonElement | null>(null);
  const [previewReference, setPreviewReference] = useState<ContextReference | null>(null);
  const [preview, setPreview] = useState<MentionPreview | null>(null);
  const [previewError, setPreviewError] = useState("");
  const [previewLoading, setPreviewLoading] = useState(false), [previewRetry, setPreviewRetry] = useState(0);
  const [query, setQuery] = useState<string | null>(null);
  const [kind, setKind] = useState<ReferenceType | "all">("all");
  const [result, setResult] = useState<{ key: string; items: MentionItem[]; error: string }>({ key: "", items: [], error: "" });
  const [selectionError, setSelectionError] = useState("");
  const [index, setIndex] = useState(0), [retry, setRetry] = useState(0);
  const mention = useRef<{ start: number; end: number } | null>(null);
  const open = query !== null;
  const key = JSON.stringify([kind, query, conversationId, retry]);
  const loading = result.key !== key;
  const error = selectionError || (result.key === key ? result.error : "");
  const matches = result.key === key ? result.items.filter(item => !references.some(reference => referenceKey(reference) === referenceKey(item))) : [];

  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      searchMentions(kind, query || "", conversationId, controller.signal)
        .then(data => { if (!controller.signal.aborted) setResult({ key, items: data.items, error: "" }); })
        .catch(reason => { if (!controller.signal.aborted) setResult({ key, items: [], error: reason instanceof Error ? reason.message : "引用对象加载失败，请重试。" }); });
    }, 120);
    return () => { clearTimeout(timer); controller.abort(); };
  }, [open, key, kind, query, conversationId]);
  useEffect(() => {
    if (open) document.getElementById(`${listId}-${index}`)?.scrollIntoView({ block: "nearest" });
  }, [open, index, key, result, listId]);
  useEffect(() => {
    if (!previewReference) return;
    const controller = new AbortController();
    inspectMention(previewReference, controller.signal)
      .then(data => { if (!controller.signal.aborted) setPreview(data); })
      .catch(reason => { if (!controller.signal.aborted) setPreviewError(reason instanceof Error ? reason.message : "引用对象已不可用，请移除后重新选择。"); })
      .finally(() => { if (!controller.signal.aborted) setPreviewLoading(false); });
    return () => controller.abort();
  }, [previewReference, previewRetry]);

  function removeReference(item: ContextReference) {
    if (props.disabled) return;
    const position = references.findIndex(reference => referenceKey(reference) === referenceKey(item));
    const next = references.filter(reference => referenceKey(reference) !== referenceKey(item));
    onReferences(next); setPreviewReference(null);
    requestAnimationFrame(() => {
      const target = chips.current?.querySelectorAll<HTMLButtonElement>(".context-chip-preview")[Math.min(position, next.length - 1)];
      if (target) target.focus(); else input.current?.focus();
    });
  }
  function track(text: string, caret: number) {
    const match = /(?:^|\s)@([^\n@]{0,120})$/.exec(text.slice(0, caret));
    mention.current = match ? { start: caret - match[1].length - 1, end: caret } : null;
    if (match) {
      const parsed = parseMentionQuery(match[1]);
      if (!open) { setResult({ key: "", items: [], error: "" }); setKind(parsed.kind || "all"); }
      else if (parsed.kind) setKind(parsed.kind);
      setQuery(parsed.query); setPreviewReference(null);
    } else setQuery(null);
    setIndex(0); setSelectionError("");
  }
  function choose(item: MentionItem) {
    if (references.length >= 8) { setSelectionError("每条消息最多引用 8 个对象"); return; }
    onReferences([...references, { type: item.type, id: item.id, title: item.title }]);
    const range = mention.current;
    const caret = range?.start ?? input.current?.selectionStart;
    if (range) onText(value.slice(0, range.start) + value.slice(range.end));
    setQuery(null); mention.current = null;
    requestAnimationFrame(() => { input.current?.focus(); if (caret !== undefined) input.current?.setSelectionRange(caret, caret); });
  }
  const previewKind = previewReference ? referenceType(previewReference) : "context";
  const previewName = (item: ContextReference) => t(item.type === "conversation" ? "查看会话引用 {0}" : item.type === "skill" ? "查看 Skill 引用 {0}" : "查看上下文 {0}", [item.title]);

  return <div className="context-input" ref={anchor}>
    {references.length ? <div className="context-references" ref={chips}>{references.map(item => <span key={referenceKey(item)} data-reference-type={referenceType(item)} onKeyDown={event => {
      if ((event.key === "Delete" || event.key === "Backspace") && !props.disabled) { event.preventDefault(); removeReference(item); }
    }}><button type="button" className="context-chip-preview" aria-label={previewName(item)} aria-expanded={previewReference ? referenceKey(previewReference) === referenceKey(item) : false} onClick={event => {
      const closing = previewReference && referenceKey(previewReference) === referenceKey(item);
      previewAnchor.current = event.currentTarget; setQuery(null); setPreview(null); setPreviewError(""); setPreviewLoading(!closing); setPreviewReference(closing ? null : item);
    }}><ReferenceIcon kind={referenceType(item)} /><span>@{item.title}</span></button><button type="button" disabled={props.disabled} aria-label={t("移除引用 {0}", [item.title])} onClick={() => removeReference(item)}><X size={12} /></button></span>)}</div> : null}
    {previewReference ? <FloatingPopover role="dialog" aria-label={previewName(previewReference)} className="context-reference-preview" anchor={previewAnchor} width={440} onClose={reason => { setPreviewReference(null); if (reason === "escape") previewAnchor.current?.focus(); }}>
      <header><strong>{preview?.title || previewReference.title}</strong><button autoFocus type="button" aria-label={t("关闭引用预览")} onClick={() => { setPreviewReference(null); previewAnchor.current?.focus(); }}><X size={15} /></button></header>
      {previewLoading ? <p role="status">{t("正在加载…")}</p> : previewError ? <p role="alert">{t(previewError)}<button type="button" onClick={() => { setPreviewLoading(true); setPreviewError(""); setPreviewRetry(value => value + 1); }}>{t("重试")}</button></p> : preview ? <>
        {preview.version ? <small>{t("当前版本 · v{0}", [preview.version])}</small> : null}
        {preview.description ? <p>{preview.source === "builtin" ? t(preview.description) : preview.description}</p> : null}
        {preview.content !== undefined ? <pre tabIndex={0}>{preview.content}</pre> : null}
        {preview.messages ? <div className="reference-conversation-preview" tabIndex={0}>{preview.messages.length ? preview.messages.map((message, i) => <section key={i}><small>{message.role === "user" ? t("用户") : "Agent"}</small><p>{message.content}</p></section>) : <p>{t("这个会话还没有可引用的消息。")}</p>}</div> : null}
        <small>{previewKind === "context" ? t("发送时使用当前内容；历史消息中的引用保持原样。") : previewKind === "conversation" ? t("预览最多 5 条消息，较长内容会截断。发送后，Agent 可按需读取截至引用时的会话文字；不会自动读取原会话文件。") : t("发送后将尚未安装的 Skill 添加到当前会话，Agent 加载说明后使用。")}</small>
        {previewKind === "conversation" ? <Link href={`/app/c/${encodeURIComponent(previewReference.id)}`} target="_blank" rel="noopener noreferrer">{t("打开原会话")}</Link> : null}
      </> : null}
    </FloatingPopover> : null}
    {open ? <FloatingPopover className="context-picker mention-picker" anchor={anchor} matchAnchor onClose={reason => { setQuery(null); if (reason === "escape") input.current?.focus(); }}>
      <header><strong>{t("添加引用")}</strong><button type="button" aria-label={t("关闭引用选择")} onClick={() => setQuery(null)}><X size={14} /></button></header>
      <div className="mention-categories" role="group" aria-label={t("引用类型")}>{kinds.map(item => <button type="button" key={item} aria-pressed={kind === item} onClick={() => { setKind(item); setIndex(0); setSelectionError(""); input.current?.focus(); }}>{t(kindLabels[item])}</button>)}</div>
      {error ? <p role="alert">{t(error)}<button type="button" onClick={() => { setSelectionError(""); setRetry(value => value + 1); }}>{t("重试")}</button></p> : loading ? <p role="status">{t("正在加载…")}</p> : <div id={listId} role="listbox" aria-label={t("引用对象")}>{matches.map((item, i) => <button type="button" role="option" id={`${listId}-${i}`} aria-selected={i === index} key={referenceKey(item)} className={i === index ? "selected" : ""} onMouseDown={event => event.preventDefault()} onClick={() => choose(item)}><span className="mention-item-heading"><ReferenceIcon kind={item.type} /><strong>{item.title}</strong><em>{t(kindLabels[item.type])}</em></span><small>{item.source === "builtin" ? t(item.description) : item.description || t("无简介")}</small></button>)}</div>}
      {!loading && !error && !matches.length ? <p>{t("没有匹配的可选对象，换个名称或类型试试。")}</p> : null}
      <footer><small>{t("输入 @名称 搜索 · ↑↓ 选择 · Enter 引用 · Esc 关闭")}</small><Link href={kind === "conversation" ? "/app" : kind === "skill" ? "/app/settings/skills" : "/app/contexts"}>{kind === "conversation" ? t("查看会话") : kind === "skill" ? t("管理 Skill") : t("管理词条")}</Link></footer>
    </FloatingPopover> : null}
    <textarea {...props} onFocus={event => { setPreviewReference(null); props.onFocus?.(event); }} aria-autocomplete="list" aria-controls={open ? listId : undefined} aria-activedescendant={open && matches[index] ? `${listId}-${index}` : undefined} ref={input} value={value} onChange={event => { onText(event.target.value); track(event.target.value, event.target.selectionStart); }} onClick={event => track(value, event.currentTarget.selectionStart)} onKeyDown={event => {
      if (event.nativeEvent.isComposing) return;
      if (event.key === "Backspace" && !value && references.length && !open) { event.preventDefault(); chips.current?.querySelectorAll<HTMLButtonElement>(".context-chip-preview").item(references.length - 1)?.focus(); return; }
      if (open) {
        if (event.key === "Escape") { event.preventDefault(); setQuery(null); return; }
        if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); setIndex(current => matches.length ? (current + (event.key === "ArrowDown" ? 1 : matches.length - 1)) % matches.length : 0); return; }
        if (event.key === "Enter" && !event.shiftKey) { event.preventDefault(); if (!loading && !error && matches[index]) choose(matches[index]); return; }
      }
      onKeyDown?.(event);
    }} />
    <div className="compose-actions"><button type="button" className="context-add" aria-label={t("@ 添加引用")} title={t("@ 添加引用")} disabled={props.disabled} onClick={() => {
      mention.current = null; setPreviewReference(null); if (!open) { setKind("all"); setResult({ key: "", items: [], error: "" }); }
      setQuery(open ? null : ""); setIndex(0); setSelectionError(""); input.current?.focus();
    }}><AtSign size={17} /></button>{tools}</div>
  </div>;
}
