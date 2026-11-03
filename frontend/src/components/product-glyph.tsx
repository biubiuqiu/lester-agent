import { BrandMark } from "./brand";

export type ProductGlyphKind = "document" | "analysis" | "web" | "code" | "review" | "project" | "conversation" | "skill" | "agent" | "designer" | "compass" | "strategy" | "bolt" | "model" | "computer" | "context" | "profile" | "automation" | "connections" | "publish" | "memory" | "onboarding";

const jade = "#438F7C", mint = "#E4F3ED", violet = "#8872BC", lilac = "#EEE8F8", gold = "#C9954B", cream = "#FAEDD6";

/** Quiet, two-tone category artwork. Action and status icons stay monochrome. */
export function ProductGlyph({ kind, size = 24, className = "" }: { kind: ProductGlyphKind; size?: number; className?: string }) {
  return <svg className={`product-glyph ${className}`.trim()} data-product-glyph={kind} width={size} height={size} viewBox="0 0 32 32" fill="none" stroke="#425F59" strokeWidth="1.65" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false"><Shapes kind={kind} /></svg>;
}

function Shapes({ kind }: { kind: ProductGlyphKind }) {
  switch (kind) {
    case "document": return <><path d="M8 4h11l6 6v16a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2Z" fill={mint} /><path d="M19 4v6h6" fill={lilac} stroke={violet} /><path d="M11 15h9m-9 5h7m-7 4h4" stroke={jade} /><path d="m20 22 2 2 4-5" stroke={gold} strokeWidth="2" /></>;
    case "analysis": return <><rect x="4" y="5" width="24" height="23" rx="4" fill={lilac} stroke={violet} /><path d="M9 22v-5m7 5V11m7 11v-8" stroke={jade} strokeWidth="3.2" /><path d="m8 11 6-3 7 1 5-5" stroke={gold} /><circle cx="26" cy="4" r="1.5" fill={gold} stroke="none" /></>;
    case "web": return <><rect x="3" y="5" width="26" height="22" rx="4" fill={mint} /><path d="M3 11h26" /><circle cx="7" cy="8" r=".85" fill={gold} stroke="none" /><circle cx="10" cy="8" r=".85" fill={violet} stroke="none" /><rect x="7" y="15" width="8" height="8" rx="2" fill={lilac} stroke={violet} /><path d="M19 16h6m-6 4h4m-4 3h5" stroke={jade} /></>;
    case "code": return <><rect x="3" y="5" width="26" height="22" rx="4" fill={lilac} stroke={violet} /><path d="m11 12-5 4 5 4m10-8 5 4-5 4m-3-10-4 13" stroke={jade} /><path d="M7 8h.01m3 0h.01" stroke={gold} strokeWidth="2" /></>;
    case "review": return <><path d="M7 4h12l6 6v16a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2Z" fill={mint} /><path d="M19 4v6h6m-14 4-3 3 3 3m5-6 3 3-3 3" stroke={jade} /><circle cx="25" cy="24" r="6" fill={lilac} stroke={violet} /><path d="m22 24 2 2 4-4" stroke={violet} /></>;
    case "project": return <><path d="M4 8a3 3 0 0 1 3-3h6l3 4h10a2 2 0 0 1 2 2v13a3 3 0 0 1-3 3H6a3 3 0 0 1-3-3V10" fill={mint} /><path d="M4 12h24" stroke={jade} /><rect x="9" y="16" width="15" height="8" rx="2" fill={lilac} stroke={violet} /><path d="m13 20 2 2 5-5" stroke={violet} /></>;
    case "conversation": return <><path d="M10 6h15a4 4 0 0 1 4 4v11l-5-3H10a3 3 0 0 1-3-3V9a3 3 0 0 1 3-3Z" fill={lilac} stroke={violet} /><path d="M7 11h13a4 4 0 0 1 4 4v7a3 3 0 0 1-3 3H9l-6 4V15a4 4 0 0 1 4-4Z" fill={mint} /><path d="M8 16h11m-11 5h7" stroke={jade} /></>;
    case "skill": return <><path d="m16 3 12 7v13l-12 7-12-7V10L16 3Z" fill={mint} /><path d="m4 10 12 7 12-7m-12 7v13" stroke={jade} /><path d="m10 6 12 7v6l-5 3v-6L6 10" fill={lilac} stroke={violet} /><circle cx="24" cy="5" r="3" fill={cream} stroke={gold} /></>;
    case "agent": return <><path d="M6 11a5 5 0 0 1 5-5h10a5 5 0 0 1 5 5v12a4 4 0 0 1-4 4H10a4 4 0 0 1-4-4V11Z" fill={mint} /><rect x="9" y="12" width="14" height="9" rx="4" fill={lilac} stroke={violet} /><path d="M12 16h.01m8 0h.01" stroke={violet} strokeWidth="2.5" /><path d="M13 24h6M3 14v6m26-6v6m-13-17v3" stroke={jade} /><circle cx="16" cy="3" r="2" fill={cream} stroke={gold} /></>;
    case "designer": return <><rect x="4" y="6" width="22" height="22" rx="4" fill={lilac} stroke={violet} /><path d="m11 21 2-6L24 4l4 4-11 11-6 2Z" fill={mint} stroke={jade} /><path d="m21 7 4 4m-12 4 4 4M9 25h11" stroke={jade} /><circle cx="7" cy="9" r="1.5" fill={gold} stroke="none" /></>;
    case "compass": return <><circle cx="16" cy="16" r="12" fill={mint} /><path d="m22 9-4 10-10 4 5-10 9-4Z" fill={lilac} stroke={violet} /><path d="m13 13 5 6M16 4V2m0 28v-2M4 16H2m28 0h-2" stroke={jade} /><circle cx="16" cy="16" r="2" fill={cream} stroke={gold} /></>;
    case "strategy": return <><path d="m16 3 13 7-13 7-13-7L16 3Z" fill={lilac} stroke={violet} /><path d="m4 16 12 7 12-7m-24 7 12 7 12-7" stroke={jade} /><path d="m11 10 4 2 6-4" stroke={gold} /></>;
    case "bolt": return <><path d="M11 4h13l-7 9h9L12 29l3-13H7l4-12Z" fill={cream} stroke={gold} /><path d="M6 7 3 10m25 12 2-3" stroke={violet} /><path d="m15 16 3-7" stroke={jade} /></>;
    case "model": return <><rect x="7" y="7" width="18" height="18" rx="5" fill={lilac} stroke={violet} /><path d="M12 3v4m8-4v4m-8 18v4m8-4v4M3 12h4m-4 8h4m18-8h4m-4 8h4" stroke={jade} /><path d="m16 11 5 5-5 5-5-5 5-5Z" fill={mint} stroke={jade} /><circle cx="23" cy="9" r="2" fill={cream} stroke={gold} /></>;
    case "computer": return <><rect x="3" y="4" width="26" height="20" rx="4" fill={mint} /><path d="M3 19h26m-16 5-1 5m9-5 1 5m-13 0h14" stroke={jade} /><rect x="8" y="8" width="16" height="7" rx="2" fill={lilac} stroke={violet} /><path d="M11 11.5h.01m4 0h.01" stroke={gold} strokeWidth="2" /></>;
    case "context": return <><path d="M5 6c5-2 8-1 11 1 3-2 6-3 11-1v21c-5-2-8-1-11 1-3-2-6-3-11-1V6Z" fill={mint} /><path d="M16 7v21m-7-17h3m-3 5h3m8-5h3m-3 5h3" stroke={jade} /><path d="M22 3h5v10l-2.5-2-2.5 2V3Z" fill={lilac} stroke={violet} /></>;
    case "profile": return <><rect x="4" y="3" width="24" height="26" rx="6" fill={lilac} stroke={violet} /><circle cx="16" cy="12" r="4.5" fill={mint} stroke={jade} /><path d="M8 26v-2a8 8 0 0 1 16 0v2" fill={mint} stroke={jade} /><circle cx="26" cy="5" r="3" fill={cream} stroke={gold} /></>;
    case "automation": return <><circle cx="14" cy="15" r="11" fill={mint} /><path d="M14 8v7l5 3" stroke={jade} strokeWidth="2" /><path d="M10 2h8m-4 0v2" stroke={violet} /><path d="m24 19 6 4-6 4v-8Z" fill={lilac} stroke={violet} /><path d="M17 27h4" stroke={gold} /></>;
    case "connections": return <><path d="m12 6 4 4-6 6-4-4a4 4 0 0 1 6-6Zm14 14-4-4-6 6 4 4a4 4 0 0 0 6-6Z" fill={mint} stroke={jade} /><path d="m12 12 8 8m-8-3-3 3m6-11 3-3m-5 20-3 3m15-16 3-3" stroke={violet} /><circle cx="25" cy="6" r="3" fill={cream} stroke={gold} /></>;
    case "publish": return <><rect x="3" y="8" width="26" height="20" rx="4" fill={mint} /><path d="M3 14h10m-6-3h.01m3 0h.01" stroke={jade} /><path d="M10 20h7m-7 4h4" stroke={violet} /><path d="M23 20V3m-6 6 6-6 6 6" stroke={violet} strokeWidth="2" /><circle cx="23" cy="3" r="2" fill={cream} stroke={gold} /></>;
    case "memory": return <><rect x="5" y="5" width="22" height="23" rx="5" fill={lilac} stroke={violet} /><path d="M10 13h12m-12 5h8m-8 5h5" stroke={jade} /><path d="M11 3v5m10-5v5" stroke={violet} /><path d="m22 20 2 3 4-6" stroke={gold} strokeWidth="2" /></>;
    case "onboarding": return <><path d="M6 28V5m0 0h18l-3 5 3 5H6" fill={mint} stroke={jade} /><path d="M15 15v9l4-2 4 2v-9" fill={lilac} stroke={violet} /><path d="M3 28h7" stroke={jade} /><circle cx="27" cy="5" r="2.5" fill={cream} stroke={gold} /></>;
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
