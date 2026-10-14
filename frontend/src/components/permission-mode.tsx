"use client";
import { useRef, useState } from "react";
import { ShieldCheck, ShieldAlert, ChevronDown, Check } from "lucide-react";
import { FloatingPopover } from "./floating-popover";
import { useT } from "./i18n";
export type PermissionModeValue = "standard" | "auto" | "full";
const modes = ["standard", "auto", "full"] as const;
const names = { standard: "默认权限", auto: "自动审批", full: "完全访问权限" };
const descriptions = { standard: "读取普通文件无需确认；修改文件和执行命令前确认。", auto: "自动完成普通文件操作；敏感路径和不确定的命令需确认。", full: "在已授权的 Computer 内执行，无需逐次确认。" };
export function PermissionMode({ mode, disabled, nextTask, onChange }: { mode: PermissionModeValue; disabled?: boolean; nextTask?: boolean; onChange: (mode: PermissionModeValue) => void }) {
 const t=useT(), anchor=useRef<HTMLButtonElement>(null);const [open,setOpen]=useState(false);
 return <><button ref={anchor} type="button" className={`permission-mode ${mode}`} disabled={disabled} aria-label={t("权限设置")} aria-expanded={open} title={t(nextTask ? "权限选择仅用于下一任务" : names[mode])} onClick={()=>setOpen(value=>!value)}>{mode==="full"?<ShieldAlert size={15}/>:<ShieldCheck size={15}/>}<span>{t(names[mode])}</span><ChevronDown size={12}/></button>{open?<FloatingPopover anchor={anchor} align="start" width={340} className="permission-menu" role="group" aria-label={t("权限设置")} onClose={()=>{setOpen(false);anchor.current?.focus();}}>{nextTask?<small>{t("权限选择仅用于下一任务")}</small>:null}{modes.map(value=><button key={value} type="button" className={value} aria-pressed={value===mode} onClick={()=>{onChange(value);setOpen(false);anchor.current?.focus();}}>{value==="full"?<ShieldAlert size={20}/>:<ShieldCheck size={20}/>}<span><strong>{t(names[value])}</strong><small>{t(descriptions[value])}</small></span>{value===mode?<Check size={18}/>:null}</button>)}</FloatingPopover>:null}</>;
}
