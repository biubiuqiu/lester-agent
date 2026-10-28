import { API, ApiError } from "./api";
import { authenticatedFetch } from "./auth-client";

export type SpeechCapabilities = { enabled: boolean; provider: string; revision: number; max_audio_bytes: number; max_duration_seconds: number };
export type SpeechReceipt = { id: string; status: "completed" | "processing" | "unknown" | "failed"; text?: string; error?: string };

export const speechMimeTypes = ["audio/webm;codecs=opus", "audio/webm", "audio/mp4", "audio/ogg;codecs=opus"] as const;
export function speechFileName(mime: string) {
  const base = mime.toLowerCase().split(";")[0];
  return `recording.${base === "audio/mp4" ? "m4a" : base === "audio/ogg" ? "ogg" : base === "audio/wav" ? "wav" : "webm"}`;
}

// Always merge the latest account-scoped draft when recognition completes.
// The recording's old text must never replace typing from this or another tab.
export function appendSpeechText(draft: string, recognized: string, limit: number) {
  const addition = recognized.trim();
  const text = addition ? `${draft}${draft && !draft.endsWith("\n") ? "\n" : ""}${addition}` : draft;
  return { text, exceeded: text.length > limit };
}

export function speechAccountScope(draftKey: string) {
  const [user, workspace, target] = draftKey.split(".");
  return user && workspace && target ? `${user}.${workspace}.` : "";
}

export async function submitSpeech(file: Blob, id: string, revision: number, signal: AbortSignal): Promise<SpeechReceipt> {
  const body = new FormData();
  body.append("file", file, speechFileName(file.type));
  body.append("request_id", id);
  body.append("configuration_revision", String(revision));
  // authenticatedFetch retries only an authentication middleware rejection,
  // which proves this handler did not accept the original upload.
  const response = await authenticatedFetch(`${API}/api/v1/speech/transcriptions`, { method: "POST", body, signal });
  const result = await response.json().catch(() => null);
  if (!response.ok) throw new ApiError(typeof result?.error === "string" ? result.error : "语音服务当前不可用，请稍后重试。", response.status);
  return readSpeechReceipt(result, id);
}

export function readSpeechReceipt(value: unknown, expectedId: string): SpeechReceipt {
  const result = value as Partial<SpeechReceipt> | null;
  if (!result || result.id !== expectedId || !["completed", "processing", "unknown", "failed"].includes(result.status || "") || result.text !== undefined && (typeof result.text !== "string" || Array.from(result.text).length > 20_000) || result.error !== undefined && typeof result.error !== "string") {
    throw new Error("转写状态尚未确认，请检查原请求；不会自动重复上传。");
  }
  return result as SpeechReceipt;
}
