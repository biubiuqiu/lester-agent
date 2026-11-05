"use client";

import { useEffect, useRef } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { ArrowRight } from "lucide-react";
import { useT } from "@/components/i18n";
import { legacyUsageLinks } from "@/lib/site-doc-navigation";

/** Fragment identifiers never reach the server; keep bookmarked usage links useful. */
export function DocLegacyNavigation() {
  const router = useRouter();
  const t = useT();
  const lastSource = useRef("");

  useEffect(() => {
    function followLegacyAnchor() {
      let id: string;
      try { id = decodeURIComponent(window.location.hash.slice(1)); }
      catch { return; }
      const link = legacyUsageLinks.find((entry) => entry.id === id);
      if (!link) return;
      const source = window.location.pathname + window.location.search + window.location.hash;
      if (lastSource.current === source) return;
      lastSource.current = source;
      const destination = new URL(link.href, window.location.origin);
      router.replace(destination.pathname + window.location.search + destination.hash);
    }

    followLegacyAnchor();
    window.addEventListener("hashchange", followLegacyAnchor);
    return () => window.removeEventListener("hashchange", followLegacyAnchor);
  }, [router]);

  return <>{legacyUsageLinks.map((link) => <aside className="site-docs-legacy-link" key={link.id} id={link.id}>
    <Link href={link.href} prefetch={false}>{t(link.title)}<ArrowRight size={16} aria-hidden="true" /></Link>
  </aside>)}</>;
}
