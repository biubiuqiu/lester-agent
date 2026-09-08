import { api, upload, type Attachment, type Conversation } from "./api";

// Called only from an explicit submit, never from a mount/navigation effect.
export async function startConversation(content: string, files: File[], model: string, onCreated: (conversation: Conversation) => void) {
  if ((!content.trim() && files.length === 0) || !model) throw new Error("请输入消息并选择可用模型");
  const conversation = await api<Conversation>("/api/v1/conversations", {
    method: "POST",
    body: JSON.stringify({ agent_slug: "lester", model_deployment_id: model, title: Array.from(content.trim() || files[0].name).slice(0, 60).join("") }),
  });
  onCreated(conversation);
  const attachments = await Promise.all(files.map((file) => {
    const form = new FormData();
    form.append("file", file);
    return upload<Attachment>(`/api/v1/conversations/${conversation.id}/attachments`, form);
  }));
  await api<{ run_id: string }>(`/api/v1/conversations/${conversation.id}/messages`, {
    method: "POST", body: JSON.stringify({ content: content.trim(), attachment_ids: attachments.map((file) => file.id) }),
  });
  return conversation;
}
