"use client";

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
        <summary>服务商连接 · {connections.length}</summary>
        <div className="provider-list">
          {connections.length === 0 ? (
            <p className="muted-block">{loading ? "正在载入…" : "还没有配置模型连接。"}</p>
          ) : (
            connections.map((item) => (
              <article key={item.id}>
                <span className="status-dot" aria-label="已保存" />
                <div>
                  <strong>{item.name}</strong>
                  <small>{providers.find((providerItem) => providerItem[0] === item.provider)?.[1] || item.provider}</small>
                </div>
                {admin && <button type="button" className="admin-edit" onClick={() => setEditing(item)}>编辑连接</button>}
              </article>
            ))
          )}
        </div>
        {!admin ? <button type="button" className="text-button" disabled={loading || busy !== ""} onClick={() => showSetup("connection")}><Plus size={15} />添加连接</button> : null}
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
              <div className="admin-model-actions">{item.shared && !admin && <span>共享</span>}{item.is_default && <span>默认</span>}{item.enabled === false && <span>已停用</span>}{admin && <button type="button" className="admin-edit" onClick={() => setEditing(item)}>编辑模型</button>}</div>
            </article>
          ))}
        </div>
  );

  return (
    <>
      <header className="settings-heading">
        <div>
          <p className="eyebrow">{admin ? "Administration / Models" : "Settings / Models"}</p>
          <h1>{admin ? "共享模型" : hasAvailableModel ? "模型" : "准备你的模型"}</h1>
          <p>{admin ? "统一配置供所有成员使用的模型。密钥加密保存，不向成员公开。" : "个人连接仅供自己使用，也可以直接选用管理员提供的共享模型。"}</p>
        </div>
        <span className="secure-badge">
          <ShieldCheck />凭证加密保存
        </span>
      </header>
      {message && (
        <p className="success-banner" role="status">
          <Check />
          {message}
        </p>
      )}
      {error ? <div className="settings-error" role="alert">{error}<button type="button" className="text-button" disabled={busy !== ""} onClick={async () => { setError(""); try { await load(); } catch { setError("配置刷新失败，请稍后重试。"); } }}>刷新配置</button></div> : null}
      {!admin && <>
        {step !== "list" && !loading ? <div className="model-setup-navigation">
        <ol className="model-setup-steps" aria-label="模型配置步骤">
          <li><button type="button" aria-current={step === "connection" ? "step" : undefined} disabled={busy !== "" || loading} onClick={() => showSetup("connection")}><span>{connections.length ? <Check size={15} /> : "1"}</span>连接服务商</button></li>
          <li><button type="button" aria-current={step === "model" ? "step" : undefined} disabled={!connections.length || busy !== "" || loading} onClick={() => showSetup("model")}><span>{hasAvailableModel ? <Check size={15} /> : "2"}</span>添加模型</button></li>
          <li className={hasAvailableModel ? "complete" : ""}><span>3</span>开始任务</li>
        </ol>
        {hasAvailableModel ? <button type="button" className="text-button" disabled={busy !== ""} onClick={showList}>返回模型列表</button> : null}
        </div> : null}
        {hasAvailableModel && step === "list" && <div className="model-ready"><div><strong>可以开始任务了</strong><p>配置已保存；实际调用将在任务运行时确认。</p></div><Link className="primary-button" href={returnTo}>返回工作区 <ArrowRight size={16} /></Link></div>}
      </>}
      {loading && <p role="status" className="field-help">正在加载模型配置…</p>}
      <section className={admin ? "settings-grid" : "settings-grid model-setup-grid"}>
        <form className="settings-card" onSubmit={addConnection} hidden={loading || (!admin && step !== "connection")} aria-label="服务商连接" aria-busy={busy === "connection"}>
          <header>
            <span className="card-icon">
              <Server />
            </span>
            <div>
              <h2>服务商连接</h2>
              <p>端点、协议与密钥</p>
            </div>
          </header>
          <label className="field">
            模型服务商
            <select ref={connectionInput} value={provider} disabled={busy !== "" || loading} onChange={(event) => setProvider(event.target.value)}>
              {providers.map(([value, label, protocol]) => (
                <option key={value} value={value}>
                  {label} · {protocol}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            连接名称（可选）
            <input name="name" maxLength={120} disabled={busy !== "" || loading} placeholder={providers.find(item => item[0] === provider)?.[1]} />
          </label>
          {needsEndpoint && <label className="field">服务地址（Endpoint）<input name="endpoint" type="url" required disabled={busy !== "" || loading} placeholder="https://你的模型服务地址" /><small className="field-help">填写服务商提供的完整接口地址。</small></label>}
          <label className="field">
            {structuredCredential ? "服务商凭证（JSON）" : provider === "vertex" ? "Access Token" : "API Key"}
            {structuredCredential ? <textarea name="credential" required rows={4} disabled={busy !== "" || loading} autoComplete="off" placeholder="填写服务商要求的 JSON 凭证" /> : <input name="credential" type="password" required disabled={busy !== "" || loading} autoComplete="off" placeholder="粘贴你的 API Key" />}
            <small className="field-help">凭证加密保存，仅用于调用你配置的模型服务。</small>
          </label>
          <details className="model-advanced" key={provider} open={cloudConfiguration ? true : undefined}>
            <summary>高级设置<span>自定义地址与服务商参数</span></summary>
            {!needsEndpoint && <label className="field">自定义服务地址（可选）<input name="endpoint" type="url" disabled={busy !== "" || loading} placeholder={cloudConfiguration ? "留空由服务商参数生成" : "留空使用官方地址"} /></label>}
            <label className="field">服务商参数（JSON）<textarea name="config" rows={3} defaultValue="{}" disabled={busy !== "" || loading} /><small className="field-help">按服务商要求填写，例如云服务的区域、项目或 API 版本；无需额外参数时保留空对象。</small></label>
          </details>
          <button className="primary-button" disabled={busy !== "" || loading}>
            <Plus />{busy === "connection" ? "保存中…" : admin ? "保存连接" : "保存连接，继续"}
          </button>
        </form>
        <form className="settings-card" onSubmit={addDeployment} hidden={loading || (!admin && step !== "model")} aria-label="添加模型" aria-busy={busy === "deployment"}>
          <header>
            <span className="card-icon">
              <Cpu />
            </span>
            <div>
              <h2>添加模型</h2>
              <p>选择连接，填写模型标识</p>
            </div>
          </header>
          <label className="field">
            连接
            <select name="connection" required disabled={busy !== "" || loading} value={selectedConnection?.id || ""} onChange={event => setSelectedConnectionID(event.target.value)}>
              <option value="">选择连接</option>
              {connections.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            显示名称（可选）
            <input name="name" maxLength={120} disabled={busy !== "" || loading} placeholder="留空使用 Model ID，例如 Claude Sonnet" />
          </label>
          <label className="field">
            Model ID
            <input ref={modelInput} name="model" required maxLength={240} disabled={busy !== "" || loading} placeholder="填写服务商提供的模型标识" />
            <small className="field-help">从服务商控制台复制模型标识。Azure OpenAI 使用部署名称；它与上面的显示名称不同。</small>
          </label>
          <label className="check-field">
            <input key={deployments.length === 0 ? "first-model" : "additional-model"} type="checkbox" name="default" defaultChecked={deployments.length === 0} disabled={busy !== "" || loading} />{admin ? "设为系统默认模型" : "设为个人默认模型"}
          </label>
          <button className="primary-button" disabled={busy !== "" || loading || connections.length === 0}>
            <Plus />{busy === "deployment" ? "保存中…" : "保存模型"}
          </button>
        </form>
      </section>
      <section className={`saved-section ${admin ? "" : "personal-model-list"}`}>
        <header className="model-list-toolbar"><h2>{admin ? "已保存的配置" : "已保存的模型"}</h2>{!admin && step === "list" ? <button type="button" className="primary-button" disabled={loading || busy !== ""} ref={addModelButton} onClick={() => showSetup(connections.length ? "model" : "connection")}><Plus size={16} />添加模型</button> : null}</header>
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
