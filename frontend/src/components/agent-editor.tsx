"use client";
import { formatMessage } from "@/lib/i18n";
import { T, useI18n } from "@/components/i18n";


import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ArrowLeft, Bot, FilePlus2, FileText, Send, Sparkles, Trash2, X } from "lucide-react";
import { API, api, upload, type Agent, type AgentFile, type Skill } from "@/lib/api";

type Draft = { name: string; description: string; instructions: string; skill_slugs: string[] };
type ChatMessage = { role: "user" | "assistant"; content: string; interfaceCopy?: boolean };
const emptyDraft: Draft = { name: "", description: "", instructions: "", skill_slugs: [] };
const maxFileBytes = 10 * 1024 * 1024;
const readableSize = (size: number) => size < 1024 ? `${size} B` : size < 1024 * 1024 ? `${Math.ceil(size / 1024)} KB` : `${(size / 1024 / 1024).toFixed(1)} MB`;

export function AgentEditor({ slug }: { slug?: string }) {
  const { locale, t } = useI18n();

  const router = useRouter();
  const [existing, setExisting] = useState<Agent | null>(null);
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [files, setFiles] = useState<AgentFile[]>([]);
  const [staged, setStaged] = useState<File[]>([]);
  const [messages, setMessages] = useState<ChatMessage[]>([{ role: "assistant", content: "说说你希望这个 Agent 帮你完成什么。我会和你一起整理名称、职责、提示词和合适的 Skill。", interfaceCopy: true }]);
  const [input, setInput] = useState("");
  const [suggestion, setSuggestion] = useState<Draft | null>(null);
  const [error, setError] = useState("");
  const [chatError, setChatError] = useState("");
  const [busy, setBusy] = useState(false);
  const [chatBusy, setChatBusy] = useState(false);
  const [confirmFile, setConfirmFile] = useState<string | null>(null);
  const [caret, setCaret] = useState({ line: 1, column: 1 });
  const pending = useRef(false);
  const chatPending = useRef(false);
  const gutter = useRef<HTMLPreElement>(null);
  const picker = useRef<HTMLInputElement>(null);

  const lineCount = useMemo(() => draft.instructions.split("\n").length, [draft.instructions]);
  const lineNumbers = useMemo(() => Array.from({ length: lineCount }, (_, i) => String(i + 1)).join("\n"), [lineCount]);
  const characterCount = useMemo(() => Array.from(draft.instructions).length, [draft.instructions]);

  useEffect(() => {
    let active = true;
    Promise.all([api<{ skills: Skill[] }>("/api/v1/skills"), slug ? api<Agent>(`/api/v1/agents/${encodeURIComponent(slug)}`) : Promise.resolve(null)]).then(async ([catalog, agent]) => {
      if (!active) return;
      setSkills(catalog.skills);
      if (agent) {
        if (agent.builtin) { setError("内置 Agent 不支持编辑"); return; }
        setExisting(agent);
        setDraft({ name: agent.name, description: agent.description, instructions: agent.instructions, skill_slugs: agent.skill_slugs });
        const result = await api<{ files: AgentFile[] }>(`/api/v1/agents/${agent.id}/files`);
        if (active) setFiles(result.files);
      }
    }).catch(reason => { if (active) setError(reason instanceof Error ? reason.message : "加载失败"); });
    return () => { active = false; };
  }, [slug]);

  function setSelection(text: string, position: number) {
    const before = text.slice(0, position);
    const lines = before.split("\n");
    setCaret({ line: lines.length, column: Array.from(lines.at(-1) || "").length + 1 });
  }
  function addFiles(selected: FileList | null) {
    if (!selected) return;
    const next = Array.from(selected);
    const invalid = next.find(file => !file.size || file.size > maxFileBytes || !file.name || /[/\\\x00-\x1f]/.test(file.name) || Array.from(file.name).length > 180);
    if (invalid) { setError(formatMessage("“{0}”无效；单个文件需在 1 字节至 10 MiB 之间，且文件名不能包含路径。", [invalid.name])); return; }
    const names = new Set([...files.map(file => file.name), ...staged.map(file => file.name)]);
    if (next.some(file => names.has(file.name)) || new Set(next.map(file => file.name)).size !== next.length) { setError("文件名不能重复；请先移除同名文件。"); return; }
    const total = files.reduce((sum, file) => sum + file.size_bytes, 0) + staged.reduce((sum, file) => sum + file.size, 0) + next.reduce((sum, file) => sum + file.size, 0);
    if (files.length + staged.length + next.length > 20 || total > 50 * 1024 * 1024) { setError("每个 Agent 最多 20 个文件，总大小最多 50 MiB。"); return; }
    setError(""); setStaged(current => [...current, ...next]);
  }
  async function removeFile(fileID: string) {
    if (!existing || busy) return;
    setBusy(true); setError("");
    try {
      await api(`/api/v1/agents/${existing.id}/files/${fileID}`, { method: "DELETE" });
      setFiles(current => current.filter(file => file.id !== fileID));
      setConfirmFile(null);
    } catch (reason) { setError(reason instanceof Error ? reason.message : "删除文件失败"); }
    finally { setBusy(false); }
  }
  async function save(event: FormEvent) {
    event.preventDefault(); if (pending.current) return;
    pending.current = true; setBusy(true); setError("");
    try {
      const saved = await api<Agent>(existing ? `/api/v1/agents/${existing.id}` : "/api/v1/agents", { method: existing ? "PATCH" : "POST", body: JSON.stringify({ ...draft, version: existing?.version || 0 }) });
      setExisting(saved);
      for (const file of staged) {
        const form = new FormData(); form.append("file", file);
        const created = await upload<AgentFile>(`/api/v1/agents/${saved.id}/files`, form);
        setFiles(current => [...current, created]);
        setStaged(current => current.filter(item => item !== file));
      }
      router.push(`/app/agents/${encodeURIComponent(saved.slug)}`);
    } catch (reason) { setError(reason instanceof Error ? reason.message : "保存失败；请检查文件列表后重试"); }
    finally { pending.current = false; setBusy(false); }
  }
  async function send(event: FormEvent) {
    event.preventDefault(); const text = input.trim(); if (!text || chatPending.current) return;
    chatPending.current = true; setChatBusy(true); setChatError("");
    const next = [...messages, { role: "user" as const, content: text }]; setMessages(next); setInput("");
    try {
      const response = await api<{ reply: string; draft: Draft | null }>("/api/v1/agent-builder/chat", { method: "POST", body: JSON.stringify({ messages: next.slice(1).slice(-20) }) });
      setMessages([...next, { role: "assistant", content: response.reply }]);
      if (response.draft) setSuggestion(response.draft);
    } catch (reason) { setChatError(reason instanceof Error ? reason.message : "创建助手无法回复，请重试"); setMessages(messages); setInput(text); }
    finally { chatPending.current = false; setChatBusy(false); }
  }
  function toggleSkill(skillSlug: string) { setDraft(current => ({ ...current, skill_slugs: current.skill_slugs.includes(skillSlug) ? current.skill_slugs.filter(item => item !== skillSlug) : [...current.skill_slugs, skillSlug] })); }

  if (slug && error === "内置 Agent 不支持编辑") return <main className="agent-page"><header className="agent-page-header"><Link href="/app/agents"><ArrowLeft size={17} /><T>{"Agent 管理"}</T></Link></header><div className="agent-page-body"><p role="alert" className="settings-error">{t(error)}</p></div></main>;

  return <main className="agent-page agent-studio">
    <header className="agent-page-header"><Link href="/app/agents"><ArrowLeft size={17} /><T>{"Agent 管理"}</T></Link><span><Bot size={18} />{existing ? t("编辑 Agent") : t("创建 Agent")}</span></header>
    <div className="agent-page-body">
      <div className="agent-page-title"><div><p className="eyebrow">Agent studio</p><h1>{existing ? t("编辑 {0}", [existing.name]) : t("创建一个专属 Agent")}</h1><p><T>{"写好核心提示词、选择 Skill 并管理文件；也可以让创建助手帮你整理初稿。"}</T></p></div></div>
      <div className="agent-editor-grid">
        <form className="agent-form" onSubmit={save}>
          <div className="agent-form-intro"><h2><T>{"基本信息"}</T></h2><p><T>{"名称和简介会显示在 Agent 落地页。"}</T></p></div>
          <div className="agent-basic-fields"><label><T>{"名称"}</T><input required maxLength={80} value={draft.name} onChange={e => setDraft({ ...draft, name: e.target.value })} placeholder={t("例如：产品需求分析师")} /></label><label><T>{"简介"}</T><input maxLength={500} value={draft.description} onChange={e => setDraft({ ...draft, description: e.target.value })} placeholder={t("一句话说明它能做什么")} /></label></div>
          <section className="agent-prompt-section"><div className="agent-prompt-heading"><div><h2><T>{"系统提示词"}</T><span><T>{"核心设置"}</T></span></h2><p><T>{"写清职责、工作步骤、边界和输出格式；运行时将与平台规则一起提供给模型。"}</T></p></div><small>{characterCount.toLocaleString(locale)} <T>{"/ 20,000 字"}</T></small></div><div className="agent-prompt-editor"><pre ref={gutter} aria-hidden="true">{lineNumbers}</pre><textarea required maxLength={20000} spellCheck={false} aria-label={t("系统提示词")} value={draft.instructions} onChange={e => { setDraft({ ...draft, instructions: e.target.value }); setSelection(e.target.value, e.target.selectionStart); }} onScroll={e => { if (gutter.current) gutter.current.scrollTop = e.currentTarget.scrollTop; }} onClick={e => setSelection(draft.instructions, e.currentTarget.selectionStart)} onKeyUp={e => setSelection(draft.instructions, e.currentTarget.selectionStart)} placeholder={t("例如：你是一名产品需求分析师。先澄清目标和约束，再产出可验证的验收标准…")} /></div><div className="agent-prompt-status"><span>{t("第 {0} 行 · 第 {1} 列", [caret.line.toLocaleString(locale), caret.column.toLocaleString(locale)])}</span><span>{t("共 {0} 行 · {1} 字", [lineCount.toLocaleString(locale), characterCount.toLocaleString(locale)])}</span></div></section>
          <section className="agent-resource-section"><div className="agent-resource-heading"><div><h2><T>{"Agent 文件"}</T></h2><p><T>{"支持文档、图片及其他资料。发起会话后，它们会复制到会话的"}</T><code>agent-resources/</code> <T>{"目录。"}</T></p></div><button type="button" className="secondary-button" disabled={busy} onClick={() => picker.current?.click()}><FilePlus2 size={16} /><T>{"上传文件"}</T></button></div><input ref={picker} hidden multiple type="file" onChange={e => { addFiles(e.target.files); e.target.value = ""; }} /><div className="agent-file-list">{files.map(file => <div className="agent-file-row" key={file.id}><FileText size={17} /><span><strong>{file.name}</strong><small>{readableSize(file.size_bytes)} <T>{"· 已保存"}</T></small></span><a href={`${API}/api/v1/agents/${existing?.id}/files/${file.id}`}><T>{"下载"}</T></a>{confirmFile === file.id ? <><button type="button" onClick={() => setConfirmFile(null)}><T>{"取消"}</T></button><button type="button" disabled={busy} onClick={() => void removeFile(file.id)}><T>{"确认删除"}</T></button></> : <button type="button" disabled={busy} aria-label={t("删除 {0}", [file.name])} onClick={() => setConfirmFile(file.id)}><Trash2 size={15} /></button>}</div>)}{staged.map((file, index) => <div className="agent-file-row" key={`${file.name}-${index}`}><FileText size={17} /><span><strong>{file.name}</strong><small>{readableSize(file.size)} <T>{"· 保存 Agent 时上传"}</T></small></span><button type="button" disabled={busy} aria-label={t("移除待上传的 {0}", [file.name])} onClick={() => setStaged(current => current.filter(item => item !== file))}><X size={15} /></button></div>)}{!files.length && !staged.length ? <p className="agent-files-empty"><T>{"还没有文件。单个文件最多 10 MiB，每个 Agent 最多 20 个文件、合计 50 MiB。"}</T></p> : null}</div></section>
          <fieldset className="agent-skill-section"><legend><T>{"预设 Skill"}</T></legend><p><T>{"首次使用此 Agent 时，会在会话中安装所选 Skill。"}</T></p><div className="agent-skill-list">{skills.map(skill => <label key={skill.slug}><input type="checkbox" checked={draft.skill_slugs.includes(skill.slug)} onChange={() => toggleSkill(skill.slug)} /><span><strong>{skill.name}</strong><small>{skill.description}</small></span></label>)}</div></fieldset>
          {error && <p className="settings-error" role="alert">{t(error)}</p>}<div className="agent-save-row"><button className="primary-button" disabled={busy}>{busy ? t("保存并上传中…") : existing ? t("保存修改") : t("创建 Agent")}</button></div>
        </form>
        <aside className="agent-builder"><header><Sparkles size={19} /><div><strong><T>{"Agent 创建助手"}</T></strong><small><T>{"聊一聊，整理出可编辑的草稿"}</T></small></div></header><div className="agent-builder-messages" aria-live="polite">{messages.map((message, index) => <p key={index} className={message.role}>{message.interfaceCopy ? t(message.content) : message.content}</p>)}{chatBusy && <p className="assistant"><T>{"正在整理建议…"}</T></p>}</div>{suggestion && <div className="agent-suggestion"><strong><T>{"建议草稿："}</T>{suggestion.name}</strong><p>{suggestion.description}</p><button type="button" onClick={() => { setDraft({ ...suggestion, skill_slugs: suggestion.skill_slugs.filter(item => skills.some(skill => skill.slug === item)) }); setSuggestion(null); }}><T>{"应用到左侧表单"}</T></button></div>}{chatError && <p role="alert" className="settings-error">{t(chatError)}</p>}<form onSubmit={send}><textarea aria-label={t("给 Agent 创建助手发消息")} value={input} onChange={e => setInput(e.target.value)} rows={3} maxLength={3000} placeholder={t("例如：我需要一个帮助梳理产品需求的 Agent…")} /><button type="submit" disabled={chatBusy || !input.trim()} aria-label={t("发送给创建助手")}><Send size={17} /></button></form></aside>
      </div>
    </div>
  </main>;
}
