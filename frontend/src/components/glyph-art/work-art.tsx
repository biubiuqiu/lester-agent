import type { GlyphArtProps } from "./types";

/** Filled capability artwork. The renderer supplies shared paints and unique gradients. */
export function WorkArt({ kind, compact, paint, gradient, depth }: GlyphArtProps) {
  switch (kind) {
    case "analysis": return <>
      <rect x="2.5" y="2.5" width="19" height="19" rx="3.5" fill={depth} />
      <rect x="3.2" y="2.5" width="18.3" height="17.8" rx="3.2" fill={gradient} />
      <rect x="5" y="5" width="14" height="13.5" rx="1.6" fill={paint.paper} />
      <rect x="7" y="12" width="2.5" height="4.5" rx=".7" fill={paint.main} />
      <rect x="10.8" y="8" width="2.5" height="8.5" rx=".7" fill={paint.accent} />
      <rect x="14.6" y="10" width="2.5" height="6.5" rx=".7" fill={paint.shade} />
      {!compact && <path d="M7 18.5h10" stroke={paint.ink} strokeOpacity=".26" strokeWidth=".8" />}
      <path d="M6.5 3.3h9" stroke={paint.paper} strokeOpacity=".65" strokeWidth=".8" />
    </>;

    case "code": return <>
      <rect x="2" y="3" width="20" height="18" rx="3.5" fill={paint.ink} />
      <rect x="2" y="3" width="20" height="16.8" rx="3.5" fill={depth} />
      <path d="M2 8V6.5A3.5 3.5 0 0 1 5.5 3h13A3.5 3.5 0 0 1 22 6.5V8H2Z" fill={gradient} />
      <rect x="5" y="5.1" width={compact ? "4" : "6"} height=".9" rx=".45" fill={paint.paper} fillOpacity=".75" />
      <path d="m8.5 11-3 2.7 3 2.8m7-5.5 3 2.7-3 2.8" stroke={paint.paper} strokeWidth={compact ? "1.55" : "1.4"} />
      <path d="m13 10.5-2 7" stroke={paint.accent} strokeWidth="1.5" />
    </>;

    case "review": return <>
      <path d="M5 2h7.8L17 6.2V18a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2Z" fill={gradient} />
      <path d="M12.8 2v3a1.2 1.2 0 0 0 1.2 1.2h3L12.8 2Z" fill={paint.light} />
      <path d="M6.5 8.5h5.8m-5.8 3h4" stroke={paint.ink} strokeOpacity=".7" strokeWidth="1.2" />
      {!compact && <path d="M6.5 14.5H10" stroke={paint.ink} strokeOpacity=".5" strokeWidth="1.2" />}
      <path d="m19.1 18 3.3 3.3a1.1 1.1 0 0 1-1.6 1.5l-3.2-3.3L19.1 18Z" fill={paint.ink} />
      <circle cx="16.5" cy="15.7" r="4.8" fill={paint.ink} />
      <circle cx="16.5" cy="15.7" r="3.4" fill={paint.accent} />
      <circle cx="16.5" cy="15.7" r="2.6" fill={paint.paper} fillOpacity=".82" />
      {!compact && <path d="M14.6 15.6a2 2 0 0 1 1.9-1.9" stroke={paint.paper} strokeWidth=".9" />}
    </>;

    case "skill": return <>
      <path d="m12 1.8 10 5.6v9.2a1.5 1.5 0 0 1-.8 1.3L12 23 2.8 17.9a1.5 1.5 0 0 1-.8-1.3V7.4l10-5.6Z" fill={depth} />
      <path d="m2 7.4 10 5.5V23l-9.2-5.1a1.5 1.5 0 0 1-.8-1.3V7.4Z" fill={gradient} />
      <path d="m12 1.8 10 5.6-10 5.5L2 7.4l10-5.6Z" fill={paint.light} />
      <path d="m6.7 4.8 10 5.6-3 1.7-10-5.6 3-1.7Z" fill={paint.accent} fillOpacity=".9" />
      {!compact && <>
        <path d="M12 13.7v7.6" stroke={paint.paper} strokeOpacity=".35" strokeWidth=".8" />
        <path d="m5 10.7 4 2.2" stroke={paint.paper} strokeOpacity=".7" strokeWidth=".8" />
      </>}
    </>;

    case "designer": return <>
      <rect x="2.5" y="5" width="16.5" height="17" rx="3" fill={depth} />
      <rect x="2.5" y="5" width="16.5" height="16" rx="3" fill={gradient} />
      <rect x="5" y="7.5" width="11.5" height="11.2" rx="1.2" fill={paint.paper} />
      {!compact && <path d="M7.5 16h5" stroke={paint.main} strokeWidth="1.2" />}
      <path d="m9 17.5 1.1-4.7L19 2.4a1.2 1.2 0 0 1 1.7-.1l1.3 1.2a1.2 1.2 0 0 1 .1 1.7l-8.9 10.4L9 17.5Z" fill={paint.accent} />
      <path d="m9 17.5 1.1-4.7 3.1 2.8L9 17.5Z" fill={paint.ink} />
      <path d="m17.8 3.8 3 2.8 1.3-1.4a1.2 1.2 0 0 0-.1-1.7l-1.3-1.2a1.2 1.2 0 0 0-1.7.1l-1.2 1.4Z" fill={paint.light} />
      {!compact && <path d="m18.2 6.4-6.2 7.3" stroke={paint.paper} strokeOpacity=".65" strokeWidth=".8" />}
    </>;

    case "strategy": return <>
      <path d="m2 15.8 10-5.2 10 5.2v1L12 22.4 2 16.8v-1Z" fill={depth} />
      <path d="m2 11 10-5.2L22 11v1L12 17.6 2 12v-1Z" fill={paint.main} />
      <path d="m2 6.2 10-5 10 5v1L12 12.8 2 7.2v-1Z" fill={gradient} />
      <path d="m2 6.2 10-5 10 5-10 5.5L2 6.2Z" fill={paint.light} />
      <path d="m7.7 6.2 4.3-2.1 4.3 2.1L12 8.5 7.7 6.2Z" fill={paint.accent} />
      {!compact && <path d="m4.8 11.3 7.2 4 7.2-4m-14.4 4.8 7.2 4 7.2-4" stroke={paint.paper} strokeOpacity=".35" strokeWidth=".7" />}
    </>;

    case "bolt": return <>
      <path d="M13.4 1.8 3.8 13h6.6l-.8 9.2L20.2 9.3h-7.3l.5-7.5Z" fill={gradient} />
      <path d="m12.9 9.3 7.3 0L9.6 22.2l4.2-9.2h-3.4l2.5-3.7Z" fill={depth} />
      {!compact && <path d="m11.5 5.8-4.4 5.3h3.6" stroke={paint.paper} strokeOpacity=".7" strokeWidth=".9" />}
    </>;

    case "model": return <>
      <path d="M8 1.8V5m8-3.2V5M8 19v3.2m8-3.2v3.2M1.8 8H5m-3.2 8H5m14-8h3.2M19 16h3.2" stroke={paint.ink} strokeWidth="1.5" />
      {!compact && <path d="M12 1.8V5m0 14v3.2M1.8 12H5m14 0h3.2" stroke={paint.ink} strokeWidth="1.3" />}
      <rect x="4" y="4" width="16" height="16" rx="4" fill={depth} />
      <rect x="5" y="4" width="14" height="14.7" rx="3.2" fill={gradient} />
      <rect x="8" y="8" width="8" height="8" rx="2" fill={paint.paper} />
      <path d="m12 9.5 2.5 2.5-2.5 2.5-2.5-2.5 2.5-2.5Z" fill={paint.accent} />
      {!compact && <path d="M7.5 5.2h4" stroke={paint.paper} strokeOpacity=".6" strokeWidth=".8" />}
    </>;

    default: return null;
  }
}
