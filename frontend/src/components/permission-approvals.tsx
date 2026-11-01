"use client";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { ChevronRight, LoaderCircle, ShieldCheck } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { useT } from "./i18n";
import { ConversationRecord } from "./conversation-presentation";
import "@/app/permission-approvals.css";

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
  const owner = `${conversationId}:${runId ?? ""}`, ownerRef = useRef(owner);
  const [selection, setSelection] = useState<{ owner: string; id: string } | null>(null);
  const handoff = useRef<{ origin: HTMLElement; target: HTMLTextAreaElement | null } | null>(null);
  useLayoutEffect(() => { ownerRef.current = owner; }, [owner]);
  useLayoutEffect(() => {
    const next = handoff.current;
    handoff.current = null;
    if (!next || next.origin.isConnected || document.activeElement !== document.body || document.visibilityState !== "visible" || !document.hasFocus()) return;
    if (next.target?.isConnected && !next.target.disabled) next.target.focus({ preventScroll: true });
  }, [items]);
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
          // Pin the first pending request. Later arrivals cannot replace the
          // decision the user is already reading, even if server order changes.
          const first = data.permissions.find(item => item.status === "pending" && active && item.run_id === runId);
          if (first) setSelection(previous => previous?.owner === owner ? previous : { owner, id: first.id });
          setError("");
        }
      } catch { if (alive) setError(t("权限请求加载失败，请重试。")); }
      finally { loading = false; }
    };
    refresh.current = () => { void load(); }; void load();
    const visible = () => { if (!document.hidden) void load(); }, timer = active ? setInterval(visible, 5000) : undefined;
    document.addEventListener("visibilitychange", visible);
    return () => { alive = false; if (timer) clearInterval(timer); document.removeEventListener("visibilitychange", visible); };
  }, [conversationId, runId, active, revision, t, owner]);

  async function respond(item: Permission, decision: "once" | "run" | "reject" | "full") {
    if (submitting.current || !active || item.run_id !== runId) return;
    const focused = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const focusAfter = focused?.closest(".permission-card") ? { origin: focused, target: focused.closest(".composer")?.querySelector<HTMLTextAreaElement>(".compose-box textarea") ?? null } : null;
    submitting.current = true; setPending(true); setError("");
    try {
      let confirmedPermission: Permission;
      try { confirmedPermission = await api<Permission>(`/api/v1/conversations/${conversationId}/permissions/${item.id}/respond`, { method: "POST", body: JSON.stringify({ request_digest: item.request_digest, decision }) }); }
      catch (reason) {
        if (reason instanceof ApiError && reason.status < 500 && reason.status !== 408) throw reason;
        confirmedPermission = await api<Permission>(`/api/v1/conversations/${conversationId}/permissions/${item.id}`);
      }
      if (confirmedPermission.id !== item.id || confirmedPermission.request_digest !== item.request_digest || confirmedPermission.decision !== decision || confirmedPermission.status !== (decision === "reject" ? "rejected" : "approved")) throw new Error();
      if (ownerRef.current !== owner) return;
      handoff.current = focusAfter;
      setItems(previous => previous.map(value => value.id === confirmedPermission.id && (value.permission_revision ?? 0) <= (confirmedPermission.permission_revision ?? 0) ? confirmedPermission : value));
    } catch { if (ownerRef.current === owner) setError(t("操作结果未确认，请检查状态；不会自动重试。")); }
    finally { submitting.current = false; setPending(false); }
  }
  const waiting = items.filter(item => item.status === "pending" && active && item.run_id === runId);
  const selectedPermission = selection?.owner === owner ? waiting.find(item => item.id === selection.id) : waiting[0];
  const reviewing = items.filter(item => item.status === "reviewing" && active && item.run_id === runId);
  const history = items.filter(item => !unresolved(item));
  if (!items.length && !error) return null;
  return <>{error || waiting.length || reviewing.length ? <section className="permission-approvals" aria-label={t("操作审批")}>
    {error ? <p role="alert">{error}<button type="button" onClick={() => refresh.current()}>{t("检查状态")}</button></p> : null}
    {reviewing.map(item => <article className="permission-card permission-reviewing" key={item.id} role="status"><header><LoaderCircle size={16} /><strong>{t("AI 正在审批命令")}</strong></header><pre>{operation(item)}</pre></article>)}
    {!selectedPermission && waiting.length ? <div className="permission-next"><ShieldCheck size={16} aria-hidden="true" /><span>{t("另有 {0} 项待确认", [waiting.length])}</span><button type="button" disabled={pending} onClick={() => setSelection({ owner, id: waiting[0].id })}>{t("查看下一项")}<ChevronRight size={14} aria-hidden="true" /></button></div> : null}
    {selectedPermission ? <article className="permission-card" key={selectedPermission.id}>
      <header><ShieldCheck size={16} aria-hidden="true" /><strong>{t(selectedPermission.tool === "github_publish" ? "推送 GitHub" : selectedPermission.tool === "github_checkout" ? "拉取 GitHub 仓库" : selectedPermission.tool === "bash" ? "执行命令" : selectedPermission.tool === "write" || selectedPermission.tool === "edit" ? "修改文件" : selectedPermission.tool === "read" ? "读取敏感文件" : "需要你的确认")}</strong>{waiting.length > 1 ? <small className="permission-remaining">{t("另有 {0} 项待确认", [waiting.length - 1])}</small> : null}</header>
      {selectedPermission.tool === "github_publish" ? <>
        <dl className="github-publish-target"><div><dt>{t("仓库")}</dt><dd>{String(selectedPermission.summary.repository || "—")}</dd></div><div><dt>{t("分支")}</dt><dd>{String(selectedPermission.summary.branch || "—")}</dd></div></dl>
        {selectedPermission.summary.summary ? <pre>{String(selectedPermission.summary.summary)}</pre> : null}
        <details className="github-publish-details"><summary>{t("发布详情")}</summary><code>{String(selectedPermission.summary.proposal_id || "")}<br />{String(selectedPermission.summary.digest || "")}</code></details>
      </> : <pre>{operation(selectedPermission)}</pre>}
      {selectedPermission.review_reason ? <p className="permission-review-reason">{t(selectedPermission.review_reason)}</p> : null}
      {selectedPermission.tool === "github_publish" ? <p>{t("此操作将发布已预览的变更，并可能创建 PR；请确认仓库和分支。")}</p> : <div className="permission-operation-meta"><small>{t("仅允许这次操作")}</small>{typeof selectedPermission.summary.content_bytes === "number" ? <small>{t("将写入 {0} 字节", [selectedPermission.summary.content_bytes])}</small> : null}</div>}
      <div className="permission-actions"><button type="button" className="permission-primary" disabled={pending} onClick={() => void respond(selectedPermission, "once")}>{t("允许一次")}</button>
        <button type="button" disabled={pending} onClick={() => void respond(selectedPermission, "reject")}>{t("拒绝")}</button>
        {selectedPermission.tool !== "github_publish" && (selectedPermission.allow_run || selectedPermission.tool === "bash") ? <details className="permission-grants"><summary aria-disabled={pending} onClick={event => { if (pending) event.preventDefault(); }}>{t("更多授权")}<ChevronRight size={13} aria-hidden="true" /></summary><div><p>{t("以下授权仅用于当前任务。")}</p>{selectedPermission.allow_run ? <button type="button" disabled={pending} onClick={() => void respond(selectedPermission, "run")}>{t("本次任务允许此类操作")}</button> : null}{selectedPermission.tool === "bash" ? <button type="button" disabled={pending} onClick={() => void respond(selectedPermission, "full")}>{t("本次任务完全访问")}</button> : null}</div></details> : null}
      </div>
    </article> : null}
  </section> : null}
    {history.length ? <ConversationRecord slot="permissions"><details className="permission-history"><summary>{t("审批记录")} · {history.length}</summary>{history.map(item => <p key={item.id}><code>{operation(item)}</code> · {item.decision_source === "llm" ? t("AI 审批") : t("操作审批")} · {t(item.status === "approved" ? "已允许" : item.status === "rejected" ? "已拒绝" : "已取消")}{item.review_reason ? <small className="permission-review-reason">{t(item.review_reason)}</small> : null}</p>)}</details></ConversationRecord> : null}
  </>;
}
