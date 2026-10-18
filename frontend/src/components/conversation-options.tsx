"use client";

import { useCallback, useEffect, useId, useRef, useState, type ReactNode } from "react";
import { MoreHorizontal } from "lucide-react";
import { useT } from "./i18n";
import { FloatingPopover } from "./floating-popover";
import { ConversationToolsSlot } from "./conversation-presentation";

export function ConversationOptions({ children, leading }: { children: ReactNode; leading?: ReactNode }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const anchor = useRef<HTMLButtonElement>(null);
  const id = useId();
  useEffect(() => {
    if (!open) return;
    const frame = requestAnimationFrame(() => document.getElementById(id)?.querySelector<HTMLElement>("select, button, a[href]")?.focus({ preventScroll: true }));
    return () => cancelAnimationFrame(frame);
  }, [open, id]);
  const close = useCallback((restoreFocus: boolean) => {
    setOpen(false);
    if (restoreFocus) anchor.current?.focus({ preventScroll: true });
  }, []);
  const closeTools = useCallback(() => close(true), [close]);
  return <>
    <button ref={anchor} type="button" className="conversation-options-button" aria-label={t("更多会话选项")} title={t("更多会话选项")} aria-haspopup="dialog" aria-expanded={open} aria-controls={open ? id : undefined} onClick={() => setOpen(value => !value)}><MoreHorizontal size={20} /></button>
    {open ? <FloatingPopover anchor={anchor} side="below" align="end" width={280} maxHeight={560} className="conversation-options-popover" id={id} role="dialog" aria-label={t("更多会话选项")} onClose={reason => close(reason === "escape")} onChange={() => close(true)} onClick={event => {
      const action = (event.target as Element).closest("button, a");
      if (action) close(action.tagName === "BUTTON");
    }}>{leading}<ConversationToolsSlot onAction={closeTools} />{children}</FloatingPopover> : null}
  </>;
}
