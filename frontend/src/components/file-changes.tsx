"use client";
import { useEffect, useMemo, useRef, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { fileDiff } from "@/lib/file-diff";
import { randomId } from "@/lib/random-id";
import { useT } from "./i18n";
type Change = { id: string; run_id: string; path: string; operation: string; status: string; before_exists: boolean; before_sha256: string; after_sha256: string; retained: boolean; rollback_status: string; before_content?: string; after_content?: string };
type Rollback = { id: string; file_change_id: string; status: string };
export function FileChanges({ conversationId, active, revision }: { conversationId: string; active: boolean; revision: number }) {
  const t = useT();
  const [changes, setChanges] = useState<Change[]>([]), [error, setError] = useState("");
  const refresh = useRef<() => void>(() => {});
  useEffect(() => {
    let alive = true, pending = false;
    const load = async () => { if (pending) return; pending = true; try { const value = await api<{ changes: Change[] }>(`/api/v1/conversations/${conversationId}/file-changes`); if (alive) { setChanges(value.changes || []); setError(""); } } catch { if (alive) setError("文件审阅加载失败，请重试。"); } finally { pending = false; } };
    refresh.current = () => { void load(); }; void load();
    const visible = () => { if (!document.hidden) void load(); };
    const timer = active ? window.setInterval(visible, 5000) : undefined;
    document.addEventListener("visibilitychange", visible);
    return () => { alive = false; if (timer) clearInterval(timer); document.removeEventListener("visibilitychange", visible); };
  }, [conversationId, active, revision]);
  if (!changes.length && !error) return null;
  return <details className="file-changes"><summary>{t("文件修改审阅")} · {changes.length}</summary><p>{t("记录 Agent 写入和编辑工具的实际文件变化；不包含命令或外部操作的撤销。")}</p>{error ? <p role="alert">{t(error)}<button type="button" onClick={() => refresh.current()}>{t("重试")}</button></p> : null}{changes.map(change => <ChangeCard key={change.id} change={change} conversationId={conversationId} active={active} onRefresh={() => refresh.current()} />)}</details>;
}
function ChangeCard({ change, conversationId, active, onRefresh }: { change: Change; conversationId: string; active: boolean; onRefresh: () => void }) {
  const t = useT();
  const [full, setFull] = useState<Change>(), [error, setError] = useState(""), [busy, setBusy] = useState(false), [confirm, setConfirm] = useState(false);
  const [opened, setOpened] = useState(false), [loading, setLoading] = useState(false);
  const attempt = useRef<string | undefined>(undefined);
  const path = `/api/v1/conversations/${conversationId}/file-changes/${change.id}`;
  async function load() { setLoading(true); try { const next = await api<Change>(path); setFull(previous => previous?.status === "confirmed" && next.status !== "confirmed" ? previous : previous?.rollback_status === "completed" ? { ...next, rollback_status: "completed" } : next); setError(""); } catch { setError("文件审阅加载失败，请重试。"); } finally { setLoading(false); } }
  useEffect(() => {
    if (!opened) return;
    let alive = true;
    const controller = new AbortController();
    const read = async () => {
      setLoading(true);
      try { const next = await api<Change>(path, { signal: controller.signal }); if (alive) { setFull(previous => previous?.status === "confirmed" && next.status !== "confirmed" ? previous : previous?.rollback_status === "completed" ? { ...next, rollback_status: "completed" } : next); setError(""); } }
      catch { if (alive) setError("文件审阅加载失败，请重试。"); }
      finally { if (alive) setLoading(false); }
    };
    void read();
    return () => { alive = false; controller.abort(); };
  }, [path, opened, change.status, change.rollback_status]);
  const diff = useMemo(() => full?.before_content !== undefined && full.after_content !== undefined ? fileDiff(full.before_content, full.after_content) : undefined, [full]);
  async function restore() {
    if (busy || loading || active) return;
    setBusy(true); setError("");
    const id = attempt.current || randomId(); attempt.current = id;
    try {
      let receipt: Rollback;
      try { receipt = await api<Rollback>(`${path}/rollback`, { method: "POST", body: JSON.stringify({ id }) }); }
      catch (reason) { if (reason instanceof ApiError && reason.status < 500) throw reason; receipt = await api<Rollback>(`${path}/rollback/${id}`); }
      if (receipt.id !== id || receipt.file_change_id !== change.id) throw new Error();
      setFull(previous => previous ? { ...previous, rollback_status: receipt.status } : previous);
      setConfirm(false); onRefresh();
      if (receipt.status !== "completed") setError(receipt.status === "conflict" ? "文件已再次修改，未执行恢复。" : "恢复结果未确认；不会重复执行，也不会根据相同内容推断成功。");
    } catch (reason) { setError(reason instanceof ApiError && reason.status === 409 ? "当前文件或任务状态不允许恢复；原文件不会被强行覆盖。" : "恢复结果未确认；不会重复执行，也不会根据相同内容推断成功。"); }
    finally { setBusy(false); }
  }
  const current = full || change;
  const canRestore = current.status === "confirmed" && current.retained && current.before_exists && !["pending", "completed", "unknown"].includes(current.rollback_status);
  function download() {
    if (!full) return;
    const url = URL.createObjectURL(new Blob([JSON.stringify(full, null, 2)], { type: "application/json" }));
    const link = document.createElement("a"); link.href = url; link.download = `lester-file-review-${change.id}.json`; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  return <details className="file-change-card" onToggle={event => setOpened(event.currentTarget.open)}><summary><code>{change.path}</code><span>{t(change.status === "confirmed" ? change.before_exists ? "已修改" : "已创建" : "写入结果未确认")}</span></summary>
    {full ? <><small>SHA-256 {full.before_sha256 || "∅"} → {full.after_sha256 || "?"}</small>{full.status === "confirmed" && full.retained && full.before_content !== undefined && full.after_content !== undefined ? <>
      {diff ? <pre className="file-diff" aria-label={t("实际文件差异")}>{diff.map((line, index) => <span className={`diff-${line.kind}`} key={index}>{line.kind === "added" ? "+" : line.kind === "removed" ? "−" : " "}{line.text}</span>)}</pre> : <div className="file-change-panes"><section><h4>{t("修改前")}</h4><pre>{full.before_content}</pre></section><section><h4>{t("修改后")}</h4><pre>{full.after_content}</pre></section></div>}
      <button type="button" onClick={download}>{t("导出审阅记录")}</button>
    </> : <p>{t(full.status === "confirmed" ? "未保留可比较文本。只有已确认且保留完整文本的修改才支持差异和恢复。" : "原工具写入尚未确认，不能显示实际差异或恢复文件。")}</p>}
    {!full.before_exists ? <p>{t("新文件暂不支持撤回删除。")}</p> : null}
    {current.rollback_status === "completed" ? <p role="status">{t("已恢复修改前的文件内容")}</p> : null}
    {["pending", "unknown"].includes(current.rollback_status) ? <p role="status">{t("恢复结果未确认；不会重复执行，也不会根据相同内容推断成功。")}</p> : null}
    {canRestore ? <button type="button" disabled={active || busy || loading} onClick={() => setConfirm(true)} title={active ? t("请先结束当前任务，再恢复文件。") : undefined}>{t("撤回此文件修改")}</button> : null}
    {confirm && canRestore ? <div className="file-restore-confirm"><p>{t("只恢复此文件的原始内容；文件再次修改时会拒绝，不会撤销命令、网络操作或其他文件。")}</p><button type="button" disabled={busy || loading || active} onClick={() => void restore()}>{t("确认恢复")}</button><button type="button" disabled={busy} onClick={() => setConfirm(false)}>{t("取消")}</button></div> : null}
    </> : loading ? <p>{t("正在加载")}</p> : null}
    {error ? <p role="alert">{t(error)}<button type="button" disabled={busy} onClick={() => void load()}>{t("检查状态")}</button></p> : null}
  </details>;
}
