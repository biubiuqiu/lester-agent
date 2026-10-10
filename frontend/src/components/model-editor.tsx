"use client";
import { T, useT } from "@/components/i18n";


import { FormEvent, useEffect, useRef, useState } from "react";
import { X } from "lucide-react";
import { api, type Deployment } from "@/lib/api";
import type { Connection } from "./model-settings";

export function ModelEditor({ item, connections, onClose, onSaved }: { item: Connection | Deployment; connections: Connection[]; onClose: () => void; onSaved: () => Promise<void> }) {
  const t = useT();

  const dialog = useRef<HTMLDialogElement>(null);
  const pending = useRef(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const deployment = "model_id" in item ? item : null;
  const connection = "provider" in item ? item : null;
  useEffect(() => { const origin = document.activeElement as HTMLElement; dialog.current?.showModal(); return () => origin?.focus(); }, []);
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (pending.current) return;
    pending.current = true; setBusy(true); setError("");
    const data = new FormData(event.currentTarget);
    try {
      const body = deployment ? { name: data.get("name"), connection_id: data.get("connection"), model_id: data.get("model"), enabled: data.get("enabled") === "on", is_default: data.get("default") === "on" } : { name: data.get("name"), endpoint: data.get("endpoint"), credential: data.get("credential"), config: JSON.parse(String(data.get("config") || "{}")) };
      if ("config" in body && (!body.config || typeof body.config !== "object" || Array.isArray(body.config))) throw new Error("配置必须是 JSON 对象");
      await api(`/api/v1/admin/${deployment ? "model-deployments" : "model-connections"}/${item.id}`, { method: "PATCH", body: JSON.stringify(body) });
      await onSaved();
    } catch (reason) { setError(reason instanceof Error ? reason.message : "保存失败"); }
    finally { pending.current = false; setBusy(false); }
  }
  return <dialog ref={dialog} className="admin-dialog" onCancel={(e) => { e.preventDefault(); if (!pending.current) onClose(); }}><form onSubmit={save}>
    <header><h2>{deployment ? t("编辑共享模型") : t("编辑模型连接")}</h2><button type="button" aria-label={t("关闭")} disabled={busy} onClick={onClose}><X size={20} /></button></header>
    {error && <p className="settings-error" role="alert">{t(error)}</p>}
    <label className="field"><T>{"名称"}</T><input name="name" required maxLength={120} defaultValue={item.name} autoFocus /></label>
    {deployment && <>
      <label className="field"><T>{"连接"}</T><select name="connection" defaultValue={deployment.connection_id} required>{connections.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}</select></label>
      <label className="field">Model ID<input name="model" required maxLength={240} defaultValue={deployment.model_id} /></label>
      <label className="check-field"><input type="checkbox" name="enabled" defaultChecked={deployment.enabled} /><T>{"启用模型"}</T></label>
      <label className="check-field"><input type="checkbox" name="default" defaultChecked={deployment.is_default} /><T>{"设为系统默认模型"}</T></label>
      <p className="admin-help"><T>{"停用后，成员无法再使用此模型发起请求，历史会话会保留。已开始的请求不受影响。"}</T></p>
    </>}
    {connection && <>
      <p className="admin-help"><T>{"服务商："}</T>{connection.provider}</p>
      <label className="field">Endpoint<input name="endpoint" defaultValue={connection.endpoint} /></label>
      <label className="field"><T>{"新凭证（留空保留原凭证）"}</T><textarea name="credential" rows={3} autoComplete="off" placeholder={t("不会显示已保存的密钥")} /></label>
      <label className="field">Provider config JSON<textarea name="config" rows={4} defaultValue={JSON.stringify(connection.config || {}, null, 2)} /></label>
    </>}
    <footer><button type="button" disabled={busy} onClick={onClose}><T>{"取消"}</T></button><button className="primary-button" disabled={busy}>{busy ? t("保存中…") : t("保存")}</button></footer>
  </form></dialog>;
}
