import type { Metadata } from "next";
import "./globals.css";
import "./account.css";
import "./workspace-errors.css";
import "@xterm/xterm/css/xterm.css";
export const metadata:Metadata={title:"Lester",description:"Open-source AI Agent Workspace"};
export default function RootLayout({children}:{children:React.ReactNode}){return <html lang="zh-CN"><body>{children}</body></html>}
