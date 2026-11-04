import type { GlyphArtProps } from "./types";

/** Object silhouettes carry the identity; fine details disappear at nav sizes. */
export function SystemArt({ kind, compact, paint, gradient, depth }: GlyphArtProps) {
  switch (kind) {
    case "agent": return <>
      <rect x="10.9" y="2" width="2.2" height="5" rx="1.1" fill={paint.ink} />
      <rect x="9" y="2" width="6" height="2.5" rx="1.25" fill={paint.accent} />
      <rect x="2.2" y="10.1" width="3.3" height="6.8" rx="1.65" fill={paint.shade} />
      <rect x="18.5" y="10.1" width="3.3" height="6.8" rx="1.65" fill={paint.shade} />
      <rect x="4.3" y="5.7" width="15.4" height="16.3" rx="4.2" fill={depth} />
      <path d="M8.6 6h6.8a4 4 0 0 1 4 4v1H4.6v-1a4 4 0 0 1 4-4Z" fill={gradient} />
      <rect x="6.8" y="9.2" width="10.4" height="7.3" rx="2.6" fill={paint.ink} />
      <rect x="8.6" y="11.6" width="1.7" height="2.5" rx=".85" fill={paint.paper} />
      <rect x="13.7" y="11.6" width="1.7" height="2.5" rx=".85" fill={paint.paper} />
      <rect x="9" y="18.7" width="6" height="1.1" rx=".55" fill={paint.paper} fillOpacity=".75" />
      {!compact && <path d="M7.5 7.5h4" stroke={paint.paper} strokeWidth=".8" strokeLinecap="round" strokeOpacity=".6" />}
    </>;
    case "compass": return <>
      <circle cx="12" cy="12" r="10" fill={depth} />
      <circle cx="12" cy="12" r="8.1" fill={paint.paper} />
      {!compact && <path d="M12 5.3v1.2m6.7 5.5h-1.2M12 18.7v-1.2M5.3 12h1.2" stroke={paint.main} strokeWidth="1" strokeLinecap="round" />}
      <path d="m16.8 7.2-3.2 6.4-3.2-3.2 6.4-3.2Z" fill={paint.ink} />
      <path d="m7.2 16.8 3.2-6.4 3.2 3.2-6.4 3.2Z" fill={paint.accent} />
      <circle cx="12" cy="12" r="1" fill={paint.paper} />
      <path d="M5 8a8.1 8.1 0 0 1 6-4" stroke={paint.light} strokeWidth=".8" strokeLinecap="round" strokeOpacity=".85" />
    </>;
    case "computer": return <>
      <path d="M9.2 16h5.6l1.6 5H7.6l1.6-5Z" fill={paint.main} />
      <rect x="5.8" y="20.2" width="12.4" height="2" rx="1" fill={paint.shade} />
      <rect x="2" y="2.8" width="20" height="15" rx="2.7" fill={depth} />
      <rect x="3.8" y="4.6" width="16.4" height="10.5" rx="1.2" fill={paint.ink} />
      <rect x="5.3" y="6" width="6.2" height="7.4" rx=".9" fill={gradient} />
      <rect x="13" y="6.4" width="5.3" height="1.2" rx=".6" fill={paint.paper} fillOpacity=".9" />
      {!compact && <><rect x="13" y="9.1" width="3.8" height="1" rx=".5" fill={paint.paper} fillOpacity=".55" /><rect x="13" y="11.6" width="4.5" height="1" rx=".5" fill={paint.accent} /></>}
      <path d="M4.5 16.2h15" stroke={paint.light} strokeWidth=".7" strokeLinecap="round" strokeOpacity=".65" />
    </>;
    case "profile": return <>
      <path d="M3 22v-1.6a9 9 0 0 1 18 0V22H3Z" fill={paint.accent} />
      <path d="M3 22v-1.6a9 9 0 0 1 4.1-7.5C5.5 16.2 5.6 19.4 7 22H3Z" fill={paint.ink} fillOpacity=".22" />
      <path d="m8.9 13.4 3.1 3.4 3.1-3.4a8.7 8.7 0 0 0-6.2 0Z" fill={paint.paper} />
      <circle cx="12" cy="7.1" r="4.6" fill={gradient} />
      {!compact && <path d="M8.6 5.6a3.7 3.7 0 0 1 2.8-2" stroke={paint.paper} strokeWidth=".8" strokeLinecap="round" strokeOpacity=".7" />}
    </>;
    case "automation": return <>
      <rect x="8" y="1.3" width="6" height="2.4" rx="1.2" fill={paint.main} />
      <rect x="10" y="2.5" width="2" height="2.5" fill={paint.shade} />
      <circle cx="11" cy="12" r="9" fill={depth} />
      <circle cx="11" cy="12" r="6.9" fill={paint.paper} />
      {!compact && <path d="M11 6.4v.7m-5.6 4.9h.7m4.9 5.6v-.7" stroke={paint.main} strokeWidth=".9" strokeLinecap="round" />}
      <path d="M11 7.8V12l3 1.8" stroke={paint.ink} strokeWidth={compact ? "1.5" : "1.3"} strokeLinecap="round" strokeLinejoin="round" fill="none" />
      <path d="M16.3 14.5a.7.7 0 0 1 1.1-.6l5 3.4a.8.8 0 0 1 0 1.3l-5 3.4a.7.7 0 0 1-1.1-.6v-6.9Z" fill={paint.accent} stroke={paint.paper} strokeWidth=".8" strokeLinejoin="round" />
    </>;
    case "connections": return <>
      <g transform="rotate(-40 8 9)"><path d="M8 3a4 4 0 0 1 4 4v4a4 4 0 0 1-8 0V7a4 4 0 0 1 4-4Zm0 3a1 1 0 0 0-1 1v4a1 1 0 1 0 2 0V7a1 1 0 0 0-1-1Z" fill={depth} fillRule="evenodd" /></g>
      <g transform="rotate(-40 16 15)"><path d="M16 9a4 4 0 0 1 4 4v4a4 4 0 0 1-8 0v-4a4 4 0 0 1 4-4Zm0 3a1 1 0 0 0-1 1v4a1 1 0 1 0 2 0v-4a1 1 0 0 0-1-1Z" fill={paint.accent} fillRule="evenodd" /></g>
      <path d="m9.4 9.5 5.2 5" stroke={paint.main} strokeWidth="3" strokeLinecap="round" fill="none" />
      {!compact && <path d="m10.1 9.1 3.8 3.7" stroke={paint.light} strokeWidth=".75" strokeLinecap="round" fill="none" strokeOpacity=".8" />}
    </>;
    case "publish": return <>
      <rect x="2" y="8" width="20" height="13.5" rx="2.5" fill={gradient} />
      <path d="M2 11.5h20V19a2.5 2.5 0 0 1-2.5 2.5h-15A2.5 2.5 0 0 1 2 19v-7.5Z" fill={depth} />
      <rect x="4" y="13.2" width="16" height="6.2" rx=".8" fill={paint.paper} />
      {!compact && <><rect x="5.5" y="15" width="4.4" height="1.1" rx=".55" fill={paint.main} /><rect x="5.5" y="17.2" width="7.3" height=".9" rx=".45" fill={paint.main} fillOpacity=".45" /></>}
      <path d="M11.4 2.4a.85.85 0 0 1 1.2 0l4.7 4.7a.65.65 0 0 1-.5 1.1H14v7.1h-4V8.2H7.2a.65.65 0 0 1-.5-1.1l4.7-4.7Z" fill={paint.ink} />
      <path d="M12 3.8 15 6.9h-2v6.8h-2V6.9H9l3-3.1Z" fill={paint.accent} />
    </>;
    case "memory": return <>
      <rect x="4" y="2.5" width="16.5" height="19" rx="2.7" fill={depth} />
      <path d="M7.4 2.5h10.4a2.7 2.7 0 0 1 2.7 2.7v12.6H7.4V2.5Z" fill={gradient} />
      <path d="M7.4 18h12.2v2H7.4a1 1 0 0 1 0-2Z" fill={paint.paper} />
      <path d="M14.1 2.5h4v8.7l-2-1.5-2 1.5V2.5Z" fill={paint.accent} />
      <rect x="9.1" y="12" width="7.2" height="1.1" rx=".55" fill={paint.ink} fillOpacity=".65" />
      {!compact && <><rect x="9.1" y="14.5" width="4.8" height=".9" rx=".45" fill={paint.ink} fillOpacity=".4" /><path d="M5.9 5v13" stroke={paint.light} strokeWidth=".7" strokeLinecap="round" strokeOpacity=".6" /></>}
    </>;
    case "onboarding": return <>
      <rect x="4" y="2" width="2" height="20" rx="1" fill={paint.ink} />
      <path d="M6 3h14.1a.65.65 0 0 1 .6 1l-2.6 4.4 2.6 4.4a.65.65 0 0 1-.6 1H6V3Z" fill={gradient} />
      <path d="M6 3h2.8v10.8H6V3Z" fill={depth} />
      <path d="M10.2 6h6.3l-1.1 2.1h-5.2V6Z" fill={paint.paper} fillOpacity=".9" />
      {!compact && <path d="M10.2 9.7h4.9" stroke={paint.accent} strokeWidth="1.1" strokeLinecap="round" />}
      <rect x="2" y="20.4" width="6" height="1.8" rx=".9" fill={paint.shade} />
    </>;
    default: return null;
  }
}
