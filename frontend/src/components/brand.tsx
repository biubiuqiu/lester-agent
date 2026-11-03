"use client";

import { useId } from "react";
import { BrandMarkArt } from "./brand-mark-art";

export function BrandMark({ className = "" }: { className?: string }) {
  const prefix = `lester-${useId().replace(/[^a-zA-Z0-9_-]/g, "")}`;
  return <BrandMarkArt prefix={prefix} className={`brand-mark ${className}`.trim()} />;
}

export function Brand() {
  return <div className="brand"><BrandMark /><strong>Lester</strong></div>;
}
