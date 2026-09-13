import { ArtifactPage } from "@/components/artifact-page";

export default async function ArtifactsPage({ searchParams }: { searchParams: Promise<{ returnTo?: string }> }) {
  const { returnTo } = await searchParams;
  const safeReturnTo = typeof returnTo === "string" && /^\/app(?:\/(?:c|p)\/[a-zA-Z0-9-]+)?$/.test(returnTo) ? returnTo : "/app";
  return <ArtifactPage returnTo={safeReturnTo} />;
}
