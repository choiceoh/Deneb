import { defineConfig } from 'vite';

// Mini App is mounted at /app/ by the gateway. Set base accordingly so all
// emitted asset URLs are absolute under that prefix (matters when the embedded
// FileServer in Go resolves /app/assets/<hash>.js).
export default defineConfig({
  base: '/app/',
  build: {
    outDir: 'dist',
    target: 'es2020',
    assetsDir: 'assets',
    emptyOutDir: true,
    // Inline assets under 4 KiB; everything else gets a content-hashed file
    // name so we can serve with long-lived cache headers in PR-C.
    assetsInlineLimit: 4096,
    sourcemap: false,
  },
  server: {
    port: 5173,
    strictPort: true,
    // Dev cross-origin: Vite serves on :5173, gateway on :18790 (or override
    // via VITE_GATEWAY_PORT). Proxying /api here keeps the browser thinking
    // both originate from :5173, so no CORS code needed on the Go side.
    proxy: {
      '/api': {
        target: `http://127.0.0.1:${process.env.VITE_GATEWAY_PORT ?? '18790'}`,
        changeOrigin: false,
      },
    },
  },
});
