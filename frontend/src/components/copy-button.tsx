"use client";

import { useEffect, useRef, useState } from "react";
import { Check, Copy, LoaderCircle } from "lucide-react";
import { useT } from "./i18n";

export function CopyButton({ text, label, html, compact = false }: { text: string; label: string; html?: () => string; compact?: boolean }) {
  const t = useT();
  const [status, setStatus] = useState<"idle" | "copying" | "copied" | "error">("idle");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const pending = useRef(false);
  useEffect(() => () => clearTimeout(timer.current), []);
  async function copy() {
    if (pending.current) return;
    pending.current = true;
    clearTimeout(timer.current);
    setStatus("copying");
    try {
      if (html && typeof ClipboardItem !== "undefined" && navigator.clipboard?.write) {
        await navigator.clipboard.write([new ClipboardItem({ "text/plain": new Blob([text], { type: "text/plain" }), "text/html": new Blob([html()], { type: "text/html" }) })]);
      } else {
        if (html) throw new Error("Formatted clipboard unavailable");
        await navigator.clipboard.writeText(text);
      }
      setStatus("copied");
      timer.current = setTimeout(() => setStatus("idle"), 2000);
    } catch { setStatus("error"); }
    finally { pending.current = false; }
  }
  return <span className="copy-control" data-copy-exclude="true">
    <button type="button" className="content-copy" title={status === "copied" ? t("已复制") : label} aria-label={label} disabled={status === "copying"} onClick={() => void copy()}>
      {status === "copied" ? <Check size={14} /> : status === "copying" ? <LoaderCircle size={14} /> : <Copy size={14} />}
      {!compact ? <span>{status === "copied" ? t("已复制") : label}</span> : null}
    </button>
    <span className={status === "error" ? "copy-error" : "interaction-sr-only"} role="status">{status === "error" ? t("复制失败，请手动选择内容；格式复制不可用时可使用普通复制。") : status === "copied" ? t("已复制") : ""}</span>
  </span>;
}
