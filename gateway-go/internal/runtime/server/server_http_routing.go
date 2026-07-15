package server

import (
	"net/http"
	"net/http/pprof"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/gatewayhttp"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/phoneevents"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
)

// buildMux configures HTTP routing for health, native-client HTTP (SSE via gatewayhttp/nativeapi), hooks, and introspection routes.
func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
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
	clientRoutes := gatewayhttp.Config{
		PushHub:           s.pushHub,
		ShutdownContext:   s.ShutdownCtx(),
		Logger:            s.logger,
		AttachmentFactory: s.newMiniappMailAttachmentClient,
		Fleet:             s.fleet,
		Version:           s.version,
	}
	// Keep route discovery available to lightweight tests and diagnostics that
	// construct a zero-value Server without the full RPC/chat bootstrap.
	if s.ServerRPC != nil {
		clientRoutes.Dispatcher = s.dispatcher
	}
	if s.ChatManager != nil {
		clientRoutes.ChatHandler = s.chatHandler
	}
	gatewayhttp.RegisterRoutes(mux, clientRoutes)
	// Production-fidelity extraction benchmark: run a real extractor against a named
	// wormhole model. Client-token guarded. See server_http_eval.go.
	mux.HandleFunc("POST /api/eval/extract", s.handleEvalExtract)
	// SparkFleet webhook → native push (loopback-only, like /api/event/ingest).
	gatewayhttp.RegisterFleetAlertRoute(mux, gatewayhttp.FleetAlertConfig{
		Gate: s.alertGate,
		Publish: func(title, body string) {
			proactive.PublishWithFallback(s.pushHub, s.pushNotifier, proactive.Event{
				Title: title,
				Body:  body,
				Kind:  proactive.PushKindFleet,
			})
		},
		Logger: s.logger,
	})
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
