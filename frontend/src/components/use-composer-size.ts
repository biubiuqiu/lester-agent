"use client";

import { useLayoutEffect, useRef, type RefObject } from "react";

/** Fit the draft, with a bounded scroll area and an immediate reduced-motion path. */
export function useComposerSize(input: RefObject<HTMLTextAreaElement | null>, value: string) {
  const measured = useRef(false);
  useLayoutEffect(() => {
    const node = input.current;
    if (!node) return;
    let animation: Animation | undefined;
    const media = matchMedia("(prefers-reduced-motion: reduce)");
    const measure = (animate: boolean) => { animation = fitTextarea(node, animate, animation, media); };
    measure(measured.current);
    measured.current = true;
    let width = node.clientWidth;
    const observer = new ResizeObserver(() => {
      if (width === node.clientWidth) return;
      width = node.clientWidth;
      measure(false);
    });
    observer.observe(node);
    const resize = () => measure(false);
    const reduce = () => { if (media.matches) animation?.cancel(); };
    window.addEventListener("resize", resize);
    media.addEventListener("change", reduce);
    return () => { observer.disconnect(); animation?.cancel(); window.removeEventListener("resize", resize); media.removeEventListener("change", reduce); };
  }, [input, value]);
}

function fitTextarea(node: HTMLTextAreaElement, animate: boolean, animation: Animation | undefined, media: MediaQueryList) {
  const before = node.getBoundingClientRect().height;
  animation?.cancel();
  const css = getComputedStyle(node);
  const line = parseFloat(css.lineHeight) || 24;
  const padding = parseFloat(css.paddingTop) + parseFloat(css.paddingBottom);
  const minimum = Math.max(parseFloat(css.minHeight) || 0, line * node.rows + padding);
  const maximum = Math.max(minimum, Math.min(line * 6 + padding, innerHeight * .3));
  node.style.height = "0px";
  const needed = node.scrollHeight;
  const height = Math.max(minimum, Math.min(needed, maximum));
  node.style.height = `${height}px`;
  node.style.overflowY = needed > height + 1 ? "auto" : "hidden";
  if (animate && before > 0 && Math.abs(before - height) > 2 && !media.matches) {
    try { animation = node.animate([{ height: `${before}px` }, { height: `${height}px` }], { duration: 150, easing: "ease-out" }); } catch { /* Sizing works without animation. */ }
  }
  return animation;
}
