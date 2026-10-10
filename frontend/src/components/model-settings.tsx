"use client";
import { T, useT } from "@/components/i18n";


import { FormEvent, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { ArrowRight, Check, Plus, Server, ShieldCheck, Cpu } from "lucide-react";
import { ModelEditor } from "@/components/model-editor";
import { api, Deployment } from "@/lib/api";

export type Connection = {
  id: string;
  name: string;
  provider: string;
  protocol: string;
  endpoint: string;
  config: Record<string, unknown>;
};

const providers = [
  ["openai", "OpenAI Official", "OpenAI"],
  ["azure_openai", "Azure OpenAI", "OpenAI"],
  ["openai_compatible", "OpenAI Compatible", "OpenAI"],
  ["anthropic", "Anthropic Official", "Anthropic"],
  ["bedrock", "AWS Bedrock Claude", "Anthropic"],
  ["vertex", "Google Vertex Claude", "Anthropic"],
  ["foundry", "Microsoft Foundry Claude", "Anthropic"],
  ["anthropic_compatible", "Anthropic Compatible", "Anthropic"],
];

export function ModelSettings({ admin = false, requestedReturn = "/app" }: { admin?: boolean; requestedReturn?: string }) {
  const t = useT();

  const returnTo = /^\/app(?:\/p\/[A-Za-z0-9_-]+)?$/.test(requestedReturn) ? requestedReturn : "/app";
  const [step, setStep] = useState<"connection" | "model" | "list">("connection");
  const [selectedConnectionID, setSelectedConnectionID] = useState("");
  const base = admin ? "/api/v1/admin" : "/api/v1";
  const pending = useRef(false);
  const modelInput = useRef<HTMLInputElement>(null);
  const connectionInput = useRef<HTMLSelectElement>(null);
  const addModelButton = useRef<HTMLButtonElement>(null);
  const [editing, setEditing] = useState<Connection | Deployment | null>(null);
  const [connections, setConnections] = useState<Connection[]>([]);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [provider, setProvider] = useState("openai");
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<"connection" | "deployment" | "">("");
  const [loading, setLoading] = useState(true);

  function showSetup(next: "connection" | "model") {
    setStep(next);
    requestAnimationFrame(() => (next === "connection" ? connectionInput.current : modelInput.current)?.focus());
  }
  function showList() {
    setStep("list");
    requestAnimationFrame(() => addModelButton.current?.focus());
  }

  async function load() {
    const [c, d] = await Promise.all([
      api<{ connections: Connection[] }>(`${base}/model-connections`),
      api<{ deployments: Deployment[] }>(`${base}/model-deployments`),
    ]);
    setConnections(c.connections);
    setDeployments(d.deployments);
  }

  useEffect(() => {
    let active = true;
    void Promise.all([
      api<{ connections: Connection[] }>(`${base}/model-connections`),
      api<{ deployments: Deployment[] }>(`${base}/model-deployments`),
    ]).then(([c, d]) => {
      if (!active) return;
      setConnections(c.connections);
      setDeployments(d.deployments);
      setStep(d.deployments.some(item => item.enabled !== false) ? "list" : c.connections.length ? "model" : "connection");
    }).catch((reason: unknown) => {
      if (active) setError(reason instanceof Error ? reason.message : "模型配置加载失败");
    }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [base]);

  async function addConnection(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending.current) return;
    pending.current = true;
    const form = event.currentTarget;
    const data = new FormData(form);
    setBusy("connection");
    setError("");
    setMessage("");
    try {
      const saved = await api<Connection>(`${base}/model-connections`, {
        method: "POST",
        body: JSON.stringify({
          name: String(data.get("name") || "").trim() || providers.find(item => item[0] === provider)?.[1] || provider,
          provider,
          endpoint: data.get("endpoint"),
          credential: data.get("credential"),
          config: parseJSON(String(data.get("config") || "{}")),
        }),
      });
      form.reset();
      setConnections(previous => [...previous, saved]);
      setSelectedConnectionID(saved.id);
      setStep("model");
      setMessage("连接已保存。接下来添加你要使用的模型。");
      requestAnimationFrame(() => modelInput.current?.focus());
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "模型连接保存失败");
    } finally {
      pending.current = false;
      setBusy("");
    }
  }

  async function addDeployment(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending.current) return;
    pending.current = true;
    const form = event.currentTarget;
    const data = new FormData(form);
    setBusy("deployment");
    setError("");
    setMessage("");
    try {
      await api<Deployment>(`${base}/model-deployments`, {
        method: "POST",
        body: JSON.stringify({
          connection_id: data.get("connection"),
          name: String(data.get("name") || "").trim() || String(data.get("model") || "").trim(),
          model_id: data.get("model"),
          is_default: data.get("default") === "on",
        }),
      });
      form.reset();
      setMessage("模型已保存，可以返回工作区开始任务。");
      try {
        await load();
        if (!admin) showList();
      } catch {
        setError("模型已保存，但列表刷新失败。请刷新配置，无需再次提交。");
      }
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "模型保存失败");
    } finally {
      pending.current = false;
      setBusy("");
    }
  }

  const selectedConnection = connections.find(item => item.id === selectedConnectionID) || connections[0];
  const needsEndpoint = ["openai_compatible", "anthropic_compatible"].includes(provider);
  const structuredCredential = provider === "bedrock";
  const cloudConfiguration = ["azure_openai", "bedrock", "vertex", "foundry"].includes(provider);
  const hasAvailableModel = deployments.some(item => item.enabled !== false);

  const savedConnections = (
        <details className="saved-connections" open={admin ? true : undefined}>
        <summary><T>{"服务商连接 ·"}</T>{connections.length}</summary>
        <div className="provider-list">
          {connections.length === 0 ? (
            <p className="muted-block">{loading ? t("正在载入…") : t("还没有配置模型连接。")}</p>
          ) : (
            connections.map((item) => (
              <article key={item.id}>
                <span className="status-dot" aria-label={t("已保存")} />
                <div>
                  <strong>{item.name}</strong>
                  <small>{providers.find((providerItem) => providerItem[0] === item.provider)?.[1] || item.provider}</small>
                </div>
                {admin && <button type="button" className="admin-edit" onClick={() => setEditing(item)}><T>{"编辑连接"}</T></button>}
              </article>
            ))
          )}
        </div>
        {!admin ? <button type="button" className="text-button" disabled={loading || busy !== ""} onClick={() => showSetup("connection")}><Plus size={15} /><T>{"添加连接"}</T></button> : null}
        </details>
  );
  const savedDeployments = (
        <div className="deployment-list">
          {deployments.map((item) => (
            <article key={item.id}>
              <div>
                <strong>{item.name}</strong>
                <small>{item.model_id}</small>
              </div>
              <div className="admin-model-actions">{item.shared && !admin && <span><T>{"共享"}</T></span>}{item.is_default && <span><T>{"默认"}</T></span>}{item.enabled === false && <span><T>{"已停用"}</T></span>}{admin && <button type="button" className="admin-edit" onClick={() => setEditing(item)}><T>{"编辑模型"}</T></button>}</div>
            </article>
          ))}
        </div>
  );

  return (
    <>
      <header className="settings-heading">
        <div>
          <p className="eyebrow">{admin ? "Administration / Models" : "Settings / Models"}</p>
          <h1>{admin ? t("共享模型") : hasAvailableModel ? t("模型") : t("准备你的模型")}</h1>
          <p>{admin ? t("统一配置供所有成员使用的模型。密钥加密保存，不向成员公开。") : t("个人连接仅供自己使用，也可以直接选用管理员提供的共享模型。")}</p>
        </div>
        <span className="secure-badge">
          <ShieldCheck /><T>{"凭证加密保存"}</T></span>
      </header>
      {message && (
        <p className="success-banner" role="status">
          <Check />
          {message}
        </p>
      )}
      {error ? <div className="settings-error" role="alert">{t(error)}<button type="button" className="text-button" disabled={busy !== ""} onClick={async () => { setError(""); try { await load(); } catch { setError("配置刷新失败，请稍后重试。"); } }}><T>{"刷新配置"}</T></button></div> : null}
      {!admin && <>
        {step !== "list" && !loading ? <div className="model-setup-navigation">
        <ol className="model-setup-steps" aria-label={t("模型配置步骤")}>
          <li><button type="button" aria-current={step === "connection" ? "step" : undefined} disabled={busy !== "" || loading} onClick={() => showSetup("connection")}><span>{connections.length ? <Check size={15} /> : "1"}</span><T>{"连接服务商"}</T></button></li>
          <li><button type="button" aria-current={step === "model" ? "step" : undefined} disabled={!connections.length || busy !== "" || loading} onClick={() => showSetup("model")}><span>{hasAvailableModel ? <Check size={15} /> : "2"}</span><T>{"添加模型"}</T></button></li>
          <li className={hasAvailableModel ? "complete" : ""}><span>3</span><T>{"开始任务"}</T></li>
        </ol>
        {hasAvailableModel ? <button type="button" className="text-button" disabled={busy !== ""} onClick={showList}><T>{"返回模型列表"}</T></button> : null}
        </div> : null}
        {hasAvailableModel && step === "list" && <div className="model-ready"><div><strong><T>{"可以开始任务了"}</T></strong><p><T>{"配置已保存；实际调用将在任务运行时确认。"}</T></p></div><Link className="primary-button" href={returnTo}><T>{"返回工作区"}</T><ArrowRight size={16} /></Link></div>}
      </>}
      {loading && <p role="status" className="field-help"><T>{"正在加载模型配置…"}</T></p>}
      <section className={admin ? "settings-grid" : "settings-grid model-setup-grid"}>
        <form className="settings-card" onSubmit={addConnection} hidden={loading || (!admin && step !== "connection")} aria-label={t("服务商连接")} aria-busy={busy === "connection"}>
          <header>
            <span className="card-icon">
              <Server />
            </span>
            <div>
              <h2><T>{"服务商连接"}</T></h2>
              <p><T>{"端点、协议与密钥"}</T></p>
            </div>
          </header>
          <label className="field">
            <T>{"模型服务商"}</T><select ref={connectionInput} value={provider} disabled={busy !== "" || loading} onChange={(event) => setProvider(event.target.value)}>
              {providers.map(([value, label, protocol]) => (
                <option key={value} value={value}>
                  {label} · {protocol}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <T>{"连接名称（可选）"}</T><input name="name" maxLength={120} disabled={busy !== "" || loading} placeholder={providers.find(item => item[0] === provider)?.[1]} />
          </label>
          {needsEndpoint && <label className="field"><T>{"服务地址（Endpoint）"}</T><input name="endpoint" type="url" required disabled={busy !== "" || loading} placeholder={t("https://你的模型服务地址")} /><small className="field-help"><T>{"填写服务商提供的完整接口地址。"}</T></small></label>}
          <label className="field">
            {structuredCredential ? t("服务商凭证（JSON）") : provider === "vertex" ? "Access Token" : "API Key"}
            {structuredCredential ? <textarea name="credential" required rows={4} disabled={busy !== "" || loading} autoComplete="off" placeholder={t("填写服务商要求的 JSON 凭证")} /> : <input name="credential" type="password" required disabled={busy !== "" || loading} autoComplete="off" placeholder={t("粘贴你的 API Key")} />}
            <small className="field-help"><T>{"凭证加密保存，仅用于调用你配置的模型服务。"}</T></small>
          </label>
          <details className="model-advanced" key={provider} open={cloudConfiguration ? true : undefined}>
            <summary><T>{"高级设置"}</T><span><T>{"自定义地址与服务商参数"}</T></span></summary>
            {!needsEndpoint && <label className="field"><T>{"自定义服务地址（可选）"}</T><input name="endpoint" type="url" disabled={busy !== "" || loading} placeholder={cloudConfiguration ? t("留空由服务商参数生成") : t("留空使用官方地址")} /></label>}
            <label className="field"><T>{"服务商参数（JSON）"}</T><textarea name="config" rows={3} defaultValue="{}" disabled={busy !== "" || loading} /><small className="field-help"><T>{"按服务商要求填写，例如云服务的区域、项目或 API 版本；无需额外参数时保留空对象。"}</T></small></label>
          </details>
          <button className="primary-button" disabled={busy !== "" || loading}>
            <Plus />{busy === "connection" ? t("保存中…") : admin ? t("保存连接") : t("保存连接，继续")}
          </button>
        </form>
        <form className="settings-card" onSubmit={addDeployment} hidden={loading || (!admin && step !== "model")} aria-label={t("添加模型")} aria-busy={busy === "deployment"}>
          <header>
            <span className="card-icon">
              <Cpu />
            </span>
            <div>
              <h2><T>{"添加模型"}</T></h2>
              <p><T>{"选择连接，填写模型标识"}</T></p>
            </div>
          </header>
          <label className="field">
            <T>{"连接"}</T><select name="connection" required disabled={busy !== "" || loading} value={selectedConnection?.id || ""} onChange={event => setSelectedConnectionID(event.target.value)}>
              <option value=""><T>{"选择连接"}</T></option>
              {connections.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <T>{"显示名称（可选）"}</T><input name="name" maxLength={120} disabled={busy !== "" || loading} placeholder={t("留空使用 Model ID，例如 Claude Sonnet")} />
          </label>
          <label className="field">
            Model ID
            <input ref={modelInput} name="model" required maxLength={240} disabled={busy !== "" || loading} placeholder={t("填写服务商提供的模型标识")} />
            <small className="field-help"><T>{"从服务商控制台复制模型标识。Azure OpenAI 使用部署名称；它与上面的显示名称不同。"}</T></small>
          </label>
          <label className="check-field">
            <input key={deployments.length === 0 ? "first-model" : "additional-model"} type="checkbox" name="default" defaultChecked={deployments.length === 0} disabled={busy !== "" || loading} />{admin ? t("设为系统默认模型") : t("设为个人默认模型")}
          </label>
          <button className="primary-button" disabled={busy !== "" || loading || connections.length === 0}>
            <Plus />{busy === "deployment" ? t("保存中…") : t("保存模型")}
          </button>
        </form>
      </section>
      <section className={`saved-section ${admin ? "" : "personal-model-list"}`}>
        <header className="model-list-toolbar"><h2>{admin ? t("已保存的配置") : t("已保存的模型")}</h2>{!admin && step === "list" ? <button type="button" className="primary-button" disabled={loading || busy !== ""} ref={addModelButton} onClick={() => showSetup(connections.length ? "model" : "connection")}><Plus size={16} /><T>{"添加模型"}</T></button> : null}</header>
        {admin ? savedConnections : null}
        {savedDeployments}
        {!admin ? savedConnections : null}
      </section>
      {editing && <ModelEditor item={editing} connections={connections} onClose={() => setEditing(null)} onSaved={async () => { setEditing(null); setMessage("配置已更新"); try { await load(); } catch (reason) { setError(reason instanceof Error ? reason.message : "刷新失败"); } }} />}
    </>
  );
}

function parseJSON(value: string) {
  try {
    const result: unknown = JSON.parse(value);
    if (!result || typeof result !== "object" || Array.isArray(result)) throw new Error("配置必须是 JSON 对象");
    return result;
  } catch {
    throw new Error("Provider config 不是合法 JSON");
  }
}
