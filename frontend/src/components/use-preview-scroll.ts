"use client";

import { useLayoutEffect, useRef } from "react";
import { readView, updateView } from "@/lib/conversation-view-state";

export function usePreviewScroll(storageKey: string, resource: string) {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const node = ref.current;
    if (!node || !storageKey) return;
    const saved = readView(storageKey).positions[resource];
    node.scrollTop = saved?.top ?? 0;
    node.scrollLeft = saved?.left ?? 0;
    let timer: ReturnType<typeof setTimeout>;
    const save = () => {
      const positions = { ...readView(storageKey).positions, [resource]: { top: node.scrollTop, left: node.scrollLeft } };
      updateView(storageKey, { positions: Object.fromEntries(Object.entries(positions).slice(-24)) });
    };
    const onScroll = () => { clearTimeout(timer); timer = setTimeout(save, 200); };
    node.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("pagehide", save);
    return () => { clearTimeout(timer); save(); node.removeEventListener("scroll", onScroll); window.removeEventListener("pagehide", save); };
  }, [storageKey, resource]);
  return ref;
}
