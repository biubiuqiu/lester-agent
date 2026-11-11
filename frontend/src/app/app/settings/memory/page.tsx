"use client";

import { usePersonalAssistantDock } from "@/components/personal-assistant-dock";
import { SettingsShell } from "@/components/settings-shell";

export default function MemorySettings() {
  const dock = usePersonalAssistantDock();
  return <SettingsShell active="memory"><div className="personal-memory-settings-host" ref={dock?.registerMemoryHost} /></SettingsShell>;
}
