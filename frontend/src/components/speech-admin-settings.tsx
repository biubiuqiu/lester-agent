"use client";

import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { Check, Mic, RefreshCw } from "lucide-react";
import { ApiError, api } from "@/lib/api";
import { useT } from "./i18n";
import { serviceForSpeechConfig, speechService, speechServices, type SpeechProvider } from "@/lib/speech-providers";

type SpeechSettings = {
  enabled: boolean;
  provider: SpeechProvider;
  endpoint: string;
  model_id: string;
  has_credential: boolean;
  revision: number;
};
type SpeechDraft = Pick<SpeechSettings, "enabled" | "provider" | "endpoint" | "model_id">;

function sameOrigin(left: string, right: string) {
  try { return new URL(left).origin === new URL(right).origin; }
  catch { return false; }
}
function draftOf(settings: SpeechSettings): SpeechDraft {
  return { enabled: settings.enabled, provider: settings.provider, endpoint: settings.endpoint, model_id: settings.model_id };
}

export function SpeechAdminSettings() {
  const t = useT();
  const modelsId = useId();
  const [settings, setSettings] = useState<SpeechSettings | null>(null);
  const [draft, setDraft] = useState<SpeechDraft | null>(null);
  const [credential, setCredential] = useState("");
  const [clearCredential, setClearCredential] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [conflict, setConflict] = useState(false);
  const pending = useRef(false), mounted = useRef(true);
  const controller = useRef<AbortController | null>(null);

  useEffect(() => {
    mounted.current = true;
    const request = new AbortController();
    controller.current = request;
    api<SpeechSettings>("/api/v1/admin/speech", { signal: request.signal })
      .then(value => { if (!request.signal.aborted) { setSettings(value); setDraft(draftOf(value)); } })
      .catch(reason => { if (!request.signal.aborted) setError(reason instanceof Error ? reason.message : "语音配置加载失败，请重试。"); })
      .finally(() => { if (!request.signal.aborted) setLoading(false); });
    return () => { mounted.current = false; controller.current?.abort(); };
  }, []);

  async function readLatest() {
    if (loading || pending.current) return;
    const request = new AbortController();
    controller.current = request;
    setLoading(true); setError(""); setNotice("");
    try {
      const value = await api<SpeechSettings>("/api/v1/admin/speech", { signal: request.signal });
      if (!mounted.current || request.signal.aborted) return;
      setSettings(value);
      if (!draft) setDraft(draftOf(value));
      else setNotice("已读取最新版本，编辑内容已保留，请核对后再保存。");
      setConflict(false);
    } catch (reason) {
      if (mounted.current && !request.signal.aborted) setError(reason instanceof Error ? reason.message : "语音配置加载失败，请重试。");
    } finally { if (mounted.current && !request.signal.aborted) setLoading(false); }
  }

  const savedCredentialApplies = !!settings?.has_credential && !!draft && settings.provider === draft.provider && sameOrigin(settings.endpoint, draft.endpoint) && !clearCredential;
  const credentialScopeChanged = !!settings?.has_credential && !!draft && (settings.provider !== draft.provider || !sameOrigin(settings.endpoint, draft.endpoint));
  const dirty = !!settings && !!draft && (JSON.stringify(draftOf(settings)) !== JSON.stringify(draft) || credential.length > 0 || clearCredential);
  const service = draft ? serviceForSpeechConfig(draft) : speechServices[0];

  function changeProvider(id: string) {
    if (!draft || pending.current) return;
    const service = speechService(id);
    if (!service) return;
    setDraft({ ...draft, provider: service.provider, endpoint: service.endpoint, model_id: service.model });
    setCredential(""); setClearCredential(false); setNotice("");
  }
  function changeEndpoint(endpoint: string) {
    if (!draft || pending.current) return;
    if (!sameOrigin(draft.endpoint, endpoint)) setCredential("");
    setDraft({ ...draft, endpoint }); setNotice("");
  }
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!settings || !draft || loading || pending.current || conflict) return;
    if (draft.enabled && (!draft.endpoint.trim() || !draft.model_id.trim() || !credential.trim() && !savedCredentialApplies)) {
      setError("开启语音输入前，请填写服务商、模型和密钥。"); return;
    }
    pending.current = true;
    const request = new AbortController();
    controller.current = request;
    setSaving(true); setError(""); setNotice("");
    try {
      const value = await api<SpeechSettings>("/api/v1/admin/speech", {
        method: "PATCH", signal: request.signal,
        body: JSON.stringify({
          revision: settings.revision, ...draft,
          endpoint: draft.endpoint.trim(), model_id: draft.model_id.trim(),
          ...(credential.trim() ? { credential: credential.trim() } : {}),
          ...(clearCredential ? { clear_credential: true } : {}),
        }),
      });
      if (!mounted.current || request.signal.aborted) return;
      setSettings(value); setDraft(draftOf(value)); setCredential(""); setClearCredential(false);
      setNotice("语音配置已保存；实际可用性需通过录音验证。");
    } catch (reason) {
      if (!mounted.current || request.signal.aborted) return;
      if (reason instanceof ApiError && reason.status === 409) { setConflict(true); setError("语音配置已被其他管理员修改，请先读取最新版本。"); }
      else setError(reason instanceof Error ? reason.message : "语音配置保存失败，编辑内容已保留。");
    } finally {
      pending.current = false;
      if (mounted.current && !request.signal.aborted) setSaving(false);
    }
  }

  return <div className="speech-admin">
    <header className="settings-heading"><div><h1>{t("语音输入")}</h1><p>{t("配置转写服务，供所有工作区使用。")}</p></div></header>
    {error ? <p className="settings-error" role="alert">{t(error)}{!settings || conflict ? <button type="button" className="text-button" disabled={loading || saving} onClick={() => void readLatest()}><RefreshCw size={14} />{t(conflict ? "读取最新版本" : "重试")}</button> : null}</p> : null}
    {notice ? <p className="success-banner" role="status"><Check size={14} />{t(notice)}</p> : null}
    {loading && !draft ? <p className="muted-block" role="status">{t("正在加载语音配置…")}</p> : null}
    {settings && draft ? <form className="speech-admin-form" onSubmit={save} aria-busy={loading || saving}>
      <fieldset disabled={loading || saving}>
        <label className="speech-admin-enable"><span><strong>{t("启用语音输入")}</strong><small>{t("用户录音转写为可编辑草稿，确认发送后才开始任务。")}</small></span><input type="checkbox" checked={draft.enabled} onChange={event => { setDraft({ ...draft, enabled: event.target.checked }); setNotice(""); }} /></label>
        <div className="speech-admin-fields">
          <label className="field">{t("转写服务")}<select aria-label={t("转写服务")} value={service.id} onChange={event => changeProvider(event.target.value)}>{speechServices.map(item => <option key={item.id} value={item.id}>{t(item.label)}</option>)}</select></label>
          <p className="admin-help">{t("模型和语言范围以服务商支持为准。")}</p>
          <label className="field">{t("转写模型")}<input name="speech-model" value={draft.model_id} required={draft.enabled} maxLength={200} placeholder={service.model} list={modelsId} autoComplete="off" spellCheck={false} onChange={event => { setDraft({ ...draft, model_id: event.target.value }); setNotice(""); }} /></label>
          <datalist id={modelsId}>{service.models.map(model => <option key={model} value={model} />)}</datalist>
          <label className="field">{t("API 密钥")}<input name="speech-credential" type="password" value={credential} autoComplete="new-password" spellCheck={false} maxLength={8192} disabled={loading || saving || clearCredential} placeholder={t(savedCredentialApplies ? "已保存密钥，留空保留" : "输入服务商 API 密钥")} onChange={event => { setCredential(event.target.value); setNotice(""); }} /></label>
          <p className="admin-help">{t("密钥仅加密保存在服务端，不会返回浏览器。")}</p>
          {credentialScopeChanged ? <p className="speech-admin-key-notice" role="status">{t("切换服务商或接口域名后，需要重新填写密钥。")}</p> : null}
          {settings.has_credential ? <div className="speech-admin-clear">{clearCredential ? <><span>{t("已选择清除密钥，保存后生效。")}</span><button className="text-button" type="button" onClick={() => setClearCredential(false)}>{t("撤销清除")}</button></> : <button className="text-button" type="button" onClick={() => { setClearCredential(true); setCredential(""); setDraft({ ...draft, enabled: false }); setNotice(""); }}>{t("清除已保存的密钥")}</button>}</div> : null}
          <details className="speech-admin-advanced"><summary>{t("高级设置")}</summary><label className="field">{t("接口基础地址")}<input type="url" value={draft.endpoint} required={draft.enabled} maxLength={2048} autoComplete="off" spellCheck={false} readOnly={draft.provider !== "openai"} onChange={event => changeEndpoint(event.target.value)} /></label><p className="admin-help">{t(draft.provider !== "openai" ? "使用服务商官方转写接口。" : "填写 API 基础地址，不包含 /audio/transcriptions。")}</p></details>
        </div>
        <footer><span className="admin-help"><Mic size={14} />{t("自动识别录音语言；语音模型独立于聊天模型。")}</span><button className="primary-button" type="submit" disabled={!dirty || conflict || loading || saving}>{t(saving ? "保存中…" : "保存")}</button></footer>
      </fieldset>
    </form> : null}
  </div>;
}
