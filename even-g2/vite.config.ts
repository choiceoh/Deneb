import { defineConfig } from "vite";

import { readFileSync } from "node:fs";

// The glasses build version, injected so the phone screen can state which build
// is running. Diagnosing a device report without that is guesswork: "화살표가
// 안떠" reads identically whether the arrow is broken or the installed package
// predates it.
const appVersion = JSON.parse(readFileSync("./app.json", "utf8")).version;

export default defineConfig({
  define: { __APP_VERSION__: JSON.stringify(appVersion) },
  base: "./",
  build: {
    outDir: "dist",
    emptyOutDir: true,
    // Even Hub runs the plugin in a modern Chromium WebView, so target
    // ES2022+ to enable top-level await used by src/main.ts (module entrypoint).
    // The default ('es2020' + browser overrides) rejects top-level await and
    // fails `vite build` — this went unnoticed because `make check` is Go-only.
    target: "esnext",
  },
  server: {
    host: true,
    port: 5173,
  },
});
