import { PersonalAssistantDock } from "@/components/personal-assistant-dock";
import "../app-connections.css";
import { GuideProvider } from "@/components/user-guide";

import { AuthSessionKeeper } from "@/components/auth-session-keeper";
import { ComposerMotionProvider } from "@/components/composer-motion";

export default function AppLayout({ children }: { children: React.ReactNode }) {
  return <GuideProvider><AuthSessionKeeper /><PersonalAssistantDock><ComposerMotionProvider>{children}</ComposerMotionProvider></PersonalAssistantDock></GuideProvider>;
}
