"use client";

import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { BookOpen, Check, ExternalLink, LoaderCircle, RefreshCw, X } from "lucide-react";
import Link from "next/link";
import { api, ApiError } from "@/lib/api";
import { appAuthorizationURL, appProviders, parseAppConnections, type AppConnection, type AppProvider } from "@/lib/app-connections";
import { nativeShellSnapshot, nativeShellServerSnapshot, subscribeNativeShell } from "@/lib/native-shell";
import { workspaceReturnPath } from "@/lib/workspace-return";
import { useT } from "./i18n";
import { AppConnectorIcon } from "./app-connector-icon";

const names: Record<AppProvider, string> = { github: "GitHub", gmail: "Gmail", x: "X", feishu: "飞书", slack: "Slack" };
const descriptions: Record<AppProvider, string> = {
  github: "搜索仓库、Issue 与代码，读取已授权仓库内容。",
  gmail: "搜索邮件，按需读取正文；不会发送邮件或标记已读。",
  x: "搜索近期帖子并读取内容，取决于 X API 套餐。",
  feishu: "搜索群聊，读取群消息与已授权的新版文档。",
  slack: "搜索消息并读取可访问的会话线程。",
};
const callbackErrors: Record<string, string> = {
  invalid_state: "连接已过期或不属于当前浏览器，请重新连接。",
  session_expired: "请重新登录 Lester 后再连接应用。",
  access_denied: "你取消了应用授权，可以随时重新连接。",
  state_changed: "连接状态已变化，请刷新后重新连接。",
  unavailable: "此应用尚未配置，请联系管理员",
  provider_error: "应用授权未完成，请检查配置后重新连接。",
};

export function AppConnections() {
  const t = useT(), native = useSyncExternalStore(subscribeNativeShell, nativeShellSnapshot, nativeShellServerSnapshot);
  const [connections, setConnections] = useState<AppConnection[] | null>(null), [loading, setLoading] = useState(true);
  const [error, setError] = useState(""), [loadError, setLoadError] = useState(""), [notice, setNotice] = useState<{ kind: "connected" | "disconnected"; provider: AppProvider } | null>(null);
  const [busy, setBusy] = useState<AppProvider | "">(""), [selected, setSelected] = useState<AppConnection | null>(null), [dialogError, setDialogError] = useState(""), [invalidRevisions, setInvalidRevisions] = useState<Partial<Record<AppProvider, number>>>({});
  const mounted = useRef(true), generation = useRef(0), pending = useRef(false), dialog = useRef<HTMLDialogElement>(null), trigger = useRef<HTMLElement | null>(null);
  const cards = useRef<Partial<Record<AppProvider, HTMLElement>>>({});
  const initialResult = useRef<{ connected: string; error: string } | null>(null);
  const resultCaptured = useRef(false);
  const currentSelection = selected ? connections?.find(item => item.provider === selected.provider) : null;
  const selectionChanged = !!selected && (!currentSelection || currentSelection.revision !== selected.revision || currentSelection.status !== selected.status || currentSelection.account_name !== selected.account_name);
  const needsReview = selectionChanged || !!selected && invalidRevisions[selected.provider] === selected.revision;
  const visibleError = loadError || error;
  const visibleDialogError = needsReview ? "应用连接已更新，请关闭此窗口并重新查看，再确认断开。" : dialogError;
  const load = useCallback(async (signal?: AbortSignal, reportError = true) => {
    const epoch = ++generation.current;
    const isCurrent = () => mounted.current && !signal?.aborted && epoch === generation.current;
    try {
      const result = await api<{ connections: AppConnection[] }>("/api/v1/connectors", { signal });
      const items = parseAppConnections(result.connections);
      if (!isCurrent()) return null;
      setConnections(items); setLoading(false); setLoadError("");
      // Keep the callback receipt until the first current, successful read.
      // Earlier failures and superseded responses cannot consume or confirm it.
      const outcome = initialResult.current;
      initialResult.current = null;
      if (outcome?.error) setError(callbackErrors[outcome.error] || callbackErrors.provider_error);
      else if (outcome?.connected && items.some(item => item.provider === outcome.connected && item.status === "connected")) setNotice({ kind: "connected", provider: outcome.connected as AppProvider });
      return items;
    } catch (reason) {
      if (!isCurrent()) return null;
      setLoading(false);
      if (reportError) setLoadError(reason instanceof Error ? reason.message : "应用连接状态读取失败，请重试。");
      throw reason;
    }
  }, []);
  useEffect(() => {
    mounted.current = true;
    if (!resultCaptured.current) {
      const url = new URL(window.location.href);
      initialResult.current = { connected: url.searchParams.get("connected") || "", error: url.searchParams.get("connection_error") || "" };
      resultCaptured.current = true;
      url.searchParams.delete("connected"); url.searchParams.delete("connection_error");
      window.history.replaceState(window.history.state, "", url.pathname + url.search + url.hash);
    }
    const controller = new AbortController();
    void Promise.resolve().then(() => controller.signal.aborted ? null : load(controller.signal)).catch(() => { /* Current read errors are rendered by load. */ });
    return () => { mounted.current = false; controller.abort(); };
  }, [load]);
  useEffect(() => {
    const refresh = () => { if (!document.hidden && !pending.current) void load().catch(() => { /* Keep the latest read error visible. */ }); };
    window.addEventListener("focus", refresh); document.addEventListener("visibilitychange", refresh);
    return () => { window.removeEventListener("focus", refresh); document.removeEventListener("visibilitychange", refresh); };
  }, [load]);
  useEffect(() => {
    if (!selected) return;
    const element = dialog.current, cardMap = cards.current; element?.showModal();
    return () => {
      element?.close();
      const origin = trigger.current;
      const card = cardMap[selected.provider];
      // A successful disconnect removes its trigger. Keep keyboard users on
      // the same app; native shells may need the card when Connect is disabled.
      const target = origin?.isConnected && !origin.hasAttribute("disabled") ? origin : card?.querySelector<HTMLButtonElement>("button:not(:disabled)") || card;
      target?.focus({ preventScroll: true });
    };
  }, [selected]);
  async function refresh() {
    if (pending.current) return;
    setLoading(true); setError(""); setLoadError("");
    try { await load(); } catch { /* Current read errors are rendered by load. */ }
  }
  async function connect(item: AppConnection, githubCode = false) {
    if (pending.current || native || !item.configured) return;
    pending.current = true; setBusy(item.provider); setError(""); setNotice(null);
    try {
      const back = workspaceReturnPath(new URLSearchParams(window.location.search).get("returnTo"));
      const response = await api<{ url: string }>(`/api/v1/connectors/${item.provider}/connect`, { method: "POST", body: JSON.stringify({ revision: item.revision, return_to: back, ...(item.provider === "github" && (githubCode || item.github_code_enabled) ? { github_code: true } : {}) }), signal: AbortSignal.timeout(20000) });
      const target = appAuthorizationURL(item.provider, response.url, window.location.origin);
      if (!target) throw new Error("应用返回的授权地址无效，请联系管理员。");
      window.location.assign(target);
    } catch (reason) {
      if (mounted.current) { setError(reason instanceof Error ? reason.message : "连接未开始，请刷新状态后重试。"); try { await load(undefined, false); } catch { /* Retain the original error and never replay an OAuth start. */ } }
    } finally { pending.current = false; if (mounted.current) setBusy(""); }
  }
  function close() { if (!pending.current) { setSelected(null); setDialogError(""); } }
  async function disconnect() {
    if (!selected || pending.current || needsReview) return;
    const item = selected;
    pending.current = true; setBusy(item.provider); setDialogError("");
    const accepted = () => {
      generation.current++;
      setConnections(previous => previous?.map(old => old.provider === item.provider && old.revision <= item.revision + 1 ? { ...old, status: "disconnected", account_name: "", scopes: [], github_code_enabled: false, revision: item.revision + 1 } : old) ?? null);
      setSelected(null); setNotice({ kind: "disconnected", provider: item.provider }); setError(""); setLoadError("");
    };
    try {
      await api(`/api/v1/connectors/${item.provider}`, { method: "DELETE", body: JSON.stringify({ revision: item.revision }), signal: AbortSignal.timeout(20000) });
      if (mounted.current) accepted();
    } catch (reason) {
      if (!mounted.current) return;
      const conflict = reason instanceof ApiError && reason.status === 409;
      if (conflict) setInvalidRevisions(previous => ({ ...previous, [item.provider]: item.revision }));
      try { const items = await load(undefined, false); if (items?.find(current => current.provider === item.provider)?.status === "disconnected") { accepted(); return; } } catch { if (conflict) setLoadError("应用连接状态读取失败，请重试。"); /* Read-only receipt recovery; do not DELETE again. */ }
      if (mounted.current) setDialogError(reason instanceof Error ? reason.message : "断开结果待确认，请刷新状态后检查。");
    } finally { pending.current = false; if (mounted.current) setBusy(""); }
  }
  return <div className="app-connections-page">
    <header className="settings-heading"><div><h1>{t("应用连接")}</h1><p>{t("连接工作应用，按授权读取资料或开展 GitHub 代码任务。")}</p></div><button type="button" className="icon-button" aria-label={t("刷新连接状态")} title={t("刷新连接状态")} disabled={loading || !!busy} onClick={() => void refresh()}><RefreshCw size={17} /></button></header>
    <div className="app-connections-intro"><span className="app-readonly"><Check size={13} />{t("资料读取")}</span><Link href="/docs/usage#app-connections" target="_blank" rel="noopener noreferrer"><BookOpen size={14} />{t("连接帮助")}</Link></div>
    {visibleError ? <p className="settings-error" role="alert">{t(visibleError)} <button type="button" className="text-button" disabled={loading || !!busy} onClick={() => void refresh()}>{t("刷新状态")}</button></p> : null}
    {notice ? <p className="app-connection-notice" role="status"><Check size={15} />{t(notice.kind === "connected" ? "{0} 已连接" : "已断开 {0}", [t(names[notice.provider])])}</p> : null}
    {native ? <p className="app-connections-hint">{t("请在浏览器版中授权应用，完成后 App 会同步连接。")}</p> : null}
    {loading && !connections ? <p className="app-connections-loading" role="status"><LoaderCircle size={17} />{t("正在读取应用连接…")}</p> : connections ? <div className="app-connections-grid" aria-busy={loading}>
      {appProviders.map(provider => {
        const item = connections.find(value => value.provider === provider);
        if (!item) return null;
        const linked = item.status !== "disconnected", ready = item.configured && item.status === "connected", invalid = invalidRevisions[provider] === item.revision;
        return <article className="app-connection-card" key={provider} tabIndex={-1} ref={element => { if (element) cards.current[provider] = element; else delete cards.current[provider]; }}>
          <header><span className="app-connector-logo"><AppConnectorIcon provider={provider} /></span><div><h2>{t(names[provider])}</h2><span className={`app-connection-state${ready ? " connected" : ""}`}>{t(!item.configured ? "管理员配置后即可连接" : ready ? "已连接" : item.status === "reauthorization_required" ? "需要重新授权" : "未连接")}</span></div></header>
          <p className="app-connection-description">{t(descriptions[provider])}</p>
          {linked && item.account_name ? <p className="app-connection-account" title={item.account_name}>{item.account_name}</p> : null}
          <footer><button type="button" className={ready ? "text-button" : "secondary-button"} disabled={!!busy || invalid || !item.configured || native} onClick={() => void connect(item)}>{busy === provider ? <LoaderCircle size={14} className="spin" /> : <ExternalLink size={14} />}{t(busy === provider ? "正在处理…" : linked ? "重新连接" : "连接")}</button>{linked ? <button type="button" className="text-button" disabled={!!busy || invalid} onClick={event => { trigger.current = event.currentTarget; setDialogError(""); setSelected(item); }}>{t("断开")}</button> : null}</footer>
          {provider === "github" ? <details className="app-github-code">
            <summary>{t("代码任务")}{ready && item.github_code_enabled ? <span><Check size={12} />{t("已开启")}</span> : null}</summary>
            <p>{t("在已授权仓库中修改代码、运行测试并推送分支或创建 PR。")}</p>
            <p>{t("代码任务需要 GitHub App 的仓库内容和 PR 读写权限；实际可用范围以安装授权和仓库权限为准。")}</p>
            <div><button type="button" className="secondary-button" disabled={!!busy || invalid || !item.configured || native} onClick={() => void connect(item, true)}>{t(item.github_code_enabled ? "重新授权代码任务" : "授权代码任务")}</button><Link href="/docs/usage#github-code" target="_blank" rel="noopener noreferrer">{t("使用帮助")}</Link></div>
          </details> : null}
          {linked && item.scopes.length ? <details className="app-connection-scopes"><summary>{t("查看授权范围")}</summary><p>{item.scopes.join(" · ")}</p></details> : null}
        </article>;
      })}
    </div> : null}
    <p className="app-connections-hint">{t("应用授权独立于 Lester 登录。GitHub 可单独开启代码任务；其他应用仅搜索和读取，不会发送内容。")}</p>
    {selected ? <dialog ref={dialog} className="app-disconnect-dialog" aria-labelledby="app-disconnect-title" onCancel={event => { event.preventDefault(); close(); }}>
      <header><h2 id="app-disconnect-title">{t("断开 {0}？", [t(names[selected.provider])])}</h2><button type="button" className="icon-button" aria-label={t("关闭")} disabled={!!busy} onClick={close}><X size={17} /></button></header>
      {selected.account_name ? <p className="app-connection-account" title={selected.account_name}>{selected.account_name}</p> : null}
      <p>{t("Lester 将删除保存的授权凭证并停止新的读取，不会删除应用中的内容。")}</p><p className="app-connections-hint">{t("如需撤销服务商端的应用授权，请到该应用的授权设置中操作。")}</p>
      {visibleDialogError ? <p className="settings-error" role="alert">{t(visibleDialogError)}</p> : null}
      <footer><button type="button" className="secondary-button" disabled={!!busy} onClick={close}>{t(needsReview ? "关闭并查看" : "取消")}</button><button type="button" className="primary-button" disabled={!!busy || needsReview} onClick={() => void disconnect()}>{t(busy ? "正在处理…" : "确认断开")}</button></footer>
    </dialog> : null}
  </div>;
}
