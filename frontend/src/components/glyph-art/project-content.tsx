import type { GlyphArtProps } from "./types";

/** Filled, layered objects: the silhouette carries identity before the detail. */
export function ProjectContentArt({ kind, compact, paint, gradient, depth }: GlyphArtProps) {
  switch (kind) {
    case "personal-project":
      return <>
        <path d="M2.7 7V5.6A2.1 2.1 0 0 1 4.8 3.5h4.1l2.5 2.8h7.8a2.1 2.1 0 0 1 2.1 2.1v10.1a2.1 2.1 0 0 1-2.1 2.1H4.8a2.1 2.1 0 0 1-2.1-2.1V7Z" fill={depth} />
        <path d="M4.3 8.3h15.4" stroke={paint.paper} strokeWidth=".8" strokeOpacity=".45" />
        <path d="M2.7 10.1c0-.8.6-1.4 1.4-1.4h15.8c.8 0 1.4.6 1.4 1.4v8.6a2 2 0 0 1-2 2H4.7a2 2 0 0 1-2-2v-8.6Z" fill={gradient} stroke={paint.shade} strokeWidth=".65" strokeOpacity=".55" />
        <rect x="8.2" y="11.1" width="7.6" height="7.4" rx="2.1" fill={paint.paper} />
        <circle cx="12" cy="13.5" r="1.2" fill={paint.ink} />
        <path d="M9.9 17.1v-.2a2.1 2.1 0 0 1 4.2 0v.2H9.9Z" fill={paint.ink} />
        {!compact && <path d="M5 18.6h1.4m10.8 0h1.8" stroke={paint.paper} strokeWidth=".8" strokeOpacity=".55" />}
      </>;
    case "project":
      return <>
        <path d="M2.7 7V5.6A2.1 2.1 0 0 1 4.8 3.5h4.1l2.5 2.8h7.8a2.1 2.1 0 0 1 2.1 2.1v10.1a2.1 2.1 0 0 1-2.1 2.1H4.8a2.1 2.1 0 0 1-2.1-2.1V7Z" fill={depth} />
        {!compact && <rect x="9.7" y="7.5" width="8.4" height="9.4" rx="1.1" transform="rotate(7 13.9 12.2)" fill={paint.accent} />}
        <rect x="6.5" y="7.3" width="9.2" height="10.3" rx="1.2" transform="rotate(-6 11.1 12.45)" fill={paint.paper} />
        <path d="M2.7 12.6c0-.8.6-1.4 1.4-1.4h6.8l1.5-1.8h7.5c.8 0 1.4.6 1.4 1.4v7.9a2 2 0 0 1-2 2H4.7a2 2 0 0 1-2-2v-6.1Z" fill={gradient} stroke={paint.shade} strokeWidth=".65" strokeOpacity=".55" />
        <path d="M5.4 17.4h4.3" stroke={paint.paper} strokeWidth="1.3" strokeLinecap="round" strokeOpacity=".9" />
        {!compact && <path d="M5 13.1h4.3l1.5-1.8h7.7" stroke={paint.paper} strokeWidth=".65" strokeOpacity=".45" />}
      </>;
    case "artifact":
      return <>
        <rect x="7" y="2.8" width="14.1" height="16.3" rx="2.4" fill={depth} />
        {!compact && <path d="M10 5h7.9" stroke={paint.paper} strokeWidth=".8" strokeOpacity=".5" />}
        <rect x="2.9" y="6.1" width="15.3" height="15.2" rx="2.6" fill={gradient} stroke={paint.shade} strokeWidth=".65" strokeOpacity=".45" />
        <rect x="5.2" y="9.1" width="10.7" height="7.2" rx="1.1" fill={paint.paper} />
        <path d="m5.9 15.5 3.2-3 2.4 2 2.3-2.6 1.4 3.6H5.9Z" fill={paint.accent} />
        <rect x="5.6" y="18.1" width={compact ? "5.5" : "7.8"} height="1.1" rx=".55" fill={paint.paper} fillOpacity=".9" />
      </>;
    case "document":
      return <>
        <path d="M7 2.6h7.7l5 5.2v11.6a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V4.6a2 2 0 0 1 2-2Z" fill={gradient} stroke={paint.shade} strokeWidth=".65" strokeOpacity=".5" />
        <path d="M14.7 2.6v4c0 .7.6 1.2 1.2 1.2h3.8l-5-5.2Z" fill={paint.paper} />
        <path d="M14.7 3.2v3.7c0 .8.6 1.4 1.4 1.4h3.1" fill="none" stroke={paint.shade} strokeWidth=".65" strokeOpacity=".3" />
        <rect x="8.1" y={compact ? "11.1" : "10.5"} width="8.1" height="1.25" rx=".625" fill={paint.ink} fillOpacity=".85" />
        <rect x="8.1" y={compact ? "15.1" : "14"} width={compact ? "5.6" : "8.1"} height="1.25" rx=".625" fill={paint.ink} fillOpacity=".65" />
        {!compact && <rect x="8.1" y="17.5" width="5.2" height="1.1" rx=".55" fill={paint.ink} fillOpacity=".45" />}
      </>;
    case "conversation":
      return <>
        <path d="M10.1 3.1h8.3a3.1 3.1 0 0 1 3.1 3.1v9.2l-4-2.4h-7.4A3.1 3.1 0 0 1 7 9.9V6.2a3.1 3.1 0 0 1 3.1-3.1Z" fill={depth} />
        {!compact && <path d="M11 5.1h6.3" stroke={paint.paper} strokeWidth=".8" strokeOpacity=".35" />}
        <path d="M5.5 6.8h10.1a3 3 0 0 1 3 3v6.6a3 3 0 0 1-3 3H8.1l-5.6 3V9.8a3 3 0 0 1 3-3Z" fill={gradient} stroke={paint.shade} strokeWidth=".6" strokeOpacity=".5" />
        <rect x="6" y="10.5" width="9" height="1.4" rx=".7" fill={paint.paper} />
        <rect x="6" y="14.2" width="6.2" height="1.4" rx=".7" fill={paint.paper} fillOpacity=".85" />
      </>;
    case "web":
      return <>
        <rect x="2.1" y="3.5" width="19.8" height="17" rx="2.9" fill={depth} stroke={paint.shade} strokeWidth=".6" strokeOpacity=".5" />
        <path d="M5 3.5h14a2.9 2.9 0 0 1 2.9 2.9v1H2.1v-1A2.9 2.9 0 0 1 5 3.5Z" fill={gradient} />
        <rect x="4.4" y="5.1" width="5.2" height="1.1" rx=".55" fill={paint.paper} fillOpacity=".9" />
        <rect x="3.5" y="8.1" width="17" height="10.9" rx="1.2" fill={paint.paper} />
        <rect x="5.2" y="10.1" width="5.7" height="6.8" rx="1.05" fill={paint.accent} />
        <rect x="12.8" y="10.3" width="4.8" height="1.3" rx=".65" fill={paint.ink} fillOpacity=".85" />
        <rect x="12.8" y="13.2" width="3.7" height="1.1" rx=".55" fill={paint.ink} fillOpacity=".5" />
        {!compact && <rect x="12.8" y="16" width="4.8" height="1" rx=".5" fill={paint.ink} fillOpacity=".3" />}
      </>;
    case "context":
      return <>
        <path d="M2.6 5.6c3.5-1.2 6.4-.6 9.4 1.2 3-1.8 5.9-2.4 9.4-1.2v14.9c-3.5-1.1-6.4-.6-9.4 1.4-3-2-5.9-2.5-9.4-1.4V5.6Z" fill={depth} />
        <path d="M3.7 4.2c3.1-.9 5.7-.2 8.3 1.5v14.5c-2.6-1.7-5.2-2.3-8.3-1.4V4.2Z" fill={gradient} />
        <path d="M12 5.7c2.6-1.7 5.2-2.4 8.3-1.5v14.6c-3.1-.9-5.7-.3-8.3 1.4V5.7Z" fill={paint.paper} />
        <path d="M12 6.2v14" stroke={paint.ink} strokeWidth=".65" strokeOpacity=".25" />
        <path d="M6 9.1c1.3-.1 2.3.2 3.6.9m-3.6 3c1.3-.1 2.3.2 3.6.9" stroke={paint.paper} strokeWidth="1.1" strokeLinecap="round" />
        <path d="M14.3 12.8c1.2-.5 2.3-.7 3.5-.5" stroke={paint.shade} strokeWidth="1.1" strokeLinecap="round" strokeOpacity=".75" />
        {!compact && <path d="M14.3 16c1.2-.5 2.3-.7 3.5-.5" stroke={paint.shade} strokeWidth="1" strokeLinecap="round" strokeOpacity=".5" />}
        <path d="M16.2 3.5h3v6.9l-1.5-1.2-1.5 1.2V3.5Z" fill={paint.accent} />
      </>;
    default:
      return null;
  }
}
