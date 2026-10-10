"use client";
import { T, useI18n } from "@/components/i18n";


import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import Link from "next/link";
import { FilePlus2, FileText, Trash2 } from "lucide-react";
import { API, api, upload, type Agent, type AgentFile, type Skill } from "@/lib/api";

type Definition = Pick<Agent, "name" | "description" | "instructions" | "skill_slugs">;
const maxFileBytes = 10 * 1024 * 1024;
const empty: Definition = { name: "", description: "", instructions: "", skill_slugs: [] };

export function AgentConfiguration({ agentId, revision }: { agentId: string; revision: number }) {
  const { locale, t } = useI18n();

  const [agent, setAgent] = useState<Agent | null>(null);
  const [draft, setDraft] = useState<Definition>(empty);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [files, setFiles] = useState<AgentFile[]>([]);
  const [staged, setStaged] = useState<File[]>([]);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const [confirmFile, setConfirmFile] = useState<string | null>(null);
  const [caret, setCaret] = useState({ line: 1, column: 1 });
  const picker = useRef<HTMLInputElement>(null);
  const gutter = useRef<HTMLPreElement>(null);
  const lines = useMemo(() => draft.instructions.split("\n").length, [draft.instructions]);
  const numbers = useMemo(() => Array.from({ length: lines }, (_, index) => String(index + 1)).join("\n"), [lines]);

  useEffect(() => {
    let active = true;
    Promise.all([
      api<Agent>(`/api/v1/agents/custom-${agentId}`),
      api<{ skills: Skill[] }>("/api/v1/skills"),
      api<{ files: AgentFile[] }>(`/api/v1/agents/${agentId}/files`),
    ]).then(([item, catalog, resources]) => {
      if (!active) return;
      setAgent(item);
      setDraft({ name: item.name, description: item.description, instructions: item.instructions, skill_slugs: item.skill_slugs });
      setSkills(catalog.skills);
      setFiles(resources.files);
    }).catch(reason => { if (active) setError(reason instanceof Error ? reason.message : "无法读取 Agent 配置"); });
    return () => { active = false; };
  }, [agentId, revision]);

  function selection(text: string, position: number) {
    const before = text.slice(0, position).split("\n");
    setCaret({ line: before.length, column: Array.from(before.at(-1) || "").length + 1 });
  }
  function addFiles(selected: FileList | null) {
    if (!selected) return;
    const incoming = Array.from(selected);
    const names = new Set([...files.map(item => item.name), ...staged.map(item => item.name)]);
    if (incoming.some(item => !item.size || item.size > maxFileBytes || !item.name || /[/\\\x00-\x1f]/.test(item.name) || Array.from(item.name).length > 180)) { setError("文件名无效，或单个文件超过 10 MiB。"); return; }
    if (incoming.some(item => names.has(item.name)) || new Set(incoming.map(item => item.name)).size !== incoming.length) { setError("文件名不能重复。"); return; }
    const count = files.length + staged.length + incoming.length;
    const total = files.reduce((sum, item) => sum + item.size_bytes, 0) + staged.reduce((sum, item) => sum + item.size, 0) + incoming.reduce((sum, item) => sum + item.size, 0);
    if (count > 20 || total > 50 * 1024 * 1024) { setError("每个 Agent 最多 20 个文件，总计 50 MiB。"); return; }
    setStaged(current => [...current, ...incoming]); setError("");
  }
  async function save(event: FormEvent) {
    event.preventDefault(); if (!agent || saving) return;
    setSaving(true); setError("");
    try {
      const saved = await api<Agent>(`/api/v1/agents/${agent.id}`, { method: "PATCH", body: JSON.stringify({ ...draft, version: agent.version }) });
      setAgent(saved);
      for (const file of staged) {
        const body = new FormData(); body.append("file", file);
        const uploaded = await upload<AgentFile>(`/api/v1/agents/${agent.id}/files`, body);
        setFiles(current => [...current, uploaded]);
        setStaged(current => current.filter(item => item !== file));
      }
    } catch (reason) { setError(reason instanceof Error ? reason.message : "保存失败"); }
    finally { setSaving(false); }
  }
  async function removeFile(fileId: string) {
    if (!agent || saving) return;
    setSaving(true); setError("");
    try {
      await api(`/api/v1/agents/${agent.id}/files/${fileId}`, { method: "DELETE" });
      setFiles(current => current.filter(item => item.id !== fileId));
      setConfirmFile(null);
    } catch (reason) { setError(reason instanceof Error ? reason.message : "删除文件失败"); }
    finally { setSaving(false); }
  }
  if (!agent) return <div className="designer-panel-state">{error ? t(error) : t("正在加载 Agent 配置…")}</div>;
  return <div className="designer-agent-panel"><header><div><strong>{agent.name}</strong><span><T>{"由智能体设计师创建 · 可在这里微调"}</T></span></div><Link href={`/app/agents/${agent.slug}`}><T>{"查看落地页"}</T></Link></header>
    <form onSubmit={save}>
      <label><T>{"名称"}</T><input required maxLength={80} value={draft.name} onChange={event => setDraft({ ...draft, name: event.target.value })} /></label>
      <label><T>{"简介"}</T><input maxLength={500} value={draft.description} onChange={event => setDraft({ ...draft, description: event.target.value })} /></label>
      <section className="designer-prompt"><div><strong><T>{"系统提示词"}</T></strong><small>{Array.from(draft.instructions).length.toLocaleString(locale)} <T>{"/ 20,000 字"}</T></small></div><div className="designer-prompt-editor"><pre ref={gutter} aria-hidden="true">{numbers}</pre><textarea required maxLength={20000} spellCheck={false} aria-label={t("系统提示词")} value={draft.instructions} onChange={event => { setDraft({ ...draft, instructions: event.target.value }); selection(event.target.value, event.target.selectionStart); }} onScroll={event => { if (gutter.current) gutter.current.scrollTop = event.currentTarget.scrollTop; }} onClick={event => selection(draft.instructions, event.currentTarget.selectionStart)} onKeyUp={event => selection(draft.instructions, event.currentTarget.selectionStart)} /></div><small>{t("第 {0} 行 · 第 {1} 列", [caret.line.toLocaleString(locale), caret.column.toLocaleString(locale)])} · {t("共 {0} 行 · {1} 字", [lines.toLocaleString(locale), Array.from(draft.instructions).length.toLocaleString(locale)])}</small></section>
      <section className="designer-files"><div><strong><T>{"Agent 文件"}</T></strong><button type="button" onClick={() => picker.current?.click()} disabled={saving}><FilePlus2 size={15} /><T>{"上传"}</T></button></div><p><T>{"新会话会复制这些文件到"}</T><code>agent-resources/</code><T>{"。"}</T></p><input ref={picker} type="file" multiple hidden onChange={event => { addFiles(event.target.files); event.target.value = ""; }} />{files.map(file => <div className="designer-file-row" key={file.id}><FileText size={15} /><span>{file.name}</span><a href={`${API}/api/v1/agents/${agent.id}/files/${file.id}`}><T>{"下载"}</T></a>{confirmFile === file.id ? <><button type="button" onClick={() => setConfirmFile(null)}><T>{"取消"}</T></button><button type="button" onClick={() => void removeFile(file.id)} disabled={saving}><T>{"确认删除"}</T></button></> : <button type="button" aria-label={t("删除 {0}", [file.name])} onClick={() => setConfirmFile(file.id)}><Trash2 size={14} /></button>}</div>)}{staged.map((file, index) => <div className="designer-file-row" key={`${file.name}-${index}`}><FileText size={15} /><span>{file.name} <T>{"· 待上传"}</T></span><button type="button" onClick={() => setStaged(current => current.filter(item => item !== file))}><T>{"移除"}</T></button></div>)}{!files.length && !staged.length ? <p className="designer-file-empty"><T>{"还没有文件"}</T></p> : null}</section>
      <fieldset><legend><T>{"预设 Skill"}</T></legend>{skills.map(skill => <label key={skill.slug}><input type="checkbox" checked={draft.skill_slugs.includes(skill.slug)} onChange={() => setDraft(current => ({ ...current, skill_slugs: current.skill_slugs.includes(skill.slug) ? current.skill_slugs.filter(item => item !== skill.slug) : [...current.skill_slugs, skill.slug] }))} /><span>{skill.name}</span></label>)}</fieldset>
      {error ? <p role="alert" className="settings-error">{t(error)}</p> : null}<button className="designer-save" disabled={saving}>{saving ? t("保存中…") : t("保存配置")}</button>
    </form>
  </div>;
}
