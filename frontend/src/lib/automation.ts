import { api } from "./api";

export type AutomationConfig = { name: string; kind: "schedule" | "webhook"; project_id: string; agent_slug: string; model_deployment_id: string; prompt: string; permission_mode: "standard" | "auto" | "full"; enabled: boolean; cron: string; timezone: string; run_at: string | null };
export type Automation = AutomationConfig & { id: string; created_by: string; next_run_at: string | null; revision: number; created_at: string };
export type AutomationDelivery = { id: string; source: "schedule" | "webhook" | "manual"; state: "pending" | "dispatching" | "accepted" | "failed" | "unknown" | "skipped" | "cancelled"; error_code: string; conversation_id: string; has_conversation: boolean; run_id: string | null; run_status: string; scheduled_at: string | null; created_at: string };
export const automations = {
 list: (signal?: AbortSignal) => api<{ automations: Automation[] }>("/api/v1/automations", { signal }),
 save: (id: string, config: AutomationConfig, revision: number) => api<{ automation: Automation; secret: string }>(`/api/v1/automations/${id}`, { method: "PUT", body: JSON.stringify({ name:config.name,kind:config.kind,project_id:config.project_id,agent_slug:config.agent_slug,model_deployment_id:config.model_deployment_id,prompt:config.prompt,permission_mode:config.permission_mode,enabled:config.enabled,cron:config.cron,timezone:config.timezone,run_at:config.run_at,revision }), signal: AbortSignal.timeout(20000) }),
 remove: (item: Automation) => api<void>(`/api/v1/automations/${item.id}?revision=${item.revision}`, { method: "DELETE", signal: AbortSignal.timeout(20000) }),
 rotate: (item: Automation) => api<{ automation: Automation; secret: string }>(`/api/v1/automations/${item.id}/rotate`, { method: "POST", body: JSON.stringify({ revision: item.revision }), signal: AbortSignal.timeout(20000) }),
 trigger: (id: string, delivery: string) => api<AutomationDelivery>(`/api/v1/automations/${id}/trigger`, { method: "POST", body: JSON.stringify({ id: delivery }), signal: AbortSignal.timeout(20000) }),
 receipt: (id: string, delivery: string) => api<AutomationDelivery>(`/api/v1/automations/${id}/deliveries/${delivery}`),
 history: (id: string, signal?: AbortSignal) => api<{ deliveries: AutomationDelivery[] }>(`/api/v1/automations/${id}/deliveries`, { signal }),
};
