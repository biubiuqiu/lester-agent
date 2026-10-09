import { Github } from "lucide-react";

export function AuthProviderIcon({ provider }: { provider: "google" | "github" }) {
  if (provider === "github") return <Github size={20} aria-hidden="true" />;
  return <svg width="20" height="20" viewBox="0 0 24 24" aria-hidden="true"><path fill="#4285f4" d="M21.6 12.23c0-.71-.06-1.39-.18-2.05H12v3.88h5.38a4.6 4.6 0 0 1-2 3.02v2.51h3.24c1.9-1.75 2.98-4.33 2.98-7.36Z" /><path fill="#34a853" d="M12 22c2.7 0 4.96-.9 6.62-2.41l-3.24-2.51c-.9.6-2.05.96-3.38.96-2.6 0-4.8-1.76-5.59-4.13H3.06v2.6A10 10 0 0 0 12 22Z" /><path fill="#fbbc05" d="M6.41 13.91a6 6 0 0 1 0-3.82v-2.6H3.06a10 10 0 0 0 0 9.02l3.35-2.6Z" /><path fill="#ea4335" d="M12 5.96c1.47 0 2.79.5 3.83 1.5l2.88-2.88A9.61 9.61 0 0 0 12 2a10 10 0 0 0-8.94 5.49l3.35 2.6A5.99 5.99 0 0 1 12 5.96Z" /></svg>;
}
