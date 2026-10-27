"use client";
import { useEffect, useRef, useState } from "react";
import { LoaderCircle, ShieldCheck } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { useT } from "./i18n";
import { ConversationRecord } from "./conversation-presentation";

type Permission = {
  id: string; run_id: string; tool: string; request_digest: string;
  status: "reviewing" | "pending" | "approved" | "rejected" | "cancelled";
  decision?: string; decision_source?: "user" | "llm" | "mode"; permission_revision?: number;
  review_decision?: "allow" | "deny" | "ask_user"; review_reason?: string;
  allow_run: boolean; summary: Record<string, string | number>;
};
const operation = (item: Permission) => item.tool === "github_publish" ? [item.summary.repository, item.summary.branch, item.summary.summary].filter(Boolean).join(" · ") : item.tool === "github_checkout" ? [item.summary.repository, item.summary.ref, item.summary.directory].filter(Boolean).join(" · ") : String(item.summary.command || item.summary.file_path || item.summary.path || item.summary.task_id || item.tool);
const unresolved = (item: Permission) => item.status === "pending" || item.status === "reviewing";

export function PermissionApprovals({ conversationId, runId, active, revision }: { conversationId: string; runId?: string; active: boolean; revision: number }) {
  const t = useT(), [items, setItems] = useState<Permission[]>([]), [error, setError] = useState("");
  const [pending, setPending] = useState(false), refresh = useRef(() => {}), submitting = useRef(false);
  useEffect(() => {
    let alive = true, loading = false;
    const load = async () => {
      if (loading) return; loading = true;
      try {
        const data = await api<{ permissions: Permission[] }>(`/api/v1/conversations/${conversationId}/permissions`);
        if (alive) {
          setItems(previous => data.permissions.map(item => {
            const old = previous.find(value => value.id === item.id);
            if (old && (old.permission_revision ?? 0) > (item.permission_revision ?? 0)) return old;
            // Confirmed decisions survive older reads. An explicit mode change
            // has a higher revision and may legitimately require review again.
            return old && (old.permission_revision ?? 0) === (item.permission_revision ?? 0) && !unresolved(old) && unresolved(item) ? old : item;
          }));
          setError("");
        }
      } catch { if (alive) setError(t("权限请求加载失败，请重试。")); }
      finally { loading = false; }
    };
    refresh.current = () => { void load(); }; void load();
    const visible = () => { if (!document.hidden) void load(); }, timer = active ? setInterval(visible, 5000) : undefined;
    document.addEventListener("visibilitychange", visible);
    return () => { alive = false; if (timer) clearInterval(timer); document.removeEventListener("visibilitychange", visible); };
  }, [conversationId, runId, active, revision, t]);

  async function respond(item: Permission, decision: "once" | "run" | "reject" | "full") {
    if (submitting.current) return;
    submitting.current = true; setPending(true); setError("");
    try {
      let current: Permission;
      try { current = await api<Permission>(`/api/v1/conversations/${conversationId}/permissions/${item.id}/respond`, { method: "POST", body: JSON.stringify({ request_digest: item.request_digest, decision }) }); }
      catch (reason) {
        if (reason instanceof ApiError && reason.status < 500 && reason.status !== 408) throw reason;
        current = await api<Permission>(`/api/v1/conversations/${conversationId}/permissions/${item.id}`);
      }
      if (current.id !== item.id || current.request_digest !== item.request_digest || current.decision !== decision || current.status !== (decision === "reject" ? "rejected" : "approved")) throw new Error();
      setItems(previous => previous.map(value => value.id === current.id && (value.permission_revision ?? 0) <= (current.permission_revision ?? 0) ? current : value));
    } catch { setError(t("操作结果未确认，请检查状态；不会自动重试。")); }
    finally { submitting.current = false; setPending(false); }
  }
  const waiting = items.filter(item => item.status === "pending" && active && item.run_id === runId);
  const reviewing = items.filter(item => item.status === "reviewing" && active && item.run_id === runId);
  const history = items.filter(item => !unresolved(item));
  if (!items.length && !error) return null;
  return <>{error || waiting.length || reviewing.length ? <section className="permission-approvals" aria-label={t("操作审批")}>
    {error ? <p role="alert">{error}<button type="button" onClick={() => refresh.current()}>{t("检查状态")}</button></p> : null}
    {reviewing.map(item => <article className="permission-card permission-reviewing" key={item.id} role="status"><header><LoaderCircle size={16} /><strong>{t("AI 正在审批命令")}</strong></header><pre>{operation(item)}</pre></article>)}
    {waiting.map(item => <article className="permission-card" key={item.id}>
      <header><ShieldCheck size={16} /><strong>{t(item.tool === "github_publish" ? "推送 GitHub" : item.tool === "github_checkout" ? "拉取 GitHub 仓库" : item.tool === "bash" ? "执行命令" : item.tool === "write" || item.tool === "edit" ? "修改文件" : item.tool === "read" ? "读取敏感文件" : "需要你的确认")}</strong></header>
      {item.tool === "github_publish" ? <>
        <dl className="github-publish-target"><div><dt>{t("仓库")}</dt><dd>{String(item.summary.repository || "—")}</dd></div><div><dt>{t("分支")}</dt><dd>{String(item.summary.branch || "—")}</dd></div></dl>
        {item.summary.summary ? <pre>{String(item.summary.summary)}</pre> : null}
        <details className="github-publish-details"><summary>{t("发布详情")}</summary><code>{String(item.summary.proposal_id || "")}<br />{String(item.summary.digest || "")}</code></details>
      </> : <pre>{operation(item)}</pre>}
      {item.review_reason ? <p className="permission-review-reason">{t(item.review_reason)}</p> : null}
      {typeof item.summary.content_bytes === "number" ? <small>{t("将写入 {0} 字节", [item.summary.content_bytes])}</small> : null}
      <p>{t(item.tool === "github_publish" ? "此操作将发布已预览的变更，并可能创建 PR；请确认仓库和分支。" : "审批仅适用于当前任务和这次操作，不会扩大 Computer 范围。")}</p>
      <div><button type="button" disabled={pending} onClick={() => void respond(item, "once")}>{t("允许一次")}</button>
        {item.allow_run && item.tool !== "github_publish" ? <button type="button" disabled={pending} onClick={() => void respond(item, "run")}>{t("本次任务允许此类操作")}</button> : null}
        {item.tool === "bash" ? <button type="button" disabled={pending} onClick={() => void respond(item, "full")}>{t("本次任务完全访问")}</button> : null}
        <button type="button" disabled={pending} onClick={() => void respond(item, "reject")}>{t("拒绝")}</button></div>
    </article>)}
  </section> : null}
    {history.length ? <ConversationRecord slot="permissions"><details className="permission-history"><summary>{t("审批记录")} · {history.length}</summary>{history.map(item => <p key={item.id}><code>{operation(item)}</code> · {item.decision_source === "llm" ? t("AI 审批") : t("操作审批")} · {t(item.status === "approved" ? "已允许" : item.status === "rejected" ? "已拒绝" : "已取消")}{item.review_reason ? <small className="permission-review-reason">{t(item.review_reason)}</small> : null}</p>)}</details></ConversationRecord> : null}
  </>;
}
