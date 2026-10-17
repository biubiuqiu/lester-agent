"use client";
import { useEffect, useRef, useState } from "react";
import { api, ApiError, type Project } from "@/lib/api";
import { useT } from "./i18n";
export type ProjectRules = { project_id: string; project_name: string; source: string; version: number; sha256: string; content: string };
export function ProjectRulesEditor({ project, onClose }: { project: Project; onClose: () => void }) {
  const t = useT();
  const dialog = useRef<HTMLDialogElement>(null);
  const [rules, setRules] = useState<ProjectRules>(), [text, setText] = useState(""), [error, setError] = useState(""), [busy, setBusy] = useState(false);
  useEffect(() => {
    const previous = document.activeElement;
    const fallback = previous instanceof HTMLElement ? previous.closest(".rail-menu")?.querySelector<HTMLElement>(".rail-actions-trigger") : null;
    dialog.current?.showModal();
    let alive = true;
    api<ProjectRules>(`/api/v1/projects/${project.id}/rules`).then(value => { if (alive) { setRules(value); setText(value.content); } }).catch(() => { if (alive) setError("项目规则加载失败，请重试。"); });
    return () => {
      alive = false;
      if (previous instanceof HTMLElement && previous.isConnected && previous.getClientRects().length) previous.focus({ preventScroll: true });
      else if (fallback?.isConnected) fallback.focus({ preventScroll: true });
    };
  }, [project.id]);
  async function reload() {
    setBusy(true);
    try { const value = await api<ProjectRules>(`/api/v1/projects/${project.id}/rules`); setRules(value); setText(value.content); setError(""); } catch { setError("项目规则加载失败，请重试。"); } finally { setBusy(false); }
  }
  async function save() {
    if (!rules || busy) return;
    setBusy(true); setError("");
    try {
      let accepted: ProjectRules;
      try { accepted = await api<ProjectRules>(`/api/v1/projects/${project.id}/rules`, { method: "PUT", body: JSON.stringify({ content: text, version: rules.version }) }); }
      catch (reason) {
        if (reason instanceof ApiError && reason.status < 500) throw reason;
        accepted = await api<ProjectRules>(`/api/v1/projects/${project.id}/rules`);
        if (accepted.content !== text) throw new Error();
      }
      setRules(accepted);
    } catch (reason) { setError(reason instanceof ApiError && reason.status === 409 ? "规则已被修改；草稿保留，重新加载后再保存。" : "保存结果未确认；草稿保留，请检查当前版本。"); }
    finally { setBusy(false); }
  }
  return <dialog ref={dialog} className="project-rules-dialog" onCancel={event => { event.preventDefault(); if (!busy) onClose(); }} aria-label={t("项目规则")}>
    <h2>{t("项目规则")} · {project.name}</h2>
    <p>{t("规则只用于之后受理的任务；不会改变运行中或已排队任务，也不会授予工具权限。")}</p>
    {rules ? <small>{t("来源：项目设置")} · v{rules.version} · SHA-256 {rules.sha256.slice(0, 12)}</small> : null}
    <label>{t("项目规则内容")}<textarea rows={10} maxLength={20000} disabled={!rules || busy} value={text} onChange={event => setText(event.target.value)} /></label>
    {error ? <p role="alert">{t(error)}</p> : null}
    <div className="interaction-actions"><button type="button" disabled={busy || !rules} onClick={() => void save()}>{t("保存")}</button><button type="button" disabled={busy} onClick={() => void reload()} title={t("重新加载将替换当前编辑草稿。")}>{t("重新加载")}</button><button type="button" disabled={busy} onClick={onClose}>{t("关闭")}</button></div>
  </dialog>;
}
export function ProjectRulesReceipt({ rules }: { rules?: ProjectRules }) {
  const t = useT();
  if (!rules?.content) return null;
  return <details className="message-context"><summary>{t("项目规则")} · {rules.project_name} · v{rules.version}</summary><small>{t("来源：项目设置")} · SHA-256 {rules.sha256}</small><pre>{rules.content}</pre></details>;
}
