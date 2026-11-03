/** The favicon is also the canonical artwork; SVG documents isolate paint IDs. */
export function BrandMark({ className = "" }: { className?: string }) {
  // eslint-disable-next-line @next/next/no-img-element -- First-party SVG stays crisp at every size without an image transformation.
  return <img className={`brand-mark ${className}`.trim()} src="/icon.svg?brand=prism-v1" width={64} height={64} alt="" aria-hidden="true" draggable={false} />;
}

export function Brand() {
  return <div className="brand"><BrandMark /><strong>Lester</strong></div>;
}
