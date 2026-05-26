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
		serveMiniappIndexOrPlaceholder(w)
		return
	}

	immutable := strings.HasPrefix(rel, "assets/")
	writeMiniappHeaders(w, rel, immutable)
	// G705: data is read from embed.FS — every byte was fixed at compile
	// time. The Mini App webview executes it as JS/HTML by design; there
	// is no user-controlled input on the write path to escape.
	_, _ = w.Write(data) //nolint:gosec // G705: embedded static asset, fixed at compile time
}

// serveMiniappIndexOrPlaceholder serves the Vite-built index.html when it
// is present in the embedded filesystem, falling back to the committed
// placeholder when the operator skipped `make embed-frontend`. Either way
// the response is cacheable-by-revalidation only, so a build-and-redeploy
// reaches users on next page load.
func serveMiniappIndexOrPlaceholder(w http.ResponseWriter) {
	for _, name := range []string{"index.html", "placeholder.html"} {
		data, err := fs.ReadFile(miniappSubFS, name)
		if err != nil {
			continue
		}
		writeMiniappHeaders(w, name, false)
		// G705: embedded static asset, not user input. See note in
		// serveMiniappStatic.
		_, _ = w.Write(data) //nolint:gosec // G705: embedded static asset, fixed at compile time
		return
	}
	http.Error(w, "mini app entry not embedded", http.StatusInternalServerError)
}

// writeMiniappHeaders sets Content-Type from the file extension and a cache
// policy based on whether the asset is hashed (assets/<hash>.<ext>) or the
// always-fresh entry point.
func writeMiniappHeaders(w http.ResponseWriter, name string, immutable bool) {
	w.Header().Set("Content-Type", contentTypeForMiniappAsset(name))
	if immutable {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
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
