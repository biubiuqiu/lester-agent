import { GuideProvider } from "@/components/user-guide";

import { AuthSessionKeeper } from "@/components/auth-session-keeper";
import { ComposerMotionProvider } from "@/components/composer-motion";

export default function AppLayout({ children }: { children: React.ReactNode }) {
  return <GuideProvider><AuthSessionKeeper /><ComposerMotionProvider>{children}</ComposerMotionProvider></GuideProvider>;
}
