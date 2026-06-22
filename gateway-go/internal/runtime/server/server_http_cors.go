// server_http_cors.go — CORS for the browser-based workstation client.
//
// The native clients (Android/iOS) speak raw HTTP and don't enforce CORS, so the
// gateway never needed it. The Andromeda workstation, however, is a browser app
// (Tauri WebView2 in dev loads http://localhost:1420; the packaged app runs from
// the tauri://localhost origin). A browser there treats every call to the
// miniapp.* HTTP surface as cross-origin and blocks the response ("Failed to
// fetch") unless the gateway answers with CORS headers.
//
// Auth is the custom X-Deneb-Client-Token header, never a cookie, which means:
//   - the browser sends a CORS preflight (OPTIONS) before each call, and that
//     preflight carries no token — so we must answer it here, ahead of the mux
//     and authenticateMiniappRequest (which would otherwise 401 it), and
//   - the actual request carries no ambient credentials. We echo the request
//     Origin (with Vary: Origin) instead of "*": this is friendlier to future
//     credentialed use and to caches, and is safe precisely because the token is
//     an explicit header — reflecting an origin does not hand the secret to any
//     site; a caller still needs the token to get past auth.
//
// We deliberately do NOT set Access-Control-Allow-Credentials: the token is a
// header, not a cookie, so credentialed mode is unnecessary (and incompatible
// with reflecting arbitrary origins).
package server

import (
	"net/http"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

// withCORS wraps the gateway mux so browser clients can reach the miniapp.* HTTP
// surface. It is a no-op for requests without an Origin header (native clients),
// so non-browser callers are entirely unaffected.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			h.Set("Access-Control-Allow-Headers", clientauth.Header+", Content-Type")
			h.Set("Access-Control-Max-Age", "600")
			// CORS preflight: OPTIONS carries no token, so short-circuit before the
			// mux/auth would reject it. 204 No Content with the headers set above.
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
