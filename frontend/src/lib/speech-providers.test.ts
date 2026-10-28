import assert from "node:assert/strict";
import test from "node:test";
import { serviceForSpeechConfig, speechService } from "./speech-providers";

test("compatible vendor identity follows the configured endpoint rather than the editable model", () => {
  const groq = speechService("groq")!;
  assert.equal(groq.provider, "openai");
  assert.equal(serviceForSpeechConfig({ provider: "openai", endpoint: "https://API.GROQ.COM:443/openai/v1/" }).id, "groq");
  assert.equal(serviceForSpeechConfig({ provider: "openai", endpoint: "https://api.together.xyz/v1/" }).id, "together");
  assert.equal(serviceForSpeechConfig({ provider: "openai", endpoint: "https://asr.example.test/openai/v1" }).id, "openai");
  assert.equal(serviceForSpeechConfig({ provider: "openai", endpoint: "https://api.groq.com/openai/v1?relay=true" }).id, "openai");
  assert.equal(serviceForSpeechConfig({ provider: "openai", endpoint: "https://api.groq.com.evil.test/openai/v1" }).id, "openai");
});

test("native speech protocols stay separate from compatible presets", () => {
  assert.equal(serviceForSpeechConfig({ provider: "mistral", endpoint: "https://api.mistral.ai/v1" }).id, "mistral");
  assert.equal(serviceForSpeechConfig({ provider: "deepgram", endpoint: "https://api.deepgram.com/v1" }).id, "deepgram");
  assert.equal(serviceForSpeechConfig({ provider: "openai", endpoint: "https://api.mistral.ai/v1" }).id, "openai");
  assert.equal(speechService("unverified-service"), undefined);
});
