"use client";

import { useEffect, useRef } from "react";
import { Bot, FileText, Folder, Plus, RotateCcw, TerminalSquare, Wrench, X } from "lucide-react";
import { useT } from "./i18n";
import { useFileWorkspace } from "./file-workspace";
import { FileGlyph } from "./file-explorer";
import { MAX_TERMINAL_TABS, MAX_WORKSPACE_TABS, workspaceTabElementId as workspaceTabId, workspacePaneElementId as workspacePaneId, type WorkspaceTab } from "@/lib/workspace-tabs";

export { workspaceTabId, workspacePaneId };

export function WorkspaceTabBar({ native, createdAgentId }: { native: boolean; createdAgentId?: string }) {
  const t = useT();
  const { workspaceTabs, activeWorkspaceTab, selectWorkspaceTab, addWorkspaceTab, closeWorkspaceTab, unreadFiles, canReopenClosed, reopenClosed } = useFileWorkspace();
  const list = useRef<HTMLElement>(null);
  const menu = useRef<HTMLDetailsElement>(null);
  const visibleTabs = workspaceTabs.filter(tab => (!native || (tab.kind !== "terminal" && tab.kind !== "skills")) && (tab.kind !== "agent" || createdAgentId));
  const hasVisibleActive = visibleTabs.some(tab => tab.id === activeWorkspaceTab);
  const activeId = hasVisibleActive ? activeWorkspaceTab : visibleTabs[0]?.id ?? null;
  const label = (tab: WorkspaceTab) => tab.kind === "file" ? tab.path.split("/").at(-1) || tab.path : tab.kind === "terminal" ? tab.serial === 1 ? t("终端") : t("终端 {0}", [tab.serial]) : t({ files: "文件", results: "成果", skills: "技能", agent: "智能体" }[tab.kind]);

  useEffect(() => {
    const element = list.current;
    if (!element) return;
    let width = element.getBoundingClientRect().width;
    const reveal = () => {
      const nextWidth = element.getBoundingClientRect().width;
      if (Math.abs(nextWidth - width) > 1 && menu.current) menu.current.open = false;
      width = nextWidth;
      element.querySelector('[aria-selected="true"]')?.parentElement?.scrollIntoView({ block: "nearest", inline: "nearest" });
    };
    const observer = new ResizeObserver(reveal);
    observer.observe(element); reveal();
    return () => observer.disconnect();
  }, [activeWorkspaceTab, visibleTabs.length]);
  useEffect(() => {
    const outside = (event: PointerEvent) => { if (menu.current && !menu.current.contains(event.target as Node)) menu.current.open = false; };
    const resized = () => { if (menu.current) menu.current.open = false; };
    document.addEventListener("pointerdown", outside);
    window.addEventListener("resize", resized);
    window.visualViewport?.addEventListener("resize", resized);
    return () => { document.removeEventListener("pointerdown", outside); window.removeEventListener("resize", resized); window.visualViewport?.removeEventListener("resize", resized); };
  }, []);

  const focusTab = (id?: string) => requestAnimationFrame(() => {
    const target = id ? document.getElementById(workspaceTabId(id)) : null;
    (target ?? document.getElementById("workspace-tab-add"))?.focus({ preventScroll: true });
  });
  const close = (tab: WorkspaceTab, index: number) => {
    const focus = document.activeElement;
    const neighbor = visibleTabs[index + 1] ?? visibleTabs[index - 1];
    closeWorkspaceTab(tab.id);
    requestAnimationFrame(() => {
      if (focus instanceof HTMLElement && !focus.isConnected && document.activeElement === document.body) {
        (document.getElementById(workspaceTabId(activeId === tab.id ? neighbor?.id ?? "" : activeId ?? "")) ?? document.getElementById("workspace-tab-add"))?.focus({ preventScroll: true });
      }
    });
  };
  const add = (kind: "files" | "results" | "terminal" | "skills" | "agent") => {
    addWorkspaceTab(kind);
    if (menu.current) menu.current.open = false;
    // Keep a predictable keyboard anchor while the new panel initializes.
    document.getElementById("workspace-tab-add")?.focus({ preventScroll: true });
  };
  const count = workspaceTabs.filter(tab => tab.kind === "terminal").length;
  const atCapacity = workspaceTabs.length >= MAX_WORKSPACE_TABS;
  return <div className="workspace-tab-bar workspace-tab-strip">
    <nav className="workspace-tabs" ref={list} role="tablist" aria-label={t("工作区标签")}>
      {visibleTabs.map((tab, index) => {
        const active = tab.id === activeId;
        const name = label(tab);
        const unread = tab.kind === "file" && unreadFiles.includes(tab.path);
        return <div className={`workspace-tab ${active ? "active" : ""}`} key={tab.id}>
          <button type="button" role="tab" id={workspaceTabId(tab.id)} aria-controls={workspacePaneId(tab.id)} aria-selected={active} tabIndex={active || (!hasVisibleActive && index === 0) ? 0 : -1} aria-describedby={unread ? `${workspaceTabId(tab.id)}-unread` : undefined} title={tab.kind === "file" ? tab.path : name} onClick={() => selectWorkspaceTab(tab.id)} onKeyDown={event => {
            if (event.key === "Delete" && event.repeat) { event.preventDefault(); return; }
            let next: WorkspaceTab | undefined;
            if (event.key === "ArrowRight") next = visibleTabs[(index + 1) % visibleTabs.length];
            if (event.key === "ArrowLeft") next = visibleTabs[(index - 1 + visibleTabs.length) % visibleTabs.length];
            if (event.key === "Home") next = visibleTabs[0];
            if (event.key === "End") next = visibleTabs.at(-1);
            if (next) { event.preventDefault(); selectWorkspaceTab(next.id); focusTab(next.id); }
            if (event.key === "Delete") { event.preventDefault(); close(tab, index); }
          }}>
            <span className="workspace-tab-icon" aria-hidden="true">{tab.kind === "file" ? <FileGlyph file={{ name, is_dir: false }} /> : tab.kind === "terminal" ? <TerminalSquare /> : tab.kind === "files" ? <Folder /> : tab.kind === "results" ? <FileText /> : tab.kind === "skills" ? <Wrench /> : <Bot />}</span>
            <span className="workspace-tab-name">{name}</span>{unread ? <i className="file-unread-dot" aria-hidden="true" /> : null}
          </button>
          <button type="button" className="workspace-tab-close" aria-label={t("关闭 {0}", [name])} title={tab.kind === "terminal" ? t("关闭终端标签会结束此 Shell。") : t("关闭 {0}", [name])} onClick={() => close(tab, index)}><X /></button>
          {unread ? <span id={`${workspaceTabId(tab.id)}-unread`} hidden>{t("有未查看的更新")}</span> : null}
        </div>;
      })}
    </nav>
    <details className="workspace-add" ref={menu} onBlur={event => {
      if (!event.currentTarget.contains(event.relatedTarget as Node | null)) event.currentTarget.open = false;
    }} onToggle={event => {
      if (!event.currentTarget.open) return;
      const bounds = event.currentTarget.getBoundingClientRect();
      const popup = event.currentTarget.querySelector<HTMLElement>(".workspace-add-menu");
      if (popup) popup.style.left = `${Math.max(8, Math.min(bounds.left, innerWidth - 218)) - bounds.left}px`;
    }} onKeyDown={event => {
      if (event.key === "Escape" && event.currentTarget.open) {
        event.preventDefault(); event.stopPropagation(); event.currentTarget.open = false;
        event.currentTarget.querySelector("summary")?.focus();
      }
    }}>
      <summary id="workspace-tab-add" aria-label={t("添加工作区标签")} title={t("添加工作区标签")}><Plus /></summary>
      <div className="workspace-add-menu" role="group" aria-label={t("选择标签内容")}>
        <button type="button" disabled={atCapacity && !workspaceTabs.some(tab => tab.kind === "files")} onClick={() => add("files")}><Folder />{t("文件")}</button>
        <button type="button" disabled={atCapacity && !workspaceTabs.some(tab => tab.kind === "results")} onClick={() => add("results")}><FileText />{t("成果")}</button>
        {!native ? <><button type="button" disabled={count >= MAX_TERMINAL_TABS || atCapacity} title={count >= MAX_TERMINAL_TABS ? t("最多同时打开 {0} 个终端", [MAX_TERMINAL_TABS]) : undefined} onClick={() => add("terminal")}><TerminalSquare />{t("终端")}</button>
          <button type="button" disabled={atCapacity && !workspaceTabs.some(tab => tab.kind === "skills")} onClick={() => add("skills")}><Wrench />{t("技能")}</button>
        </> : null}
        {createdAgentId ? <button type="button" disabled={atCapacity && !workspaceTabs.some(tab => tab.kind === "agent")} onClick={() => add("agent")}><Bot />{t("智能体")}</button> : null}
        {canReopenClosed ? <button type="button" className="workspace-reopen" onClick={() => { reopenClosed(); if (menu.current) menu.current.open = false; document.getElementById("workspace-tab-add")?.focus({ preventScroll: true }); }}><RotateCcw />{t("重新打开刚关闭的标签")}</button> : null}
      </div>
    </details>
  </div>;
}
