import { getI18n } from "@/lib/i18n/server";
import { I18nProvider } from "@/components/i18n";
import type { Metadata } from "next";
import "./globals.css";
import "./preview.css";
import "./avatar-crop.css";
import "./account.css";
import "./workspace-errors.css";
import "./file-workspace.css";
import "./workspace-ui.css";
import "./terminal.css";
import "./agent-designer.css";
import "./artifacts-projects.css";
import "./onboarding-ui.css";
import "./user-guide.css";
import "./deliverables.css";
import "@xterm/xterm/css/xterm.css";
export const metadata:Metadata={title:"Lester",description:"Open-source AI Agent Workspace"};
export default async function RootLayout({children}:{children:React.ReactNode}){const {locale,messages}=await getI18n();return <html lang={locale}><body><I18nProvider locale={locale} messages={messages}>{children}</I18nProvider></body></html>}
