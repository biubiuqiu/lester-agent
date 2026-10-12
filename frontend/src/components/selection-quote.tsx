"use client";

import { useEffect, useEffectEvent, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { MessageSquareQuote } from "lucide-react";
import { useOptionalFileWorkspace } from "./file-workspace";
import { useT } from "./i18n";

type Quote = { text: string; source: string; top: number; left: number; truncated: boolean; root: HTMLElement; position: "absolute" | "fixed" };
export function SelectionQuote({ children, source, extractText, enabled = true }: { children: ReactNode; source: string | ((range: Range) => string); extractText?: (range: Range) => string; enabled?: boolean }) {
  const t = useT();
  const workspace = useOptionalFileWorkspace();
  const host = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const available = Boolean(workspace);
  const [quote, setQuote] = useState<Quote | null>(null);
  function track() {
    if (!enabled || !workspace || !host.current) return;
    const selection = window.getSelection();
    if (!selection?.rangeCount || selection.isCollapsed) { setQuote(null); return; }
    const range = selection.getRangeAt(0);
    if (!host.current.contains(range.startContainer) || !host.current.contains(range.endContainer)) { setQuote(null); return; }
    const text = (extractText ? extractText(range) : selection.toString()).replace(/\r\n/g, "\n").replace(/\n$/, "");
    if (!text.trim()) { setQuote(null); return; }
    const rect = range.getBoundingClientRect();
    const viewport = window.visualViewport;
    const left = viewport?.offsetLeft ?? 0, top = viewport?.offsetTop ?? 0;
    const right = left + (viewport?.width ?? innerWidth), bottom = top + (viewport?.height ?? innerHeight);
    const dialog = host.current.closest<HTMLDialogElement>("dialog");
    const root = dialog || document.body;
    const bounds = dialog?.getBoundingClientRect();
    const y = Math.min(bottom - 60, Math.max(top + 8, rect.top - 46));
    const x = Math.max(left + 8, Math.min(right - 280, rect.left));
    const next: Quote = { text: text.slice(0, 6000), truncated: text.length > 6000, source: typeof source === "function" ? source(range) : source, root, position: dialog ? "absolute" : "fixed", top: y - (bounds?.top || 0) + (dialog?.scrollTop || 0), left: x - (bounds?.left || 0) + (dialog?.scrollLeft || 0) };
    setQuote(previous => previous && previous.text === next.text && previous.source === next.source && previous.top === next.top && previous.left === next.left ? previous : next);
  }
  const onSelectionChange = useEffectEvent(track);
  useEffect(() => {
    if (!enabled || !available) return;
    document.addEventListener("selectionchange", onSelectionChange);
    return () => document.removeEventListener("selectionchange", onSelectionChange);
  }, [enabled, available]);
  useEffect(() => {
    if (!quote) return;
    const dismiss = () => setQuote(null);
    const outside = (event: PointerEvent) => { if (!host.current?.contains(event.target as Node) && !button.current?.contains(event.target as Node)) dismiss(); };
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape") dismiss(); };
    const selectionChanged = () => { if (window.getSelection()?.isCollapsed) dismiss(); };
    document.addEventListener("pointerdown", outside);
    document.addEventListener("keydown", escape);
    document.addEventListener("selectionchange", selectionChanged);
    document.addEventListener("scroll", dismiss, true);
    window.addEventListener("resize", dismiss);
    window.visualViewport?.addEventListener("resize", dismiss);
    return () => { document.removeEventListener("pointerdown", outside); document.removeEventListener("keydown", escape); document.removeEventListener("selectionchange", selectionChanged); document.removeEventListener("scroll", dismiss, true); window.removeEventListener("resize", dismiss); window.visualViewport?.removeEventListener("resize", dismiss); };
  }, [quote]);
  return <div className="selection-quote-host" ref={host} onPointerUp={track} onKeyUp={event => { if (event.shiftKey || event.key === "Shift") track(); }}>
    {children}
    {quote && workspace ? createPortal(<button ref={button} type="button" className="selection-quote-button" data-copy-exclude="true" style={{ position: quote.position, top: quote.top, left: quote.left }} onPointerDown={event => event.preventDefault()} onClick={() => {
      workspace.quoteSelection(quote.text, quote.source);
      window.getSelection()?.removeAllRanges(); setQuote(null);
    }}><MessageSquareQuote size={15} />{quote.truncated ? t("引用前 {0} 个字符并追问", [6000]) : t("引用并追问")}</button>, quote.root) : null}
  </div>;
}
