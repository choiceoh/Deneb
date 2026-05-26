// dialog.ts — thin wrapper around Telegram's native confirm dialog.
//
// Prefers tg.showConfirm so the user sees a familiar OS-styled prompt
// (with haptic feedback on supported clients); falls back to
// window.confirm for local browser dev / older Telegram clients that
// don't expose the WebApp API.
//
// Failure modes guarded:
//   - showConfirm exists but throws synchronously (older WebApp builds
//     that surface "WebAppMethodUnsupported" or similar) — caught
//     inside the executor and the promise resolves via the fallback.
//   - showConfirm exists but never invokes the callback (some Android
//     dismissal gestures on older Telegram builds) — the resulting
//     forever-pending promise would freeze callers. We don't time out
//     here (we can't tell "slow user" from "client bug"), but callers
//     guard against re-entry by disabling their button up-front.

export function confirmAction(message: string): Promise<boolean> {
  const tg = window.Telegram?.WebApp;
  if (tg && typeof tg.showConfirm === 'function') {
    return new Promise((resolve) => {
      try {
        tg.showConfirm(message, (ok: boolean) => resolve(ok));
      } catch {
        resolve(window.confirm(message));
      }
    });
  }
  return Promise.resolve(window.confirm(message));
}
