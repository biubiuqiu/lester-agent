"use client";
import { T, useT } from "@/components/i18n";


import { ContextInput, useContextReferences } from "./context-input";
import { FloatingPopover } from "./floating-popover";
import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { ArrowRight, BarChart3, Bot, ChevronDown, FileText, Globe, Send } from "lucide-react";
import { api, type Agent, type Conversation, type Deployment, type UserProfile } from "@/lib/api";
import { pastedImageFiles } from "@/lib/clipboard";
import { appendAttachments } from "@/lib/attachments";
import { PendingAttachments } from "./pending-attachments";
import { readView, updateView, viewKey } from "@/lib/conversation-view-state";
import { mentionPayload } from "@/lib/mentions";
import { startConversation } from "@/lib/start-conversation";
import { useComposerDock, useComposerMotion } from "./composer-motion";
import { PermissionMode } from "./permission-mode";

export function NewConversationComposer({ deployments, user, projectId, projectName, initialAgentSlug }: { deployments: Deployment[]; user: UserProfile; projectId?:string; projectName?:string; initialAgentSlug?: string }) {
  const t = useT();

  const router = useRouter();
  const storageKey = viewKey(user.user_id, user.workspace_id, `new.${projectId || "default"}`);
  const [contexts, setContexts] = useContextReferences(storageKey);
  const [text, setText] = useState(() => readView(storageKey).text);
  const [files, setFiles] = useState<File[]>(() => readView(storageKey).files);
  const [missing, setMissing] = useState(() => readView(storageKey).missingFiles);
  const [model, setModel] = useState(deployments.find((item) => item.is_default)?.id || deployments[0]?.id || "");
  const [agents, setAgents] = useState<Agent[]>([]);
  const [agentSlug, setAgentSlug] = useState(initialAgentSlug || readView(storageKey).agentSlug);
  const executionMode = "execute" as const;
  const [permissionMode, setPermissionMode] = useState(() => readView(storageKey).permissionMode);
  const [agentError, setAgentError] = useState("");
  useEffect(() => { let active=true; api<{agents:Agent[]}>("/api/v1/agents").then(result=>{if(active){setAgents(result.agents);setAgentSlug(current=>result.agents.some(a=>a.slug===current)?current:"lester");}}).catch(reason=>{if(active)setAgentError(reason instanceof Error?reason.message:"Agent 加载失败");});return()=>{active=false}; }, []);
  useEffect(() => { updateView(storageKey,{agentSlug}); }, [storageKey,agentSlug]);
  const selectedAgent = agents.find(a=>a.slug===agentSlug);
  const [busy, setBusy] = useState(false);
  const composeBox = useRef<HTMLDivElement>(null);
  const capturePosition = useComposerDock(busy, composeBox);
  const { handoff } = useComposerMotion();
  useEffect(() => {
    const media = window.matchMedia("(prefers-reduced-motion: reduce)");
    const cancel = () => { if (media.matches) composeBox.current?.getAnimations().forEach(animation => animation.cancel()); };
    media.addEventListener("change", cancel);
    return () => media.removeEventListener("change", cancel);
  }, []);
  const [error, setError] = useState("");
  const [attachmentNotice, setAttachmentNotice] = useState("");
  const submitting = useRef(false);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const input = useRef<HTMLInputElement>(null);
  const messageInput = useRef<HTMLTextAreaElement>(null);
  const examplesDisclosure = useRef<HTMLDetailsElement>(null);
  useEffect(() => { if (!busy && error) messageInput.current?.focus({ preventScroll: true }); }, [busy, error]);
  const returnTo = projectId ? `/app/p/${encodeURIComponent(projectId)}` : "/app";
  const modelSettingsURL = `/app/settings/models?returnTo=${encodeURIComponent(returnTo)}`;
  function chooseExample(prompt: string) {
    if (examplesDisclosure.current) examplesDisclosure.current.open = false;
    setText(prompt);
    updateView(storageKey, { text: prompt });
    messageInput.current?.focus();
    if (!window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      try { composeBox.current?.animate([{ boxShadow: "0 0 0 5px #52755d18" }, { boxShadow: "0 0 0 0px #52755d00" }], { duration: 420, easing: "ease-out" }); } catch { /* Visual feedback never changes the draft or sends it. */ }
    }
  }
  const hasDraft = Boolean(text.trim() || files.length || contexts.length || missing.length);
  const examples = [
    { title: t("整理需求文档"), label: t("文档"), prompt: t("帮我整理产品需求，生成一份包含目标、功能范围和验收标准的评审文档。"), icon: FileText },
    { title: t("分析一份表格"), label: t("表格"), prompt: t("分析我上传的表格，找出主要趋势和异常，并整理成简明报告。"), icon: BarChart3 },
    { title: t("制作一个网页"), label: t("网页"), prompt: t("帮我制作一个产品介绍网页，先和我确认目标用户、内容和视觉风格。"), icon: Globe },
  ];
  const changeFiles = (next: File[]) => { setFiles(next); updateView(storageKey, { files: next }); };
  const addFiles = (incoming: File[]) => {
    if (busy) return;
    const next = appendAttachments(readView(storageKey).files, incoming);
    changeFiles(next.files);
    setAttachmentNotice([...next.oversized.map(name => t("{0} 超过 25 MiB，请选择更小的文件。", [name])), ...next.duplicates.map(name => t("已忽略重复附件：{0}", [name]))].join(" "));
  };

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (text.trim() === "/recap") { setError(t("请在已有会话中使用 /recap。")); return; }
    if (submitting.current || (!text.trim() && !files.length) || !model) return;
    submitting.current = true;
    capturePosition();
    setBusy(true); setError("");
    let created: Conversation | undefined;
    const clear = () => {
      const draft = readView(storageKey);
      // Navigation may have let the user start a different draft meanwhile.
      if (JSON.stringify(draft.contexts) === JSON.stringify(contexts) && draft.text === text && draft.files.length === files.length && draft.files.every((file, i) => file === files[i])) {
        updateView(storageKey, { contexts: [], agentSlug:"lester", text: "", files: [], missingFiles: [] });
      }
    };
    try {
      const conversation = await startConversation(text, files, model, (item) => {
        created = item;
        // Preserve the submitted draft before uploads/send; never auto-resend on reload.
        updateView(viewKey(user.user_id, user.workspace_id, item.id), { contexts, text, files, missingFiles: missing, executionMode, permissionMode });
      },projectId,mentionPayload(contexts).context_ids,agentSlug,mentionPayload(contexts).references || [],executionMode,permissionMode);
      updateView(viewKey(user.user_id, user.workspace_id, conversation.id), { contexts: [], text: "", files: [], missingFiles: [], sendNotice: "" });
      clear();
      if (mounted.current) {
        if (composeBox.current) handoff(conversation.id, composeBox.current);
        router.push(`/app/c/${conversation.id}`);
      }
    } catch (reason) {
      const message = reason instanceof Error ? reason.message : t("发送失败");
      if (created) {
        updateView(viewKey(user.user_id, user.workspace_id, created.id), { sendNotice: t("会话已创建，但首条消息未确认发送成功：{0}。草稿已保留，请先检查消息和运行状态，再决定是否重新发送。", [t(message)]) });
        clear();
        if (mounted.current) {
          if (composeBox.current) handoff(created.id, composeBox.current);
          router.push(`/app/c/${created.id}`);
        }
      } else {
        capturePosition();
        setError(message);
        submitting.current = false; setBusy(false);
      }
    }
  }

  return <div className={`new-chat-home ${busy ? "composer-docked" : ""}`}><div className="new-chat-content">
    {projectName?<p className="new-project-context">{projectName}</p>:null}
    <h1>{t("有什么想交给 {0}？", [selectedAgent?.builtin ? t(selectedAgent.name) : selectedAgent?.name || "Lester"])}</h1>
    <form className="composer new-chat-composer" onSubmit={submit} aria-label={t("开始新对话")} aria-busy={busy}>
      {busy ? <p role="status" className="composer-status active"><T>{"正在创建会话并发送消息…"}</T></p> : null}
      <div className="compose-box" ref={composeBox}>
        {missing.length ? <p className="draft-attachment-notice" role="status"><T>{"请重新选择刷新前的附件："}</T>{missing.join("、")}<button type="button" onClick={() => { setMissing([]); updateView(storageKey, { missingFiles: [] }); }}><T>{"知道了"}</T></button></p> : null}
        <PendingAttachments files={files} disabled={busy} onRemove={index => { changeFiles(files.filter((_, i) => i !== index)); setAttachmentNotice(""); }} />
        {attachmentNotice ? <p className="attachment-notice" role="status">{attachmentNotice}</p> : null}
        <ContextInput inputRef={messageInput} references={contexts} onReferences={setContexts} rows={3} autoFocus aria-label={t("消息输入框")} placeholder={t("描述你的目标，上传文件或粘贴图片…")} value={text} disabled={busy} onText={(value) => { setText(value); updateView(storageKey, { text: value }); }} onPaste={(event) => { const images = pastedImageFiles(event); if (images.length) { event.preventDefault(); addFiles(images); } }} onKeyDown={(event) => { if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); event.currentTarget.form?.requestSubmit(); } }} onUpload={() => input.current?.click()} menuTools={<AgentPicker agents={agents} selected={agentSlug} disabled={busy} onSelect={value => { setAgentSlug(value); updateView(storageKey, { agentSlug: value }); }} />} tools={<>

          <input ref={input} type="file" multiple hidden onChange={(event) => { addFiles(Array.from(event.target.files || [])); event.target.value = ""; }} />
          <PermissionMode mode={permissionMode} disabled={busy} onChange={mode => { setPermissionMode(mode); updateView(storageKey, { permissionMode: mode }); }} />
          <label className="model-selector composer-model"><select aria-label={t("当前模型")} value={model} disabled={busy || !deployments.length} onChange={(event) => setModel(event.target.value)}>{!deployments.length ? <option value=""><T>{"请先配置模型"}</T></option> : null}{deployments.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select><ChevronDown /></label>
          <button className="send-button" aria-label={t("发送消息")} disabled={busy || !model || (!text.trim() && !files.length)}><Send /></button>
        </>} />
      </div>
      <div className="new-chat-options">
      {deployments.length ? <p className="new-chat-hint"><T>{"Enter 发送 · Shift + Enter 换行"}</T></p> : null}</div>
      {agentError ? <p role="alert" className="compose-error">{t(agentError)}</p> : null}
      {error ? <p role="alert" className="compose-error">{t(error)}</p> : null}
      {!deployments.length ? <div className="model-setup-prompt"><div><strong><T>{"再准备一个模型，就可以开始了"}</T></strong><p><T>{"连接你的模型服务，或使用管理员提供的共享模型。你的草稿会保留。"}</T></p></div><Link className="primary-button" href={modelSettingsURL}><T>{"配置第一个模型"}</T><ArrowRight size={16} /></Link></div> : null}
    </form>
    <div className="home-task-examples">{hasDraft ? <details ref={examplesDisclosure} className="task-examples" onKeyDown={event => {
      if (event.key === "Escape") {
        event.preventDefault();
        event.currentTarget.open = false;
        event.currentTarget.querySelector("summary")?.focus();
      }
    }}><summary><T>{"试试一个任务"}</T><ChevronDown size={15} aria-hidden="true" /></summary><div aria-label={t("任务示例")}>{examples.map(({ title, prompt }) => <button type="button" key={title} disabled={busy} onClick={() => chooseExample(prompt)}>{title}</button>)}</div></details> : <div className="task-suggestions" role="group" aria-label={t("任务示例")}>{examples.map(({ title, label, prompt, icon: Icon }) => <button type="button" key={title} disabled={busy} aria-label={title} title={title} onClick={() => chooseExample(prompt)}><Icon size={15} aria-hidden="true" />{label}</button>)}</div>}</div>
  </div></div>;
}

function AgentPicker({ agents, selected, disabled, onSelect }: { agents: Agent[]; selected: string; disabled: boolean; onSelect: (slug: string) => void }) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const anchor = useRef<HTMLButtonElement>(null);
  const id = useId();
  const agent = agents.find(item => item.slug === selected);
  return <><button type="button" ref={anchor} className="composer-agent-picker" disabled={disabled || !agents.length} aria-label={t("选择 Agent")} aria-expanded={open} aria-controls={open ? id : undefined} onClick={() => setOpen(value => !value)}><Bot size={17} />{agent?.builtin ? t(agent.name) : agent?.name || "Lester"}<ChevronDown size={13} /></button>
    {open ? <FloatingPopover anchor={anchor} className="composer-agent-popover" onClose={reason => { setOpen(false); if (reason === "escape") anchor.current?.focus(); }}>
      <div id={id}><label className="field"><T>{"选择 Agent"}</T><select autoFocus aria-label={t("选择 Agent")} value={selected} onChange={event => { onSelect(event.target.value); setOpen(false); anchor.current?.focus(); }}>{agents.map(item => <option key={item.id} value={item.slug}>{item.builtin ? t(item.name) : item.name}</option>)}</select></label>
        <Link href={`/app/agents/${encodeURIComponent(selected)}`}><T>{"了解这个 Agent"}</T></Link><Link href="/app/agents"><T>{"管理 Agent"}</T></Link>
      </div>
    </FloatingPopover> : null}</>;
}
