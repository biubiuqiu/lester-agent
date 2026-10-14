import { api, type Conversation, type MentionInput } from "./api";
import { uploadConversationAttachments } from "./attachment-uploads";
import { sendConversationMessage } from "./message-submission";

// Called only from an explicit submit, never from a mount/navigation effect.
export async function startConversation(content: string, files: File[], model: string, onCreated: (conversation: Conversation) => void, projectId?: string, contextIds: string[] = [], agentSlug = "lester", references: MentionInput[] = [], mode: "execute" | "plan" = "execute", permission: "standard" | "auto" | "full" = "standard") {
  if ((!content.trim() && files.length === 0) || !model) throw new Error("请输入消息并选择可用模型");
  const conversation = await api<Conversation>("/api/v1/conversations", {
    method: "POST",
    body: JSON.stringify({ project_id: projectId, agent_slug: agentSlug, model_deployment_id: model, title: Array.from(content.trim() || files[0].name).slice(0, 60).join("") }),
  });
  onCreated(conversation);
  const attachments = await uploadConversationAttachments(conversation.id, files);
  await sendConversationMessage(conversation.id, { content: content.trim(), context_ids: contextIds, ...(references.length ? { references } : {}), attachment_ids: attachments.map((file) => file.id), ...(mode === "plan" ? { execution_mode: mode } : {}), permission_mode: permission });
  return conversation;
}
