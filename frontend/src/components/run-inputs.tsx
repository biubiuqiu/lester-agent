"use client";
import { useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import type { RunInput } from "@/lib/run-input";
import { useT } from "./i18n";
const labels = { pending: "等待加入当前任务", queued: "等待下一任务", applied: "已加入当前任务", held: "已暂停，等待处理", started: "下一任务已开始", cancelled: "已取消" };
export function RunInputs({ conversationId, revision, active, onRestore }: { conversationId: string; revision: number; active: boolean; onRestore: (content: string) => void }) {
  const t = useT();
  const [items, setItems] = useState<RunInput[]>([]), [error, setError] = useState(""), [pending, setPending] = useState(false);
  const refresh = useRef<() => void>(() => {});
  const waiting = items.some(item => item.status === "queued" || item.status === "pending");
  useEffect(() => {
    let alive = true, loading = false;
    const load = async () => {
      if (loading) return;
      loading = true;
      try { const data = await api<{ inputs: RunInput[] }>(`/api/v1/conversations/${conversationId}/inputs`); if (alive) { setItems(previous => (data.inputs || []).map(item => { const old = previous.find(value => value.id === item.id); return old && old.revision > item.revision ? old : item; })); setError(""); } } catch { if (alive) setError(t("任务输入加载失败，请重试。")); } finally { loading = false; }
    };
    refresh.current = () => { void load(); };
    void load();
    const visible = () => { if (!document.hidden) void load(); };
    const timer = active || waiting ? window.setInterval(visible, 5000) : undefined;
    document.addEventListener("visibilitychange", visible);
    return () => { alive = false; if (timer) clearInterval(timer); document.removeEventListener("visibilitychange", visible); };
  }, [conversationId, revision, active, t, waiting]);
  async function action(item: RunInput, kind: "resume" | "cancel") {
    if (pending) return;
    setPending(true);
    try {
      let current: RunInput;
      try { current = await api<RunInput>(`/api/v1/conversations/${conversationId}/inputs/${item.id}/action`, { method: "POST", body: JSON.stringify({ action: kind }) }); }
      catch { current = await api<RunInput>(`/api/v1/conversations/${conversationId}/inputs/${item.id}`); if (kind === "cancel" ? current.status !== "cancelled" : !["queued", "started"].includes(current.status)) throw new Error(); }
      setItems(previous => previous.map(old => old.id === current.id && current.revision >= old.revision ? current : old)); setError("");
    } catch { setError(t("操作结果未确认，请检查状态；不会自动重试。")); } finally { setPending(false); }
  }
  if (!items.length && !error) return null;
  return <details className="run-input-receipts"><summary>{t("任务输入与队列")} · {items.length}</summary>
    {error ? <p role="alert">{error}<button type="button" onClick={() => refresh.current()}>{t("检查状态")}</button></p> : null}
    {items.toReversed().map(item => <article key={item.id}><header><strong>{t(item.kind === "adjust" ? "调整当前任务" : "排队下一任务")}</strong><span>{t(labels[item.status])}</span></header><p>{item.content}</p>{["pending", "queued", "held"].includes(item.status) ? <div>
      {item.status === "held" && item.kind === "next" ? <button type="button" disabled={pending || active} onClick={() => void action(item, "resume")}>{t("继续排队")}</button> : null}
      {item.status === "held" && item.kind === "adjust" ? <button type="button" disabled={pending} onClick={async () => { setPending(true); try { const current = await api<RunInput>(`/api/v1/conversations/${conversationId}/inputs/${item.id}`); onRestore(current.content); } catch { setError(t("任务输入加载失败，请重试。")); } finally { setPending(false); } }}>{t("放回草稿")}</button> : null}
      <button type="button" disabled={pending} onClick={() => void action(item, "cancel")}>{t("取消")}</button>
    </div> : null}</article>)}
  </details>;
}
