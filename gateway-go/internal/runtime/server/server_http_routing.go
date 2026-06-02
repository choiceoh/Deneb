package server

import (
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
)

// buildMux configures HTTP routing for health, RPC/WS, API, hooks, and plugin routes.
func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /ready", s.handleReady)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("POST /api/cron/run", s.handleCronRun)
	mux.HandleFunc("POST /api/v1/miniapp/rpc", s.handleMiniappRPC)
	mux.HandleFunc("POST /api/v1/miniapp/chat/stream", s.handleMiniappChatStream)
	mux.HandleFunc("GET /api/v1/miniapp/events", s.handleMiniappEvents)
	mux.HandleFunc("GET /api/v1/miniapp/gmail/attachment", s.handleMiniappGmailAttachment)

	// /debug/pprof/* is only mounted for loopback binds. Runtime profiling
	// exposes heap/goroutine internals and must not bypass normal auth when the
	// gateway is reachable from a network interface.
	if s.pprofLoopbackOnly() {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}

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
	mux.HandleFunc("/api/v1/miniapp/rpc", methodNotAllowed)
	mux.HandleFunc("/api/v1/miniapp/chat/stream", methodNotAllowed)
	mux.HandleFunc("/api/v1/miniapp/events", methodNotAllowed)
	mux.HandleFunc("/api/v1/miniapp/gmail/attachment", methodNotAllowed)
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

func (s *Server) pprofLoopbackOnly() bool {
	if s == nil || s.runtimeCfg == nil {
		return true
	}
	host := strings.TrimSpace(s.runtimeCfg.BindHost)
	if host == "" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
