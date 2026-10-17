import { api, type Deliverable } from "./api";

export type ProjectDeliverable = Deliverable & { conversation_title: string; run_status: string };
export type ProjectResultPage = { deliverables: ProjectDeliverable[]; next_cursor: string };

export function projectResults(projectId: string, search: string, kind: string, cursor: string, signal: AbortSignal) {
  const params = new URLSearchParams({ q: search, kind, cursor });
  return api<ProjectResultPage>(`/api/v1/projects/${projectId}/deliverables?${params}`, { signal });
}
