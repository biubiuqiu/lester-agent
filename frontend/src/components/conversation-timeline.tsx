import type { Message } from "@/lib/api";
import { MessageContent } from "./message-content";
import { RunEvent, RunNarrative } from "./tool-timeline";
import { FileText } from "lucide-react";

type TimelineItem =
  | { kind: "message"; id: string; timestamp: number; message: Message }
  | { kind: "run"; id: string; timestamp: number; events: RunEvent[]; hideFinalText: boolean };

export function ConversationTimeline({ messages, events }: { messages: Message[]; events: RunEvent[] }) {
  const items = buildTimeline(messages, events);
  return <div className="conversation-stream">{items.map((item) => item.kind === "message"
    ? <ChatMessage message={item.message} key={item.id} />
    : <RunNarrative events={item.events} hideFinalText={item.hideFinalText} key={item.id} />)}</div>;
}

function ChatMessage({ message }: { message: Message }) {
  const isUser = message.role === "user";
  return (
    <article className={`chat-message ${isUser ? "user" : "assistant"}`}>
      {isUser && <span className="message-avatar">W</span>}
      <div className="chat-message-content">
        <MessageContent content={message.content} />
        {message.metadata?.attachments?.length ? <div className="message-attachments">{message.metadata.attachments.map((attachment) => <span key={attachment.id}><FileText /><span><strong>{attachment.original_name}</strong><small>{formatBytes(attachment.size_bytes)} · .agent/upload</small></span></span>)}</div> : null}
        <time>{new Date(message.created_at).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}</time>
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
  const items: TimelineItem[] = messages.map((message) => ({
    kind: "message",
    id: `message-${message.id}`,
    timestamp: new Date(message.created_at).getTime(),
    message,
  }));
  const runs = new Map<string, RunEvent[]>();
  for (const event of events) {
    const run = runs.get(event.run_id) ?? [];
    run.push(event);
    runs.set(event.run_id, run);
  }
  for (const [runID, runEvents] of runs) {
    const sortedEvents = runEvents.toSorted((a, b) => a.id - b.id);
    const hideFinalText = hasPersistedFinalMessage(sortedEvents, messages);
    if (!hasVisibleNarrative(sortedEvents, hideFinalText)) continue;
    items.push({
      kind: "run",
      id: `run-${runID}`,
      timestamp: new Date(sortedEvents[0]?.created_at ?? 0).getTime(),
      events: sortedEvents,
      hideFinalText,
    });
  }
  return items.toSorted((a, b) => a.timestamp - b.timestamp || itemPriority(a) - itemPriority(b));
}

function hasVisibleNarrative(events: RunEvent[], hideFinalText: boolean) {
  if (events.some((event) => event.type === "RUN_FAILED" || event.type === "RUN_CANCELLED" || event.type === "TOOL_STARTED")) return true;
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
