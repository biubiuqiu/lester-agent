import { GuideProvider } from "@/components/user-guide";

import { AuthSessionKeeper } from "@/components/auth-session-keeper";

export default function AppLayout({ children }: { children: React.ReactNode }) {
  return <GuideProvider><AuthSessionKeeper />{children}</GuideProvider>;
}
