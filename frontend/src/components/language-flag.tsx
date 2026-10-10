import type { Locale } from "@/lib/i18n";

function Star({ x, y, radius, rotation = 0 }: { x: number; y: number; radius: number; rotation?: number }) {
  const points = Array.from({ length: 10 }, (_, i) => {
    const angle = (i * 36 - 90 + rotation) * Math.PI / 180;
    const r = i % 2 ? radius * .382 : radius;
    return `${(x + Math.cos(angle) * r).toFixed(3)},${(y + Math.sin(angle) * r).toFixed(3)}`;
  }).join(" ");
  return <polygon points={points} />;
}

/** Small inline SVGs stay consistent across platforms without external assets. */
export function LanguageFlag({ locale }: { locale: Locale }) {
  return <svg className="language-flag" viewBox="0 0 30 20" aria-hidden="true" focusable="false">
    {locale === "zh-CN" && <><path fill="#de2910" d="M0 0h30v20H0z" /><g fill="#ffde00"><Star x={5} y={5} radius={3} /><Star x={10} y={2} radius={1} rotation={30} /><Star x={12} y={4} radius={1} rotation={-15} /><Star x={12} y={7} radius={1} rotation={15} /><Star x={10} y={9} radius={1} rotation={-30} /></g></>}
    {locale === "en" && <><path fill="#fff" d="M0 0h30v20H0z" /><g fill="#b22234">{Array.from({ length: 7 }, (_, i) => <rect key={i} x={0} y={i * 40 / 13} width={30} height={20 / 13} />)}</g><path fill="#3c3b6e" d="M0 0h12v10.77H0z" /><g fill="#fff">{Array.from({ length: 9 }, (_, row) => Array.from({ length: row % 2 ? 5 : 6 }, (_, column) => <circle key={`${row}-${column}`} cx={(row % 2 ? 2 : 1) + column * 2} cy={.8 + row * 1.14} r={.3} />))}</g></>}
    {locale === "ja" && <><path fill="#fff" d="M0 0h30v20H0z" /><circle fill="#bc002d" cx={15} cy={10} r={6} /></>}
    {locale === "ko" && <><path fill="#fff" d="M0 0h30v20H0z" /><g transform="rotate(-30 15 10)"><circle fill="#cd2e3a" cx={15} cy={10} r={4} /><path fill="#0047a0" d="M11 10a4 4 0 0 0 8 0a2 2 0 0 1-4 0a2 2 0 0 0-4 0" /></g>{[[6, 5, -35, 0], [24, 5, 35, 1], [6, 15, 35, 2], [24, 15, -35, 3]].map(([x, y, angle, broken]) => <g key={`${x}-${y}`} transform={`translate(${x} ${y}) rotate(${angle})`} fill="#171717">{[0, 1, 2].map(row => <g key={row}><rect x={-2.6} y={row * 1.3 - 1.7} width={5.2} height={.8} />{(broken === 3 || (broken === 1 && row !== 1) || (broken === 2 && row === 1)) && <rect x={-.35} y={row * 1.3 - 1.7} width={.7} height={.8} fill="#fff" />}</g>)}</g>)}</>}
    {locale === "fr" && <><path fill="#002395" d="M0 0h10v20H0z" /><path fill="#fff" d="M10 0h10v20H10z" /><path fill="#ed2939" d="M20 0h10v20H20z" /></>}
    {locale === "es" && <><path fill="#aa151b" d="M0 0h30v20H0z" /><path fill="#f1bf00" d="M0 5h30v10H0z" /><path fill="#aa151b" d="M8 8h3v3.5c0 1.5-3 1.5-3 0z" /><path fill="#fff" d="M8.4 8.4h1v1.4h-1zm1.2 1.6h1v1.4h-1z" /><path fill="#aa151b" d="M7.8 7h3.4v.6H7.8zM7 9h.6v3H7zm4.4 0h.6v3h-.6z" /></>}
  </svg>;
}
