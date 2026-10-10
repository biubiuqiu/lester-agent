"use client";

import { useEffect, useEffectEvent, useLayoutEffect, useRef, useState, type CSSProperties, type HTMLAttributes, type RefObject } from "react";
import { createPortal } from "react-dom";

type Props = HTMLAttributes<HTMLDivElement> & {
  anchor: RefObject<HTMLElement | null>;
  onClose: (reason: "outside" | "escape") => void;
  side?: "above" | "below";
  align?: "start" | "end";
  width?: number;
  matchAnchor?: boolean;
};

/** Body-level positioning keeps composer scroll containers from clipping menus. */
export function FloatingPopover({ anchor, onClose, side = "above", align = "start", width = 400, matchAnchor = false, children, ...props }: Props) {
  const popup = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState<CSSProperties | null>(null);
  const close = useEffectEvent(onClose);

  useLayoutEffect(() => {
    let frame = 0;
    const update = () => {
      const element = anchor.current;
      if (!element) return;
      const rect = element.getBoundingClientRect();
      const viewport = window.visualViewport;
      const left = viewport?.offsetLeft ?? 0, top = viewport?.offsetTop ?? 0;
      const right = left + (viewport?.width ?? innerWidth), bottom = top + (viewport?.height ?? innerHeight);
      const margin = 8, gap = 8;
      const anchorTop = Math.max(top + margin, Math.min(rect.top, bottom - margin));
      const anchorBottom = Math.max(top + margin, Math.min(rect.bottom, bottom - margin));
      const room = { above: Math.max(0, anchorTop - top - margin - gap), below: Math.max(0, bottom - anchorBottom - margin - gap) };
      const other = side === "above" ? "below" : "above";
      const placement = room[side] >= Math.min(240, room[other]) ? side : other;
      const popupWidth = Math.max(0, Math.min(width, matchAnchor ? rect.width : width, right - left - margin * 2));
      const next: CSSProperties = {
        position: "fixed", width: popupWidth,
        left: Math.max(left + margin, Math.min(align === "end" ? rect.right - popupWidth : rect.left, right - margin - popupWidth)),
        top: placement === "above" ? anchorTop - gap : anchorBottom + gap,
        transform: placement === "above" ? "translateY(-100%)" : undefined,
        maxHeight: Math.min(320, room[placement]),
      };
      setPosition(previous => JSON.stringify(previous) === JSON.stringify(next) ? previous : next);
    };
    const schedule = () => { cancelAnimationFrame(frame); frame = requestAnimationFrame(update); };
    update();
    const resize = new ResizeObserver(schedule);
    if (anchor.current) resize.observe(anchor.current);
    window.addEventListener("resize", schedule);
    document.addEventListener("scroll", schedule, true);
    window.visualViewport?.addEventListener("resize", schedule);
    window.visualViewport?.addEventListener("scroll", schedule);
    return () => {
      cancelAnimationFrame(frame); resize.disconnect();
      window.removeEventListener("resize", schedule);
      document.removeEventListener("scroll", schedule, true);
      window.visualViewport?.removeEventListener("resize", schedule);
      window.visualViewport?.removeEventListener("scroll", schedule);
    };
  }, [anchor, side, align, width, matchAnchor]);

  useEffect(() => {
    const outside = (event: PointerEvent) => {
      if (!anchor.current?.contains(event.target as Node) && !popup.current?.contains(event.target as Node)) close("outside");
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); close("escape"); }
    };
    document.addEventListener("pointerdown", outside);
    document.addEventListener("keydown", escape, true);
    return () => { document.removeEventListener("pointerdown", outside); document.removeEventListener("keydown", escape, true); };
  }, [anchor]);

  if (typeof document === "undefined") return null;
  return createPortal(<div {...props} ref={popup} style={{ ...position, visibility: position ? "visible" : "hidden" }} onPointerDown={event => event.stopPropagation()}>{children}</div>, document.body);
}
