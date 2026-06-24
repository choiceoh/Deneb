import { useCallback, useState } from "react";

import { type GatewayConfig, callRpc } from "@/gateway";
import { errText } from "@/format";

// Unambiguous even when an RPC legitimately returns no payload — `ok` distinguishes
// success from the failure the hook already turned into `status` (so `undefined`
// data isn't mistaken for an error).
export type RpcResult<T> = { ok: true; data: T } | { ok: false };

// Call wrapper for the query-driven (non-CRUD) panes — wiki / search and friends —
// that hit miniapp RPCs directly instead of through the data provider. It owns the
// busy flag, an optional pending status, and turning a thrown error into `status`,
// so panes drop the repeated `setStatus("…중"); try { await callRpc … } catch { … }`.
// The SUCCESS status stays the caller's (it's pane-specific: "", "결과 없음", "저장됨").
export function useRpc(cfg: GatewayConfig) {
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState(false);

  const call = useCallback(
    async <T>(method: string, params?: Record<string, unknown>, pending?: string): Promise<RpcResult<T>> => {
      setBusy(true);
      if (pending !== undefined) setStatus(pending);
      try {
        return { ok: true, data: await callRpc<T>(cfg, method, params) };
      } catch (e) {
        setStatus(`오류: ${errText(e)}`);
        return { ok: false };
      } finally {
        setBusy(false);
      }
    },
    [cfg],
  );

  return { call, status, setStatus, busy };
}
