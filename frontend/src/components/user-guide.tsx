"use client";
import { T, useT } from "@/components/i18n";


import { createContext, useContext, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { usePathname, useRouter } from "next/navigation";
import { ArrowLeft, ArrowRight, BookOpen, Check, ChevronRight, Code, FileText, Play, Sparkles, X } from "lucide-react";
import { api } from "@/lib/api";
import { guideFor, guides, pageGuide, resumeGuide, type GuideProgress, type GuideStep, type GuideTopic } from "@/lib/user-guides";

type GuideContextValue = { open: () => void };
const GuideContext = createContext<GuideContextValue | null>(null);
export function useGuide() { return useContext(GuideContext); }

export function GuideLauncher({ label = "新手引导" }: { label?: string }) {
  const t = useT();

  const guide = useContext(GuideContext);
  if (!guide) return null;
  return <button className="guide-launcher" type="button" aria-label={t(label)} title={t(label)} onClick={guide.open}><BookOpen size={16} /><span>{t(label)}</span></button>;
}

export function GuideProvider({ children }: { children: ReactNode }) {
  const t = useT();

  const pathname = usePathname();
  const router = useRouter();
  const [records, setRecords] = useState<GuideProgress[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [hub, setHub] = useState(false);
  const [topic, setTopic] = useState<GuideTopic | null>(null);
  const [step, setStep] = useState(0);
  const [busy, setBusy] = useState(false);
  const [saveError, setSaveError] = useState("");
  const pending = useRef(false);
  const autoOpened = useRef(false);
  const request = useRef(0);
  const previousPath = useRef(pathname);
  const currentGuide = topic ? guideFor(topic) : null;
  const currentStep = currentGuide?.steps[step];
  const featureTopic = pageGuide(pathname);
  const featureRecord = records.find(item => item.topic === featureTopic);
  const [hiddenHint, setHiddenHint] = useState<GuideTopic | null>(null);

  useEffect(() => {
    const tracker = request, token = ++tracker.current;
    void api<{ guides: GuideProgress[] }>("/api/v1/me/guides", { signal: AbortSignal.timeout(8000) }).then(result => {
      if (token !== request.current) return;
      setRecords(result.guides);
      setLoaded(true); setLoadError("");
      // In-progress tours are resumed explicitly, so refresh never interrupts work.
      if (!autoOpened.current && !result.guides.some(item => item.topic === "welcome")) {
        autoOpened.current = true;
        setHub(false); setTopic("welcome"); setStep(0);
      }
    }).catch(() => {
      if (token === request.current) setLoadError("引导进度暂时无法加载。你可以照常使用工作区，或稍后重试。");
    });
    return () => { tracker.current++; };
  }, [attempt]);
  // Navigation from browser history must not leave a tour blocking a different page.
  useEffect(() => {
    if (previousPath.current !== pathname) {
      previousPath.current = pathname;
      setTopic(null); setHub(false); setSaveError(""); setHiddenHint(null);
    }
  }, [pathname]);

  async function save(value: GuideProgress, after?: () => void) {
    if (pending.current) return;
    pending.current = true; setBusy(true); setSaveError("");
    const requestedPath = window.location.pathname;
    try {
      const result = await api<GuideProgress>(`/api/v1/me/guides/${value.topic}`, { method: "PATCH", signal: AbortSignal.timeout(8000), body: JSON.stringify({ step: value.step, status: value.status }) });
      setRecords(items => [...items.filter(item => item.topic !== result.topic), result]);
      if (window.location.pathname === requestedPath) after?.();
    } catch {
      setSaveError("进度未保存，请重试。也可以仅关闭本次教学，稍后重新打开。");
    } finally {
      pending.current = false; setBusy(false);
    }
  }
  function start(next: GuideTopic) {
    if (!loaded || pending.current) return;
    const position = resumeGuide(records.find(item => item.topic === next), guideFor(next).steps.length);
    void save({ topic: next, step: position, status: "in_progress" }, () => { setHub(false); setTopic(next); setStep(position); });
  }
  function close() {
    if (pending.current) return;
    if (!topic) { setHub(false); setSaveError(""); return; }
    void save({ topic, step, status: "skipped" }, () => { setTopic(null); setHub(false); });
  }
  function next() {
    if (!topic || !currentGuide) return;
    const last = step === currentGuide.steps.length - 1;
    void save({ topic, step: last ? step : step + 1, status: last ? "completed" : "in_progress" }, () => { if (last) { setTopic(null); setHub(false); } else setStep(step + 1); });
  }
  function openPage() {
    if (!currentGuide?.path || !topic) return;
    let path = currentGuide.path;
    if (topic === "models") {
      const returnTo = /^\/app(?:\/p\/[A-Za-z0-9_-]+)?$/.test(pathname) ? pathname : "/app";
      path += `?returnTo=${encodeURIComponent(returnTo)}`;
    }
    void save({ topic, step, status: "skipped" }, () => { setTopic(null); setHub(false); router.push(path); });
  }
  const showHint = loaded && !topic && !hub && featureTopic && hiddenHint !== featureTopic && (!featureRecord || featureRecord.status === "in_progress");

  return <GuideContext.Provider value={{ open: () => { if (!pending.current) { setSaveError(""); setTopic(null); setHub(true); } } }}>
    {children}
    {showHint && <aside className="guide-feature-hint" aria-label={t("功能教学提示")}>
      <BookOpen size={19} /><div><strong>{featureRecord ? t("继续了解") : t("第一次使用")}{t(guideFor(featureTopic).title)}<T>{"？"}</T></strong><p>{t(guideFor(featureTopic).description)}</p><button type="button" disabled={busy} onClick={() => start(featureTopic)}>{featureRecord ? t("继续教学") : t("看看操作步骤")}<ChevronRight size={14} /></button>{saveError && <p role="alert">{t(saveError)}</p>}</div>
      <button className="guide-icon-button" type="button" disabled={busy} aria-label={t("暂时关闭功能教学提示")} onClick={() => { void save({ topic: featureTopic, step: featureRecord?.step || 0, status: "skipped" }, () => setHiddenHint(featureTopic)); }}><X size={16} /></button>
    </aside>}
    {(hub || (currentGuide && currentStep)) && <GuideDialog step={currentStep} title={hub ? t("随用随学，认识你的工作区") : t(currentStep!.title)} onClose={close} busy={busy}>
      {hub ? <>
        <div className="guide-heading-icon"><BookOpen size={24} /></div>
        <p className="guide-eyebrow">LESTER GUIDE</p><h2 id="guide-title" tabIndex={-1}><T>{"随用随学，认识你的工作区"}</T></h2>
        <p className="guide-description"><T>{"每个功能都有逐步教学。可以继续上次的进度，也可以重新看一遍。"}</T></p>
        {loadError ? <div className="guide-error" role="alert"><p>{t(loadError)}</p><button type="button" onClick={() => { setLoadError(""); setLoaded(false); setAttempt(value => value + 1); }}><T>{"重新加载"}</T></button></div> : !loaded ? <p role="status"><T>{"正在加载进度…"}</T></p> : <div className="guide-topic-grid">{guides.map(item => {
          const record = records.find(progress => progress.topic === item.topic);
          return <button key={item.topic} type="button" disabled={busy} onClick={() => start(item.topic)}><span><strong>{t(item.title)}</strong><small>{t(item.description)}</small></span><span className={`guide-topic-status ${record?.status || ""}`}>{record?.status === "completed" ? <><Check size={13} /><T>{"再看一遍"}</T></> : record ? t("继续 {0}/{1}", [Math.min(record.step + 1, item.steps.length), item.steps.length]) : t("{0} 步", [item.steps.length])}<ChevronRight size={14} /></span></button>;
        })}</div>}
        <footer className="guide-hub-footer"><a href="/docs/usage" target="_blank" rel="noopener noreferrer"><T>{"阅读帮助文档"}</T></a><button type="button" className="primary-button" disabled={busy} onClick={close}><T>{"返回工作区"}</T></button></footer>
      </> : <>
        <p className="guide-eyebrow">{t(currentGuide!.title)} <span>{step + 1} / {currentGuide!.steps.length}</span></p>
        <h2 id="guide-title" tabIndex={-1} aria-live="polite">{t(currentStep!.title)}</h2>
        <p className="guide-description">{t(currentStep!.body)}</p>
        {currentStep!.visual === "journey" && <div className="guide-journey" aria-label={t("从目标到成果")}><span><Sparkles size={20} /><T>{"描述目标"}</T></span><ArrowRight size={17} /><span><Play size={20} /><T>{"交给 Agent"}</T></span><ArrowRight size={17} /><span><FileText size={20} /><T>{"查看成果"}</T></span></div>}
        {currentStep!.visual === "files" && <div className="guide-file-example" aria-label={t("文件标签示意")}><div><span><Code size={14} />index.html</span><span><FileText size={14} />report.md</span></div><p><T>{"网页预览"}</T><span>⇄</span> <T>{"源码"}</T></p><small><T>{"操作示意 · 实际文件在任务中生成"}</T></small></div>}
        {currentStep!.hint && <p className="guide-hint">{t(currentStep!.hint)}</p>}
        {currentGuide!.path && pathname !== currentGuide!.path && <button className="guide-page-link" type="button" disabled={busy} onClick={openPage}>{t("打开 {0} 页面，稍后继续", [t(currentGuide!.title)])}<ArrowRight size={15} /></button>}
        <div className="guide-progress" role="progressbar" aria-label={t("教学进度")} aria-valuemin={0} aria-valuemax={currentGuide!.steps.length} aria-valuenow={step + 1}>{currentGuide!.steps.map((_, index) => <i key={index} className={index <= step ? "done" : ""} />)}</div>
        <footer className="guide-step-footer"><button type="button" className="guide-skip" disabled={busy} onClick={close}><T>{"稍后再学"}</T></button><div>{step > 0 && <button className="secondary-button" type="button" disabled={busy} onClick={() => { void save({ topic: topic!, step: step - 1, status: "in_progress" }, () => setStep(step - 1)); }}><ArrowLeft size={15} /><T>{"上一步"}</T></button>}<button type="button" className="primary-button" disabled={busy} onClick={next}>{busy ? t("保存中…") : step === currentGuide!.steps.length - 1 ? t("完成教学") : step === 0 && topic === "welcome" ? t("开始了解") : t("下一步")}{!busy && <ArrowRight size={15} />}</button></div></footer>
      </>}
      {saveError && <div className="guide-error" role="alert"><p>{t(saveError)}</p><button type="button" disabled={busy} onClick={() => { setTopic(null); setHub(false); setSaveError(""); }}><T>{"仅关闭本次"}</T></button></div>}
    </GuideDialog>}
  </GuideContext.Provider>;
}

function GuideDialog({ step, title, busy, onClose, children }: { step?: GuideStep; title: string; busy: boolean; onClose: () => void; children: ReactNode }) {
  const t = useT();

  const dialog = useRef<HTMLDialogElement>(null);
  const spotlight = useRef<HTMLDivElement>(null);
  const card = useRef<HTMLElement>(null);
  useLayoutEffect(() => {
    const node = dialog.current;
    if (!node) return;
    node.showModal();
    return () => node.close();
  }, []);
  useLayoutEffect(() => {
    dialog.current?.querySelector<HTMLElement>("#guide-title")?.focus();
    const shade = spotlight.current, panel = card.current, modal = dialog.current;
    if (!shade || !panel || !modal) return;
    const reset = () => { shade.hidden = true; modal.classList.remove("guide-has-target"); panel.style.removeProperty("left"); panel.style.removeProperty("top"); panel.style.removeProperty("position"); };
    reset();
    const target = step?.target ? document.querySelector<HTMLElement>(step.target) : null;
    if (!target || !target.getClientRects().length) return;
    target.scrollIntoView({ block: "nearest", inline: "nearest", behavior: "instant" });
    const measure = () => {
      const bounds = target.getBoundingClientRect();
      const top = Math.max(6, bounds.top - 5), left = Math.max(6, bounds.left - 5);
      const bottom = Math.min(window.innerHeight - 6, bounds.bottom + 5), right = Math.min(window.innerWidth - 6, bounds.right + 5);
      if (bottom <= top || right <= left) { reset(); return; }
      shade.hidden = false; modal.classList.add("guide-has-target");
      Object.assign(shade.style, { top: `${top}px`, left: `${left}px`, width: `${right - left}px`, height: `${bottom - top}px` });
      const width = panel.offsetWidth, height = panel.offsetHeight, gap = 14, edge = 12;
      let x = (window.innerWidth - width) / 2, y = (window.innerHeight - height) / 2;
      if (window.innerWidth >= 700) {
        if (window.innerWidth - right > width + gap + edge) { x = right + gap; y = top; }
        else if (left > width + gap + edge) { x = left - width - gap; y = top; }
        else if (window.innerHeight - bottom > height + gap + edge) y = bottom + gap;
        else if (top > height + gap + edge) y = top - height - gap;
      } else if (top < window.innerHeight / 2) y = window.innerHeight - height - edge;
      else y = edge;
      Object.assign(panel.style, { position: "fixed", left: `${Math.max(edge, Math.min(window.innerWidth - width - edge, x))}px`, top: `${Math.max(edge, Math.min(window.innerHeight - height - edge, y))}px` });
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(target);
    observer.observe(panel);
    window.addEventListener("resize", measure);
    window.addEventListener("scroll", measure, true);
    return () => { observer.disconnect(); window.removeEventListener("resize", measure); window.removeEventListener("scroll", measure, true); reset(); };
  }, [step, title]);

  return <dialog ref={dialog} className="guide-dialog" aria-labelledby="guide-title" onKeyDown={event => { if (event.key === "Escape") event.stopPropagation(); }} onCancel={event => { event.preventDefault(); if (!busy) onClose(); }}>
    <div ref={spotlight} hidden className="guide-spotlight" aria-hidden="true" />
    <section ref={card} className={`guide-card ${step ? "guide-card-step" : "guide-card-hub"}`}>
      <button type="button" className="guide-close guide-icon-button" disabled={busy} onClick={onClose} aria-label={t("关闭新手引导")}><X size={18} /></button>
      <div className="guide-card-content">{children}</div>
    </section>
  </dialog>;
}
