"use client";

import { FormEvent, useEffect, useRef, useState } from "react";
import { T, useT } from "@/components/i18n";
import { api, type Deployment, type ModelVerification } from "@/lib/api";

const verificationErrors: Record<string, string> = {
  authentication: "验证失败：请检查服务商凭证。",
  rate_limit: "验证失败：服务商限流，请稍后再试。",
  context_length_exceeded: "验证失败：服务商拒绝了请求长度。",
  cancelled: "验证已取消。",
  transport: "验证失败：无法连接服务商。",
  protocol: "验证失败：服务商返回格式不兼容。",
};

export function ModelDeploymentControls({ item, admin, onSaved }: { item: Deployment; admin: boolean; onSaved: () => Promise<void> }) {
  const t = useT();
  const base = admin ? "/api/v1/admin" : "/api/v1";
  const [checking, setChecking] = useState(false);
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const pendingCheck = useRef(false);
  const pendingSave = useRef(false);
  const abortCheck = useRef<AbortController | null>(null);
  useEffect(() => () => abortCheck.current?.abort(), []);

  async function verify() {
    if (pendingCheck.current) return;
    pendingCheck.current = true;
    setChecking(true); setNotice(""); setError("");
    const controller = new AbortController();
    abortCheck.current = controller;
    try {
      const result = await api<ModelVerification>(`${base}/model-deployments/${item.id}/test`, { method: "POST", signal: controller.signal });
      if (controller.signal.aborted) return;
      if (result.ok && result.verified && result.status === "verified_text_call" && result.capability_scope === "text_call_only") {
        setNotice("文本调用已验证；工具与附件尚未验证。");
      } else {
        setError(verificationErrors[result.error_kind || ""] || "验证失败，请检查模型与服务商配置。");
      }
    } catch {
      if (!controller.signal.aborted) setError("验证结果未确认，请稍后再试。");
    } finally {
      if (!controller.signal.aborted) setChecking(false);
      pendingCheck.current = false;
    }
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pendingSave.current) return;
    const form = new FormData(event.currentTarget);
    const budget = Object.fromEntries(["context_window", "output_reserve", "policy_window"].map(name => [name, Number(form.get(name))]));
    if (Object.values(budget).some(value => !Number.isSafeInteger(value) || value < 0 || value > 2147483647)) {
      setError("预算必须为非负整数。"); return;
    }
    pendingSave.current = true;
    setSaving(true); setNotice(""); setError("");
    try {
      await api(`${base}/model-deployments/${item.id}`, { method: "PATCH", body: JSON.stringify(budget) });
      setNotice("模型预算已保存。");
      try { await onSaved(); } catch { setError("预算已保存，但列表刷新失败。请刷新配置。"); }
    } catch { setError("模型预算保存失败，请稍后重试。"); }
    finally { pendingSave.current = false; setSaving(false); }
  }

  return <div className="model-deployment-controls">
    <button type="button" className="text-button" disabled={checking || item.enabled === false} onClick={verify}>{checking ? t("正在验证文本调用…") : t("验证连接")}</button>
    <small className="field-help"><T>{"会向服务商发送一次短文本请求，可能产生费用。"}</T></small>
    {notice && <p role="status" className="field-help">{t(notice)}</p>}
    {error && <p role="alert" className="settings-error">{t(error)}</p>}
    {(!item.shared || admin) && <details className="model-budget-settings">
      <summary><T>{"上下文预算"}</T></summary>
      <form onSubmit={save} aria-label={t("上下文预算")} aria-busy={saving}>
        <label className="field"><T>{"模型上下文窗口（tokens）"}</T><input name="context_window" type="number" min={0} max={2147483647} step={1} required defaultValue={item.context_window || 0} disabled={saving} /></label>
        <label className="field"><T>{"输出预留（tokens）"}</T><input name="output_reserve" type="number" min={0} max={2147483647} step={1} required defaultValue={item.output_reserve || 0} disabled={saving} /></label>
        <label className="field"><T>{"本地上下文策略（tokens）"}</T><input name="policy_window" type="number" min={0} max={2147483647} step={1} required defaultValue={item.policy_window || 0} disabled={saving} /></label>
        <p className="field-help"><T>{"0 表示窗口未知、自动预留或默认 64k 策略。策略预算不代表模型上限，预留不会限制普通回复长度。"}</T></p>
        <button type="submit" className="text-button" disabled={saving}>{saving ? t("保存中…") : t("保存预算")}</button>
      </form>
    </details>}
  </div>;
}
