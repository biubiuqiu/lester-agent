"use client";

import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { ChevronRight, ListTree } from "lucide-react";
import { useT } from "./i18n";

type RecordSlot = "activity" | "questions" | "commands" | "changes" | "permissions" | "inputs" | "files";
const slots: RecordSlot[] = ["activity", "questions", "commands", "changes", "permissions", "inputs", "files"];
type ToolsHost = { element: HTMLDivElement; close: () => void };
type Presentation = {
  history: HTMLDivElement | null;
  bindHistory: (node: HTMLDivElement | null) => void;
  tools: HTMLDivElement | null;
  bindTools: (host: ToolsHost | null) => void;
  closeTools: () => void;
  reveal: (slot: RecordSlot) => void;
};
const Context = createContext<Presentation | null>(null);
const noop = () => {};

// Presentation only: the existing readers, receipts and action guards retain
// ownership of their data. Stable portal slots preserve open logs and forms.
export function ConversationPresentation({ children }: { children: ReactNode }) {
  const [history, bindHistory] = useState<HTMLDivElement | null>(null);
  const [toolsHost, bindTools] = useState<ToolsHost | null>(null);
  const reveal = useCallback((slot: RecordSlot) => {
    const root = history;
    const disclosure = root?.closest("details");
    if (!root || !disclosure) return;
    disclosure.open = true;
    const section = root.querySelector<HTMLElement>(`[data-record-slot="${slot}"]`);
    const nested = section?.querySelector("details");
    if (nested) nested.open = true;
    requestAnimationFrame(() => {
      if (!section?.isConnected) return;
      section.scrollIntoView({ block: "nearest" });
      section.querySelector<HTMLElement>("summary, button")?.focus({ preventScroll: true });
    });
  }, [history]);
  const value = useMemo(() => ({ history, bindHistory, tools: toolsHost?.element ?? null, bindTools, closeTools: toolsHost?.close ?? noop, reveal }), [history, toolsHost, reveal]);
  return <Context.Provider value={value}>{children}</Context.Provider>;
}

export function useConversationPresentation() { return useContext(Context); }

export function ConversationRecords() {
  const t = useT(), presentation = useContext(Context);
  const bind = presentation?.bindHistory;
  const attach = useCallback((node: HTMLDivElement | null) => { bind?.(node); }, [bind]);
  if (!presentation) return null;
  return <details className="conversation-records">
    <summary><ChevronRight size={14} aria-hidden="true" /><ListTree size={15} aria-hidden="true" /><span>{t("执行详情")}</span></summary>
    <div className="conversation-records-body" ref={attach}>{slots.map(slot => <div key={slot} data-record-slot={slot} />)}</div>
  </details>;
}

export function ConversationRecord({ slot, children }: { slot: RecordSlot; children: ReactNode }) {
  const presentation = useContext(Context);
  if (!presentation) return children;
  const target = presentation.history?.querySelector(`[data-record-slot="${slot}"]`);
  return target ? createPortal(children, target) : null;
}

export function ConversationToolsSlot({ onAction }: { onAction: () => void }) {
  const presentation = useContext(Context);
  const bind = presentation?.bindTools;
  const attach = useCallback((element: HTMLDivElement | null) => { bind?.(element ? { element, close: onAction } : null); }, [bind, onAction]);
  return presentation ? <div className="conversation-tools-slot" ref={attach} /> : null;
}

export function ConversationTools({ children }: { children: ReactNode }) {
  const presentation = useContext(Context);
  if (!presentation) return children;
  return presentation.tools ? createPortal(<div onClick={presentation.closeTools}>{children}</div>, presentation.tools) : null;
}
