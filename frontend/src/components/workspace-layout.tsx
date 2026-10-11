"use client";

import { useEffect, useRef, useSyncExternalStore, type ComponentProps } from "react";
import { useFileWorkspace } from "./file-workspace";

const compactQuery = "(max-width: 1000px)";
const subscribe = (notify: () => void) => {
  const media = window.matchMedia(compactQuery);
  media.addEventListener("change", notify);
  return () => media.removeEventListener("change", notify);
};
const compactSnapshot = () => window.matchMedia(compactQuery).matches;
const serverSnapshot = () => false;

export function WorkspaceSurface({ className = "", ...props }: ComponentProps<"main">) {
  const { panelOpen, expanded } = useFileWorkspace();
  return <main {...props} className={`${className} ${panelOpen || expanded ? "workspace-panel-open" : "workspace-panel-closed"}`} />;
}

// Native modal dialogs make the covered workspace inert and restore focus on
// dismissal, including nested deployment dialogs. Desktop previews stay inline.
export function useWorkspaceDialog(open: boolean, expanded: boolean) {
  const dialog = useRef<HTMLDialogElement>(null);
  const compact = useSyncExternalStore(subscribe, compactSnapshot, serverSnapshot);
  const overlay = compact || expanded;
  useEffect(() => {
    const element = dialog.current;
    if (!element) return;
    if (!open) { if (element.open) element.close(); return; }
    if (element.open && element.matches(":modal") === overlay) return;
    const previous = document.activeElement;
    if (element.open) element.close();
    if (overlay) element.showModal();
    else {
      element.show();
      // Opening a live preview must not interrupt typing in the composer.
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus({ preventScroll: true });
    }
  }, [open, overlay]);
  return { dialog, overlay };
}
