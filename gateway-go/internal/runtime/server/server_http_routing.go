package server

import (
	"net/http"
	"net/http/pprof"
	"os"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/appupdate"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/fleetapi"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/mcpapi"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativeapi"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativeauth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/phoneevents"
)

// buildMux configures HTTP routing for health, RPC/WS, API, hooks, and plugin routes.
func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	nativeHandler := func() *nativeapi.Handler {
		return nativeapi.New(nativeapi.Config{
			Dispatcher:        s.dispatcher,
			ChatHandler:       s.chatHandler,
			PushHub:           s.pushHub,
			ShutdownContext:   s.ShutdownCtx(),
			Logger:            s.logger,
			AttachmentFactory: s.newMiniappMailAttachmentClient,
		})
	}
	phoneEventHandler := func() *phoneevents.Handler {
		return phoneevents.New(phoneevents.Config{
			ChatHandler:     s.chatHandler,
			Relay:           &s.proactiveRelay,
			ShutdownContext: s.ShutdownCtx(),
			Logger:          s.logger,
			Ledger:          s.phoneEventLedgerInstance(),
			OnLocationPlace: s.siteVisitOnLocation(),
			ResolvePhoneAction: func(res phoneevents.ActionResult) bool {
				if s.phoneActions == nil {
					return false
				}
				return s.phoneActions.resolve(phoneActionResult{ID: res.ID, OK: res.OK, Error: res.Error})
			},
		})
	}
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /health/gpu", s.handleHealthGPU)
	mux.HandleFunc("GET /ready", s.handleReady)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("POST /api/cron/run", s.handleCronRun)
	mux.HandleFunc("POST /api/event/ingest", func(w http.ResponseWriter, r *http.Request) { phoneEventHandler().ServeHTTP(w, r) })
	// Production-fidelity extraction benchmark: run a real extractor against a named
	// wormhole model. Client-token guarded. See server_http_eval.go.
	mux.HandleFunc("POST /api/eval/extract", s.handleEvalExtract)
	mux.HandleFunc("POST /api/v1/miniapp/rpc", func(w http.ResponseWriter, r *http.Request) { nativeHandler().RPC(w, r) })
	mux.HandleFunc("POST /api/v1/miniapp/chat/stream", func(w http.ResponseWriter, r *http.Request) { nativeHandler().ChatStream(w, r) })
	mux.HandleFunc("GET /api/v1/miniapp/events", func(w http.ResponseWriter, r *http.Request) { nativeHandler().Events(w, r) })
	mux.HandleFunc("GET /api/v1/miniapp/gmail/attachment", func(w http.ResponseWriter, r *http.Request) { nativeHandler().GmailAttachment(w, r) })
	// MCP gateway — read-only Deneb memory (wiki/projects/diary/calendar/search)
	// as Model Context Protocol tools for external AI clients (Claude Code 등).
	// Same client-token auth as the miniapp surface. See server_http_mcp.go.
	if os.Getenv("DENEB_MCP_DISABLE") != "1" {
		mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
			mcpapi.New(mcpapi.Config{
				Authenticate: func(w http.ResponseWriter, r *http.Request) (*clientauth.Identity, bool) {
					return nativeauth.Authenticate(w, r, s.logger)
				},
				Dispatcher: s.dispatcher,
				Version:    s.version,
				Logger:     s.logger,
			}).ServeHTTP(w, r)
		})
	}
	mux.HandleFunc("GET /api/v1/app/update/manifest", appupdate.New(s.logger).Manifest)
	mux.HandleFunc("GET /api/v1/app/update/download", appupdate.New(s.logger).Download)
	mux.HandleFunc("GET /api/v1/files/download", func(w http.ResponseWriter, r *http.Request) { nativeHandler().FilesDownload(w, r) })
	// Fleet passthrough — the native app manages SparkFleet through the gateway
	// (subtree route; the handler enforces method+path allowlist + client token).
	mux.Handle("/api/v1/fleet/", fleetapi.New(s.fleet, s.logger))
	// SparkFleet webhook → native push (loopback-only, like /api/event/ingest).
	mux.HandleFunc("POST /api/hooks/fleet", s.handleFleetHook)
	// Self-improvement telemetry digest for an agent/puppeteer (loopback-only).
	mux.HandleFunc("GET /api/observatory", s.handleObservatory)

	// /debug/pprof/* — runtime profiling + goroutine dumps for live diagnosis.
	// Safe to expose because the gateway binds loopback by default in
	// production; these endpoints are never reachable from outside the host.
	// Visit /debug/pprof/goroutine?debug=2 when the gateway appears hung —
	// it returns a full stack dump without killing the process.
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

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

func (s *Server) handleRoot(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{
		"name":    "deneb-gateway",
		"version": s.version,
		"status":  "ok",
	})
}
