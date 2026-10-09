import { SettingsShell } from "@/components/settings-shell";
import { ModelSettings } from "@/components/model-settings";
export default async function Models({ searchParams }: { searchParams: Promise<{ returnTo?: string | string[] }> }) {
  const { returnTo } = await searchParams;
  const requestedReturn = typeof returnTo === "string" ? returnTo : undefined;
  return <SettingsShell active="models" returnTo={requestedReturn}><ModelSettings requestedReturn={requestedReturn} /></SettingsShell>;
}
