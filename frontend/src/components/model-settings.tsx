"use client";

import { FormEvent, useEffect, useRef, useState } from "react";
import { Check, Plus, Server, ShieldCheck, Cpu } from "lucide-react";
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

export function ModelSettings({ admin = false }: { admin?: boolean }) {
  const base = admin ? "/api/v1/admin" : "/api/v1";
  const pending = useRef(false);
  const [editing, setEditing] = useState<Connection | Deployment | null>(null);
  const [connections, setConnections] = useState<Connection[]>([]);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [provider, setProvider] = useState("openai");
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<"connection" | "deployment" | "">("");
  const [loading, setLoading] = useState(true);

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
      await api(`${base}/model-connections`, {
        method: "POST",
        body: JSON.stringify({
          name: data.get("name"),
          provider,
          endpoint: data.get("endpoint"),
          credential: data.get("credential"),
          config: parseJSON(String(data.get("config") || "{}")),
        }),
      });
      form.reset();
      setMessage("模型连接已保存");
      await load();
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
      await api(`${base}/model-deployments`, {
        method: "POST",
        body: JSON.stringify({
          connection_id: data.get("connection"),
          name: data.get("name"),
          model_id: data.get("model"),
          is_default: data.get("default") === "on",
        }),
      });
      form.reset();
      setMessage("模型已保存");
      await load();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "模型保存失败");
    } finally {
      pending.current = false;
      setBusy("");
    }
  }

  return (
    <>
      <header className="settings-heading">
        <div>
          <p className="eyebrow">{admin ? "Administration / Models" : "Settings / Models"}</p>
          <h1>{admin ? "共享模型" : "个人模型连接"}</h1>
          <p>{admin ? "统一配置供所有成员使用的模型。密钥加密保存，不向成员公开。" : "个人连接仅供自己使用，也可以直接选用管理员提供的共享模型。"}</p>
        </div>
        <span className="secure-badge">
          <ShieldCheck />凭证加密保存
        </span>
      </header>
      {message && (
        <p className="success-banner">
          <Check />
          {message}
        </p>
      )}
      {error ? <p className="settings-error" role="alert">{error}</p> : null}
      <section className="settings-grid">
        <form className="settings-card" onSubmit={addConnection}>
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
            Provider
            <select value={provider} onChange={(event) => setProvider(event.target.value)}>
              {providers.map(([value, label, protocol]) => (
                <option key={value} value={value}>
                  {label} · {protocol}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            名称
            <input name="name" required placeholder="Production OpenAI" />
          </label>
          <label className="field">
            Endpoint（官方 Provider 可留空）
            <input name="endpoint" placeholder="https://…" />
          </label>
          <label className="field">
            Credential
            <textarea
              name="credential"
              required
              rows={3}
              placeholder={provider === "bedrock" ? "Bedrock JSON credentials" : "API key / access token"}
            />
          </label>
          <label className="field">
            Provider config JSON
            <textarea name="config" rows={3} defaultValue="{}" />
          </label>
          <button className="primary-button" disabled={busy !== ""}>
            <Plus />{busy === "connection" ? "保存中…" : "保存连接"}
          </button>
        </form>
        <form className="settings-card" onSubmit={addDeployment}>
          <header>
            <span className="card-icon">
              <Cpu />
            </span>
            <div>
              <h2>可用模型</h2>
              <p>Agent 可选择的模型</p>
            </div>
          </header>
          <label className="field">
            连接
            <select name="connection" required>
              <option value="">选择连接</option>
              {connections.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            显示名称
            <input name="name" required placeholder="Claude Sonnet" />
          </label>
          <label className="field">
            Model ID
            <input name="model" required placeholder="claude-sonnet-4-6" />
          </label>
          <label className="check-field">
            <input type="checkbox" name="default" />{admin ? "设为系统默认模型" : "设为个人默认模型"}
          </label>
          <button className="primary-button" disabled={busy !== "" || connections.length === 0}>
            <Plus />{busy === "deployment" ? "保存中…" : "保存模型"}
          </button>
        </form>
      </section>
      <section className="saved-section">
        <h2>已配置</h2>
        <div className="provider-list">
          {connections.length === 0 ? (
            <p className="muted-block">{loading ? "正在载入…" : "还没有配置模型连接。"}</p>
          ) : (
            connections.map((item) => (
              <article key={item.id}>
                <span className="status-dot" />
                <div>
                  <strong>{item.name}</strong>
                  <small>{providers.find((providerItem) => providerItem[0] === item.provider)?.[1] || item.provider}</small>
                </div>
                {admin && <button type="button" className="admin-edit" onClick={() => setEditing(item)}>编辑连接</button>}
              </article>
            ))
          )}
        </div>
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
