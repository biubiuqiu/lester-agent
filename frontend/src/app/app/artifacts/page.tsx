import { ArtifactPage } from "@/components/artifact-page";

export default async function ArtifactsPage({ searchParams }: { searchParams: Promise<{ returnTo?: string; project?: string }> }) {
  const { returnTo, project } = await searchParams;
  const safeReturnTo = typeof returnTo === "string" && /^\/app(?:\/(?:c\/[a-zA-Z0-9-]+|p\/[a-zA-Z0-9-]+(?:\/overview)?))?$/.test(returnTo) ? returnTo : "/app";
  const initialProject = typeof project === "string" && /^[a-zA-Z0-9-]{1,64}$/.test(project) ? project : "";
  return <ArtifactPage returnTo={safeReturnTo} initialProject={initialProject} />;
}
