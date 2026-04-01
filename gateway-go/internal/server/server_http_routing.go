package server

import "net/http"

// buildMux configures HTTP routing for health, RPC/WS, API, hooks, and plugin routes.
func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /ready", s.handleReady)
	mux.HandleFunc("GET /readyz", s.handleReady)

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
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.HandleFunc("POST /api/v1/rpc", s.handleRPC)
	mux.HandleFunc("GET /ws", s.handleWsUpgrade)

	// HTTP API endpoints (P2 migration).
	mux.HandleFunc("POST /tools/invoke", s.handleToolsInvoke)
	mux.HandleFunc("POST /sessions/{key}/kill", s.handleSessionKill)
	mux.HandleFunc("GET /sessions/{key}/history", s.handleSessionHistory)

	// OpenAI-compatible HTTP API endpoints.
	mux.HandleFunc("POST /v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("POST /v1/responses", s.handleResponses)

	// Hooks HTTP webhook endpoint — intercepts /hooks/* before the fallback.
	if s.hooksHTTP != nil {
		hooksHandler := s.hooksHTTP
		mux.HandleFunc("/hooks/", func(w http.ResponseWriter, r *http.Request) {
			if !hooksHandler.Handle(w, r) {
				http.NotFound(w, r)
			}
		})
		mux.HandleFunc("/hooks", func(w http.ResponseWriter, r *http.Request) {
			if !hooksHandler.Handle(w, r) {
				http.NotFound(w, r)
			}
		})
	}

	// GitHub webhook endpoint — receives push/PR/issue events from GitHub.
	// Auth: HMAC-SHA256 signature (X-Hub-Signature-256). No bearer token needed.
	mux.HandleFunc("POST /webhook/github", s.handleGitHubWebhook)
	mux.HandleFunc("/webhook/github", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeText(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		} else {
			s.handleGitHubWebhook(w, r)
		}
	})

	// Catch-all handler: plugin HTTP routes → root fallback.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Plugin HTTP routes.
		if s.pluginRouter != nil && s.pluginRouter.Handle(w, r) {
			return
		}
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
