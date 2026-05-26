// dialog.ts — thin wrapper around Telegram's native confirm dialog.
//
// Prefers tg.showConfirm so the user sees a familiar OS-styled prompt
// (with haptic feedback on supported clients); falls back to
// window.confirm for local browser dev / older Telegram clients that
// don't expose the WebApp API.

export function confirmAction(message: string): Promise<boolean> {
  const tg = window.Telegram?.WebApp;
  if (tg && typeof tg.showConfirm === 'function') {
    return new Promise((resolve) => {
      tg.showConfirm(message, (ok: boolean) => resolve(ok));
    });
  }
  return Promise.resolve(window.confirm(message));
}
