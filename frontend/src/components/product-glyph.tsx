"use client";

import { useId, type SVGProps } from "react";
import { BrandMark } from "./brand";
import { ProjectContentArt } from "./glyph-art/project-content";
import { WorkArt } from "./glyph-art/work-art";
import { SystemArt } from "./glyph-art/system-art";
import { glyphFamilies, glyphPaint } from "./glyph-art/palette";
import type { GlyphArtProps, ProductGlyphKind } from "./glyph-art/types";
export type { ProductGlyphKind } from "./glyph-art/types";

/** Filled object marks with local paints and simplified compact geometry. */
export function ProductGlyph({ kind, size = 24, className = "", appearance = "color", style, strokeWidth, stroke, ...props }: Omit<SVGProps<SVGSVGElement>, "children"> & { kind: ProductGlyphKind; size?: number; appearance?: "color" | "monochrome" }) {
  const instance = useId();
  const prefix = `pg-${kind}-${instance.replace(/[^a-zA-Z0-9_-]/g, "")}`;
  const compact = size <= 20;
  const inherited = appearance === "monochrome" || stroke && stroke !== "none" ? stroke || "currentColor" : undefined;
  const paint = glyphPaint(kind, inherited);
  const art: GlyphArtProps = { kind, compact, paint, gradient: `url(#${prefix}-light)`, depth: `url(#${prefix}-depth)` };
  return <svg className={`product-glyph ${className}`.trim()} data-product-glyph={kind} data-glyph-family={glyphFamilies[kind]} data-glyph-appearance={inherited ? "monochrome" : "color"} data-optical-size={compact ? "compact" : "regular"} width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="none" style={{ strokeWidth: strokeWidth ?? 1.2, ...style }} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false" {...props}>
    <defs>
      <linearGradient id={`${prefix}-light`} x1="4" y1="3" x2="20" y2="22" gradientUnits="userSpaceOnUse"><stop stopColor={paint.light} /><stop offset="1" stopColor={paint.main} /></linearGradient>
      <linearGradient id={`${prefix}-depth`} x1="4" y1="4" x2="20" y2="22" gradientUnits="userSpaceOnUse"><stop stopColor={paint.main} /><stop offset="1" stopColor={paint.shade} /></linearGradient>
    </defs>
    <ProjectContentArt {...art} /><WorkArt {...art} /><SystemArt {...art} />
  </svg>;
}

export function AgentGlyph({ slug, size = 32 }: { slug: string; size?: number }) {
  if (slug === "lester") return <span className="product-agent-mark" style={{ width: size, height: size }} aria-hidden="true"><BrandMark /></span>;
  const kind: ProductGlyphKind = slug === "agent-designer" ? "designer" : slug === "franklin" ? "compass" : slug === "michael" ? "strategy" : slug === "trevor" ? "bolt" : "agent";
  return <ProductGlyph kind={kind} size={size} />;
}

export function SkillGlyph({ slug, size = 28 }: { slug: string; size?: number }) {
  const kind: ProductGlyphKind = slug === "code-review" ? "review" : slug === "project-planner" ? "project" : slug === "data-explorer" ? "analysis" : slug === "conversation-reader" ? "conversation" : "skill";
  return <ProductGlyph kind={kind} size={size} />;
}
