"use client";
import { useEffect, useRef, useState } from "react";
import { ListPlus, CornerDownRight, Trash2, RotateCcw } from "lucide-react";
import { api } from "@/lib/api";
import type { RunInput } from "@/lib/run-input";
import { useT } from "./i18n";
import { ConversationRecord } from "./conversation-presentation";
const labels = { pending: "等待加入当前任务", queued: "等待下一任务", applied: "已加入当前任务", held: "已暂停，等待处理", started: "下一任务已开始", cancelled: "已取消" };
const isWaiting = (item: RunInput) => ["pending", "queued", "held"].includes(item.status);
export function RunInputs({ conversationId, revision, active, accepted, onRestore }: { conversationId: string; revision: number; active: boolean; accepted?: RunInput[]; onRestore: (content: string) => void }) {
  const t = useT();
  const [items, setItems] = useState<RunInput[]>([]), [error, setError] = useState(""), [pending, setPending] = useState(false);
  const refresh = useRef<() => void>(() => {});
  // Show the confirmed mutation receipt immediately, before the next list poll.
  const merged = new Map(items.map(item => [item.id, item]));
  for (const item of accepted || []) { const old = merged.get(item.id); if (!old || item.revision > old.revision) merged.set(item.id, item); }
  const receipts = [...merged.values()];
  const queued = receipts.filter(isWaiting).toSorted((a, b) => a.accepted_order - b.accepted_order);
  const history = receipts.filter(item => !isWaiting(item)).toReversed();
  const waiting = queued.some(item => item.status !== "held");
  useEffect(() => {
    let alive = true, loading = false;
    const load = async () => {
      if (loading) return;
      loading = true;
      try { const data = await api<{ inputs: RunInput[] }>(`/api/v1/conversations/${conversationId}/permissions/inputs`); if (alive) { setItems(previous => { const merged = new Map(previous.map(item => [item.id, item])); for (const item of data.inputs || []) { const old = merged.get(item.id); if (!old || item.revision >= old.revision) merged.set(item.id, item); } return [...merged.values()]; }); setError(""); } } catch { if (alive) setError(t("任务输入加载失败，请重试。")); } finally { loading = false; }
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
      try { current = await api<RunInput>(`/api/v1/conversations/${conversationId}/permissions/inputs/${item.id}/action`, { method: "POST", body: JSON.stringify({ action: kind }) }); }
      catch { current = await api<RunInput>(`/api/v1/conversations/${conversationId}/permissions/inputs/${item.id}`); if (kind === "cancel" ? current.status !== "cancelled" : !["queued", "started"].includes(current.status)) throw new Error(); }
      setItems(previous => [...previous.filter(old => old.id !== current.id), previous.find(old => old.id === current.id && old.revision > current.revision) || current]); setError("");
    } catch { setError(t("操作结果未确认，请检查状态；不会自动重试。")); } finally { setPending(false); }
  }
  if (!receipts.length && !error) return null;
  return <>
    {history.length ? <ConversationRecord slot="inputs"><details className="run-input-receipts"><summary>{t("已处理消息")} · {history.length}</summary><div className="queue-history">{history.map(item => <article key={item.id}><header><strong>{t(item.kind === "adjust" ? "调整当前任务" : "排队下一任务")}</strong><span>{t(labels[item.status])}</span></header><p>{item.content}</p></article>)}</div></details></ConversationRecord> : null}
    {error || queued.length ? <section className="composer-queue" aria-label={t("任务输入与队列")}>
    {error ? <p className="queue-error" role="alert">{error}<button type="button" onClick={() => refresh.current()}>{t("检查状态")}</button></p> : null}
    {queued.length ? <div className="composer-queue-list" aria-live="polite" aria-relevant="additions text">{queued.map(item => <article className="composer-queue-item" key={item.id} data-input-id={item.id}>
      {item.kind === "next" ? <ListPlus size={15} aria-hidden="true" /> : <CornerDownRight size={15} aria-hidden="true" />}
      <details className="queue-message"><summary title={item.content}><span>{item.content || (item.request?.attachment_ids?.length ? t("{0} 个附件", [item.request.attachment_ids.length]) : t("排队下一任务"))}</span></summary><p>{item.content}</p>{item.kind === "next" ? <small className="queue-permission">{t("权限设置")} · {t(item.request?.permission_mode === "full" ? "完全访问权限" : item.request?.permission_mode === "auto" ? "自动审批" : "默认权限")}</small> : null}</details>
      <span className={`queue-status ${item.status}`} title={t(labels[item.status])}>{t(labels[item.status])}</span>
      <div className="queue-actions">
        {item.status === "held" && item.kind === "next" ? <button type="button" disabled={pending || active} onClick={() => void action(item, "resume")} title={t("继续排队")} aria-label={t("继续排队")}><RotateCcw size={14} /></button> : null}
        {item.status === "held" && item.kind === "adjust" ? <button type="button" disabled={pending} onClick={async () => { setPending(true); try { const current = await api<RunInput>(`/api/v1/conversations/${conversationId}/permissions/inputs/${item.id}`); onRestore(current.content); } catch { setError(t("任务输入加载失败，请重试。")); } finally { setPending(false); } }} title={t("放回草稿")} aria-label={t("放回草稿")}><CornerDownRight size={14} /></button> : null}
        <button type="button" disabled={pending} onClick={() => void action(item, "cancel")} title={t("取消")} aria-label={t("取消")}><Trash2 size={14} /></button>
      </div>
    </article>)}</div> : null}

  </section> : null}</>;
}
