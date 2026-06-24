// Tiny localStorage helpers that swallow the throw (quota / private mode / no
// storage) so callers never re-wrap try/catch. JSON variants parse/stringify;
// string variants for plain values. This is the single home for "best-effort
// browser persistence"; the keychain-backed token path lives in tauri.ts instead.

export function getJSON<T>(key: string): T | undefined {
  try {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : undefined;
  } catch {
    return undefined;
  }
}

export function setJSON(key: string, value: unknown): void {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    /* ignore storage quota / private mode failures */
  }
}

export function getString(key: string): string {
  try {
    return localStorage.getItem(key) ?? "";
  } catch {
    return "";
  }
}

export function setString(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* ignore storage quota / private mode failures */
  }
}

export function remove(key: string): void {
  try {
    localStorage.removeItem(key);
  } catch {
    /* ignore storage quota / private mode failures */
  }
}
