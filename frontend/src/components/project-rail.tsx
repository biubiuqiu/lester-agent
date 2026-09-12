"use client";

import { useState } from "react";
import {
  ChevronDown,
  ChevronRight,
  Folder,
  MoreHorizontal,
  Pin,
  Plus,
} from "lucide-react";
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
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
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
  return (
    <div className="project-rail">
      <div className="project-rail-heading">
        <span>项目</span>
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
      {sorted.map((project) => {
        const items = conversations
          .filter((c) => c.project_id === project.id)
          .sort(
            (a, b) =>
              Number(b.pinned) - Number(a.pinned) ||
              b.updated_at.localeCompare(a.updated_at),
          );
        const closed = collapsed.has(project.id);
        return (
          <section key={project.id} className="rail-project">
            <div
              className={`project-heading ${selectedProject === project.id ? "selected" : ""}`}
            >
              <button
                className="rail-icon"
                aria-label={`${closed ? "展开" : "收起"}项目 ${project.name}`}
                aria-expanded={!closed}
                onClick={() =>
                  setCollapsed((previous) => {
                    const next = new Set(previous);
                    if (next.has(project.id)) next.delete(project.id);
                    else next.add(project.id);
                    return next;
                  })
                }
              >
                {closed ? (
                  <ChevronRight size={14} />
                ) : (
                  <ChevronDown size={14} />
                )}
              </button>
              <button
                className="project-name"
                title={project.name}
                onClick={() => onProject(project.id)}
              >
                <Folder size={15} />
                <span>{project.name}</span>
                {project.pinned ? <Pin size={12} /> : null}
              </button>
              <details className="rail-menu" name="rail-actions"
              onClick={(event) => { if ((event.target as Element).closest("button")) event.currentTarget.open = false; }}
              onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget)) event.currentTarget.open = false; }}
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
            {!closed ? (
              <div className="project-conversations">
                {items.map((c) => (
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
                    <details className="rail-menu" name="rail-actions"
              onClick={(event) => { if ((event.target as Element).closest("button")) event.currentTarget.open = false; }}
              onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget)) event.currentTarget.open = false; }}
            >
                      <summary aria-label={`会话 ${c.title} 的操作`}>
                        <MoreHorizontal size={15} />
                      </summary>
                      <div>
                        <button
                          disabled={!!busy}
                          onClick={() =>
                            void organize(c, { pinned: !c.pinned })
                          }
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
                {!items.length ? (
                  <p className="project-empty">暂无会话</p>
                ) : null}
              </div>
            ) : null}
          </section>
        );
      })}
    </div>
  );
}
