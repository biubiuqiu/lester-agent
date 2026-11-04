# Design system and tokens

[Documentation index](README.md) · [Project home](../README.md) · [简体中文](zh-CN/design-system.md)

Lester's design reference collects its brand, semantic colors, typography, spacing, radii, motion, icon family and shared control states in one browsable HTML file. Use it to review a style change before changing individual screens.

The default direction is **Graphite**, selected on November 4, 2026 from the first round's fourth color study: warm-white surfaces (`#FAF9F6`), graphite actions (`#373735`), a warm stone/ivory folded L and a small champagne terminal arrow (`#D9BC78`). Jade and Indigo remain local comparison palettes in the reference page. They do not add application theme preferences or change a user's task, draft, permissions or model configuration.

## Open the reference

- On a running deployment, open `/design-system.html`, for example [http://localhost:13000/design-system.html](http://localhost:13000/design-system.html).
- For offline review, download or open [`frontend/public/design-system.html`](../frontend/public/design-system.html) in a browser. SVG, styles and token data are embedded; the page does not need a CDN or application API.
- The page includes token roles and values, local palette previews, Logo and icon sizes, icon search, shared component states, contrast information, copy/export controls, reference sources and the current migration scope. Preview controls perform local design-review actions only.

This is a developer reference, separate from the six-language product help under `/docs`.

## Source and generated files

| Layer | Owner | Purpose |
| --- | --- | --- |
| Token source | [`frontend/src/design/tokens.json`](../frontend/src/design/tokens.json) | Shared values and their design roles; change the source to change the system. |
| Token core | [`frontend/scripts/design-token-core.mjs`](../frontend/scripts/design-token-core.mjs) | Resolves token data and provides the shared generation functions. |
| Builder | [`frontend/scripts/build-design-system.mjs`](../frontend/scripts/build-design-system.mjs) | Produces the CSS and self-contained reference. |
| Generated CSS | [`frontend/src/app/design-tokens.css`](../frontend/src/app/design-tokens.css) | CSS custom properties consumed by the frontend. |
| Generated reference | [`frontend/public/design-system.html`](../frontend/public/design-system.html) | Reviewable, offline-capable HTML. |
| Product artwork | [`frontend/src/components/product-glyph.tsx`](../frontend/src/components/product-glyph.tsx) | The category, Agent and Skill SVG family. |
| Brand source | [`frontend/src/components/brand-mark-art.tsx`](../frontend/src/components/brand-mark-art.tsx) | Shared geometry and paint roles for the app, reference and favicon. |
| Generated favicon | [`frontend/src/app/icon.svg`](../frontend/src/app/icon.svg) | Browser favicon generated from the brand source and default palette; the app renders the same source inline. |

Keep raw palette values distinct from semantic roles such as surface, text, border, accent and status, then let components consume those roles. This makes a palette change traceable instead of requiring unrelated replacements of similar hex values. The reference exposes the actual project token names; do not invent a second set of values in its examples.

Do not hand-edit generated CSS, HTML or Logo colors. Update their source or builder, then regenerate them together.

## Change and validate

From the repository root:

```bash
cd frontend
pnpm design:build
pnpm design:check
pnpm lint
pnpm build
```

`design:build` updates generated artifacts. `design:check` verifies that the committed artifacts match the current inputs; run it after changing tokens, artwork or reference generation.

For a design change:

1. Choose the semantic role being changed. Brand accents, destructive actions and errors are independent roles.
2. Update the source data or relevant SVG geometry, then run the commands above.
3. Review text and background pairs, default/hover/pressed/disabled/focus states, and the Logo on light and dark backgrounds. Use actual values when checking contrast.
4. Check icons at 16, 20 and 24px as well as larger previews. Check a real desktop page and a narrow phone layout; a large icon grid alone is insufficient.
5. Exercise the affected control, keyboard focus and draft return path using the existing browser fixtures. A reference-page demonstration does not validate a live model or app connector.

## Icon and brand rules

- Use a 24-unit icon grid and a 1.7-unit reference stroke, with consistent rounded caps and joins. Simplify secondary detail at 16/20px instead of scaling a detailed illustration indefinitely.
- Personal projects, ordinary projects, conversations and results use shared SVGs. A person outline inside the folder identifies the actual default project; filesystem directories retain their file-type symbols. Menus, references and project pages reuse the same geometry, and the public demo uses the real brand mark.
- Keep category artwork neutral: a document or model icon does not imply that a task passed validation. Installation and approval indicators remain separate, driven by real state.
- Use the shared graphite ink and quiet surface layer for category artwork. Keep ordinary action and status controls legible; preserve the established third-party provider marks and user-authored identity.
- Preserve the folded **L** and terminal arrow in the Logo. Use warm stone/ivory layers on the deep graphite tile, with champagne confined to the small arrow. Its colors come from the same brand tokens as the interface; status colors retain their independent meanings.
- Decorative icons remain hidden from assistive technologies beside their real text labels. Icons must not introduce remote asset requests, task actions or permissions.

## Current coverage

This extraction covers the foundation, shared control styling and SVG presentation. The reference states which values are connected to the product. It does **not** claim that every historical CSS color has been migrated.

Conversation/sidebar dimensions, domain-specific layouts, terminal and source-highlighting palettes, provider branding, rendered user documents and isolated HTML previews retain their own boundaries. Migrate an additional surface through its semantic roles and verify that surface, rather than replacing every matching literal globally.

The deployment URL is a design reference, not a new workspace route or a product-wide theme switch. These visual changes require an updated Web build; they introduce no database migration or backend configuration.

## Research

The design rules draw on verified official sources from Lucide, Phosphor, Fluent System Icons, Radix Colors and IBM Carbon. Fixed revisions, observed rules, the previous Lester baseline and evidence limits are recorded in [the research notes](../research/lester-design-system/README.md). The notes distinguish a source's documented behavior from Lester's own design decisions; external libraries are references, not a claim that their complete systems have been adopted.
