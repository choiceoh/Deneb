import { useState } from "react";

import { callRpc } from "./gateway";
import { errText } from "./format";
import { useWorkspace } from "./workspaceContext";

type ActionOptions = {
  onResult?: (data: unknown, method: string, params: Record<string, unknown>) => void | Promise<void>;
};

// Fire a non-CRUD gateway action RPC (mail archive, cron run, workfeed ack, …),
// then refetch the pane's list. Surfaces a one-line error and a busy flag so panes
// can disable buttons mid-flight. Mirrors the native client's optimistic actions
// (here: simplest correct form — act, then re-read authoritative state).
export function useAction(refetch: () => void | Promise<void>, options: ActionOptions = {}) {
  const { cfg } = useWorkspace();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function run<T = unknown>(method: string, params: Record<string, unknown> = {}): Promise<T | undefined> {
    setBusy(true);
    try {
      const data = await callRpc<T>(cfg, method, params);
      setError("");
      try {
        await options.onResult?.(data, method, params);
      } finally {
        await refetch();
      }
      return data;
    } catch (e) {
      setError(errText(e));
      return undefined;
    } finally {
      setBusy(false);
    }
  }

  return { run, error, busy };
}
