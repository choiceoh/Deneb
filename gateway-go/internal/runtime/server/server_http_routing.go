package server

import (
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
)

// buildMux configures HTTP routing for health, RPC/WS, API, hooks, and plugin routes.
func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /health/gpu", s.handleHealthGPU)
	mux.HandleFunc("GET /ready", s.handleReady)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("POST /api/cron/run", s.handleCronRun)
	mux.HandleFunc("POST /api/event/ingest", s.handleEventIngest)
	// Production-fidelity extraction benchmark: run a real extractor against a named
	// wormhole model. Client-token guarded. See server_http_eval.go.
	mux.HandleFunc("POST /api/eval/extract", s.handleEvalExtract)
	mux.HandleFunc("POST /api/v1/miniapp/rpc", s.handleMiniappRPC)
	mux.HandleFunc("POST /api/v1/miniapp/chat/stream", s.handleMiniappChatStream)
	mux.HandleFunc("GET /api/v1/miniapp/events", s.handleMiniappEvents)
	mux.HandleFunc("GET /api/v1/miniapp/gmail/attachment", s.handleMiniappGmailAttachment)
	// MCP gateway — read-only Deneb memory (wiki/projects/diary/calendar/search)
	// as Model Context Protocol tools for external AI clients (Claude Code 등).
	// Same client-token auth as the miniapp surface. See server_http_mcp.go.
	if os.Getenv("DENEB_MCP_DISABLE") != "1" {
		mux.HandleFunc("/mcp", s.handleMCP)
	}
	mux.HandleFunc("GET /api/v1/app/update/manifest", s.handleAppUpdateManifest)
	mux.HandleFunc("GET /api/v1/app/update/download", s.handleAppUpdateDownload)
	mux.HandleFunc("GET /api/v1/files/download", s.handleFilesDownload)
	// Fleet passthrough — the native app manages SparkFleet through the gateway
	// (subtree route; the handler enforces method+path allowlist + client token).
	mux.HandleFunc("/api/v1/fleet/", s.handleFleetProxy)
	// SparkFleet webhook → native push (loopback-only, like /api/event/ingest).
	mux.HandleFunc("POST /api/hooks/fleet", s.handleFleetHook)
	// Self-improvement telemetry digest for an agent/puppeteer (loopback-only).
	mux.HandleFunc("GET /api/observatory", s.handleObservatory)

	// /debug/pprof/* — runtime profiling + goroutine dumps for live diagnosis.
	// These stay open on loopback for local debugging, but non-loopback binds
	// must prove the same client token the native app uses so profiler dumps
	// cannot be fetched from the network without authentication.
	// Visit /debug/pprof/goroutine?debug=2 when the gateway appears hung —
	// it returns a full stack dump without killing the process.
	mux.HandleFunc("/debug/pprof/", s.protectPprof(pprof.Index))
	mux.HandleFunc("/debug/pprof/cmdline", s.protectPprof(pprof.Cmdline))
	mux.HandleFunc("/debug/pprof/profile", s.protectPprof(pprof.Profile))
	mux.HandleFunc("/debug/pprof/symbol", s.protectPprof(pprof.Symbol))
	mux.HandleFunc("/debug/pprof/trace", s.protectPprof(pprof.Trace))

	// Explicit method-not-allowed for health/ready endpoints.
	// Without these, non-GET requests fall through to the catch-all "/" handler
	// and return 404 instead of the correct 405.
	methodNotAllowed := func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
	mux.HandleFunc("/health", methodNotAllowed)
	mux.HandleFunc("/healthz", methodNotAllowed)
	mux.HandleFunc("/health/gpu", methodNotAllowed)
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

func (s *Server) protectPprof(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.pprofRequiresClientToken() {
			if _, ok := s.authenticateMiniappRequest(w, r); !ok {
				return
			}
		}
		next(w, r)
	}
}

func (s *Server) pprofRequiresClientToken() bool {
	host := ""
	if s != nil && s.runtimeCfg != nil {
		host = strings.TrimSpace(s.runtimeCfg.BindHost)
	}
	if host == "" && s != nil {
		if addr := strings.TrimSpace(s.BoundAddr()); addr != "" {
			if splitHost, _, err := net.SplitHostPort(addr); err == nil {
				host = splitHost
			} else {
				host = addr
			}
		}
	}
	if host == "" || host == "localhost" {
		return false
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

func (s *Server) handleRoot(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{
		"name":    "deneb-gateway",
		"version": s.version,
		"status":  "ok",
	})
}
