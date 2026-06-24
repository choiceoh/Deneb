// Ambient types for Vite's import.meta.env (only the keys we read). Declared
// directly (no triple-slash reference) to keep ESLint happy.
interface ImportMetaEnv {
  readonly DEV?: boolean;
  readonly MODE?: string;
  readonly VITE_MOCK?: string;
  readonly VITE_LOG_LEVEL?: string;
  readonly VITE_GATEWAY_URL?: string;
  readonly VITE_GATEWAY_TOKEN?: string;
}
interface ImportMeta {
  readonly env: ImportMetaEnv;
}

// Injected by Vite `define` (see vite.config.ts) — the app version from package.json.
declare const __APP_VERSION__: string;
