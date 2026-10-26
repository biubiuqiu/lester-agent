import { SettingsShell } from "@/components/settings-shell";
import { AppConnections } from "@/components/app-connections";

export default function Connections() {
  return <SettingsShell active="connectors"><AppConnections /></SettingsShell>;
}
