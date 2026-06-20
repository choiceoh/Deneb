package server

import (
	"net/http"
	"net/http/pprof"
)

// buildMux configures HTTP routing for health, RPC/WS, API, hooks, and plugin routes.
func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /ready", s.handleReady)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("POST /api/cron/run", s.handleCronRun)
	mux.HandleFunc("POST /api/event/ingest", s.handleEventIngest)
	mux.HandleFunc("POST /api/v1/miniapp/rpc", s.handleMiniappRPC)
	mux.HandleFunc("POST /api/v1/miniapp/chat/stream", s.handleMiniappChatStream)
	mux.HandleFunc("GET /api/v1/miniapp/events", s.handleMiniappEvents)
	mux.HandleFunc("GET /api/v1/miniapp/gmail/attachment", s.handleMiniappGmailAttachment)
	mux.HandleFunc("GET /api/v1/app/update/manifest", s.handleAppUpdateManifest)
	mux.HandleFunc("GET /api/v1/app/update/download", s.handleAppUpdateDownload)
	mux.HandleFunc("GET /api/v1/files/download", s.handleFilesDownload)
	// Fleet passthrough — the native app manages SparkFleet through the gateway
	// (subtree route; the handler enforces method+path allowlist + client token).
	mux.HandleFunc("/api/v1/fleet/", s.handleFleetProxy)
	// SparkFleet webhook → native push (loopback-only, like /api/event/ingest).
	mux.HandleFunc("POST /api/hooks/fleet", s.handleFleetHook)

	// /debug/pprof/* — runtime profiling + goroutine dumps for live diagnosis.
	// These handlers stay loopback-only even if the gateway is accidentally
	// bound wider or placed behind a proxy, because the responses expose full
	// process internals and some endpoints can consume significant CPU.
	pprofLocalOnly := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !isLoopbackRemote(r.RemoteAddr) {
				s.writeJSON(w, http.StatusForbidden, map[string]any{"error": "localhost only"})
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("/debug/pprof/", pprofLocalOnly(pprof.Index))
	mux.HandleFunc("/debug/pprof/cmdline", pprofLocalOnly(pprof.Cmdline))
	mux.HandleFunc("/debug/pprof/profile", pprofLocalOnly(pprof.Profile))
	mux.HandleFunc("/debug/pprof/symbol", pprofLocalOnly(pprof.Symbol))
	mux.HandleFunc("/debug/pprof/trace", pprofLocalOnly(pprof.Trace))

	// Explicit method-not-allowed for health/ready endpoints.
	// Without these, non-GET requests fall through to the catch-all "/" handler
	// and return 404 instead of the correct 405.
	methodNotAllowed := func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
	mux.HandleFunc("/health", methodNotAllowed)
	mux.HandleFunc("/healthz", methodNotAllowed)
	mux.HandleFunc("/ready", methodNotAllowed)
	mux.HandleFunc("/readyz", methodNotAllowed)
	mux.HandleFunc("/api/cron/run", methodNotAllowed)
	mux.HandleFunc("/api/event/ingest", methodNotAllowed)
	mux.HandleFunc("/api/v1/miniapp/rpc", methodNotAllowed)
	mux.HandleFunc("/api/v1/miniapp/chat/stream", methodNotAllowed)
	mux.HandleFunc("/api/v1/miniapp/events", methodNotAllowed)
	mux.HandleFunc("/api/v1/miniapp/gmail/attachment", methodNotAllowed)
	mux.HandleFunc("/api/v1/files/download", methodNotAllowed)
	// Catch-all handler: root fallback.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Root fallback for exact "/" GET.
		if r.Method == http.MethodGet && r.URL.Path == "/" {
			s.handleRoot(w, r)
			return
		}
		http.NotFound(w, r)
	})

	return mux
}

func (s *Server) handleRoot(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{
		"name":    "deneb-gateway",
		"version": s.version,
		"status":  "ok",
	})
}
