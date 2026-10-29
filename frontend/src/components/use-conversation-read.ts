"use client";

import { useEffect, useRef, type RefObject } from "react";
import { api, type Message, type UserProfile } from "@/lib/api";
import { viewStateEpoch } from "@/lib/conversation-view-state";

type Reply = { id: string; seq: number };
export type ConversationReadNotice = { userId: string; workspaceId: string; conversationId: string; lastReadSeq: number };

// Durable reply end markers, rather than route visits or streamed text, define
// the reading boundary. A background tab or an obscured reply cannot advance it.
export function useConversationRead({ conversationId, ownerId, user, messages, thread }: {
  conversationId: string;
  ownerId: string;
  user: UserProfile | null;
  messages: Message[];
  thread: RefObject<HTMLDivElement | null>;
}) {
  const userId = user?.user_id ?? "", workspaceId = user?.workspace_id ?? "";
  const scope = `${userId}.${workspaceId}.${conversationId}`;
  const confirmed = useRef({ scope, seq: 0 });
  useEffect(() => {
    const root = thread.current;
    if (!root || !userId || !workspaceId || ownerId !== userId) return;
    if (confirmed.current.scope !== scope) confirmed.current = { scope, seq: 0 };
    const epoch = viewStateEpoch();
    const replies = new Map(messages.filter(message => message.role === "assistant" && message.content.trim() && Number.isSafeInteger(message.seq) && message.seq! > 0)
      .map(message => [message.id, { id: message.id, seq: message.seq! }]));
    if (!replies.size) return;
    let active = true, inFlight = false, attempted = 0, frame = 0;
    let controller: AbortController | null = null;
    const visible = new Map<Element, Reply>();
    let channel: BroadcastChannel | undefined;
    try { channel = new BroadcastChannel(`lester.conversation-read.v1.${userId}.${workspaceId}`); } catch { /* Reopening still reads the durable server state. */ }
    const current = () => active && epoch === viewStateEpoch();
    const foreground = () => current() && !document.hidden && document.visibilityState === "visible" && document.hasFocus();
    const exposed = (element: Element) => {
      const rect = element.getBoundingClientRect(), bounds = root.getBoundingClientRect();
      const x = rect.left + rect.width / 2, y = rect.top + rect.height / 2;
      if (!rect.width || !rect.height || x < Math.max(0, bounds.left) || x >= Math.min(innerWidth, bounds.right) || y < Math.max(0, bounds.top) || y >= Math.min(innerHeight, bounds.bottom)) return false;
      const hit = document.elementFromPoint(x, y);
      return !!hit && (element.contains(hit) || element.closest(".chat-message")?.contains(hit));
    };
    const send = async () => {
      frame = 0;
      if (!foreground() || inFlight) return;
      let next: Reply | undefined;
      for (const [element, reply] of visible) {
        if (reply.seq > Math.max(confirmed.current.seq, attempted) && exposed(element) && (!next || reply.seq > next.seq)) next = reply;
      }
      if (!next) return;
      attempted = next.seq; inFlight = true;
      const request = new AbortController(); controller = request;
      const deadline = window.setTimeout(() => request.abort(), 15_000);
      try {
        const result = await api<{ last_read_seq: number }>(`/api/v1/conversations/${conversationId}/read`, { method: "POST", signal: request.signal, body: JSON.stringify({ message_id: next.id }) });
        if (!current() || request.signal.aborted || !Number.isSafeInteger(result.last_read_seq) || result.last_read_seq < next.seq) return;
        confirmed.current.seq = Math.max(confirmed.current.seq, result.last_read_seq);
        const notice: ConversationReadNotice = { userId, workspaceId, conversationId, lastReadSeq: result.last_read_seq };
        window.dispatchEvent(new CustomEvent("lester:conversation-read", { detail: notice }));
        channel?.postMessage(notice);
      } catch { /* Do not publish a read state that the server has not confirmed. */ }
      finally {
        clearTimeout(deadline);
        inFlight = false;
        if (current()) schedule();
      }
    };
    const schedule = () => { if (current() && !frame) frame = requestAnimationFrame(() => { void send(); }); };
    const retry = () => { attempted = confirmed.current.seq; schedule(); };
    const clear = () => { active = false; controller?.abort(); cancelAnimationFrame(frame); };
    const observer = new IntersectionObserver(entries => {
      for (const entry of entries) {
        const reply = replies.get((entry.target as HTMLElement).dataset.replyReadId ?? "");
        if (entry.isIntersecting && reply) visible.set(entry.target, reply);
        else visible.delete(entry.target);
      }
      schedule();
    }, { root, threshold: 1 });
    for (const marker of root.querySelectorAll<HTMLElement>("[data-reply-read-id]")) if (replies.has(marker.dataset.replyReadId ?? "")) observer.observe(marker);
    root.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("focus", retry);
    window.addEventListener("resize", schedule);
    // Closing a native modal can expose the same marker without changing its
    // intersection or the window's focus. Recheck after focus restoration/click.
    document.addEventListener("focusin", schedule);
    document.addEventListener("pointerup", retry, { passive: true });
    document.addEventListener("visibilitychange", retry);
    window.addEventListener("lester:session-cleared", clear);
    return () => {
      clear(); observer.disconnect(); channel?.close();
      root.removeEventListener("scroll", schedule);
      window.removeEventListener("focus", retry);
      window.removeEventListener("resize", schedule);
      document.removeEventListener("focusin", schedule);
      document.removeEventListener("pointerup", retry);
      document.removeEventListener("visibilitychange", retry);
      window.removeEventListener("lester:session-cleared", clear);
    };
  }, [scope, userId, workspaceId, conversationId, ownerId, messages, thread]);
}
