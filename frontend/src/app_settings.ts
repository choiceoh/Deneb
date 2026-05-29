// app_settings.ts — Telegram haptic feedback helpers.
//
// The Mini App used to keep a handful of local presentation toggles here
// (compact mode, theme, reduce motion, diagnostics, enter-to-send). Those
// were removed: for a single operator they were set-once cosmetics that
// added a settings row no one actually flips mid-use. The only preference
// that survives is the active model, which is server-side and lives in the
// settings view directly.
//
// Haptics are now an opinionated always-on default rather than a toggle.
// Each wrapper stays null-safe so it no-ops in local dev or on older
// Telegram clients where the WebApp object (or HapticFeedback) is absent.
//
//   selection    : tab change, page navigation, row selection
//   impact-light : menu word tap, secondary button
//   impact-med   : action button tap (analyze / read / archive)
//   impact-heavy : destructive primary (trash on confirm)
//   impact-soft  : long-press selection enter (gentler than tap)
//   notify-ok    : RPC the user is waiting on completed (analyze/save done)
//   notify-err   : optimistic action failed and rolled back
//   notify-warn  : reserved for future use

// runHaptic shields the tap from older/partial Telegram WebViews: the
// HapticFeedback object can exist while a given method is missing (guarded
// by `?.()`) or throws WebAppMethodUnsupported on clients that predate that
// primitive (guarded by try/catch). These calls fire *before* the click's
// real work (navigation / RPC), so an unguarded throw would silently turn
// taps into no-ops — feedback is non-essential and must never block it.
function runHaptic(invoke: () => void): void {
  try {
    invoke();
  } catch {
    // Unsupported on this client — swallow; the caller's work continues.
  }
}

export function triggerSelectionHaptic(): void {
  runHaptic(() => window.Telegram?.WebApp?.HapticFeedback?.selectionChanged?.());
}

type HapticImpactStyle = 'light' | 'medium' | 'heavy' | 'rigid' | 'soft';

export function triggerImpactHaptic(style: HapticImpactStyle = 'light'): void {
  runHaptic(() => window.Telegram?.WebApp?.HapticFeedback?.impactOccurred?.(style));
}

type HapticNotificationType = 'error' | 'success' | 'warning';

export function triggerNotificationHaptic(type: HapticNotificationType): void {
  runHaptic(() => window.Telegram?.WebApp?.HapticFeedback?.notificationOccurred?.(type));
}
