import { GuideProvider } from "@/components/user-guide";

export default function AppLayout({ children }: { children: React.ReactNode }) {
  return <GuideProvider>{children}</GuideProvider>;
}
