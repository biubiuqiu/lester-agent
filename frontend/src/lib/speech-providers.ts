// A service preset selects configuration, not another credential or an
// automatic fallback. Compatible services intentionally keep provider=openai.
export type SpeechProvider = "openai" | "elevenlabs" | "deepgram" | "mistral";
export type SpeechService = {
  id: string;
  provider: SpeechProvider;
  label: string;
  endpoint: string;
  model: string;
  models: readonly string[];
};

export const speechServices: readonly SpeechService[] = [
  { id: "openai", provider: "openai", label: "OpenAI 兼容接口", endpoint: "https://api.openai.com/v1", model: "gpt-4o-transcribe", models: ["gpt-4o-transcribe", "gpt-4o-mini-transcribe", "whisper-1"] },
  { id: "groq", provider: "openai", label: "Groq Whisper", endpoint: "https://api.groq.com/openai/v1", model: "whisper-large-v3-turbo", models: ["whisper-large-v3-turbo", "whisper-large-v3"] },
  { id: "together", provider: "openai", label: "Together Whisper", endpoint: "https://api.together.xyz/v1", model: "openai/whisper-large-v3", models: ["openai/whisper-large-v3"] },
  { id: "elevenlabs", provider: "elevenlabs", label: "ElevenLabs Scribe", endpoint: "https://api.elevenlabs.io/v1", model: "scribe_v2", models: ["scribe_v2", "scribe_v1"] },
  { id: "deepgram", provider: "deepgram", label: "Deepgram Nova", endpoint: "https://api.deepgram.com/v1", model: "nova-3", models: ["nova-3"] },
  { id: "mistral", provider: "mistral", label: "Mistral Voxtral", endpoint: "https://api.mistral.ai/v1", model: "voxtral-mini-latest", models: ["voxtral-mini-latest"] },
];

function normalizedEndpoint(value: string) {
  try {
    const url = new URL(value.trim());
    if (url.username || url.password || url.search || url.hash) return "";
    return `${url.origin}${url.pathname.replace(/\/+$/, "")}`;
  } catch { return ""; }
}

export function speechService(id: string): SpeechService | undefined {
  return speechServices.find(service => service.id === id);
}

// Keep custom model names and infer only from the stored protocol + API base.
// A custom relay must remain the generic compatibility option, even when it
// happens to serve a model with a familiar vendor's name.
export function serviceForSpeechConfig(config: { provider: SpeechProvider; endpoint: string }): SpeechService {
  if (config.provider !== "openai") return speechServices.find(service => service.provider === config.provider) || speechServices[0];
  const endpoint = normalizedEndpoint(config.endpoint);
  return speechServices.find(service => service.provider === "openai" && normalizedEndpoint(service.endpoint) === endpoint) || speechServices[0];
}
