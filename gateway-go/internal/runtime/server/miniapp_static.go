// miniapp_static.go — serves the Vite-built Mini App bundle from an embedded
// filesystem at /app/*.
//
// Asset pipeline:
//
//	frontend/dist/                ← Vite build output (PR-B)
//	    │   make embed-frontend
//	    ▼
//	gateway-go/internal/runtime/server/miniapp_dist/
//	    │   //go:embed all:miniapp_dist
//	    ▼
//	deneb-gateway binary          ← single binary, no extra files to ship
//
// Routing semantics:
//
//	GET /app/                     → index.html (200, no-cache)
//	GET /app/assets/<hash>.js     → bundled asset (200, immutable cache)
//	GET /app/<unknown-path>       → index.html (200, no-cache) — SPA fallback
//
// A placeholder index.html is committed so `go build` succeeds before anyone
// runs `make build-frontend`. Production builds overwrite it during
// `make embed-frontend`.

package server

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// miniappDist is the embedded copy of frontend/dist/. The `all:` prefix
// ensures dotfiles (e.g. .vite/) are included so we never accidentally
// drop a file Vite expects to ship.
//
//go:embed all:miniapp_dist
var miniappDist embed.FS

// miniappSubFS strips the "miniapp_dist/" prefix so callers can request
// "index.html" instead of "miniapp_dist/index.html". Computed once at
// startup; nil only if the embed directive somehow points at an empty tree
// (which the committed placeholder prevents).
var miniappSubFS = func() fs.FS {
	sub, err := fs.Sub(miniappDist, "miniapp_dist")
	if err != nil {
		// embed FS always succeeds Sub for a directory we control at
		// build time; a failure here would mean the placeholder is gone.
		return nil
	}
	return sub
}()

// serveMiniappStatic handles GET /app/{path...}. It looks up the requested
// path in the embedded filesystem, serves it with appropriate cache headers,
// and falls back to index.html on miss (SPA single-page-app behavior).
func (s *Server) serveMiniappStatic(w http.ResponseWriter, r *http.Request) {
	if miniappSubFS == nil {
		http.Error(w, "mini app assets not embedded", http.StatusInternalServerError)
		return
	}

	// Strip the "/app" prefix so we work in the dist-relative namespace.
	// http.ServeMux already routed us here, so r.URL.Path always starts
	// with "/app". A trailing "" maps to "index.html".
	rel := strings.TrimPrefix(r.URL.Path, "/app")
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		rel = "index.html"
	}
	// Guard against ".." traversal — even though embed.FS rejects it,
	// reject early so the served path is always normalized.
	if rel != path.Clean(rel) || strings.Contains(rel, "..") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	// Don't let the wire reach in and ask for the pre-compressed copy
	// directly — that would skip the Content-Encoding negotiation and
	// hand the client a body it might not be able to decode. The
	// compressed siblings are an implementation detail of how we serve
	// the canonical file.
	if strings.HasSuffix(rel, ".br") || strings.HasSuffix(rel, ".gz") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	data, err := fs.ReadFile(miniappSubFS, rel)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			s.logger.Error("miniapp asset read", "path", rel, "error", err)
			http.Error(w, "asset read failure", http.StatusInternalServerError)
			return
		}
		// SPA fallback: unknown route serves index.html (or, on a fresh
		// clone where the Vite bundle has not been built, placeholder.html)
		// with 200 so client-side routing can take over. We keep "no-cache"
		// so the fallback never gets pinned in the browser cache.
		serveMiniappIndexOrPlaceholder(w, r)
		return
	}

	immutable := strings.HasPrefix(rel, "assets/")
	writeMiniappAsset(w, r, rel, data, immutable)
}

// writeMiniappAsset writes `data` for path `rel` with cache headers and,
// when the client accepts it and a pre-compressed sibling exists in the
// embed FS, swaps in the compressed payload + sets Content-Encoding.
// `Vary: Accept-Encoding` is set unconditionally on text-like assets so
// intermediate caches don't fuse encoded/raw responses together.
func writeMiniappAsset(
	w http.ResponseWriter,
	r *http.Request,
	rel string,
	rawData []byte,
	immutable bool,
) {
	encoded, encoding := pickPrecompressed(r.Header.Get("Accept-Encoding"), rel)
	body := rawData
	if encoded != "" {
		if data, err := fs.ReadFile(miniappSubFS, encoded); err == nil {
			body = data
			w.Header().Set("Content-Encoding", encoding)
		}
	}
	w.Header().Set("Content-Type", contentTypeForMiniappAsset(rel))
	if immutable {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		// no-store forces Telegram's embedded WebView to always fetch a
		// fresh copy. no-cache alone was insufficient — Telegram served
		// stale index.html from its internal WebView cache even after the
		// binary was updated and the app was closed/reopened.
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Tell upstreams (and the browser's HTTP cache) that the response
	// body depends on Accept-Encoding, so a gzip-only client doesn't
	// reuse a brotli payload they cached for someone else. Cheap to set
	// even for assets that have no precompressed sibling.
	w.Header().Set("Vary", "Accept-Encoding")
	// G705: body is read from embed.FS — every byte was fixed at compile
	// time. The Mini App webview executes it as JS/HTML by design; there
	// is no user-controlled input on the write path to escape.
	_, _ = w.Write(body) //nolint:gosec // G705: embedded static asset, fixed at compile time
}

// pickPrecompressed returns the embed-FS path for the best pre-compressed
// sibling of `rel` that the client said they accept, along with the
// Content-Encoding token to emit. It returns ("", "") when no acceptable
// encoded sibling exists — caller serves the raw bytes in that case.
//
// We prefer brotli over gzip when the client lists both (or `*`); this
// matches RFC 7231 § 5.3.4 ("the server selects one of the supported
// codings") and gives us the ~15% smaller payload. Both encodings need
// `q != 0` in the Accept-Encoding list to be eligible — RFC 7231 lets a
// client explicitly reject an encoding with q=0.
func pickPrecompressed(acceptEncoding, rel string) (string, string) {
	if rel == "" {
		return "", ""
	}
	wantsBr, wantsGz := parseEncodingPreference(acceptEncoding)
	if wantsBr {
		if encoded := rel + ".br"; embedHas(encoded) {
			return encoded, "br"
		}
	}
	if wantsGz {
		if encoded := rel + ".gz"; embedHas(encoded) {
			return encoded, "gzip"
		}
	}
	return "", ""
}

// parseEncodingPreference is a permissive Accept-Encoding parser: it
// returns whether brotli and gzip are acceptable. Quality values are
// honored only at the q=0 boundary (explicitly rejected vs. accepted at
// any positive weight) — the wire never carries meaningful intermediate
// weights for static assets in practice, so a full weighted negotiation
// would be overkill.
func parseEncodingPreference(header string) (br bool, gz bool) {
	// Most clients send "gzip, deflate, br" or "br, gzip" — we just walk
	// the comma-separated list. An empty header means "no encoding
	// preference" per RFC 7231, which we treat as "identity only".
	for _, part := range strings.Split(header, ",") {
		token := strings.TrimSpace(part)
		if token == "" {
			continue
		}
		// Split off ";q=..." if present.
		coding := token
		q := 1.0
		if i := strings.IndexByte(token, ';'); i != -1 {
			coding = strings.TrimSpace(token[:i])
			rest := strings.TrimSpace(token[i+1:])
			if strings.HasPrefix(rest, "q=") || strings.HasPrefix(rest, "Q=") {
				if v, err := strconv.ParseFloat(rest[2:], 64); err == nil {
					q = v
				}
			}
		}
		if q <= 0 {
			continue
		}
		switch strings.ToLower(coding) {
		case "br":
			br = true
		case "gzip", "x-gzip":
			gz = true
		case "*":
			br = true
			gz = true
		}
	}
	return br, gz
}

// embedHas reports whether the embed FS contains the named file. Wrapped
// so the precompression negotiation can run without leaking an error
// path through the caller — a missing sibling just means "fall back to
// raw" and is not loggable.
func embedHas(name string) bool {
	f, err := miniappSubFS.Open(name)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// serveMiniappIndexOrPlaceholder serves the Vite-built index.html when it
// is present in the embedded filesystem, falling back to the committed
// placeholder when the operator skipped `make embed-frontend`. Either way
// the response is cacheable-by-revalidation only, so a build-and-redeploy
// reaches users on next page load. Compression negotiation runs against
// whichever entry file we end up serving.
func serveMiniappIndexOrPlaceholder(w http.ResponseWriter, r *http.Request) {
	for _, name := range []string{"index.html", "placeholder.html"} {
		data, err := fs.ReadFile(miniappSubFS, name)
		if err != nil {
			continue
		}
		writeMiniappAsset(w, r, name, data, false)
		return
	}
	http.Error(w, "mini app entry not embedded", http.StatusInternalServerError)
}

// contentTypeForMiniappAsset returns the MIME type for a Mini App asset.
// Kept inline (no mime.TypeByExtension) because we control the asset set
// and want byte-identical headers across hosts (some DGX Spark mime DBs are
// stripped down and would return "" for .map).
func contentTypeForMiniappAsset(name string) string {
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".map":
		return "application/json; charset=utf-8"
	}
	return "application/octet-stream"
}
