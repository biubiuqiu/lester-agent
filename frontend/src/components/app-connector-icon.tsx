import { AuthProviderIcon } from "./auth-provider-icon";
import type { AppProvider } from "@/lib/app-connections";

export function AppConnectorIcon({ provider }: { provider: AppProvider }) {
  if (provider === "github") return <AuthProviderIcon provider="github" />;
  return <svg viewBox="0 0 24 24" aria-hidden="true" fill="none">
    {provider === "gmail" ? <><path d="M3 6v13h4V9l5 4 5-4v10h4V6l-3-2-6 5-6-5z" fill="#EA4335" /><path d="M3 7v12h4V10z" fill="#4285F4" /><path d="M17 10v9h4V7z" fill="#34A853" /><path d="m17 5 4 2V5a2 2 0 0 0-3-1z" fill="#FBBC04" /></> : null}
    {provider === "x" ? <path d="M4 3h5l4.5 6.3L19 3h2l-6.6 7.8L22 21h-5l-5.4-7.4L5 21H3l7.6-8.8L4 3Zm3.1 1.6L17.9 19.4h1L8.1 4.6h-1Z" fill="currentColor" /> : null}
    {provider === "slack" ? <><rect x="2" y="9" width="9" height="4" rx="2" fill="#36C5F0" /><rect x="7" y="2" width="4" height="6" rx="2" fill="#36C5F0" /><rect x="12" y="2" width="4" height="9" rx="2" fill="#2EB67D" /><rect x="17" y="7" width="5" height="4" rx="2" fill="#2EB67D" /><rect x="13" y="12" width="9" height="4" rx="2" fill="#ECB22E" /><rect x="13" y="17" width="4" height="5" rx="2" fill="#ECB22E" /><rect x="7" y="13" width="4" height="9" rx="2" fill="#E01E5A" /><rect x="2" y="14" width="4" height="4" rx="2" fill="#E01E5A" /></> : null}
    {provider === "feishu" ? <><path d="m3 4 11 4-6 5z" fill="#00C6D7" /><path d="m8 13 13-6-7 14-6-3z" fill="#3370FF" /><path d="m8 13 6-5 7-1-10 8z" fill="#5CB5FF" /></> : null}
  </svg>;
}
