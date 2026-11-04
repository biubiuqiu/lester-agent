export type ProductGlyphKind = "personal-project" | "artifact" | "document" | "analysis" | "web" | "code" | "review" | "project" | "conversation" | "skill" | "agent" | "designer" | "compass" | "strategy" | "bolt" | "model" | "computer" | "context" | "profile" | "automation" | "connections" | "publish" | "memory" | "onboarding";

export type GlyphPaint = { ink: string; main: string; light: string; shade: string; accent: string; paper: string };
export type GlyphArtProps = { kind: ProductGlyphKind; compact: boolean; paint: GlyphPaint; gradient: string; depth: string };
