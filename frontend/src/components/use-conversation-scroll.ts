"use client";

import { useLayoutEffect, useRef, useState } from "react";
import { readView, updateView } from "@/lib/conversation-view-state";

export function useConversationScroll(key: string, revision: string) {
  const thread = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const following = useRef(readView(key).thread.following);
  const [unseen, setUnseen] = useState(false);
  const previousRevision = useRef(revision);
  useLayoutEffect(() => {
    const node = thread.current;
    if (!node) return;
    const saved = readView(key).thread;
    following.current = saved.following;
    node.scrollTop = saved.following ? node.scrollHeight : saved.top;
    const save = () => updateView(key, { thread: { top: node.scrollTop, following: following.current } });
    const onScroll = () => {
      following.current = node.scrollHeight - node.clientHeight - node.scrollTop < 64;
      if (following.current) setUnseen(false);
    };
    const observer = new ResizeObserver(() => {
      if (following.current) node.scrollTop = node.scrollHeight;
    });
    if (content.current) observer.observe(content.current);
    observer.observe(node);
    node.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("pagehide", save);
    return () => { save(); observer.disconnect(); node.removeEventListener("scroll", onScroll); window.removeEventListener("pagehide", save); };
  }, [key]);
  useLayoutEffect(() => {
    if (revision === previousRevision.current) return;
    previousRevision.current = revision;
    const node = thread.current;
    if (following.current && node) node.scrollTop = node.scrollHeight;
    else setUnseen(true);
  }, [revision]);
  const jump = () => {
    following.current = true;
    setUnseen(false);
    if (thread.current) thread.current.scrollTop = thread.current.scrollHeight;
  };
  return { thread, content, unseen, jump };
}
