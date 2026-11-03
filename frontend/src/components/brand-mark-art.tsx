/** Canonical artwork shared by the app, favicon and offline design workbench. */
export function BrandMarkArt({ prefix, className = "", size = 64, colors }: { prefix: string; className?: string; size?: number; colors?: Record<string, string> }) {
  const paint = (key: string) => colors?.[key] ?? `var(--lester-${key})`;
  return <svg xmlns="http://www.w3.org/2000/svg" className={className || undefined} width={size} height={size} viewBox="0 0 64 64" fill="none" aria-hidden="true" focusable="false">
    <defs>
      <linearGradient id={`${prefix}-tile`} x1="9" y1="3" x2="53" y2="63" gradientUnits="userSpaceOnUse"><stop stopColor={paint("logo-tile")} /><stop offset="1" stopColor={paint("logo-tile-end")} /></linearGradient>
      <linearGradient id={`${prefix}-stem`} x1="18" y1="14" x2="29" y2="46" gradientUnits="userSpaceOnUse"><stop stopColor={paint("logo-stem-light")} /><stop offset=".52" stopColor={paint("logo-stem-mid")} /><stop offset="1" stopColor={paint("logo-stem-dark")} /></linearGradient>
      <linearGradient id={`${prefix}-fold`} x1="18" y1="45" x2="47" y2="37" gradientUnits="userSpaceOnUse"><stop stopColor={paint("logo-fold-start")} /><stop offset="1" stopColor={paint("logo-fold-end")} /></linearGradient>
    </defs>
    <rect x="2" y="2" width="60" height="60" rx="18" fill={`url(#${prefix}-tile)`} />
    <rect x="2.5" y="2.5" width="59" height="59" rx="17.5" stroke={paint("logo-rim")} strokeOpacity=".19" />
    <path d="M17 15a1 1 0 0 1 1-1h9a1 1 0 0 1 1 1v21L17 47V15Z" fill={`url(#${prefix}-stem)`} />
    <path d="m17 39 11-11v8L17 47v-8Z" fill={paint("logo-fold-shadow")} fillOpacity=".3" />
    <path d="m17 47 11-11h21a1 1 0 0 1 .76 1.65l-9.56 11a1 1 0 0 1-.76.35H19a2 2 0 0 1-2-2Z" fill={`url(#${prefix}-fold)`} />
    <path d="M18 15h9M18 46l10-10h20" stroke={paint("logo-rim")} strokeOpacity=".4" strokeWidth=".65" strokeLinecap="round" strokeLinejoin="round" />
    <path d="m38 15 8 7-8 7" stroke={paint("logo-arrow")} strokeWidth="4.6" strokeLinecap="round" strokeLinejoin="round" />
  </svg>;
}
