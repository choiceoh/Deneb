import type { DenebConfig } from "../../config/config.js";

export const EMBEDDED_COMPACTION_TIMEOUT_MS = 900_000;

const MAX_SAFE_TIMEOUT_MS = 2_147_000_000;

export function resolveCompactionTimeoutMs(cfg?: DenebConfig): number {
  const raw = cfg?.agents?.defaults?.compaction?.timeoutSeconds;
  if (typeof raw === "number" && Number.isFinite(raw) && raw > 0) {
    return Math.min(Math.floor(raw) * 1000, MAX_SAFE_TIMEOUT_MS);
  }
  return EMBEDDED_COMPACTION_TIMEOUT_MS;
}

export async function compactWithSafetyTimeout<T>(
  compact: () => Promise<T>,
  timeoutMs: number = EMBEDDED_COMPACTION_TIMEOUT_MS,
  opts?: {
    abortSignal?: AbortSignal;
    onCancel?: () => void;
  },
): Promise<T> {
  const resolvedTimeout =
    typeof timeoutMs === "number" && Number.isFinite(timeoutMs)
      ? Math.max(1, Math.floor(timeoutMs))
      : EMBEDDED_COMPACTION_TIMEOUT_MS;

  const abortCtrl = new AbortController();
  const timer = setTimeout(
    () => abortCtrl.abort(new Error("Compaction timed out")),
    resolvedTimeout,
  );
  timer.unref?.();

  // Forward external abort signal
  const externalSignal = opts?.abortSignal;
  const onExternalAbort = externalSignal
    ? () => abortCtrl.abort(externalSignal.reason ?? new Error("aborted"))
    : undefined;
  if (externalSignal?.aborted) {
    clearTimeout(timer);
    opts?.onCancel?.();
    const err = new Error("aborted");
    err.name = "AbortError";
    throw err;
  }
  if (onExternalAbort) {
    externalSignal!.addEventListener("abort", onExternalAbort, { once: true });
  }

  const onAbort = () => {
    try {
      opts?.onCancel?.();
    } catch {
      // Best-effort cancellation
    }
  };
  abortCtrl.signal.addEventListener("abort", onAbort, { once: true });

  try {
    const abortPromise = new Promise<never>((_, reject) => {
      if (abortCtrl.signal.aborted) {
        reject(abortCtrl.signal.reason);
        return;
      }
      abortCtrl.signal.addEventListener(
        "abort",
        () => reject(abortCtrl.signal.reason ?? new Error("Compaction timed out")),
        { once: true },
      );
    });
    return await Promise.race([compact(), abortPromise]);
  } finally {
    clearTimeout(timer);
    abortCtrl.signal.removeEventListener("abort", onAbort);
    if (onExternalAbort) {
      externalSignal!.removeEventListener("abort", onExternalAbort);
    }
  }
}
