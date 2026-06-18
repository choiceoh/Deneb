// Tauri shell integration. On the desktop the client token lives in the OS
// keychain (via the token_get/token_set Rust commands); on the web it falls back
// to localStorage. The @tauri-apps/api import is dynamic so the web bundle never
// loads it unless we're actually running inside Tauri.
const TOKEN_KEY = "andromeda.token";
const DEFAULT_ACCOUNT = "client:main";

export function isTauri(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

export async function secureGetToken(account: string = DEFAULT_ACCOUNT): Promise<string | null> {
  if (isTauri()) {
    const { invoke } = await import("@tauri-apps/api/core");
    return (await invoke<string | null>("token_get", { account })) ?? null;
  }
  return localStorage.getItem(TOKEN_KEY);
}

export async function secureSetToken(token: string, account: string = DEFAULT_ACCOUNT): Promise<void> {
  if (isTauri()) {
    const { invoke } = await import("@tauri-apps/api/core");
    await invoke("token_set", { account, token });
    return;
  }
  localStorage.setItem(TOKEN_KEY, token);
}

// Desktop auto-connect: resolve the client token without manual entry — first the
// OS keychain, then the canonical ~/.deneb/client_token file the gateway writes.
// Returns null off-desktop or when no token is found.
export async function readDesktopToken(): Promise<string | null> {
  if (!isTauri()) return null;
  const { invoke } = await import("@tauri-apps/api/core");
  const fromKeychain = await invoke<string | null>("token_get", { account: DEFAULT_ACCOUNT }).catch(() => null);
  if (fromKeychain) return fromKeychain;
  return (await invoke<string | null>("token_from_file").catch(() => null)) ?? null;
}
