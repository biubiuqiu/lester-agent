import { Workspace } from "@/components/workspace";
export default async function ProjectPage({
  params,
}: {
  params: Promise<{ projectId: string }>;
}) {
  const { projectId } = await params;
  return <Workspace projectId={projectId} />;
}
