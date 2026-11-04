import type { SVGProps } from "react";
import { BrandMark } from "./brand";

export type ProductGlyphKind = "personal-project" | "artifact" | "document" | "analysis" | "web" | "code" | "review" | "project" | "conversation" | "skill" | "agent" | "designer" | "compass" | "strategy" | "bolt" | "model" | "computer" | "context" | "profile" | "automation" | "connections" | "publish" | "memory" | "onboarding";

const tint = "var(--lester-icon-fill, transparent)";

/** A shared 24px grid; compact sizes omit secondary detail and gain optical weight. */
export function ProductGlyph({ kind, size = 24, className = "", style, strokeWidth, ...props }: Omit<SVGProps<SVGSVGElement>, "children"> & { kind: ProductGlyphKind; size?: number }) {
  const compact = size <= 20;
  return <svg className={`product-glyph ${className}`.trim()} data-product-glyph={kind} data-optical-size={compact ? "compact" : "regular"} width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="var(--lester-icon-stroke, currentColor)" style={{ strokeWidth: strokeWidth ?? (compact ? "var(--lester-icon-stroke-width-small, 1.8)" : "var(--lester-icon-stroke-width, 1.7)"), ...style }} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false" {...props}><Shapes kind={kind} compact={compact} /></svg>;
}

function Shapes({ kind, compact }: { kind: ProductGlyphKind; compact: boolean }) {
  switch (kind) {
    case "personal-project": return <><path d="M3 7V5a2 2 0 0 1 2-2h5l3 4h6a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z" fill={tint} /><path d="M3 9h18" /><circle cx="12" cy="13" r="1.75" /><path d={compact ? "M9 18.5a3 3 0 0 1 6 0" : "M8.5 19v-.5a3.5 3.5 0 0 1 7 0v.5"} /></>;
    case "artifact": return <><path d="M7 3h12a2 2 0 0 1 2 2v12" /><rect x="3" y="6" width="15" height="15" rx="2.5" fill={tint} /><path d="M7 11h7m-7 4h4" />{!compact && <path d="M7 18h6" />}</>;
    case "document": return <><path d="M7 3h8l4 4v13H7a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2Z" fill={tint} /><path d="M15 3v5h4M9 12h6m-6 4h6" />{!compact && <path d="M9 8h2" />}</>;
    case "analysis": return <><rect x="3" y="3" width="18" height="18" rx="3" fill={tint} /><path d="M7 16v-4m5 4V7m5 9v-6" />{!compact && <path d="M7 18h10" />}</>;
    case "web": return <><rect x="2.5" y="4" width="19" height="16" rx="2.5" fill={tint} /><path d="M2.5 9h19M6 6.5h.01" /><rect x="6" y="12" width="5" height="5" rx="1" />{!compact && <path d="M14 12h4m-4 4h3" />}</>;
    case "code": return <><rect x="2.5" y="4" width="19" height="16" rx="2.5" fill={tint} /><path d="m8 9-3 3 3 3m8-6 3 3-3 3m-3-7-2 9" /></>;
    case "review": return <><path d="M5 3h9l4 4v5M14 3v5h4M5 3a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h6" fill={tint} /><path d="m8 10-2 2 2 2m3-4 2 2-2 2" /><circle cx="16.5" cy="16.5" r="3.5" fill={tint} /><path d="m19 19 3 3" /></>;
    case "project": return <><path d="M3 7V5a2 2 0 0 1 2-2h5l3 4h6a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z" fill={tint} /><path d="M3 9h18" />{!compact && <path d="M7 14h5m-5 3h9" />}</>;
    case "conversation": return <><path d="M4 4h16a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H9l-7 4V6a2 2 0 0 1 2-2Z" fill={tint} /><path d="M7 9h10m-10 4h6" /></>;
    case "skill": return <><path d="m12 2 9 5v10l-9 5-9-5V7l9-5Z" fill={tint} /><path d="m3 7 9 5 9-5m-9 5v10" />{!compact && <path d="m8 4 9 5v4" />}</>;
    case "agent": return <><rect x="4" y="6" width="16" height="15" rx="4" fill={tint} /><path d="M12 3v3M2 11v5m20-5v5M8 12h.01m8 0h.01m-7 5h6" />{!compact && <path d="M10 3h4" />}</>;
    case "designer": return <><path d="M11 4H5a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2h13a2 2 0 0 0 2-2v-6" fill={tint} /><path d="m9 15 1-4 9-9 3 3-9 9-4 1Zm7-10 3 3" />{!compact && <path d="M7 18h7" />}</>;
    case "compass": return <><circle cx="12" cy="12" r="9.5" fill={tint} /><path d="m16.5 7.5-3 6-6 3 3-6 6-3Z" />{!compact && <path d="m10.5 10.5 3 3" />}</>;
    case "strategy": return <><path d="m12 2 10 5-10 5L2 7l10-5Z" fill={tint} /><path d="m3 12 9 5 9-5m-18 5 9 5 9-5" /></>;
    case "bolt": return <path d="m13 2-9 12h7l-1 8 10-13h-7l1-7Z" fill={tint} />;
    case "model": return <><rect x="6" y="6" width="12" height="12" rx="3" fill={tint} /><path d="M9 2v4m6-4v4M9 18v4m6-4v4M2 9h4m-4 6h4m12-6h4m-4 6h4" />{!compact && <path d="m12 9 3 3-3 3-3-3 3-3Z" />}</>;
    case "computer": return <><rect x="2" y="3" width="20" height="14" rx="2.5" fill={tint} /><path d="M9 17v4m6-4v4m-8 0h10" />{!compact && <path d="M2 13h20m-15-6h4" />}</>;
    case "context": return <><path d="M3 4c4-1 6 0 9 2 3-2 5-3 9-2v16c-4-1-6 0-9 2-3-2-5-3-9-2V4Z" fill={tint} /><path d="M12 6v16" />{!compact && <path d="M6 9h3m-3 4h3m6-4h3m-3 4h3" />}</>;
    case "profile": return <><circle cx="12" cy="8" r="4" fill={tint} /><path d="M4 22v-2a8 8 0 0 1 16 0v2" fill={tint} /></>;
    case "automation": return <><circle cx="11" cy="12" r="9" fill={tint} /><path d="M11 7v5l3 2m4 1 5 3-5 3v-6Z" fill={tint} />{!compact && <path d="M8 1h6" />}</>;
    case "connections": return <><path d="m9 6-2-2a3.5 3.5 0 0 0-5 5l3 3a3.5 3.5 0 0 0 5 0m5 6 2 2a3.5 3.5 0 0 0 5-5l-3-3a3.5 3.5 0 0 0-5 0" fill={tint} /><path d="m8 16 8-8" /></>;
    case "publish": return <><path d="M4 12v7a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-7" fill={tint} /><path d="M12 16V2m-5 5 5-5 5 5" /></>;
    case "memory": return <><rect x="4" y="3" width="16" height="18" rx="2" fill={tint} /><path d="M14 3v7l2-1.5 2 1.5V3M8 13h8m-8 4h5" />{!compact && <path d="M8 7h2" />}</>;
    case "onboarding": return <><path d="M5 22V3m0 1h15l-3 5 3 5H5" fill={tint} />{!compact && <path d="M2 22h6" />}</>;
  }
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
