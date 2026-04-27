package server

import (
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/openaiapi"
)

// buildMux configures HTTP routing for health, RPC/WS, API, hooks, and plugin routes.
func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /ready", s.handleReady)
	mux.HandleFunc("GET /readyz", s.handleReady)

	// /v1/* — OpenAI-compatible API for IDE clients (Zed, OpenCode) over Tailscale.
	authToken := ""
	if s.runtimeCfg != nil {
		authToken = s.runtimeCfg.ResolvedAuth.Token
	}
	openaiapi.Mount(mux, openaiapi.Deps{
		Logger:        s.logger,
		AuthToken:     authToken,
		ModelRegistry: s.modelRegistry,
		ChatClient: func(role modelrole.Role) openaiapi.ChatStreamer {
			if s.modelRegistry == nil {
				return nil
			}
			c := s.modelRegistry.Client(role)
			if c == nil {
				return nil
			}
			return c
		},
		StartedAt: func() time.Time { return s.startedAt },
	})

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
	mux.HandleFunc("/ready", methodNotAllowed)
	mux.HandleFunc("/readyz", methodNotAllowed)
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
