import type { GlyphPaint, ProductGlyphKind } from "./types";

type Family = "amber" | "blue" | "violet" | "teal" | "indigo" | "coral" | "slate";
export const glyphFamilies: Record<ProductGlyphKind, Family> = {
  "personal-project": "amber", project: "amber", artifact: "violet", document: "blue",
  conversation: "blue", web: "teal", context: "indigo", analysis: "teal", code: "indigo",
  review: "violet", skill: "violet", designer: "coral", strategy: "blue", bolt: "amber",
  model: "indigo", agent: "indigo", compass: "teal", computer: "slate", profile: "coral",
  automation: "blue", connections: "blue", publish: "teal", memory: "coral", onboarding: "teal",
};

/** Object identity has its own palette; application status and theme remain separate. */
export function glyphPaint(kind: ProductGlyphKind, inherited?: string): GlyphPaint {
  if (inherited) return { ink: inherited, main: inherited, light: inherited, shade: inherited, accent: inherited, paper: "var(--lester-color-surface, #fff)" };
  const family = glyphFamilies[kind];
  const color = (role: keyof GlyphPaint) => `var(--lester-glyph-${family}-${role})`;
  return { ink: color("ink"), main: color("main"), light: color("light"), shade: color("shade"), accent: kind === "automation" || kind === "context" ? "var(--lester-glyph-amber-main)" : color("accent"), paper: color("paper") };
}
