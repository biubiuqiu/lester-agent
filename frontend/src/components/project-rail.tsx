"use client";

import { useState } from "react";
import { Folder, FolderOpen, MoreHorizontal, Pin, Plus } from "lucide-react";
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
        onProject(project.id);
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
        .filter((conversation) => conversation.project_id === activeProject.id)
        .sort(
          (a, b) =>
            Number(b.pinned) - Number(a.pinned) ||
            b.updated_at.localeCompare(a.updated_at),
        )
    : [];

  return (
    <div className="project-rail">
      <div className="project-rail-heading">
        <div>
          <span className="section-kicker">项目</span>
          <span className="section-count">{sorted.length}</span>
        </div>
        <button
          type="button"
          className="rail-icon"
          aria-label="新建项目"
          onClick={() => {
            setEditing("");
            setName("");
            setError("");
          }}
        >
          <Plus size={15} />
        </button>
      </div>
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
      <div className="project-list" aria-label="项目列表">
        {sorted.map((project) => {
          const selected = activeProject?.id === project.id;
          return (
            <div
              key={project.id}
              className={`project-heading ${selected ? "selected" : ""}`}
            >
              <button
                className="project-select"
                aria-current={selected ? "page" : undefined}
                onClick={() => onProject(project.id)}
              >
                {selected ? <FolderOpen size={15} /> : <Folder size={15} />}
                <span className="project-name-text" title={project.name}>
                  {project.name}
                </span>
                <span className="project-count">
                  {project.conversation_count}
                </span>
                {project.pinned ? <Pin size={12} /> : null}
              </button>
              <details
                className="rail-menu"
                name="rail-actions"
                onClick={(event) => {
                  if ((event.target as Element).closest("button"))
                    event.currentTarget.open = false;
                }}
                onBlur={(event) => {
                  if (!event.currentTarget.contains(event.relatedTarget))
                    event.currentTarget.open = false;
                }}
              >
                <summary aria-label={`项目 ${project.name} 的操作`}>
                  <MoreHorizontal size={15} />
                </summary>
                <div>
                  <button
                    disabled={!!busy}
                    onClick={() => void pinProject(project)}
                  >
                    {project.pinned ? "取消项目置顶" : "置顶项目"}
                  </button>
                  <button
                    onClick={() => {
                      setEditing(project.id);
                      setName(project.name);
                    }}
                  >
                    重命名项目
                  </button>
                  <button onClick={() => onProject(project.id)}>
                    在此项目新建会话
                  </button>
                </div>
              </details>
            </div>
          );
        })}
      </div>

      {activeProject ? (
        <section className="project-conversation-section">
          <header className="project-conversation-header">
            <div>
              <span className="section-kicker">当前项目</span>
              <strong>{activeProject.name}</strong>
            </div>
            <button
              type="button"
              className="project-new-conversation"
              onClick={() => onProject(activeProject.id)}
            >
              <Plus size={14} />
              新会话
            </button>
          </header>
          <div
            className="project-conversations"
            aria-label={`${activeProject.name}中的会话`}
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
                <details
                  className="rail-menu"
                  name="rail-actions"
                  onClick={(event) => {
                    if ((event.target as Element).closest("button"))
                      event.currentTarget.open = false;
                  }}
                  onBlur={(event) => {
                    if (!event.currentTarget.contains(event.relatedTarget))
                      event.currentTarget.open = false;
                  }}
                >
                  <summary aria-label={`会话 ${c.title} 的操作`}>
                    <MoreHorizontal size={15} />
                  </summary>
                  <div>
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
                  </div>
                </details>
              </div>
            ))}
            {!activeConversations.length ? (
              <div className="project-empty-state">
                <Folder size={18} />
                <strong>这个项目还没有会话</strong>
                <span>从上方新建会话开始</span>
              </div>
            ) : null}
          </div>
        </section>
      ) : null}
    </div>
  );
}
