"use client";

import { useMemo, useState } from "react";
import { Search } from "lucide-react";
import type { Conversation, Project } from "@/lib/api";
import { useI18n, useT } from "./i18n";
import { ConversationRunMark, type UnreadRunResult } from "./run-awareness";

export function RecentConversationList({ conversations, projects, currentId, unread, onOpen }: { conversations: Conversation[]; projects: Project[]; currentId?: string; unread: Record<string, UnreadRunResult>; onOpen: (id: string) => void }) {
  const t = useT(), { locale } = useI18n();
  const [query, setQuery] = useState(""), [limit, setLimit] = useState(20);
  const projectNames = useMemo(() => new Map(projects.map(project => [project.id, project.name])), [projects]);
  const items = useMemo(() => {
    const search = query.trim().toLocaleLowerCase();
    return conversations.filter(conversation => !search || `${conversation.title}\n${projectNames.get(conversation.project_id) ?? ""}`.toLocaleLowerCase().includes(search))
      .toSorted((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at) || a.id.localeCompare(b.id));
  }, [conversations, projectNames, query]);
  const formatter = useMemo(() => new Intl.DateTimeFormat(locale, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }), [locale]);
  return <>
    <label className="recent-conversation-search"><Search size={16} aria-hidden="true" /><input type="search" value={query} onChange={event => { setQuery(event.target.value); setLimit(20); }} placeholder={t("搜索会话或项目")} aria-label={t("搜索会话或项目")} /></label>
    <div className="recent-conversation-scroll">
      <ul className="recent-conversation-list" aria-label={t("最近会话")}>
        {items.slice(0, limit).map(conversation => <li key={conversation.id}>
          <button type="button" className="recent-conversation-row" aria-current={conversation.id === currentId ? "page" : undefined} onClick={() => onOpen(conversation.id)}>
            <span className="recent-conversation-identity"><strong title={conversation.title}>{conversation.title}</strong><small title={projectNames.get(conversation.project_id)}>{projectNames.get(conversation.project_id) ?? t("项目不可用")}</small></span>
            <span className="recent-conversation-meta"><ConversationRunMark status={conversation.run_status} unread={unread[conversation.id]} /><time dateTime={conversation.updated_at}>{formatter.format(new Date(conversation.updated_at))}</time></span>
          </button>
        </li>)}
      </ul>
      {!items.length ? <p className="recent-conversation-empty" role="status">{t(query.trim() ? "没有找到匹配的会话" : "还没有会话，发送一个目标开始吧。")}</p> : null}
      {items.length > limit ? <button type="button" className="cloud-task-more" onClick={() => setLimit(value => value + 20)}>{t("显示更多会话")}</button> : null}
    </div>
  </>;
}
