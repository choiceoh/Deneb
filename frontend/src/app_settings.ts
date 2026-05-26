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

export function triggerSelectionHaptic(): void {
  if (!readAppSettings().hapticFeedback) return;
  window.Telegram?.WebApp?.HapticFeedback?.selectionChanged();
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
