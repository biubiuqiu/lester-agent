"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type RefObject } from "react";
import { usePathname } from "next/navigation";
import { api, ApiError } from "@/lib/api";
import { readView, viewStateEpoch } from "@/lib/conversation-view-state";
import { randomId } from "@/lib/random-id";
import { appendSpeechText, readSpeechReceipt, speechAccountScope, speechMimeTypes, submitSpeech, type SpeechCapabilities, type SpeechReceipt } from "@/lib/speech";

type Target = { owner: string; draftKey: string; limit: number; input: RefObject<HTMLTextAreaElement | null>; onText: (text: string) => void };
type Phase = "idle" | "permission" | "recording" | "transcribing" | "error" | "completed";
type View = { owner: string; phase: Phase; seconds: number; message: string; recovery?: "check" | "retry"; recognized?: string };
type Capture = Target & {
  epoch: number; path: string; revision: number; id: string; phase: Phase | "stopping";
  stream?: MediaStream; recorder?: MediaRecorder; chunks: Blob[]; bytes: number; blob?: Blob;
  started: number; expires: number; limitBytes: number; limitSeconds: number; delivered: boolean;
  clock?: number; poll?: number; expiry?: number; controller?: AbortController;
};
type SpeechContext = {
  capabilities: SpeechCapabilities | null; view: View;
  register: (draftKey: string) => void; start: (target: Target) => void;
  stop: (owner: string) => void; cancel: (owner?: string) => void;
  recover: (owner: string) => void; retry: (owner: string) => void;
};
const Context = createContext<SpeechContext | null>(null);
export const useSpeech = () => useContext(Context);
const empty: View = { owner: "", phase: "idle", seconds: 0, message: "" };

function microphoneError(reason: unknown) {
  if (reason instanceof DOMException) {
    if (reason.name === "NotFoundError" || reason.name === "DevicesNotFoundError") return "未找到可用麦克风。";
    if (reason.name === "NotReadableError" || reason.name === "TrackStartError") return "麦克风正被占用，请稍后重试。";
  }
  return "无法访问麦克风，请检查浏览器和系统权限。";
}

// Own only recording and transcription. This context cannot send a message,
// create a run, approve an operation or write to another draft.
export function SpeechProvider({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const path = useRef(pathname);
  const scope = useRef("");
  const [accountScope, setAccountScope] = useState("");
  const [capabilities, setCapabilities] = useState<SpeechCapabilities | null>(null);
  const capability = useRef<SpeechCapabilities | null>(null);
  const [view, setView] = useState<View>(empty);
  const current = useRef<Capture | null>(null);
  const reading = useRef<AbortController | null>(null);

  const cancel = useCallback((owner?: string) => {
    const capture = current.current;
    if (owner && capture?.owner !== owner) return;
    current.current = null;
    if (capture) {
      capture.controller?.abort();
      clearInterval(capture.clock); clearTimeout(capture.poll); clearTimeout(capture.expiry);
      if (capture.recorder) {
        capture.recorder.ondataavailable = null; capture.recorder.onstop = null; capture.recorder.onerror = null;
        if (capture.recorder.state !== "inactive") { try { capture.recorder.stop(); } catch { /* Already stopped by the browser. */ } }
      }
      capture.stream?.getTracks().forEach(track => track.stop());
      capture.chunks = []; capture.blob = undefined;
    }
    setView(empty);
  }, []);

  const live = useCallback((capture: Capture) => current.current === capture && capture.epoch === viewStateEpoch() && capture.path === path.current && speechAccountScope(capture.draftKey) === scope.current, []);
  const update = useCallback((capture: Capture, patch: Partial<View>) => {
    if (live(capture)) setView(previous => ({ ...previous, owner: capture.owner, ...patch }));
  }, [live]);
  const fail = useCallback((capture: Capture, message: string, recovery?: "check" | "retry") => {
    if (!live(capture)) return;
    clearInterval(capture.clock); clearTimeout(capture.poll);
    capture.stream?.getTracks().forEach(track => track.stop());
    if (capture.recorder && capture.recorder.state !== "inactive") {
      capture.recorder.onstop = null; capture.recorder.ondataavailable = null;
      try { capture.recorder.stop(); } catch { /* Browser already closed it. */ }
    }
    capture.phase = "error";
    update(capture, { phase: "error", message, recovery });
  }, [live, update]);

  const refresh = useCallback(async () => {
    if (!scope.current) return;
    reading.current?.abort();
    const controller = new AbortController(); reading.current = controller;
    const requested = scope.current, epoch = viewStateEpoch();
    try {
      const value = await api<SpeechCapabilities>("/api/v1/speech/capabilities", { signal: controller.signal, cache: "no-store" });
      if (controller.signal.aborted || requested !== scope.current || epoch !== viewStateEpoch()) return;
      const valid = value && typeof value.enabled === "boolean" && Number.isSafeInteger(value.revision) && value.revision >= 0 && typeof value.provider === "string";
      const next = valid ? { ...value, max_audio_bytes: Math.min(10 << 20, value.max_audio_bytes || 10 << 20), max_duration_seconds: Math.min(120, value.max_duration_seconds || 120) } : null;
      if (current.current && (!next?.enabled || next.revision !== current.current.revision && ["permission", "recording", "stopping", "transcribing"].includes(current.current.phase))) cancel();
      capability.current = next; setCapabilities(next);
    } catch {
      // A failed capability read must not interrupt ordinary text input or
      // imply that an unconfigured microphone is available.
      if (!controller.signal.aborted && requested === scope.current && epoch === viewStateEpoch()) { capability.current = null; setCapabilities(null); cancel(); }
    }
  }, [cancel]);
  const register = useCallback((draftKey: string) => {
    const next = speechAccountScope(draftKey);
    if (!next || scope.current === next) return;
    cancel(); scope.current = next; setAccountScope(next); capability.current = null; setCapabilities(null);
  }, [cancel]);

  useEffect(() => {
    if (accountScope) void refresh();
    const focus = () => { if (document.visibilityState === "visible") void refresh(); };
    window.addEventListener("focus", focus); window.addEventListener("lester:native-foreground", focus);
    return () => { reading.current?.abort(); window.removeEventListener("focus", focus); window.removeEventListener("lester:native-foreground", focus); };
  }, [accountScope, refresh]);
  useEffect(() => {
    path.current = pathname;
    cancel();
  }, [pathname, cancel]);
  useEffect(() => {
    const hide = () => { if (document.visibilityState !== "visible") cancel(); };
    const leave = () => cancel();
    const forget = () => {
      reading.current?.abort(); cancel(); scope.current = "";
      capability.current = null; setCapabilities(null); setAccountScope("");
    };
    document.addEventListener("visibilitychange", hide); window.addEventListener("pagehide", leave); window.addEventListener("lester:session-cleared", forget);
    return () => { document.removeEventListener("visibilitychange", hide); window.removeEventListener("pagehide", leave); window.removeEventListener("lester:session-cleared", forget); reading.current?.abort(); cancel(); };
  }, [cancel]);

  const observe = useCallback(async (capture: Capture, receipt: SpeechReceipt) => {
    if (!live(capture)) return;
    if (receipt.status === "completed") {
      if (capture.delivered) return;
      if (!receipt.text?.trim()) { fail(capture, "未识别到文字，请重新录音。"); return; }
      const merged = appendSpeechText(readView(capture.draftKey).text, receipt.text, capture.limit);
      capture.delivered = true; capture.blob = undefined; capture.chunks = [];
      capture.phase = merged.exceeded ? "error" : "completed";
      clearTimeout(capture.poll); clearTimeout(capture.expiry);
      if (merged.exceeded) {
        update(capture, { phase: "error", message: "语音文字过长，请复制后分段发送。", recognized: receipt.text, recovery: undefined });
        return;
      }
      capture.onText(merged.text);
      // Do not steal focus from another control the user selected while waiting.
      if (document.activeElement === capture.input.current || document.activeElement?.closest(".speech-controls")) capture.input.current?.focus({ preventScroll: true });
      update(capture, { phase: "completed", message: "转写已加入草稿，请确认后发送。", recovery: undefined });
    } else if (receipt.status === "failed") {
      capture.blob = undefined;
      fail(capture, receipt.error || "转写失败，请重新录音。");
    } else if (receipt.status === "unknown") {
      fail(capture, "转写状态尚未确认，请检查原请求；不会自动重复上传。", "check");
    } else {
      capture.phase = "transcribing";
      update(capture, { phase: "transcribing", message: "", recovery: undefined });
    }
  }, [live, fail, update]);

  const inspect = useCallback(async function inspect(capture: Capture, poll: boolean, rejection?: ApiError) {
    if (!live(capture)) return;
    capture.controller?.abort(); capture.controller = new AbortController();
    const controller = capture.controller;
    const timeout = window.setTimeout(() => {
      controller.abort();
      if (live(capture)) fail(capture, "转写状态尚未确认，请检查原请求；不会自动重复上传。", "check");
    }, 15_000);
    try {
      const raw = await api<SpeechReceipt>(`/api/v1/speech/transcriptions/${capture.id}`, { signal: controller.signal, cache: "no-store" });
      if (!live(capture) || controller.signal.aborted) return;
      const receipt = readSpeechReceipt(raw, capture.id);
      await observe(capture, receipt);
      if (receipt.status === "processing" && live(capture)) {
        if (poll && Date.now() < capture.expires) capture.poll = window.setTimeout(() => { void inspect(capture, true); }, 2000);
        else fail(capture, "转写状态尚未确认，请检查原请求；不会自动重复上传。", "check");
      }
    } catch (reason) {
      if (!live(capture) || controller.signal.aborted) return;
      if (reason instanceof ApiError && reason.status === 410) {
        capture.blob = undefined;
        fail(capture, "语音转写记录已过期，请重新录音");
      } else if (reason instanceof ApiError && reason.status === 404 && rejection && ([400, 401, 403, 409, 410, 413, 415, 422, 429].includes(rejection.status) || rejection.message === "语音输入尚未启用，请联系管理员")) {
        capture.blob = undefined;
        fail(capture, rejection.message);
        if (rejection.status === 409 || rejection.status === 503) void refresh();
      } else if (reason instanceof ApiError && reason.status === 404 && capture.blob && Date.now() < capture.expires) fail(capture, "服务器未收到录音，可以重试转写。", "retry");
      else fail(capture, "转写状态尚未确认，请检查原请求；不会自动重复上传。", "check");
    } finally { clearTimeout(timeout); }
  }, [live, observe, fail, refresh]);

  const upload = useCallback(async (capture: Capture) => {
    if (!live(capture) || !capture.blob || Date.now() >= capture.expires) return;
    capture.phase = "transcribing";
    update(capture, { phase: "transcribing", message: "", recovery: undefined });
    capture.controller?.abort(); capture.controller = new AbortController();
    const controller = capture.controller;
    const timeout = window.setTimeout(() => {
      controller.abort();
      if (live(capture)) void inspect(capture, true);
    }, 90_000);
    try {
      const receipt = await submitSpeech(capture.blob, capture.id, capture.revision, controller.signal);
      if (!live(capture) || controller.signal.aborted) return;
      await observe(capture, receipt);
      if (receipt.status === "processing") capture.poll = window.setTimeout(() => { void inspect(capture, true); }, 1500);
    } catch (reason) {
      // Upload uncertainty is resolved by the original receipt, never by a
      // second POST. A confirmed 404 offers an explicit same-ID retry instead.
      clearTimeout(timeout);
      if (live(capture) && !controller.signal.aborted) await inspect(capture, true, reason instanceof ApiError ? reason : undefined);
    } finally { clearTimeout(timeout); }
  }, [live, update, observe, inspect]);

  const stop = useCallback((owner: string) => {
    const capture = current.current;
    if (!capture || capture.owner !== owner || capture.phase !== "recording" || !capture.recorder) return;
    clearInterval(capture.clock);
    capture.phase = "stopping";
    update(capture, { phase: "transcribing", message: "" });
    try { capture.recorder.stop(); }
    catch { fail(capture, "录音失败，请重新录音。"); }
    capture.stream?.getTracks().forEach(track => track.stop());
  }, [fail, update]);

  const start = useCallback(async (target: Target) => {
    const configured = capability.current;
    if (!configured?.enabled || speechAccountScope(target.draftKey) !== scope.current || current.current && ["permission", "recording", "stopping", "transcribing"].includes(current.current.phase)) return;
    cancel();
    const capture: Capture = { ...target, epoch: viewStateEpoch(), path: path.current, revision: configured.revision, id: randomId(), phase: "permission", chunks: [], bytes: 0, started: 0, expires: Date.now() + 5 * 60_000, limitBytes: configured.max_audio_bytes, limitSeconds: configured.max_duration_seconds, delivered: false };
    current.current = capture;
    setView({ owner: target.owner, phase: "permission", seconds: 0, message: "" });
    if (!window.isSecureContext) { fail(capture, "请使用 HTTPS 或 localhost 开启语音输入。"); return; }
    if (window.top !== window || !navigator.mediaDevices?.getUserMedia || typeof MediaRecorder === "undefined") { fail(capture, "当前浏览器不支持录音，请使用支持录音的浏览器。"); return; }
    capture.expiry = window.setTimeout(() => cancel(capture.owner), 5 * 60_000);
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true }, video: false });
      if (!live(capture)) { stream.getTracks().forEach(track => track.stop()); return; }
      capture.stream = stream;
      const mime = speechMimeTypes.find(value => MediaRecorder.isTypeSupported(value));
      const recorder = mime ? new MediaRecorder(stream, { mimeType: mime }) : new MediaRecorder(stream);
      capture.recorder = recorder;
      recorder.ondataavailable = event => {
        if (!live(capture) || capture.phase !== "recording" && capture.phase !== "stopping") return;
        capture.bytes += event.data.size;
        if (capture.bytes > capture.limitBytes) { fail(capture, "录音超过大小限制，请缩短录音。"); capture.chunks = []; return; }
        if (event.data.size) capture.chunks.push(event.data);
      };
      recorder.onerror = () => fail(capture, "录音失败，请重新录音。");
      recorder.onstop = () => {
        if (!live(capture) || capture.phase !== "stopping") return;
        const blob = new Blob(capture.chunks, { type: recorder.mimeType || capture.chunks[0]?.type || mime || "" });
        capture.chunks = [];
        if (!blob.size) { fail(capture, "没有录到声音，请重新录音。"); return; }
        if (blob.size > capture.limitBytes) { fail(capture, "录音超过大小限制，请缩短录音。"); return; }
        capture.blob = blob;
        capture.expires = Date.now() + 4 * 60_000;
        clearTimeout(capture.expiry);
        capture.expiry = window.setTimeout(() => {
          if (!live(capture)) return;
          capture.controller?.abort(); capture.blob = undefined;
          fail(capture, "语音转写记录已过期，请重新录音");
        }, 4 * 60_000);
        void upload(capture);
      };
      recorder.start(1000);
      capture.phase = "recording"; capture.started = Date.now();
      update(capture, { phase: "recording", seconds: 0 });
      capture.clock = window.setInterval(() => {
        if (!live(capture)) { cancel(capture.owner); return; }
        const seconds = Math.floor((Date.now() - capture.started) / 1000);
        update(capture, { seconds });
        if (seconds >= capture.limitSeconds) stop(capture.owner);
      }, 1000);
      stream.getAudioTracks().forEach(track => track.addEventListener("ended", () => { if (capture.phase === "recording") fail(capture, "录音失败，请重新录音。"); }, { once: true }));
    } catch (reason) { fail(capture, microphoneError(reason)); }
  }, [cancel, fail, live, stop, update, upload]);
  const recover = useCallback((owner: string) => { const capture = current.current; if (capture?.owner === owner && capture.phase === "error") { capture.phase = "transcribing"; update(capture, { phase: "transcribing", message: "", recovery: undefined }); void inspect(capture, true); } }, [inspect, update]);
  const retry = useCallback((owner: string) => { const capture = current.current; if (capture?.owner === owner && capture.phase === "error") void upload(capture); }, [upload]);
  const value = useMemo(() => ({ capabilities, view, register, start, stop, cancel, recover, retry }), [capabilities, view, register, start, stop, cancel, recover, retry]);
  return <Context.Provider value={value}>{children}</Context.Provider>;
}
