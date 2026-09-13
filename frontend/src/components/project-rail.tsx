"use client";

import { useId, useRef, useState, type ReactNode } from "react";
import { ChevronRight, MoreHorizontal, Pin, Plus, Search, X } from "lucide-react";
import { ProjectIcon, ConversationIcon } from "./workspace-icons";
import { api, type Conversation, type Project } from "@/lib/api";
import { ConversationRunMark, type UnreadRunResult } from "./run-awareness";

export function ProjectRail({
  projects,
  conversations,
  currentId,
  selectedProject,
  unread,
  onOpen,
  onNew,
  search,
  onSearch,
  onProjects,
  onConversation,
}: {
  projects: Project[];
  conversations: Conversation[];
  currentId?: string;
  selectedProject?: string;
  unread: Record<string, UnreadRunResult>;
  onOpen: (id: string) => void;
  onNew: (id: string) => void;
  search: string;
  onSearch: (value: string) => void;
  onProjects: (projects: Project[]) => void;
  onConversation: (id: string, patch: Partial<Conversation>) => void;
}) {
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const [showAll, setShowAll] = useState<Record<string, boolean>>({});
  const [editing, setEditing] = useState<string | null>(null);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  async function saveProject(id: string) {
    if (busy || !name.trim()) return;
    setBusy(id || "new");
    setError("");
    try {
      if (id) {
        await api(`/api/v1/projects/${id}`, {
          method: "PATCH",
          body: JSON.stringify({ name }),
        });
        onProjects(
          projects.map((p) => (p.id === id ? { ...p, name: name.trim() } : p)),
        );
      } else {
        const project = await api<Project>("/api/v1/projects", {
          method: "POST",
          body: JSON.stringify({ name }),
        });
        onProjects([...projects, project]);
        onNew(project.id);
      }
      setEditing(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "保存失败");
    } finally {
      setBusy("");
    }
  }
  async function pinProject(p: Project) {
    if (busy) return;
    setBusy(p.id);
    setError("");
    try {
      await api(`/api/v1/projects/${p.id}`, {
        method: "PATCH",
        body: JSON.stringify({ pinned: !p.pinned }),
      });
      onProjects(
        projects.map((item) =>
          item.id === p.id ? { ...item, pinned: !item.pinned } : item,
        ),
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : "置顶失败");
    } finally {
      setBusy("");
    }
  }
  async function organize(c: Conversation, patch: Partial<Conversation>) {
    if (busy) return;
    setBusy(c.id);
    setError("");
    try {
      await api(`/api/v1/conversations/${c.id}/organization`, {
        method: "PATCH",
        body: JSON.stringify(patch),
      });
      onConversation(c.id, patch);
    } catch (e) {
      setError(e instanceof Error ? e.message : "操作失败");
    } finally {
      setBusy("");
    }
  }
  const sorted = [...projects].sort(
    (a, b) =>
      Number(b.pinned) - Number(a.pinned) ||
      Number(b.is_default) - Number(a.is_default) ||
      a.created_at.localeCompare(b.created_at),
  );
  const searching = Boolean(search.trim());
  return (
    <div className="project-rail project-tree-rail">
      <label className="conversation-search"><Search /><input value={search} onChange={e => onSearch(e.target.value)} placeholder="搜索会话" aria-label="搜索会话" />{search ? <button type="button" onClick={() => onSearch("")} aria-label="清空会话搜索"><X /></button> : null}</label>
      <div className="tree-section-heading"><span>{searching ? "搜索结果" : "项目与会话"}</span><button className="rail-icon" aria-label="新建项目" disabled={!!busy} onClick={() => { setEditing(""); setName(""); setError(""); }}><Plus size={15} /></button></div>
      {error ? (
        <p className="rail-error" role="alert">
          {error}
        </p>
      ) : null}
      {editing !== null ? (
        <form
          className="project-name-form"
          onSubmit={(e) => {
            e.preventDefault();
            void saveProject(editing);
          }}
        >
          <label>
            {editing ? "重命名项目" : "新建项目"}
            <input
              autoFocus
              value={name}
              maxLength={80}
              aria-label="项目名称"
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          <div>
            <button disabled={!!busy || !name.trim()}>保存</button>
            <button
              type="button"
              disabled={!!busy}
              onClick={() => setEditing(null)}
            >
              取消
            </button>
          </div>
        </form>
      ) : null}
      <div className="project-tree-scroll">
        {searching && !conversations.length ? <p className="tree-empty">没有找到匹配的会话</p> : null}
        <ul className="project-tree" aria-label="项目与会话">
          {sorted.map(project => {
            const items = conversations.filter(c => c.project_id === project.id).sort((a, b) => Number(b.pinned) - Number(a.pinned) || b.updated_at.localeCompare(a.updated_at));
            if (searching && !items.length) return null;
            const opened = searching || !collapsed[project.id];
            const currentIndex = items.findIndex(c => c.id === currentId);
            const limit = searching || showAll[project.id] ? items.length : Math.max(5, currentIndex + 1);
            return <li key={project.id} className={`tree-project ${selectedProject === project.id ? "is-current" : ""}`}>
              <div className="tree-project-row">
                <button className="tree-project-toggle" aria-label={`${opened ? "收起" : "展开"}项目 ${project.name}`} aria-expanded={opened} onClick={() => setCollapsed(prev => ({ ...prev, [project.id]: opened }))}>
                  <ChevronRight className={opened ? "tree-chevron expanded" : "tree-chevron"} size={12} />
                  <span className="tree-project-icon"><ProjectIcon size={17} /></span>
                  <span className="tree-project-name" title={project.name}>{project.name}</span>
                  {project.pinned ? <Pin size={11} /> : null}
                  <small>{items.length}</small>
                </button>
                <button className="rail-icon tree-new" aria-label={`在 ${project.name} 中新建会话`} title="新建会话" onClick={() => { setCollapsed(prev => ({ ...prev, [project.id]: false })); onNew(project.id); }}><Plus size={14} /></button>
                <RailActions label={`项目 ${project.name} 的操作`}>
                  <button disabled={!!busy} onClick={() => void pinProject(project)}>{project.pinned ? "取消项目置顶" : "置顶项目"}</button>
                  <button disabled={!!busy} onClick={() => { setEditing(project.id); setName(project.name); }}>重命名项目</button>
                </RailActions>
              </div>
              {opened ? <ul className="tree-conversations" aria-label={`${project.name}中的会话`}>
                {items.slice(0, limit).map(c => <li key={c.id} className={`tree-conversation ${c.id === currentId ? "active" : ""}`}>
                  <button className="tree-conversation-link" title={c.title} aria-current={c.id === currentId ? "page" : undefined} onClick={() => onOpen(c.id)}>
                    <ConversationIcon size={14} /><span>{c.title}</span>{c.pinned ? <Pin size={10} /> : null}<ConversationRunMark status={c.run_status} unread={unread[c.id]} />
                  </button>
                  <RailActions label={`会话 ${c.title} 的操作`}>
                    <button disabled={!!busy} onClick={() => void organize(c, { pinned: !c.pinned })}>{c.pinned ? "取消会话置顶" : "置顶会话"}</button>
                    <label>移动到项目<select aria-label={`移动会话 ${c.title}`} disabled={!!busy} value={c.project_id} onChange={e => void organize(c, { project_id: e.target.value })}>{sorted.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select></label>
                  </RailActions>
                </li>)}
                {!items.length ? <li className="tree-empty"><button onClick={() => onNew(project.id)}>开始第一个会话</button></li> : null}
                {items.length > 5 && !searching ? <li><button className="tree-show-more" onClick={() => setShowAll(prev => ({ ...prev, [project.id]: !prev[project.id] }))}>{showAll[project.id] ? "收起历史会话" : `查看全部 ${items.length} 个会话`}</button></li> : null}
              </ul> : null}
            </li>;
          })}
        </ul>
      </div>
    </div>
  );
}

function RailActions({ label, children }: { label: string; children: ReactNode }) {
  const id = useId();
  const popup = useRef<HTMLDivElement>(null);
  return <div className="rail-menu">
    <button type="button" className="rail-actions-trigger" aria-label={label} popoverTarget={id}
      onClick={(event) => {
        const rect = event.currentTarget.getBoundingClientRect();
        if (popup.current) {
          popup.current.style.left = `${Math.max(8, Math.min(rect.right - 200, window.innerWidth - 208))}px`;
          popup.current.style.top = `${Math.max(8, Math.min(rect.bottom + 4, window.innerHeight - 230))}px`;
        }
      }}><MoreHorizontal size={15} /></button>
    <div ref={popup} id={id} popover="auto" className="rail-actions-popover" aria-label={label}
      onClick={(event) => {
        if ((event.target as Element).closest("button")) popup.current?.hidePopover();
      }}>{children}</div>
  </div>;
}
