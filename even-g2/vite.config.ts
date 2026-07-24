import { defineConfig } from 'vite'

export default defineConfig({
  base: './',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // Even Hub runs the plugin in a modern Chromium WebView, so target
    // ES2022+ to enable top-level await used by src/main.ts (module entrypoint).
    // The default ('es2020' + browser overrides) rejects top-level await and
    // fails `vite build` — this went unnoticed because `make check` is Go-only.
    target: 'esnext',
  },
  server: {
    host: true,
    port: 5173,
  },
})
