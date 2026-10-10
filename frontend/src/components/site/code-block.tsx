"use client";
import { useT } from "@/components/i18n";


import { useEffect, useRef, useState } from "react";
import { Check, Copy } from "lucide-react";

export function CodeBlock({ children, label = "Shell" }: { children: string; label?: string }) {
  const t = useT();

  const [status, setStatus] = useState<"idle" | "copying" | "copied" | "error">("idle");
  const reset = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(reset.current), []);
  async function copy() {
    clearTimeout(reset.current);
    setStatus("copying");
    try {
      await navigator.clipboard.writeText(children);
      setStatus("copied");
      reset.current = setTimeout(() => setStatus("idle"), 2500);
    } catch {
      setStatus("error");
    }
  }
  return <div className="site-code-block">
    <div className="site-code-header"><span>{t(label)}</span><button type="button" onClick={copy} disabled={status === "copying"} aria-label={t("复制 {0} 命令", [t(label)])}>{status === "copied" ? <Check size={15} aria-hidden="true" /> : <Copy size={15} aria-hidden="true" />}{status === "copied" ? t("已复制") : status === "copying" ? t("复制中…") : t("复制命令")}</button></div>
    <pre tabIndex={0} aria-label={t("{0} 命令", [t(label)])}><code>{children}</code></pre>
    <span className={status === "error" ? "site-code-error" : "site-sr-only"} role="status">{status === "error" ? t("无法访问剪贴板，请在代码区手动选择并复制。") : status === "copied" ? t("命令已复制到剪贴板") : ""}</span>
  </div>;
}
