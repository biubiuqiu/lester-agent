"use client";

import { useLayoutEffect, useRef } from "react";
import { readView, updateView } from "@/lib/conversation-view-state";

export function usePreviewScroll(storageKey: string, resource: string, enabled = true) {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const node = ref.current;
    if (!node || !storageKey || !enabled) return;
    const saved = readView(storageKey).positions[resource];
    let lastPosition = { top: saved?.top ?? 0, left: saved?.left ?? 0 };
    let restored = false;
    const visible = () => node.isConnected && node.getClientRects().length > 0 && node.clientWidth > 0 && node.clientHeight > 0;
    const restore = () => {
      if (restored || !visible()) return;
      node.scrollTop = lastPosition.top;
      node.scrollLeft = lastPosition.left;
      restored = true;
    };
    restore();
    // A native panel dialog can open after layout effects have run. Restore
    // when it becomes measurable, rather than assigning to display:none DOM.
    const observer = new ResizeObserver(restore);
    observer.observe(node);
    let timer: ReturnType<typeof setTimeout>;
    const persist = () => {
      const positions = { ...readView(storageKey).positions, [resource]: lastPosition };
      updateView(storageKey, { positions: Object.fromEntries(Object.entries(positions).slice(-24)) });
    };
    const sample = () => {
      if (restored && visible()) lastPosition = { top: node.scrollTop, left: node.scrollLeft };
    };
    const save = () => { sample(); persist(); };
    const onScroll = () => {
      if (!visible()) return;
      // Capture while visible: a queued save may run after this tab is hidden.
      sample(); clearTimeout(timer); timer = setTimeout(persist, 200);
    };
    node.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("pagehide", save);
    return () => { clearTimeout(timer); save(); observer.disconnect(); node.removeEventListener("scroll", onScroll); window.removeEventListener("pagehide", save); };
  }, [storageKey, resource, enabled]);
  return ref;
}
