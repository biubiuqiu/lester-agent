"use client";

import { useEffect } from "react";
import { ensureSession, redirectToLogin, SessionExpired } from "@/lib/auth-client";

export function AuthSessionKeeper() {
  useEffect(() => {
    let active = true;
    const check = () => {
      if (document.visibilityState !== "visible") return;
      void ensureSession().catch(error => {
        if (active && error instanceof SessionExpired) redirectToLogin();
        // Connectivity/server failures retain drafts and retry on the next tick.
      });
    };
    check();
    const timer = setInterval(check, 60000);
    window.addEventListener("focus", check);
    document.addEventListener("visibilitychange", check);
    return () => { active = false; clearInterval(timer); window.removeEventListener("focus", check); document.removeEventListener("visibilitychange", check); };
  }, []);
  return null;
}
