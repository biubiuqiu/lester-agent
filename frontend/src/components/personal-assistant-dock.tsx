"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { createPortal } from "react-dom";
import { LoaderCircle, MessageCircle, X } from "lucide-react";
import { usePathname } from "next/navigation";
import { api, type Deployment, type Project, type UserProfile } from "@/lib/api";
import { resumeViewState, viewStateEpoch } from "@/lib/conversation-view-state";
import { useT } from "./i18n";
import { PersonalAssistant } from "./personal-assistant";

type Resources = { user: UserProfile; projects: Project[]; deployments: Deployment[] };
type DockContext = {
  publish: (resources: Resources) => void;
  tasksChanged: () => void;
  delegatedRevision: number;
  registerLauncher: (host: HTMLSpanElement | null) => void;
};
const Context = createContext<DockContext | null>(null);
export const usePersonalAssistantDock = () => useContext(Context);
const mobileSnapshot = () => window.matchMedia("(max-width: 1000px)").matches;
const desktopSnapshot = () => false;
const subscribeMobile = (change: () => void) => {
  const media = window.matchMedia("(max-width: 1000px)");
  media.addEventListener("change", change); return () => media.removeEventListener("change", change);
};
export function AssistantLauncherSlot() {
  const dock = usePersonalAssistantDock();
  return <span className="personal-launcher-slot" ref={dock?.registerLauncher} />;
}

// App-layout ownership keeps the same dialogue, draft and in-flight receipt
// mounted while task/project/settings pages change. It owns no event stream.
export function PersonalAssistantDock({ children }: { children: React.ReactNode }) {
  const t = useT();
  const pathname = usePathname();
  const [open, setOpen] = useState(false), [visited, setVisited] = useState(false);
  const [resources, setResources] = useState<Resources | null>(null);
  const [revision, setRevision] = useState(0), [delegatedRevision, setDelegatedRevision] = useState(0);
  const [loading, setLoading] = useState(false), [error, setError] = useState("");
  const [working, setWorking] = useState(false), [unread, setUnread] = useState(false);
  const [launcherHost, registerLauncher] = useState<HTMLSpanElement | null>(null);
  const mobile = useSyncExternalStore(subscribeMobile, mobileSnapshot, desktopSnapshot);
  const dialog = useRef<HTMLDialogElement>(null), launcher = useRef<HTMLButtonElement>(null);
  const openRef = useRef(false), mounted = useRef(true), request = useRef<AbortController | null>(null);
  const wasWorking = useRef(false);
  const publish = useCallback((next: Resources) => setResources(next), []);
  const tasksChanged = useCallback(() => setRevision(value => value + 1), []);
  const context = useMemo(() => ({ publish, tasksChanged, delegatedRevision, registerLauncher }), [publish, tasksChanged, delegatedRevision]);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; request.current?.abort(); }; }, []);
  useEffect(() => {
    // Keep the entry above bottom input controls, including the right-hand
    // workspace, and follow taller unsent task drafts.
    let observer: ResizeObserver | undefined;
    const measure = () => {
      const composer = [...document.querySelectorAll<HTMLElement>(".compose-box")].find(element => element.getBoundingClientRect().height > 0);
      if (!composer) { launcher.current?.style.setProperty("--personal-launcher-bottom", "20px"); return false; }
      const controls = composer.closest<HTMLElement>(".composer") ?? composer;
      const question = document.querySelector<HTMLElement>(".question-dock");
      const place = () => {
        const rect = composer.getBoundingClientRect();
        if (!composer.isConnected || !rect.height) {
          observer?.disconnect(); pending.observe(document.body, { childList: true, subtree: true });
          return;
        }
        const overlaps = rect.bottom > window.innerHeight - 100;
        launcher.current?.style.setProperty("--personal-launcher-bottom", `${overlaps ? Math.max(20, window.innerHeight - Math.min(controls.getBoundingClientRect().top, question?.getBoundingClientRect().height ? question.getBoundingClientRect().top : rect.top) + 12) : 20}px`);
      };
      observer = new ResizeObserver(place); observer.observe(controls); if (question) observer.observe(question); place();
      return true;
    };
    const pending = new MutationObserver(() => { if (measure()) pending.disconnect(); });
    if (!measure()) pending.observe(document.body, { childList: true, subtree: true });
    const resize = () => { observer?.disconnect(); if (!measure()) pending.observe(document.body, { childList: true, subtree: true }); };
    window.addEventListener("resize", resize);
    return () => { pending.disconnect(); observer?.disconnect(); window.removeEventListener("resize", resize); };
  }, [pathname]);

  const load = useCallback(async () => {
    request.current?.abort();
    const controller = new AbortController(); request.current = controller;
    const epoch = viewStateEpoch();
    setLoading(true); setError("");
    try {
      const [user, projects, models] = await Promise.all([
        api<UserProfile>("/api/v1/me", { signal: controller.signal }),
        api<{ projects: Project[] }>("/api/v1/projects", { signal: controller.signal }),
        api<{ deployments: Deployment[] }>("/api/v1/model-deployments", { signal: controller.signal }),
      ]);
      if (mounted.current && !controller.signal.aborted && resumeViewState(`${user.user_id}.${user.workspace_id}.`, epoch)) {
        setResources({ user, projects: projects.projects, deployments: models.deployments });
      }
    } catch (reason) {
      if (mounted.current && !controller.signal.aborted) setError(reason instanceof Error ? reason.message : "个人助理加载失败，请稍后检查状态");
    } finally { if (mounted.current && !controller.signal.aborted) setLoading(false); }
  }, []);
  const close = useCallback(() => {
    openRef.current = false; setOpen(false); dialog.current?.close();
    launcher.current?.focus({ preventScroll: true });
  }, []);
  const show = () => {
    openRef.current = true; setOpen(true); setVisited(true); setUnread(false);
    if (!loading) void load();
  };
  const activity = useCallback((busy: boolean) => {
    if (wasWorking.current && !busy && !openRef.current) setUnread(true);
    wasWorking.current = busy; setWorking(busy);
  }, []);
  useEffect(() => {
    if (!open) return;
    const element = dialog.current;
    if (!element) return;
    const mobile = window.matchMedia("(max-width: 1279px)");
    const present = () => {
      // Native modal semantics trap focus and block the page only on phones.
      const focused = element.contains(document.activeElement) ? document.activeElement as HTMLElement : null;
      if (element.open) element.close();
      if (mobile.matches) element.showModal(); else element.show();
      (focused ?? element.querySelector<HTMLTextAreaElement>("textarea"))?.focus({ preventScroll: true });
    };
    present(); mobile.addEventListener("change", present);
    const viewport = window.visualViewport;
    const resize = () => {
      element.style.setProperty("--personal-viewport-height", `${viewport?.height ?? window.innerHeight}px`);
      element.style.setProperty("--personal-viewport-top", `${viewport?.offsetTop ?? 0}px`);
    };
    resize(); viewport?.addEventListener("resize", resize); viewport?.addEventListener("scroll", resize);
    return () => { mobile.removeEventListener("change", present); viewport?.removeEventListener("resize", resize); viewport?.removeEventListener("scroll", resize); element.close(); };
  }, [open]);


  const entry = <button ref={launcher} type="button" className={`personal-dock-launcher${working ? " is-working" : ""}`} title={t("个人助理")} aria-label={t(open ? "收起个人助理" : "打开个人助理")} aria-expanded={open} aria-controls="personal-assistant-panel" onClick={() => open ? close() : show()}>
      {open ? <X size={18} /> : working ? <LoaderCircle size={18} className="personal-spinner" /> : <MessageCircle size={18} />}
      <span>Lester</span><span className="personal-mobile-mark" aria-hidden="true">L</span>{unread ? <i aria-label={t("有更新")} /> : null}
    </button>;
  return <Context.Provider value={context}>
    <div className={`personal-app-surface${open ? " personal-companion-open" : ""}`}>{children}</div>
    {mobile && launcherHost ? createPortal(entry, launcherHost) : entry}
    <dialog ref={dialog} id="personal-assistant-panel" className="personal-dock" aria-label={t("个人助理")} onCancel={event => { if (event.target === event.currentTarget) { event.preventDefault(); close(); } }} onKeyDown={event => { if (event.key === "Escape" && !event.nativeEvent.isComposing && event.target instanceof Element && !event.target.closest(".personal-memory-dialog")) { event.preventDefault(); event.stopPropagation(); close(); } }} onClick={event => { if (event.target === event.currentTarget) { const rect = event.currentTarget.getBoundingClientRect(); if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) close(); } }}>
      {visited && resources ? <PersonalAssistant key={`${resources.user.user_id}:${resources.user.workspace_id}`} {...resources} revision={revision} active={open} onClose={close} onActivity={activity} onDelegated={() => { setDelegatedRevision(value => value + 1); tasksChanged(); }} /> : <div className="personal-dock-loading"><header><strong>{t("个人助理")}</strong><button type="button" className="icon-button" aria-label={t("收起个人助理")} onClick={close}><X size={18} /></button></header>{error ? <p role="alert">{t(error)}<button type="button" onClick={() => void load()}>{t("检查状态")}</button></p> : <p role="status">{t("正在载入…")}</p>}</div>}
    </dialog>
  </Context.Provider>;
}
