"use client";

import { useId, useRef, useState, type ReactNode } from "react";
import { Folder, MoreHorizontal, Pin, Plus, Search, X } from "lucide-react";
import { api, type Conversation, type Project } from "@/lib/api";
import { ConversationRunMark, type UnreadRunResult } from "./run-awareness";

export function ProjectRail({
  projects,
  conversations,
  currentId,
  selectedProject,
  unread,
  onOpen,
  onProject,
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
  onProject: (id: string) => void;
  onNew: (id: string) => void;
  search: string;
  onSearch: (value: string) => void;
  onProjects: (projects: Project[]) => void;
  onConversation: (id: string, patch: Partial<Conversation>) => void;
}) {
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
  const activeProject =
    sorted.find((project) => project.id === selectedProject) ??
    sorted.find((project) => project.is_default) ??
    sorted[0];
  const activeConversations = activeProject
    ? conversations
        .filter((conversation) => search.trim() || conversation.project_id === activeProject.id)
        .sort(
          (a, b) =>
            Number(b.pinned) - Number(a.pinned) ||
            b.updated_at.localeCompare(a.updated_at),
        )
    : [];

  return (
    <div className="project-rail">
      <div className="project-switcher-row">
        <label className="project-switcher">
          <Folder size={16} />
          <select aria-label="切换项目" value={activeProject?.id || ""} onChange={e => onProject(e.target.value)}>
            {sorted.map(p => <option key={p.id} value={p.id}>{p.pinned ? "★ " : ""}{p.name}</option>)}
          </select>
        </label>
        {activeProject ? <RailActions label={`项目 ${activeProject.name} 的操作`}>
          <button disabled={!!busy} onClick={() => void pinProject(activeProject)}>{activeProject.pinned ? "取消项目置顶" : "置顶项目"}</button>
          <button disabled={!!busy} onClick={() => { setEditing(activeProject.id); setName(activeProject.name); }}>重命名项目</button>
          <button disabled={!!busy} onClick={() => { setEditing(""); setName(""); setError(""); }}>新建项目</button>
        </RailActions> : null}
      </div>
      <button className="project-compose-button" disabled={!activeProject} onClick={() => activeProject && onNew(activeProject.id)} title={`在${activeProject?.name || "项目"}中新建会话`}>
        <Plus size={16} />新会话
      </button>
      <label className="conversation-search"><Search /><input value={search} onChange={e => onSearch(e.target.value)} placeholder="搜索所有会话" aria-label="搜索会话" />{search ? <button type="button" onClick={() => onSearch("")} aria-label="清空会话搜索"><X /></button> : null}</label>
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
      {activeProject ? (
        <section className="project-conversation-section">
          <header className="project-conversation-header">
            <div>
              <span className="section-kicker">{search.trim() ? "搜索结果 · 所有项目" : "最近会话"}</span>
              <span className="section-count">{activeConversations.length}</span>
            </div>

          </header>
          <div
            className="project-conversations"
            aria-label={search.trim() ? "会话搜索结果" : `${activeProject.name}中的会话`}
          >
            {activeConversations.map((c) => (
              <div
                key={c.id}
                className={`project-conversation-row ${c.id === currentId ? "active" : ""}`}
              >
                <button
                  className="conversation-item"
                  title={c.title}
                  aria-current={c.id === currentId ? "page" : undefined}
                  onClick={() => onOpen(c.id)}
                >
                  <span className="conversation-item-copy">
                    <strong>
                      {c.pinned ? <Pin size={11} /> : null}
                      {c.title}
                    </strong>
                    <small>
                      {search.trim() ? `${projects.find(p => p.id === c.project_id)?.name || "项目"} · ` : ""}
                      {c.run_status === "running"
                        ? "正在工作"
                        : c.run_status === "cancelling"
                          ? "正在停止"
                          : new Date(c.updated_at).toLocaleDateString()}
                    </small>
                  </span>
                  <ConversationRunMark
                    status={c.run_status}
                    unread={unread[c.id]}
                  />
                </button>
                <RailActions label={`会话 ${c.title} 的操作`}>
                    <button
                      disabled={!!busy}
                      onClick={() => void organize(c, { pinned: !c.pinned })}
                    >
                      {c.pinned ? "取消会话置顶" : "置顶会话"}
                    </button>
                    <label>
                      移动到项目
                      <select
                        aria-label={`移动会话 ${c.title}`}
                        disabled={!!busy}
                        value={c.project_id}
                        onChange={(e) =>
                          void organize(c, { project_id: e.target.value })
                        }
                      >
                        {sorted.map((p) => (
                          <option key={p.id} value={p.id}>
                            {p.name}
                          </option>
                        ))}
                      </select>
                    </label>
                </RailActions>
              </div>
            ))}
            {!activeConversations.length ? (
              <div className="project-empty-state">
                <Folder size={18} />
                <strong>{search.trim() ? "没有找到匹配的会话" : "这个项目还没有会话"}</strong>
                <span>{search.trim() ? "换个关键词试试" : "从上方新建会话开始"}</span>
              </div>
            ) : null}
          </div>
        </section>
      ) : null}
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
