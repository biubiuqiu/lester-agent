"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { readConversationFile } from "@/lib/api";
import { buildHTMLPreview } from "@/lib/html-preview";
import { listDirectory } from "@/lib/file-inventory";
import { AuthSessionKeeper } from "./auth-session-keeper";

function validPath(path: string) {
  return path.length <= 4096 && !/^[\\/]|[\\\x00-\x1f]/.test(path) && !path.split("/").some(part => !part || part === "." || part === "..") && /\.html?$/i.test(path);
}

export function StandaloneHTMLPreview({ conversationId }: { conversationId: string }) {
  const router = useRouter();
  const path = useSearchParams().get("path") ?? "";
  const frame = useRef<HTMLIFrameElement>(null);
  useEffect(() => { if (validPath(path)) document.title = `${path.split("/").at(-1)} · Lester 预览`; }, [path]);
  const [attempt, setAttempt] = useState(0);
  const [preview, setPreview] = useState({ path: "", content: "", error: "" });
  useEffect(() => {
    const controller = new AbortController();
    const load = async () => {
      if (!validPath(path)) throw new Error("预览地址无效，请从会话文件面板重新打开 HTML。");
      const source = await readConversationFile(conversationId, path, controller.signal);
      if (source.length > 768 * 1024) throw new Error("HTML 文件过大，请下载查看。");
      const content = await buildHTMLPreview(conversationId, path, source, controller.signal);
      if (!controller.signal.aborted) setPreview({ path, content, error: "" });
    };
    void load().catch(error => { if (!controller.signal.aborted) setPreview({ path, content: "", error: error instanceof Error ? error.message : "预览加载失败" }); });
    return () => controller.abort();
  }, [conversationId, path, attempt]);
  useEffect(() => {
    let active = true;
    const controller = new AbortController();
    const navigate = async (event: MessageEvent) => {
      if (event.source !== frame.current?.contentWindow || event.data?.type !== "lester:preview:navigate") return;
      const target = event.data.path;
      if (typeof target !== "string" || !validPath(target)) return;
      // Resolve only files verified in this conversation, even for script-created
      // messages from the isolated frame. Never mount a workspace in this route.
      const parent = target.split("/").slice(0, -1).join("/") || ".";
      try {
        const entries = await listDirectory(conversationId, parent, controller.signal);
        if (active && entries.some(file => !file.is_dir && file.path === target)) router.push(`/preview/${conversationId}?path=${encodeURIComponent(target)}`);
      } catch { /* Leave the current preview intact on unavailable targets. */ }
    };
    window.addEventListener("message", navigate);
    return () => { active = false; controller.abort(); window.removeEventListener("message", navigate); };
  }, [conversationId, path, router]);
  const ready = preview.path === path;
  return <main className="standalone-preview"><AuthSessionKeeper />{ready && preview.content ? <iframe ref={frame} title={`HTML 预览：${path}`} srcDoc={preview.content} sandbox="allow-scripts" /> : <div className="standalone-preview-state" role={ready && preview.error ? "alert" : "status"}>{ready && preview.error ? <><p>{preview.error}</p><button type="button" className="secondary-button" onClick={() => setAttempt(value => value + 1)}>重新加载</button></> : <p>正在加载 HTML 预览…</p>}</div>}</main>;
}
