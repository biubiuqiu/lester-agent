"use client";

import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ArrowUpRight, FileText, Folder, Globe, MessageSquare, Plus, RefreshCw, Search, SlidersHorizontal } from "lucide-react";
import type { Conversation, Project, UserProfile } from "@/lib/api";
import { updateView, viewKey } from "@/lib/conversation-view-state";
import { listDirectory, relativeFilePath } from "@/lib/file-inventory";
import { projectResults, type ProjectDeliverable, type ProjectResultPage } from "@/lib/project-workbench";
import { useI18n, useT } from "./i18n";
import { ProjectRulesEditor } from "./project-rules";
import { ConversationRunMark, type UnreadRunResult } from "./run-awareness";

export function ProjectWorkbench({ project, conversations, user, unread, onOpen }: {
  project: Project; conversations: Conversation[]; user: UserProfile;
  unread: Record<string, UnreadRunResult>; onOpen: (id: string) => void;
}) {
  const t = useT(), { locale } = useI18n(), router = useRouter(), id = useId();
  const [tab, setTab] = useState<"results" | "conversations">("results");
  const [search, setSearch] = useState(""), [query, setQuery] = useState(""), [kind, setKind] = useState("");
  const [feed, setFeed] = useState<{ key: string; data: ProjectResultPage } | null>(null);
  const [loading, setLoading] = useState(true), [error, setError] = useState(""), [attempt, setAttempt] = useState(0);
  const [more, setMore] = useState(false), [opening, setOpening] = useState("");
  const [actionError, setActionError] = useState(""), [rules, setRules] = useState(false), [conversationLimit, setConversationLimit] = useState(24);
  const generation = useRef(0), paged = useRef(false), morePending = useRef(false), action = useRef<AbortController | null>(null);
  const queryKey = JSON.stringify([project.id, query, kind]);
  useEffect(() => { const timer = setTimeout(() => setQuery(search.trim()), 250); return () => clearTimeout(timer); }, [search]);
  useEffect(() => () => action.current?.abort(), []);
  useEffect(() => {
    if (tab !== "results") return;
    const epoch = ++generation.current, controller = new AbortController();
    let pending = false; paged.current = false; morePending.current = false;
    const load = async () => {
      if (pending || document.hidden) return;
      pending = true; setLoading(true); setError(""); setMore(false);
      try {
        const data = await projectResults(project.id, query, kind, "", controller.signal);
        if (!controller.signal.aborted && generation.current === epoch) setFeed({ key: queryKey, data });
      } catch { if (!controller.signal.aborted && generation.current === epoch) setError(t("项目成果加载失败，请重试")); }
      finally { pending = false; if (!controller.signal.aborted && generation.current === epoch) setLoading(false); }
    };
    const visible = () => { if (!paged.current && !morePending.current) void load(); };
    void load(); const timer = setInterval(visible, 15000); document.addEventListener("visibilitychange", visible);
    return () => { controller.abort(); generation.current = epoch + 1; clearInterval(timer); document.removeEventListener("visibilitychange", visible); };
  }, [project.id, query, kind, queryKey, attempt, tab, t]);

  const current = feed?.key === queryKey ? feed.data : null;
  const loadMore = async () => {
    if (morePending.current || loading || !current?.next_cursor) return;
    morePending.current = true; setMore(true); setError(""); const epoch = generation.current;
    try {
      const data = await projectResults(project.id, query, kind, current.next_cursor, AbortSignal.timeout(20000));
      if (generation.current === epoch) {
        paged.current = true;
        setFeed(previous => previous?.key === queryKey ? { key: queryKey, data: { ...data, deliverables: [...previous.data.deliverables, ...data.deliverables.filter(item => !previous.data.deliverables.some(old => old.id === item.id))] } } : previous);
      }
    } catch { if (generation.current === epoch) setError(t("项目成果加载失败，请重试")); }
    finally { if (generation.current === epoch) { morePending.current = false; setMore(false); } }
  };

  const openResult = async (item: ProjectDeliverable, modify: boolean) => {
    if (action.current) return;
    const controller = new AbortController(); action.current = controller;
    setOpening(item.id); setActionError("");
    try {
      const path = relativeFilePath(item.conversation_id, item.entry_path);
      if (!path || path !== item.entry_path) throw new Error("missing");
      const directory = path.split("/").slice(0, -1).join("/");
      const files = await listDirectory(item.conversation_id, directory, AbortSignal.any([controller.signal, AbortSignal.timeout(20000)]));
      if (controller.signal.aborted) return;
      if (!files.some(file => file.path === path && !file.is_dir)) throw new Error("missing");
      if (modify) {
        updateView(viewKey(user.user_id, user.workspace_id, item.conversation_id), { reference: path, selected: null });
        onOpen(item.conversation_id);
      } else router.push(`/app/c/${item.conversation_id}?preview=${encodeURIComponent(path)}`);
    } catch { if (!controller.signal.aborted) setActionError(t("无法打开成果，请到来源会话检查文件或重试。")); }
    finally { if (!controller.signal.aborted) { action.current = null; setOpening(""); } }
  };
  const changeTab = (value: "results" | "conversations") => { setTab(value); setSearch(""); setQuery(""); setConversationLimit(24); };
  const tabKey = (event: KeyboardEvent) => {
    if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    const next = event.key === "Home" ? "results" : event.key === "End" ? "conversations" : tab === "results" ? "conversations" : "results";
    changeTab(next); document.getElementById(`${id}-${next}`)?.focus();
  };
  const projectConversations = conversations.filter(item => item.project_id === project.id && item.title.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase())).toSorted((a, b) => Number(b.pinned) - Number(a.pinned) || b.updated_at.localeCompare(a.updated_at));
  const stamp = (value: string) => new Intl.DateTimeFormat(locale, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(new Date(value));
  const states: Record<string, string> = { idle: "已就绪", running: "正在工作", cancelling: "正在停止", completed: "已完成", failed: "需要检查", cancelled: "已停止" };

  return <div className="project-workbench">
    <div className="project-workbench-content">
      <header className="project-workbench-heading">
        <div><p className="project-eyebrow"><Folder size={14} />{t("项目工作台")}</p><h1>{project.name}</h1></div>
        <div className="project-heading-actions"><button type="button" className="project-rules-button" onClick={() => setRules(true)} aria-label={t("项目规则")} title={t("项目规则")}><SlidersHorizontal size={17} /></button><Link className="project-primary" href={`/app/p/${project.id}`}><Plus size={16} />{t("新任务")}</Link></div>
      </header>
      <div className="project-workbench-navigation"><div role="tablist" aria-label={t("项目内容")} onKeyDown={tabKey}>{(["results", "conversations"] as const).map(value => <button key={value} type="button" role="tab" id={`${id}-${value}`} aria-selected={tab === value} aria-controls={`${id}-panel`} tabIndex={tab === value ? 0 : -1} onClick={() => changeTab(value)}>{t(value === "results" ? "成果" : "会话")}{value === "conversations" ? <small>{conversations.filter(item => item.project_id === project.id).length}</small> : null}</button>)}</div><Link href={`/app/artifacts?project=${encodeURIComponent(project.id)}&returnTo=${encodeURIComponent(`/app/p/${project.id}/overview`)}`}>{t("已发布站点")}<ArrowUpRight size={13} /></Link></div>
      <div className="project-search-row"><label><Search size={15} /><input maxLength={120} value={search} onChange={event => setSearch(event.target.value)} placeholder={t(tab === "results" ? "搜索成果或来源会话" : "搜索项目会话")} aria-label={t(tab === "results" ? "搜索项目成果" : "搜索项目会话")} /></label>{tab === "results" ? <><select aria-label={t("成果类型")} value={kind} onChange={event => setKind(event.target.value)}><option value="">{t("所有类型")}</option><option value="html">{t("网页")}</option><option value="markdown">{t("文档")}</option></select><button type="button" className="project-refresh" aria-label={t("刷新成果")} disabled={loading || more} onClick={() => setAttempt(value => value + 1)}><RefreshCw size={16} /></button></> : null}</div>
      <section id={`${id}-panel`} role="tabpanel" aria-labelledby={`${id}-${tab}`} aria-busy={tab === "results" && loading}>
        {tab === "results" ? <>
          <p className="project-catalogue-note">{t("汇总项目会话中已登记的网页和文档；预览时检查当前文件。")}</p>
          {error ? <p className="project-workbench-error" role="alert">{error}<button type="button" onClick={() => setAttempt(value => value + 1)}>{t("重试")}</button></p> : null}
          {actionError ? <p className="project-workbench-error" role="alert">{actionError}</p> : null}
          {!current && !error ? <p className="project-workbench-status" role="status">{t("正在加载…")}</p> : null}
          {current?.deliverables.length === 0 && !loading && !error ? <div className="project-workbench-empty"><FileText size={27} /><h2>{t(search.trim() || kind ? "没有匹配的成果" : "项目的成果，从这里积累")}</h2><p>{t(search.trim() || kind ? "试试其他关键词或成果类型。" : "在项目会话中生成并登记网页或文档后，会出现在这里。其他文件可在来源会话中查看。")}</p><Link href={`/app/p/${project.id}`}>{t("开始新任务")}<ArrowUpRight size={14} /></Link></div> : null}
          <div className="project-result-grid">{current?.deliverables.map(item => <article className="project-result-card" key={item.id}>
            <div className={`project-result-icon ${item.kind}`}>{item.kind === "html" ? <Globe size={21} /> : <FileText size={21} />}</div>
            <div className="project-result-copy"><h2>{item.title}</h2><p className="project-result-path" title={item.entry_path}>{item.entry_path}</p>{item.summary ? <p className="project-result-summary">{item.summary}</p> : null}</div>
            <div className="project-result-meta"><span>{t(item.kind === "html" ? "网页" : "文档")}</span><span>{t("待验收")}</span>{item.run_status === "running" || item.run_status === "cancelling" ? <span>{t("来源任务仍在运行")}</span> : item.run_status === "failed" || item.run_status === "cancelled" ? <span>{t("来源任务未正常完成")}</span> : null}</div>
            <div className="project-result-source"><Link href={`/app/c/${item.conversation_id}`} aria-label={t("来源会话：{0}", [item.conversation_title])} title={item.conversation_title}><MessageSquare size={12} />{item.conversation_title}</Link><time dateTime={item.updated_at} title={t("成果登记更新于 {0}", [stamp(item.updated_at)])}>{stamp(item.updated_at)}</time></div>
            <footer><button type="button" className="project-result-preview" disabled={Boolean(opening)} onClick={() => void openResult(item, false)} aria-label={t("预览项目成果 {0}", [item.title])}>{t(opening === item.id ? "正在打开…" : "预览")}</button><button type="button" disabled={Boolean(opening)} onClick={() => void openResult(item, true)} aria-label={t("继续修改项目成果 {0}", [item.title])}>{t("继续修改")}<ArrowUpRight size={13} /></button></footer>
          </article>)}</div>
          {current?.next_cursor ? <button type="button" className="project-load-more" disabled={loading || more} onClick={() => void loadMore()}>{t(more ? "正在加载…" : "加载更多成果")}</button> : null}
        </> : <>
          {projectConversations.length ? <div className="project-conversation-list">{projectConversations.slice(0, conversationLimit).map(item => <button type="button" key={item.id} onClick={() => onOpen(item.id)}><span className="project-conversation-icon"><MessageSquare size={16} /></span><span><strong>{item.title}</strong><small>{t(states[item.run_status] || "状态尚未确认")} · {stamp(item.updated_at)}</small></span><ConversationRunMark status={item.run_status} unread={unread[item.id]} /><ArrowUpRight size={15} /></button>)}</div> : <div className="project-workbench-empty"><MessageSquare size={27} /><h2>{t(search.trim() ? "没有找到匹配的会话" : "还没有项目会话")}</h2><Link href={`/app/p/${project.id}`}>{t("开始新任务")}<ArrowUpRight size={14} /></Link></div>}
          {projectConversations.length > conversationLimit ? <button type="button" className="project-load-more" onClick={() => setConversationLimit(value => value + 24)}>{t("加载更多会话")}</button> : null}
        </>}
      </section>
    </div>
    {rules ? <ProjectRulesEditor project={project} onClose={() => setRules(false)} /> : null}
  </div>;
}
