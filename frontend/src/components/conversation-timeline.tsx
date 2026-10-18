"use client";
import { useI18n, useT } from "@/components/i18n";
import type { Message } from "@/lib/api";
import Link from "next/link";
import { ProjectRulesReceipt } from "./project-rules";
import { MessageContent } from "./message-content";
import { RunEvent, RunNarrative } from "./tool-timeline";
import { FileText, MessageSquare, Puzzle } from "lucide-react";
import { withoutPersistedModelText } from "@/lib/model-text-events";
import { orderedMessages, runSegments, segmentOrder } from "@/lib/timeline-order";
import { useMemo } from "react";
import { ConversationRecord } from "./conversation-presentation";

type TimelineItem =
  | { kind: "message"; id: string; order: number; message: Message }
  | { kind: "run"; id: string; order: number; events: RunEvent[]; hideFinalText: boolean };

export function ConversationTimeline({ messages, events, detailed = false }: { messages: Message[]; events: RunEvent[]; detailed?: boolean }) {
  const items = useMemo(() => buildTimeline(messages, events), [messages, events]);
  return <><div className="conversation-stream">{items.map((item) => item.kind === "message"
    ? <ChatMessage message={item.message} key={item.id} />
    : <RunNarrative presentation={detailed ? "full" : "focus"} detailed={detailed} events={item.events} hideFinalText={item.hideFinalText} key={item.id} />)}</div>
    {!detailed ? <ConversationRecord slot="activity">{items.filter(item => item.kind === "run").map(item => <RunNarrative presentation="history" detailed events={item.events} hideFinalText={item.hideFinalText} key={item.id} />)}</ConversationRecord> : null}
  </>;
}

function ChatMessage({ message }: { message: Message }) {
  const { locale } = useI18n();
  const t = useT();
  const isUser = message.role === "user";
  return (
    <article className={`chat-message ${isUser ? "user" : "assistant"}`}>
      {isUser && <span className="message-avatar">W</span>}
      <div className="chat-message-content">
        <MessageContent content={message.content} linkFiles={!isUser} actions={!isUser} quoteSource={!isUser ? t("引用回答") : undefined} />
        {message.metadata?.attachments?.length ? <div className="message-attachments">{message.metadata.attachments.map((attachment) => <span key={attachment.id}><FileText /><span><strong>{attachment.original_name}</strong><small>{formatBytes(attachment.size_bytes)} · .agent/upload</small></span></span>)}</div> : null}
        {message.metadata?.contexts?.map((entry) => <details className="message-context" key={entry.id}><summary>@{entry.title} · v{entry.version}</summary><pre>{entry.content}</pre></details>)}
        {message.metadata?.conversations?.length || message.metadata?.skills?.length ? <div className="message-reference-list">
          {message.metadata.conversations?.map(entry => <Link key={`conversation:${entry.id}`} href={`/app/c/${encodeURIComponent(entry.id)}`} target="_blank" rel="noopener noreferrer" title={t("会话引用 · 截至消息序号 {0}", [entry.max_seq])}><MessageSquare size={13} aria-hidden="true" /><span>@{entry.title}</span><small>{t("会话")}</small></Link>)}
          {message.metadata.skills?.map(entry => <span key={`skill:${entry.id}`} title={`${entry.slug} · v${entry.version}`}><Puzzle size={13} aria-hidden="true" /><span>@{entry.name}</span><small>Skill</small></span>)}
        </div> : null}
        <ProjectRulesReceipt rules={message.metadata?.project_rules} />
        <time>{new Date(message.created_at).toLocaleTimeString(locale, { hour: "2-digit", minute: "2-digit" })}</time>
      </div>
    </article>
  );
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function buildTimeline(messages: Message[], events: RunEvent[]): TimelineItem[] {
  messages = orderedMessages(messages);
  const items: TimelineItem[] = messages.map((message, index) => ({
    kind: "message",
    id: `message-${message.id}`,
    order: index * 10,
    message,
  }));
  const runs = new Map<string, RunEvent[]>();
  for (const event of events) {
    const run = runs.get(event.run_id) ?? [];
    run.push(event);
    runs.set(event.run_id, run);
  }
  for (const [runID, runEvents] of runs) {
    for (const segment of runSegments(runEvents)) {
      const sortedEvents = withoutPersistedModelText(segment, messages);
      const hideFinalText = hasPersistedFinalMessage(sortedEvents, messages);
      if (!hasVisibleNarrative(sortedEvents, hideFinalText)) continue;
      items.push({
        kind: "run",
        id: `run-${runID}-${segment[0].id}`,
        order: segmentOrder(segment, messages),
        events: sortedEvents,
        hideFinalText,
      });
    }
  }
  return items.toSorted((a, b) => a.order - b.order || itemPriority(a) - itemPriority(b));
}

function hasVisibleNarrative(events: RunEvent[], hideFinalText: boolean) {
  if (events.some((event) => event.type === "RUN_FAILED" || event.type === "RUN_CANCELLED" || event.type === "TOOL_STARTED" || event.type.startsWith("CONTEXT_COMPACT"))) return true;
  const textCount = events.filter((event) => event.type === "MODEL_TEXT" || event.type === "MODEL_DELTA").length;
  const completed = events.some((event) => event.type === "RUN_COMPLETED");
  return completed && hideFinalText ? textCount > 1 : textCount > 0;
}

function hasPersistedFinalMessage(events: RunEvent[], messages: Message[]) {
  if (!events.some((event) => event.type === "RUN_COMPLETED")) return false;
  let content = "";
  for (const event of events) {
    if (event.type === "MODEL_STARTED") content = "";
    if (event.type === "MODEL_TEXT") content = String(event.payload.text ?? "");
    if (event.type === "MODEL_DELTA") content += String(event.payload.delta ?? "");
  }
  content = content.trim();
  if (!content) return false;
  const startedAt = new Date(events[0]?.created_at ?? 0).getTime();
  return messages.some((message) => message.role === "assistant"
    && new Date(message.created_at).getTime() >= startedAt
    && message.content.trim() === content);
}

function itemPriority(item: TimelineItem) {
  if (item.kind === "run") return 1;
  return item.message.role === "user" ? 0 : 2;
}
