"use client";

import { useEffect, useId, useRef, useState } from "react";
import Link from "next/link";
import { ArrowUpRight, ChevronDown, Inbox } from "lucide-react";
import { readPersonalInbox, type PersonalInbox as InboxSnapshot } from "@/lib/personal";
import { useI18n } from "./i18n";

// Listing and previewing replies are reads only. A conversation's own rendered
// transcript records the confirmed reading boundary, never this summary.
export function PersonalInbox({ snapshot, active }: { snapshot?: InboxSnapshot; active: boolean }) {
  const { t, locale } = useI18n();
  const listId = useId();
  const [open, setOpen] = useState(false);
  const [page, setPage] = useState<{ source: InboxSnapshot | undefined; value: InboxSnapshot } | null>(null);
  const [work, setWork] = useState<{ source: InboxSnapshot | undefined; controller: AbortController; pending: boolean; error: string } | null>(null);
  const request = useRef<AbortController | null>(null);
  const currentSnapshot = useRef(snapshot);
  const currentActive = useRef(active);
  const inbox = page && page.source === snapshot ? page.value : snapshot;
  const pending = !!work && work.source === snapshot && !work.controller.signal.aborted && work.pending;
  const error = work?.source === snapshot ? work?.error ?? "" : "";
  useEffect(() => {
    currentSnapshot.current = snapshot;
    currentActive.current = active;
    request.current?.abort();
    request.current = null;
    return () => request.current?.abort();
  }, [snapshot, active]);

  async function load(more = false) {
    if (request.current) return;
    const controller = new AbortController();
    request.current = controller;
    const source = currentSnapshot.current;
    const previous = inbox;
    setWork({ source, controller, pending: true, error: "" });
    try {
      const next = await readPersonalInbox(more ? previous?.next_cursor : "", controller.signal);
      if (controller.signal.aborted || !currentActive.current || currentSnapshot.current !== source) return;
      const tasks = more ? [...new Map([...(previous?.tasks ?? []), ...next.tasks].map(task => [task.id, task])).values()] : next.tasks;
      setPage({ source, value: { ...next, tasks } });
    } catch {
      if (!controller.signal.aborted && currentActive.current && currentSnapshot.current === source) setWork({ source, controller, pending: false, error: "未读回复加载失败，请重试。" });
    } finally {
      if (request.current === controller && !controller.signal.aborted) { request.current = null; setWork(previous => previous?.controller === controller ? { ...previous, pending: false } : previous); }
    }
  }

  return <section className="personal-inbox">
    <button type="button" className="personal-inbox-toggle" aria-expanded={open} aria-controls={listId} onClick={() => { setOpen(value => !value); if (!open && !inbox) void load(); }}>
      <Inbox size={14} aria-hidden="true" /><span>{t("未读回复")}</span>{inbox ? <span className="personal-inbox-count">{inbox.total}</span> : null}<ChevronDown size={13} aria-hidden="true" />
    </button>
    {open ? <div id={listId} className="personal-inbox-content" aria-busy={pending}>
      {error ? <p className="personal-inbox-error" role="alert">{t(error)}<button type="button" disabled={pending} onClick={() => void load(!!inbox?.next_cursor)}>{t("重试")}</button></p> : null}
      {!inbox && pending ? <p className="personal-inbox-empty" role="status">{t("正在载入…")}</p> : null}
      {inbox && !inbox.tasks.length && !error ? <p className="personal-inbox-empty">{t("暂无未读回复")}</p> : null}
      {inbox?.tasks.length ? <ul>{inbox.tasks.map(task => <li key={task.id}>
        <Link href={`/app/c/${task.id}`} aria-label={t("打开会话 {0}", [task.title])}><span><strong>{task.title}</strong><small><span className="personal-inbox-project" title={task.project_name}>{task.project_name}</span>{task.reply_at && Number.isFinite(Date.parse(task.reply_at)) ? <time dateTime={task.reply_at}>{new Date(task.reply_at).toLocaleString(locale, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })}</time> : null}</small></span><ArrowUpRight size={14} aria-hidden="true" /></Link>
        {task.reply ? <details><summary>{t("预览回复")}</summary><p>{task.reply}</p></details> : null}
      </li>)}</ul> : null}
      {inbox?.next_cursor ? <button type="button" className="personal-inbox-more" disabled={pending} onClick={() => void load(true)}>{t(pending ? "正在载入…" : "加载更多回复")}</button> : null}
    </div> : null}
  </section>;
}
