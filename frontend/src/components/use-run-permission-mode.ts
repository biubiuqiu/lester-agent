"use client";

import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { PermissionModeValue } from "./permission-mode";

type Snapshot = { run_id: string; permission_mode: PermissionModeValue; revision: number; status: string };
const valid = (value: Snapshot, run: string) => value.run_id === run && ["standard", "auto", "full"].includes(value.permission_mode) && Number.isSafeInteger(value.revision) && value.revision >= 0;

export function useRunPermissionMode({ conversationId, runId, active, revision, draftMode, onConfirmed }: {
  conversationId: string; runId?: string; active: boolean; revision: number; draftMode: PermissionModeValue; onConfirmed: (mode: PermissionModeValue) => void;
}) {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null), current = useRef<Snapshot | null>(null);
  const [pendingOwner, setPendingOwner] = useState<string | null>(null), [error, setError] = useState("");
  const [retry, setRetry] = useState(0), sending = useRef<string | null>(null);
  const owner = `${conversationId}:${runId ?? ""}`, ownerRef = useRef(owner);
  const confirmed = useRef(onConfirmed);
  useLayoutEffect(() => { ownerRef.current = owner; confirmed.current = onConfirmed; }, [owner, onConfirmed]);

  useEffect(() => {
    if (!active || !runId) return;
    let alive = true, loading = false;
    const load = async () => {
      if (loading) return; loading = true;
      try {
        const value = await api<Snapshot>(`/api/v1/conversations/${conversationId}/runs/${runId}/permissions`, { cache: "no-store", signal: AbortSignal.timeout(10000) });
        if (!valid(value, runId)) throw new Error();
        if (alive && ownerRef.current === owner) {
          if (!current.current || current.current.run_id !== runId || value.revision >= current.current.revision) {
            current.current = value; setSnapshot(value); confirmed.current(value.permission_mode);
          }
          setError(previous => previous === "当前任务权限读取失败，请检查状态。" ? "" : previous);
        }
      } catch { if (alive) setError("当前任务权限读取失败，请检查状态。"); }
      finally { loading = false; }
    };
    void load();
    const visible = () => { if (!document.hidden) void load(); };
    const timer = setInterval(visible, 5000);
    document.addEventListener("visibilitychange", visible);
    return () => { alive = false; clearInterval(timer); document.removeEventListener("visibilitychange", visible); };
  }, [active, conversationId, runId, revision, retry, owner]);

  async function change(mode: PermissionModeValue) {
    if (sending.current === owner) return;
    if (!active) { confirmed.current(mode); return; }
    const before = current.current;
    if (!runId || !before || before.run_id !== runId || before.status !== "running") { setError("当前任务权限读取失败，请检查状态。"); return; }
    sending.current = owner; setPendingOwner(owner); setError("");
    const path = `/api/v1/conversations/${conversationId}/runs/${runId}/permissions`;
    try {
      let result: Snapshot;
      try { result = await api<Snapshot>(path, { method: "PUT", body: JSON.stringify({ permission_mode: mode, revision: before.revision }), signal: AbortSignal.timeout(10000) }); }
      catch (reason) {
        if (reason instanceof ApiError && reason.status < 500 && reason.status !== 408) throw reason;
        // A lost response only reads the actual state; never replay the PUT.
        result = await api<Snapshot>(path, { cache: "no-store", signal: AbortSignal.timeout(10000) });
      }
      if (!valid(result, runId) || result.permission_mode !== mode || result.status !== "running" || result.revision < before.revision) throw new Error();
      if (ownerRef.current === owner && (!current.current || current.current.run_id !== runId || result.revision >= current.current.revision)) {
        current.current = result; setSnapshot(result); confirmed.current(result.permission_mode);
      }
    } catch { if (ownerRef.current === owner) setError("权限修改结果未确认，请检查状态；不会自动重试。"); }
    finally { if (sending.current === owner) sending.current = null; setPendingOwner(value => value === owner ? null : value); }
  }

  const known = snapshot !== null && snapshot.run_id === runId && snapshot.status === "running";
  return { mode: active && known ? snapshot.permission_mode : draftMode, busy: pendingOwner === owner || (active && !known), error: active ? error : "",
    change, refresh: () => { setError(""); setRetry(value => value + 1); } };
}
