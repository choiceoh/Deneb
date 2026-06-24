import type { AuthProvider } from "@refinedev/core";
import { type GatewayConfig, ping } from "./gateway";

// Token-based auth provider (DESIGN §4 "auth provider(=토큰)"). There is no login
// flow — connection IS the URL+token pair entered in the sidebar — so `check`
// simply reports whether we're configured, and `getIdentity` surfaces the live
// gateway identity (version/model) via ping. Wiring this into <Refine> gives the
// rest of the app Refine's auth-aware hooks (useIsAuthenticated, <Authenticated>)
// for free as the workstation grows.
export function denebAuthProvider(cfg: GatewayConfig): AuthProvider {
  const configured = Boolean(cfg.url && cfg.token);
  return {
    check: async () =>
      configured
        ? { authenticated: true }
        : { authenticated: false, error: { name: "미연결", message: "게이트웨이 URL/토큰을 입력하세요" } },
    login: async () => ({ success: configured }),
    logout: async () => ({ success: true }),
    onError: async (error) => ({ error }),
    getIdentity: async () => {
      if (!configured) return null;
      try {
        const r = await ping(cfg);
        return { name: `게이트웨이 v${r.version ?? "?"}`, model: r.model };
      } catch {
        return null;
      }
    },
  };
}
