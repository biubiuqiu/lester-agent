import type { ReactNode } from "react";

export function Note({ children }: { children: ReactNode }) {
  return <aside className="site-doc-note">{children}</aside>;
}
