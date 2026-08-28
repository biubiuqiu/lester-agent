"use client";
import {ReactNode} from "react";
import {Box,ChevronLeft,Database,ServerCog} from "lucide-react";
import {useRouter} from "next/navigation";
import {Brand} from "./brand";
export function SettingsShell({active,children}:{active:'models'|'sandbox';children:ReactNode}){const router=useRouter();return <main className="settings-shell"><aside className="settings-sidebar"><Brand/><nav><button className={active==='models'?'active':''} onClick={()=>router.push('/app/settings/models')}><Database/>模型</button><button className={active==='sandbox'?'active':''} onClick={()=>router.push('/app/settings/sandbox')}><ServerCog/>Computer</button><span className="disabled"><Box/>Skills <small>Phase 5</small></span></nav><button className="back-button" onClick={()=>router.push('/app')}><ChevronLeft/>返回工作区</button></aside><section className="settings-main">{children}</section></main>}
