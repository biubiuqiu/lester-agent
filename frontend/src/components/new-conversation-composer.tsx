"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";
import { useRouter } from "next/navigation";
import { ChevronDown, FileText, Paperclip, Send, X } from "lucide-react";
import type { Conversation, Deployment, UserProfile } from "@/lib/api";
import { pastedImageFiles } from "@/lib/clipboard";
import { readView, updateView, viewKey } from "@/lib/conversation-view-state";
import { startConversation } from "@/lib/start-conversation";

export function NewConversationComposer({ deployments, user, projectId, projectName }: { deployments: Deployment[]; user: UserProfile; projectId?:string; projectName?:string }) {
  const router = useRouter();
  const storageKey = viewKey(user.user_id, user.workspace_id, `new.${projectId || "default"}`);
  const [text, setText] = useState(() => readView(storageKey).text);
  const [files, setFiles] = useState<File[]>(() => readView(storageKey).files);
  const [missing, setMissing] = useState(() => readView(storageKey).missingFiles);
  const [model, setModel] = useState(deployments.find((item) => item.is_default)?.id || deployments[0]?.id || "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const submitting = useRef(false);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const input = useRef<HTMLInputElement>(null);
  const changeFiles = (next: File[]) => { setFiles(next); updateView(storageKey, { files: next }); };

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (submitting.current || (!text.trim() && !files.length) || !model) return;
    submitting.current = true;
    setBusy(true); setError("");
    let created: Conversation | undefined;
    const clear = () => {
      const draft = readView(storageKey);
      // Navigation may have let the user start a different draft meanwhile.
      if (draft.text === text && draft.files.length === files.length && draft.files.every((file, i) => file === files[i])) {
        updateView(storageKey, { text: "", files: [], missingFiles: [] });
      }
    };
    try {
      const conversation = await startConversation(text, files, model, (item) => {
        created = item;
        // Preserve the submitted draft before uploads/send; never auto-resend on reload.
        updateView(viewKey(user.user_id, user.workspace_id, item.id), { text, files, missingFiles: missing });
      },projectId);
      updateView(viewKey(user.user_id, user.workspace_id, conversation.id), { text: "", files: [], missingFiles: [], sendNotice: "" });
      clear();
      if (mounted.current) router.push(`/app/c/${conversation.id}`);
    } catch (reason) {
      const message = reason instanceof Error ? reason.message : "发送失败";
      if (created) {
        updateView(viewKey(user.user_id, user.workspace_id, created.id), { sendNotice: `会话已创建，但首条消息未确认发送成功：${message}。草稿已保留，请先检查消息和运行状态，再决定是否重新发送。` });
        clear();
        if (mounted.current) router.push(`/app/c/${created.id}`);
      } else {
        setError(message);
        submitting.current = false; setBusy(false);
      }
    }
  }

  return <div className="new-chat-home"><div className="new-chat-content">
    {projectName?<p className="new-project-context">{projectName}</p>:null}
    <h1>有什么想交给 Lester？</h1>
    <p className="new-chat-intro">从一个想法开始，把它变成看得见的成果。</p>
    <form className="composer new-chat-composer" onSubmit={submit} aria-label="开始新对话" aria-busy={busy}>
      <div className="compose-box">
        {missing.length ? <p className="draft-attachment-notice" role="status">请重新选择刷新前的附件：{missing.join("、")}<button type="button" onClick={() => { setMissing([]); updateView(storageKey, { missingFiles: [] }); }}>知道了</button></p> : null}
        {files.length ? <div className="pending-attachments">{files.map((file, index) => <span key={`${file.name}-${index}`}><FileText />{file.name}<button type="button" disabled={busy} aria-label={`移除 ${file.name}`} onClick={() => changeFiles(files.filter((_, i) => i !== index))}><X /></button></span>)}</div> : null}
        <textarea rows={3} autoFocus aria-label="消息输入框" placeholder="描述你的目标，上传文件或粘贴图片…" value={text} disabled={busy} onChange={(event) => { setText(event.target.value); updateView(storageKey, { text: event.target.value }); }} onPaste={(event) => { const images = pastedImageFiles(event); if (images.length) { event.preventDefault(); changeFiles([...files, ...images]); } }} onKeyDown={(event) => { if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); event.currentTarget.form?.requestSubmit(); } }} />
        <div className="compose-actions">
          <input ref={input} type="file" multiple hidden onChange={(event) => { changeFiles([...files, ...Array.from(event.target.files || [])]); event.target.value = ""; }} />
          <button type="button" className="icon-button upload-button" aria-label="添加附件" title="添加附件或直接粘贴图片" disabled={busy} onClick={() => input.current?.click()}><Paperclip /></button>
          <label className="model-selector"><select aria-label="当前模型" value={model} disabled={busy || !deployments.length} onChange={(event) => setModel(event.target.value)}>{!deployments.length ? <option value="">请先配置模型</option> : null}{deployments.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select><ChevronDown /></label>
          <button className="send-button" aria-label="发送消息" disabled={busy || !model || (!text.trim() && !files.length)}><Send /></button>
        </div>
      </div>
      {busy ? <p role="status" className="composer-status active">正在创建会话并发送消息…</p> : null}
      {error ? <p role="alert" className="compose-error">{error}</p> : null}
      {!deployments.length ? <p className="new-chat-hint">还没有可用模型，<a href="/app/settings/models">前往配置模型</a>后即可开始。你的草稿会保留。</p> : <p className="new-chat-hint">发送后开始新会话 · 可直接粘贴图片 · Enter 发送，Shift + Enter 换行</p>}
    </form>
  </div></div>;
}
