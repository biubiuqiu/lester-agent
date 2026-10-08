import { SettingsShell } from "@/components/settings-shell";
import { ModelSettings } from "@/components/model-settings";
export default async function Models({ searchParams }: { searchParams: Promise<{ returnTo?: string | string[] }> }) {
  const { returnTo } = await searchParams;
  return <SettingsShell active="models"><ModelSettings requestedReturn={typeof returnTo === "string" ? returnTo : undefined} /></SettingsShell>;
}
