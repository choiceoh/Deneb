import { defineConfig, type Plugin } from 'vite';

// Inject a <link rel="preload"> for the latin Inter subset so the font
// fetch starts in parallel with HTML/JS/CSS parsing instead of waiting
// for the CSS bundle to arrive + parse before discovery. Other Inter
// subsets (cyrillic, greek, vietnamese, …) are skipped automatically by
// the browser via @font-face `unicode-range` — they never get fetched
// on a Korean-first UI rendering English labels, so we only preload the
// one subset that always runs.
function preloadLatinFont(): Plugin {
  return {
    name: 'preload-latin-font',
    apply: 'build',
    transformIndexHtml: {
      order: 'post',
      handler(html, ctx) {
        if (!ctx.bundle) return html;
        const latin = Object.keys(ctx.bundle).find(
          (f) => f.includes('inter-latin-wght-normal') && f.endsWith('.woff2'),
        );
        if (!latin) return html;
        const tag = `<link rel="preload" as="font" type="font/woff2" crossorigin href="/app/${latin}">`;
        return html.replace('</title>', `</title>\n    ${tag}`);
      },
    },
  };
}

// Mini App is mounted at /app/ by the gateway. Set base accordingly so all
// emitted asset URLs are absolute under that prefix (matters when the embedded
// FileServer in Go resolves /app/assets/<hash>.js).
export default defineConfig({
  base: '/app/',
  plugins: [preloadLatinFont()],
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
