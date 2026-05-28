// app_settings.ts — persisted Mini App presentation preferences.
//
// These settings intentionally stay local to the Telegram WebView. They tune
// the client-side experience without changing gateway or assistant behavior.

export interface AppSettings {
  compactMode: boolean;
  enterToSend: boolean;
  hapticFeedback: boolean;
  showDiagnostics: boolean;
}

const STORAGE_KEY = 'deneb.miniapp.settings.v1';

export const DEFAULT_APP_SETTINGS: AppSettings = {
  compactMode: false,
  enterToSend: true,
  hapticFeedback: true,
  showDiagnostics: true,
};

export function readAppSettings(): AppSettings {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return { ...DEFAULT_APP_SETTINGS };
    const parsed = JSON.parse(raw) as Partial<AppSettings>;
    return normalizeSettings(parsed);
  } catch {
    return { ...DEFAULT_APP_SETTINGS };
  }
}

export function updateAppSettings(patch: Partial<AppSettings>): AppSettings {
  const next = normalizeSettings({ ...readAppSettings(), ...patch });
  writeSettings(next);
  applyAppSettings(next);
  return next;
}

export function resetAppSettings(): AppSettings {
  const next = { ...DEFAULT_APP_SETTINGS };
  writeSettings(next);
  applyAppSettings(next);
  return next;
}

export function applyAppSettings(settings = readAppSettings()): void {
  document.body.classList.toggle('compact-ui', settings.compactMode);
}

// Telegram's HapticFeedback exposes three primitives. We wrap each so
// every call site reads identical: feature-gated by the user's haptic
// setting + null-safe if the WebApp object hasn't loaded (local dev or
// older Telegram clients).
//
//   selection    : panorama tab change, toggle flip, page navigation
//   impact-light : menu word tap, secondary button
//   impact-med   : action button tap (analyze / read / archive)
//   impact-heavy : destructive primary (trash on confirm)
//   impact-soft  : long-press selection enter (gentler than tap)
//   notify-ok    : RPC completed successfully on an action the user
//                  is waiting on (analyze done, save complete)
//   notify-err   : optimistic action failed and rolled back
//   notify-warn  : reserved for future use
//
// Setting the toggle off in settings silences everything — we still
// keep wiring the calls so the code reads consistently.

export function triggerSelectionHaptic(): void {
  if (!readAppSettings().hapticFeedback) return;
  window.Telegram?.WebApp?.HapticFeedback?.selectionChanged();
}

type HapticImpactStyle = 'light' | 'medium' | 'heavy' | 'rigid' | 'soft';

export function triggerImpactHaptic(style: HapticImpactStyle = 'light'): void {
  if (!readAppSettings().hapticFeedback) return;
  window.Telegram?.WebApp?.HapticFeedback?.impactOccurred(style);
}

type HapticNotificationType = 'error' | 'success' | 'warning';

export function triggerNotificationHaptic(type: HapticNotificationType): void {
  if (!readAppSettings().hapticFeedback) return;
  window.Telegram?.WebApp?.HapticFeedback?.notificationOccurred(type);
}

function normalizeSettings(input: Partial<AppSettings>): AppSettings {
  return {
    compactMode: input.compactMode ?? DEFAULT_APP_SETTINGS.compactMode,
    enterToSend: input.enterToSend ?? DEFAULT_APP_SETTINGS.enterToSend,
    hapticFeedback: input.hapticFeedback ?? DEFAULT_APP_SETTINGS.hapticFeedback,
    showDiagnostics: input.showDiagnostics ?? DEFAULT_APP_SETTINGS.showDiagnostics,
  };
}

function writeSettings(settings: AppSettings): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(settings));
  } catch {
    // Storage can be unavailable in locked-down WebViews. The live class has
    // already been applied, so silently keep this as an in-memory preference.
  }
}
