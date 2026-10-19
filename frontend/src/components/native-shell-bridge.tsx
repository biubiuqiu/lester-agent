"use client";

import { useEffect } from "react";
import { usePathname, useRouter } from "next/navigation";
import { isNativeShell, nativeCommandPath, postNative } from "@/lib/native-shell";

export function NativeShellBridge() {
  const router = useRouter(), pathname = usePathname();
  useEffect(() => {
    if (!isNativeShell()) return;
    document.documentElement.dataset.lesterNative = "ios";
    const command = (event: Event) => {
      const detail = (event as CustomEvent).detail;
      if (detail?.action === "foreground") {
        window.dispatchEvent(new Event("lester:native-foreground"));
        return;
      }
      const path = nativeCommandPath(detail);
      if (path) router.push(path);
    };
    window.addEventListener("lester:native-command", command);
    return () => window.removeEventListener("lester:native-command", command);
  }, [router]);
  useEffect(() => {
    if (!isNativeShell()) return;
    postNative("route", { path: pathname });
    if (pathname === "/login") postNative("signed_out", {});
  }, [pathname]);
  return null;
}
