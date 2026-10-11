"use client";

import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useRef, type ReactNode, type RefObject } from "react";
import { usePathname } from "next/navigation";

const duration = 380;
const easing = "cubic-bezier(.22, 1, .36, 1)";
const reducedMotion = () => window.matchMedia("(prefers-reduced-motion: reduce)").matches;
const invert = (from: DOMRect, to: DOMRect) => `translate(${from.x - to.x}px, ${from.y - to.y}px) scale(${from.width / to.width}, ${from.height / to.height})`;
function play(element: HTMLElement, frames: Keyframe[], options: KeyframeAnimationOptions) {
  // A visual fallback must never turn a confirmed send into a send error.
  try { return element.animate(frames, options); }
  catch { return null; }
}

type Handoff = { id: string; fromPath: string; ghost?: HTMLElement; source: HTMLElement; visibility: string; timer: number };
type Motion = { handoff: (id: string, box: HTMLElement) => void; arrive: (id: string, box: HTMLElement, input: HTMLTextAreaElement | null) => void };
const MotionContext = createContext<Motion>({ handoff: () => {}, arrive: () => {} });

// The app layout survives navigation. Keep only a noninteractive visual copy
// while the destination loads; drafts and mutations stay in their existing flow.
export function ComposerMotionProvider({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const pending = useRef<Handoff | null>(null);
  const animations = useRef<Animation[]>([]);
  const clear = useCallback(() => {
    animations.current.forEach(animation => animation.cancel());
    animations.current = [];
    if (pending.current) {
      window.clearTimeout(pending.current.timer);
      pending.current.ghost?.remove();
      pending.current.source.style.visibility = pending.current.visibility;
      pending.current = null;
    }
  }, []);
  const handoff = useCallback((id: string, box: HTMLElement) => {
    clear();
    const rect = box.getBoundingClientRect();
    const moving = box.getAnimations();
    const remaining = Math.max(0, ...moving.map(animation => duration - Number(animation.currentTime ?? 0)));
    moving.forEach(animation => { try { animation.finish(); } catch { animation.cancel(); } });
    const docked = box.getBoundingClientRect();
    const visibility = box.style.visibility;
    let ghost: HTMLElement | undefined;
    if (!reducedMotion()) {
      ghost = document.createElement("div");
      ghost.className = "workspace-shell composer-motion-ghost";
      ghost.setAttribute("aria-hidden", "true");
      ghost.inert = true;
      Object.assign(ghost.style, { left: `${rect.x}px`, top: `${rect.y}px`, width: `${rect.width}px`, height: `${rect.height}px` });
      const wrapper = document.createElement("div");
      wrapper.className = "new-chat-composer";
      const copy = box.cloneNode(true) as HTMLElement;
      copy.querySelectorAll("[id]").forEach(element => element.removeAttribute("id"));
      copy.querySelectorAll("textarea").forEach((input, index) => { input.value = box.querySelectorAll("textarea")[index].value; });
      wrapper.append(copy);
      ghost.append(wrapper);
      document.body.append(ghost);
      box.style.visibility = "hidden";
      if (remaining > 0) play(ghost, [{ transform: "none" }, { transform: `translate(${docked.x - rect.x}px, ${docked.y - rect.y}px) scale(${docked.width / rect.width}, ${docked.height / rect.height})` }], { duration: remaining, easing, fill: "forwards" });
    }
    pending.current = { id, fromPath: window.location.pathname, ghost, source: box, visibility, timer: window.setTimeout(clear, 6000) };
  }, [clear]);
  const arrive = useCallback((id: string, box: HTMLElement, input: HTMLTextAreaElement | null) => {
    const entry = pending.current;
    if (!entry || entry.id !== id) return;
    const active = document.activeElement;
    if (active === document.body || (active && entry.source.contains(active))) input?.focus({ preventScroll: true });
    if (reducedMotion() || !entry.ghost) { clear(); return; }
    animations.current.forEach(animation => animation.cancel());
    const origin = entry.ghost.getBoundingClientRect();
    entry.ghost.getAnimations().forEach(animation => animation.cancel());
    Object.assign(entry.ghost.style, { left: `${origin.x}px`, top: `${origin.y}px`, width: `${origin.width}px`, height: `${origin.height}px` });
    const target = box.getBoundingClientRect();
    const incoming = play(box, [
      { transform: invert(origin, target), opacity: 0 },
      { opacity: 0, offset: .25 },
      { transform: "none", opacity: 1 },
    ], { duration, easing });
    const outgoing = play(entry.ghost, [
      { transform: "none", opacity: 1 },
      { opacity: 1, offset: .25 },
      { transform: `translate(${target.x - origin.x}px, ${target.y - origin.y}px) scale(${target.width / origin.width}, ${target.height / origin.height})`, opacity: 0 },
    ], { duration, easing, fill: "forwards" });
    if (!incoming || !outgoing) { incoming?.cancel(); outgoing?.cancel(); clear(); return; }
    animations.current = [incoming, outgoing];
    const finish = () => { if (animations.current.includes(incoming)) clear(); };
    void incoming.finished.then(finish, finish);
  }, [clear]);
  useEffect(() => {
    if (pending.current && pathname !== pending.current.fromPath && pathname !== `/app/c/${pending.current.id}`) clear();
  }, [pathname, clear]);
  useEffect(() => {
    window.addEventListener("resize", clear);
    const media = window.matchMedia("(prefers-reduced-motion: reduce)");
    const reduce = () => { if (media.matches) clear(); };
    media.addEventListener("change", reduce);
    return () => { window.removeEventListener("resize", clear); media.removeEventListener("change", reduce); clear(); };
  }, [clear]);
  return <MotionContext.Provider value={{ handoff, arrive }}>{children}</MotionContext.Provider>;
}

export const useComposerMotion = () => useContext(MotionContext);

// Measure before changing the home layout, then animate the same live input
// from its previous position. Failed creation reverses the same transition.
export function useComposerDock(busy: boolean, box: RefObject<HTMLDivElement | null>) {
  const previous = useRef<DOMRect | null>(null);
  const animation = useRef<Animation | null>(null);
  const capture = () => { previous.current = box.current?.getBoundingClientRect() ?? null; };
  useLayoutEffect(() => {
    const element = box.current, from = previous.current;
    previous.current = null;
    animation.current?.cancel();
    if (!element || !from || reducedMotion()) return;
    animation.current = play(element, [{ transform: invert(from, element.getBoundingClientRect()) }, { transform: "none" }], { duration, easing });
    return () => animation.current?.cancel();
  }, [busy, box]);
  return capture;
}
