import { defineConfig, type Plugin } from 'vite';
import { brotliCompressSync, gzipSync, constants as zlibConstants } from 'node:zlib';

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

// Pre-compress text assets (JS, CSS, HTML, SVG, JSON, .map) at build time
// so the Go gateway can serve `<file>.br` or `<file>.gz` straight from the
// embedded FS without paying gzip CPU on every request. We emit both
// encodings: every modern Telegram WebView supports brotli (~70%
// smaller than raw), but a small slice of legacy clients still only
// accept gzip, and stale CDNs in between can strip `br`. Shipping both
// makes the negotiation lossless.
//
// We deliberately do NOT compress assets the browser already receives
// in a compressed container (woff2 is brotli-encoded internally; png/
// jpg/webp are their own formats). Compressing them again wastes binary
// size for ~0% wire savings.
function precompressTextAssets(): Plugin {
  const compressibleExt = new Set([
    '.js',
    '.mjs',
    '.css',
    '.html',
    '.htm',
    '.svg',
    '.json',
    '.map',
    '.txt',
  ]);
  // Only emit a compressed copy when it actually wins bytes. For tiny
  // assets the compressed form can be larger than the raw form because
  // of the format header; in that case the gateway should fall back to
  // serving raw, so omitting the compressed file is the right move.
  const minSavingsBytes = 128;

  return {
    name: 'precompress-text-assets',
    apply: 'build',
    enforce: 'post',
    generateBundle(_options, bundle) {
      for (const [fileName, asset] of Object.entries(bundle)) {
        const lower = fileName.toLowerCase();
        const dot = lower.lastIndexOf('.');
        if (dot === -1) continue;
        if (!compressibleExt.has(lower.slice(dot))) continue;

        const source =
          asset.type === 'asset'
            ? typeof asset.source === 'string'
              ? Buffer.from(asset.source)
              : Buffer.from(asset.source)
            : Buffer.from(asset.code);
        if (source.length === 0) continue;

        const br = brotliCompressSync(source, {
          params: {
            [zlibConstants.BROTLI_PARAM_QUALITY]: zlibConstants.BROTLI_MAX_QUALITY,
          },
        });
        if (source.length - br.length >= minSavingsBytes) {
          this.emitFile({
            type: 'asset',
            fileName: `${fileName}.br`,
            source: br,
          });
        }

        const gz = gzipSync(source, { level: 9 });
        if (source.length - gz.length >= minSavingsBytes) {
          this.emitFile({
            type: 'asset',
            fileName: `${fileName}.gz`,
            source: gz,
          });
        }
      }
    },
  };
}

// Mini App is mounted at /app/ by the gateway. Set base accordingly so all
// emitted asset URLs are absolute under that prefix (matters when the embedded
// FileServer in Go resolves /app/assets/<hash>.js).
export default defineConfig({
  base: '/app/',
  plugins: [preloadLatinFont(), precompressTextAssets()],
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
